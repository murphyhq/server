package incidents

import (
	"context"
	"errors"
	"fmt"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/internal/telemetry"
	"github.com/murphyhq/server/internal/tracer"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *Service) TimerExpired(ctx context.Context, messageID string, timer *murphy.Timer) (err error) {
	ctx, span := tracer.Span(ctx)
	ctx = span.Set(ctx,
		telemetry.WithField("incident.id", timer.GetIncidentId()),
		telemetry.WithField("escalation.level", timer.GetEscalationLevel()),
		telemetry.WithField("escalation.round", timer.GetEscalationRound()),
	)
	defer span.End(&err)

	handled := &Change{MessageID: messageID, IncidentID: timer.GetIncidentId()}
	incident, err := s.repo.GetIncident(ctx, timer.GetIncidentId())
	if errors.Is(err, murphy.ErrNotFound) {
		_, err = s.repo.Record(ctx, handled)
		return err
	}
	if err != nil {
		return fmt.Errorf("get incident: %w", err)
	}
	policy, err := s.escalating(ctx, incident)
	if err != nil {
		return err
	}
	level, round := next(incident.GetEscalationLevel(), incident.GetEscalationRound(), policy)
	if policy == nil || timer.GetEscalationLevel() != level || timer.GetEscalationRound() != round {
		_, err = s.repo.Record(ctx, handled)
		return err
	}
	return s.moveOn(ctx, &Change{MessageID: messageID, IncidentID: incident.GetId()}, incident, policy)
}

func (s *Service) PageCreated(ctx context.Context, messageID string, page *murphy.Page) (err error) {
	ctx, span := tracer.Span(ctx)
	ctx = span.Set(ctx,
		telemetry.WithField("incident.id", page.GetIncident().GetId()),
		telemetry.WithField("escalation.level", page.GetEscalationLevel()),
		telemetry.WithField("escalation.round", page.GetEscalationRound()),
	)
	defer span.End(&err)

	incident, err := s.repo.GetIncident(ctx, page.GetIncident().GetId())
	if err != nil {
		return fmt.Errorf("get incident: %w", err)
	}
	change := &Change{MessageID: messageID, IncidentID: incident.GetId(), Timeline: pageEntries(incident, page, murphy.Now())}
	policy, err := s.escalating(ctx, incident)
	if err != nil {
		return err
	}
	current := incident.GetEscalationLevel() == page.GetEscalationLevel() && incident.GetEscalationRound() == page.GetEscalationRound()
	if len(page.GetReached()) > 0 || policy == nil || !current {
		_, err = s.repo.Record(ctx, change)
		return err
	}
	change.CancelTimers = true
	return s.moveOn(ctx, change, incident, policy)
}

func (s *Service) escalating(ctx context.Context, incident *murphy.Incident) (*murphy.EscalationPolicy, error) {
	if incident.GetStatus() != murphy.IncidentStatus_INCIDENT_STATUS_TRIGGERED || incident.GetEscalationExhausted() {
		return nil, nil
	}
	policy, err := s.policyVersion(ctx, incident.GetEscalationPolicyId(), incident.GetEscalationPolicyVersion())
	if errors.Is(err, murphy.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get the incident's escalation policy version: %w", err)
	}
	return policy, nil
}

func (s *Service) moveOn(ctx context.Context, change *Change, incident *murphy.Incident, policy *murphy.EscalationPolicy) error {
	previous := clone(incident)
	now := murphy.Now()
	incident.Version, incident.UpdatedAt = incident.GetVersion()+1, now
	change.Updated = incident
	level, round := next(incident.GetEscalationLevel(), incident.GetEscalationRound(), policy)
	if level == 0 {
		incident.EscalationExhausted = true
		change.CancelTimers = true
		change.Timeline = append(change.Timeline, note(incident, murphy.TimelineEntryType_TIMELINE_ENTRY_TYPE_ESCALATION_EXHAUSTED, "Escalation is exhausted: every level and repeat was paged", now))
		change.Messages = s.messages(ctx, incident, murphy.IncidentVerb_INCIDENT_VERB_UPDATED, previous, nil)
	} else {
		escalation, err := s.escalate(ctx, incident, level, round, now)
		if err != nil {
			return err
		}
		change.Messages = escalation
	}
	if _, err := s.repo.Record(ctx, change); errors.Is(err, murphy.ErrConflict) {
		return fmt.Errorf("the incident changed while escalating; retrying: %w", err)
	} else if err != nil {
		return fmt.Errorf("record escalation: %w", err)
	}
	return nil
}

func (s *Service) escalate(ctx context.Context, incident *murphy.Incident, level, round int32, now *timestamppb.Timestamp) (func() ([]*ScheduledMessage, error), error) {
	if incident.GetEscalationExhausted() {
		return func() ([]*ScheduledMessage, error) { return nil, nil }, nil
	}
	policy, err := s.policyVersion(ctx, incident.GetEscalationPolicyId(), incident.GetEscalationPolicyVersion())
	if err != nil {
		return nil, fmt.Errorf("get the incident's escalation policy version: %w", err)
	}
	if level < 1 || int(level) > len(policy.GetLevels()) {
		return nil, fmt.Errorf("escalation level %d is not in policy version %d", level, policy.GetVersion())
	}
	previous := clone(incident)
	incident.EscalationLevel, incident.EscalationRound = level, round
	followingLevel, followingRound := next(level, round, policy)
	due := now.AsTime().Add(policy.GetLevels()[level-1].GetDelay().AsDuration())
	return s.messages(ctx, incident, murphy.IncidentVerb_INCIDENT_VERB_ESCALATED, previous, func() ([]*ScheduledMessage, error) {
		timer, err := s.timer(ctx, incident, followingLevel, followingRound, due)
		return []*ScheduledMessage{timer}, err
	}), nil
}

func pageEntries(incident *murphy.Incident, page *murphy.Page, now *timestamppb.Timestamp) []*murphy.TimelineEntry {
	level, round := page.GetEscalationLevel(), page.GetEscalationRound()
	if len(page.GetReached()) == 0 {
		entry := note(incident, murphy.TimelineEntryType_TIMELINE_ENTRY_TYPE_ESCALATION_SKIPPED, fmt.Sprintf("Level %d reached nobody", level), now)
		entry.Escalation = &murphy.TimelineEscalation{Level: level, Round: round, ReachedNobody: page.GetUnreached()}
		return []*murphy.TimelineEntry{entry}
	}
	paged := make([]*murphy.TimelinePage, len(page.GetReached()))
	for i, recipient := range page.GetReached() {
		paged[i] = &murphy.TimelinePage{UserId: recipient.GetUserId(), Via: recipient.GetVia()}
	}
	entry := note(incident, murphy.TimelineEntryType_TIMELINE_ENTRY_TYPE_ESCALATED, fmt.Sprintf("Level %d paged", level), now)
	entry.Escalation = &murphy.TimelineEscalation{Level: level, Round: round, Paged: paged, ReachedNobody: page.GetUnreached()}
	entries := []*murphy.TimelineEntry{entry}
	for _, recipient := range page.GetReached() {
		if recipient.GetContactMethods() == 0 {
			failed := note(incident, murphy.TimelineEntryType_TIMELINE_ENTRY_TYPE_NOTIFICATION_FAILED, "Not paged: the user has no contact methods", now)
			failed.Notification = &murphy.TimelineNotification{Reason: murphy.NotificationReason_NOTIFICATION_REASON_PAGED, UserId: recipient.GetUserId(), Failure: "the user has no contact methods"}
			entries = append(entries, failed)
		}
	}
	return entries
}
