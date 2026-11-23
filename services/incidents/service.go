package incidents

import (
	"context"
	"errors"
	"fmt"
	"time"

	murphy "github.com/murphyhq/server"
	"github.com/murphyhq/server/internal/pagination"
	"github.com/murphyhq/server/internal/telemetry"
	"github.com/murphyhq/server/internal/tracer"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Repository interface {
	FindOpenIncident(ctx context.Context, serviceID, dedupKey string) (*murphy.Incident, error)
	Record(ctx context.Context, change *Change) (recorded bool, err error)
	GetIncident(ctx context.Context, id string) (*murphy.Incident, error)
	ListIncidents(ctx context.Context, filter Filter, before string, limit int) ([]*murphy.Incident, error)
	ListTimeline(ctx context.Context, incidentID string) ([]*murphy.TimelineEntry, error)
	LatestFiringAt(ctx context.Context, incidentID string) (time.Time, error)
	OpenInstances(ctx context.Context, incidentID string) ([]string, error)
	PurgeHandledMessages(ctx context.Context, before time.Time) (int64, error)
	ClaimDueMessages(ctx context.Context, now time.Time, limit int, lease time.Duration) ([]*ScheduledMessage, error)
	DeleteMessages(ctx context.Context, ids []string) error
}

type Catalog interface {
	GetIntegration(ctx context.Context, id string) (*murphy.Integration, error)
	GetService(ctx context.Context, id string) (*murphy.Service, error)
}

type Policies interface {
	GetPolicy(ctx context.Context, id string) (*murphy.EscalationPolicy, error)
	GetPolicyVersion(ctx context.Context, id string, version int32) (*murphy.EscalationPolicy, error)
}

type OnCall interface {
	WhoIsOnCall(ctx context.Context, scheduleIDs []string, at time.Time) (map[string]string, error)
}

type People interface {
	ActiveUsers(ctx context.Context, ids []string) (map[string]bool, error)
}

type Change struct {
	MessageID    string
	IncidentID   string
	Created      *murphy.Incident
	Updated      *murphy.Incident
	Alert        *murphy.Alert
	CancelTimers bool
	Timeline     []*murphy.TimelineEntry
	Messages     func() ([]*ScheduledMessage, error)
}

type MessageKind string

const (
	MessageEvent MessageKind = "event"
	MessageTimer MessageKind = "timer"
)

type ScheduledMessage struct {
	ID         string
	IncidentID string
	Kind       MessageKind
	DueAt      time.Time
	Envelope   []byte
}

const (
	HandledRetention = 7 * 24 * time.Hour
	clockSkew        = time.Second
	resolveWait      = time.Hour
	changeAttempts   = 3
)

var errNothingToResolve = errors.New("no open incident to resolve yet; retrying until its firing alert is handled")

type Service struct {
	repo     Repository
	catalog  Catalog
	policies Policies
	oncall   OnCall
	people   People
}

func New(repo Repository, catalog Catalog, policies Policies, oncall OnCall, people People) *Service {
	return &Service{repo: repo, catalog: catalog, policies: policies, oncall: oncall, people: people}
}

func (s *Service) AlertReceived(ctx context.Context, messageID string, alert *murphy.Alert) (err error) {
	ctx, span := tracer.Span(ctx)
	ctx = span.Set(ctx,
		telemetry.WithField("alert.id", alert.GetId()),
		telemetry.WithField("integration.id", alert.GetIntegrationId()),
		telemetry.WithField("service.id", alert.GetServiceId()),
	)
	defer span.End(&err)

	if err := alert.Validate(); err != nil {
		return err
	}
	service, err := s.accept(ctx, alert)
	if err != nil {
		return err
	}

	open, err := s.repo.FindOpenIncident(ctx, alert.GetServiceId(), alert.GetDedupKey())
	if err != nil && !errors.Is(err, murphy.ErrNotFound) {
		return fmt.Errorf("find open incident: %w", err)
	}

	now := murphy.Now()
	change := &Change{MessageID: messageID, Alert: alert}
	firing := alert.GetStatus() == murphy.AlertStatus_ALERT_STATUS_FIRING
	switch {
	case open == nil && firing:
		incident, err := s.open(ctx, service, alert, now)
		if err != nil {
			return err
		}
		change.IncidentID, change.Created = incident.GetId(), incident
		change.Timeline = append(change.Timeline, entry(incident, murphy.TimelineEntryType_TIMELINE_ENTRY_TYPE_TRIGGERED, alert, now))
		if incident.GetEscalationExhausted() {
			change.Timeline = append(change.Timeline, note(incident, murphy.TimelineEntryType_TIMELINE_ENTRY_TYPE_ESCALATION_EXHAUSTED, "Not paged: the service's escalation policy no longer exists", now))
		}
		change.Messages = s.messages(ctx, incident, murphy.IncidentVerb_INCIDENT_VERB_CREATED, nil, func() ([]*ScheduledMessage, error) {
			if incident.GetEscalationExhausted() {
				return nil, nil
			}
			first, err := s.timer(ctx, incident, 1, 1, now.AsTime())
			return []*ScheduledMessage{first}, err
		})
	case open == nil:
		if time.Since(alert.GetReceivedAt().AsTime()) < resolveWait {
			return errNothingToResolve
		}
	default:
		previous := clone(open)
		change.IncidentID, change.Updated = open.GetId(), open
		open.Version, open.UpdatedAt = open.GetVersion()+1, now
		resolves := false
		if !firing {
			stillFiring, err := s.stillFiring(ctx, open, alert)
			if err != nil {
				return err
			}
			resolves = !stillFiring
		}
		if resolves {
			open.Resolve(now)
			change.CancelTimers = true
			change.Timeline = append(change.Timeline, entry(open, murphy.TimelineEntryType_TIMELINE_ENTRY_TYPE_RESOLVED, alert, now))
			change.Messages = s.messages(ctx, open, murphy.IncidentVerb_INCIDENT_VERB_RESOLVED, previous, nil)
			break
		}
		change.Timeline = append(change.Timeline, entry(open, murphy.TimelineEntryType_TIMELINE_ENTRY_TYPE_ALERTED, alert, now))
		change.Messages = s.messages(ctx, open, murphy.IncidentVerb_INCIDENT_VERB_UPDATED, previous, nil)
	}
	if change.IncidentID != "" {
		ctx = span.Set(ctx, telemetry.WithField("incident.id", change.IncidentID))
	}

	recorded, err := s.repo.Record(ctx, change)
	if err != nil {
		return fmt.Errorf("record alert: %w", err)
	}
	span.Set(ctx, telemetry.WithField("message.duplicate", !recorded))
	return nil
}

func (s *Service) open(ctx context.Context, service *murphy.Service, alert *murphy.Alert, now *timestamppb.Timestamp) (*murphy.Incident, error) {
	incident := &murphy.Incident{
		Id:                 murphy.NewID(),
		ServiceId:          alert.GetServiceId(),
		DedupKey:           alert.GetDedupKey(),
		Title:              alert.GetSummary(),
		Status:             murphy.IncidentStatus_INCIDENT_STATUS_TRIGGERED,
		Urgency:            service.GetUrgency(),
		Version:            1,
		CreatedAt:          now,
		UpdatedAt:          now,
		EscalationPolicyId: service.GetEscalationPolicyId(),
		EscalationRound:    1,
	}
	policy, err := s.policies.GetPolicy(ctx, service.GetEscalationPolicyId())
	switch {
	case errors.Is(err, murphy.ErrNotFound):
		incident.EscalationExhausted = true
	case err != nil:
		return nil, fmt.Errorf("get the service's escalation policy: %w", err)
	default:
		incident.EscalationPolicyVersion = policy.GetVersion()
	}
	return incident, nil
}

func (s *Service) stillFiring(ctx context.Context, incident *murphy.Incident, resolve *murphy.Alert) (bool, error) {
	if resolve.GetInstance() != "" {
		instances, err := s.repo.OpenInstances(ctx, incident.GetId())
		if err != nil {
			return false, fmt.Errorf("find open instances: %w", err)
		}
		for _, instance := range instances {
			if instance != resolve.GetInstance() {
				return true, nil
			}
		}
		return false, nil
	}
	latest, err := s.repo.LatestFiringAt(ctx, incident.GetId())
	if err != nil {
		return false, fmt.Errorf("find latest firing alert: %w", err)
	}
	return resolve.GetReceivedAt().AsTime().Sub(latest) <= clockSkew, nil
}

func (s *Service) accept(ctx context.Context, alert *murphy.Alert) (*murphy.Service, error) {
	integration, err := s.catalog.GetIntegration(ctx, alert.GetIntegrationId())
	if errors.Is(err, murphy.ErrNotFound) {
		return nil, invalidKey("its integration no longer exists")
	}
	if err != nil {
		return nil, err
	}
	if integration.GetServiceId() != alert.GetServiceId() {
		return nil, invalidKey("it belongs to another service")
	}
	if source := murphy.Or(alert.GetSource(), murphy.IntegrationType_INTEGRATION_TYPE_GENERIC); source != integration.GetType() {
		return nil, invalidKey("it was sent in the " + source.Text() + " format, and the integration expects " + integration.GetType().Text())
	}
	if !integration.Accepts(alert.GetKeyVersion(), alert.GetReceivedAt().AsTime()) {
		return nil, invalidKey("the integration is disabled, or the key was rotated")
	}
	service, err := s.catalog.GetService(ctx, alert.GetServiceId())
	if errors.Is(err, murphy.ErrNotFound) {
		return nil, invalidKey("its service no longer exists")
	}
	if err != nil {
		return nil, err
	}
	if service.GetStatus() != murphy.ServiceStatus_SERVICE_STATUS_ACTIVE {
		return nil, &murphy.FieldError{Field: "service_id", Reason: "the service is disabled"}
	}
	return service, nil
}

func invalidKey(reason string) error {
	return &murphy.FieldError{Field: "key", Reason: "refused because " + reason}
}

func entry(incident *murphy.Incident, kind murphy.TimelineEntryType, alert *murphy.Alert, at *timestamppb.Timestamp) *murphy.TimelineEntry {
	return &murphy.TimelineEntry{
		Id:         murphy.NewID(),
		IncidentId: incident.GetId(),
		Type:       kind,
		AlertId:    alert.GetId(),
		Summary:    alert.GetSummary(),
		At:         at,
	}
}

func (s *Service) GetIncident(ctx context.Context, id string) (_ *murphy.Incident, _ []*murphy.TimelineEntry, err error) {
	ctx, span := tracer.Span(ctx)
	ctx = span.Set(ctx, telemetry.WithField("incident.id", id))
	defer span.End(&err)

	if err := murphy.LookupID("incident", id); err != nil {
		return nil, nil, err
	}
	incident, err := s.repo.GetIncident(ctx, id)
	if err != nil {
		return nil, nil, fmt.Errorf("get incident: %w", err)
	}
	timeline, err := s.repo.ListTimeline(ctx, id)
	if err != nil {
		return nil, nil, fmt.Errorf("list timeline: %w", err)
	}
	return incident, timeline, nil
}

type Filter struct {
	Statuses   []murphy.IncidentStatus
	ServiceIDs []string
}

func (s *Service) ListIncidents(ctx context.Context, filter Filter, page pagination.Page) (_ []*murphy.Incident, _ string, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	for _, status := range filter.Statuses {
		if status == murphy.IncidentStatus_INCIDENT_STATUS_UNSPECIFIED || murphy.IncidentStatus_name[int32(status)] == "" {
			return nil, "", &murphy.FieldError{Field: "status", Reason: "must be triggered, acknowledged or resolved"}
		}
	}
	for _, id := range filter.ServiceIDs {
		if err := murphy.LookupID("service", id); err != nil {
			return nil, "", err
		}
	}
	window, err := pagination.Resolve(page)
	if err != nil {
		return nil, "", err
	}
	incidents, err := s.repo.ListIncidents(ctx, filter, window.Before, window.Rows())
	if err != nil {
		return nil, "", fmt.Errorf("list incidents: %w", err)
	}
	items, next := pagination.Trim(incidents, window)
	return items, next, nil
}

func (s *Service) PurgeHandledMessages(ctx context.Context) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	purged, err := s.repo.PurgeHandledMessages(ctx, time.Now().Add(-HandledRetention))
	if err != nil {
		return fmt.Errorf("purge handled messages: %w", err)
	}
	span.Set(ctx, telemetry.WithField("messages.purged", purged))
	return nil
}

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
	if incident.GetStatus() != murphy.IncidentStatus_INCIDENT_STATUS_TRIGGERED || incident.GetEscalationExhausted() {
		_, err = s.repo.Record(ctx, handled)
		return err
	}

	policy, err := s.policies.GetPolicyVersion(ctx, incident.GetEscalationPolicyId(), incident.GetEscalationPolicyVersion())
	if err != nil && !errors.Is(err, murphy.ErrNotFound) {
		return fmt.Errorf("get the incident's escalation policy version: %w", err)
	}
	expectedLevel, expectedRound := next(incident.GetEscalationLevel(), incident.GetEscalationRound(), policy)
	if incident.GetEscalationLevel() == 0 {
		expectedLevel, expectedRound = 1, 1
	}
	if timer.GetEscalationLevel() != expectedLevel || timer.GetEscalationRound() != expectedRound {
		_, err = s.repo.Record(ctx, handled)
		return err
	}

	previous := clone(incident)
	now := murphy.Now()
	incident.Version, incident.UpdatedAt = incident.GetVersion()+1, now
	change := &Change{MessageID: messageID, IncidentID: incident.GetId(), Updated: incident}
	var nextTimer func() (*ScheduledMessage, error)

	level, round := timer.GetEscalationLevel(), timer.GetEscalationRound()
	levels := policy.GetLevels()
	for skipped := 0; ; skipped++ {
		if level == 0 || skipped >= len(levels) {
			incident.EscalationExhausted = true
			change.Timeline = append(change.Timeline, note(incident, murphy.TimelineEntryType_TIMELINE_ENTRY_TYPE_ESCALATION_EXHAUSTED, "Escalation is exhausted: every level and repeat was paged", now))
			break
		}
		current := levels[level-1]
		users, err := s.reach(ctx, current, now.AsTime())
		if err != nil {
			return err
		}
		incident.EscalationLevel, incident.EscalationRound = level, round
		if len(users) == 0 {
			entry := note(incident, murphy.TimelineEntryType_TIMELINE_ENTRY_TYPE_ESCALATION_SKIPPED, fmt.Sprintf("Level %d reached nobody", level), now)
			entry.Escalation = &murphy.TimelineEscalation{Level: level, Round: round}
			change.Timeline = append(change.Timeline, entry)
			level, round = next(level, round, policy)
			continue
		}
		entry := note(incident, murphy.TimelineEntryType_TIMELINE_ENTRY_TYPE_ESCALATED, fmt.Sprintf("Level %d paged", level), now)
		entry.Escalation = &murphy.TimelineEscalation{Level: level, Round: round, UserIds: users}
		change.Timeline = append(change.Timeline, entry)
		followingLevel, followingRound := next(level, round, policy)
		due := now.AsTime().Add(current.GetDelay().AsDuration())
		nextTimer = func() (*ScheduledMessage, error) { return s.timer(ctx, incident, followingLevel, followingRound, due) }
		break
	}

	change.Messages = s.messages(ctx, incident, murphy.IncidentVerb_INCIDENT_VERB_ESCALATED, previous, func() ([]*ScheduledMessage, error) {
		if nextTimer == nil {
			return nil, nil
		}
		message, err := nextTimer()
		return []*ScheduledMessage{message}, err
	})
	recorded, err := s.repo.Record(ctx, change)
	if errors.Is(err, murphy.ErrConflict) {
		return fmt.Errorf("the incident changed while escalating; retrying: %w", err)
	}
	if err != nil {
		return fmt.Errorf("record escalation: %w", err)
	}
	span.Set(ctx, telemetry.WithField("message.duplicate", !recorded))
	return nil
}

func next(level, round int32, policy *murphy.EscalationPolicy) (int32, int32) {
	switch {
	case int(level) < len(policy.GetLevels()):
		return level + 1, round
	case round <= policy.GetRepeatCount():
		return 1, round + 1
	default:
		return 0, 0
	}
}

func (s *Service) reach(ctx context.Context, level *murphy.EscalationLevel, at time.Time) ([]string, error) {
	var users, schedules []string
	for _, target := range level.GetTargets() {
		switch target.GetType() {
		case murphy.EscalationTargetType_ESCALATION_TARGET_TYPE_USER:
			users = append(users, target.GetId())
		case murphy.EscalationTargetType_ESCALATION_TARGET_TYPE_SCHEDULE:
			schedules = append(schedules, target.GetId())
		}
	}
	onCall, err := s.oncall.WhoIsOnCall(ctx, schedules, at)
	if err != nil {
		return nil, fmt.Errorf("find who is on call: %w", err)
	}
	for _, schedule := range schedules {
		if user, ok := onCall[schedule]; ok {
			users = append(users, user)
		}
	}
	active, err := s.people.ActiveUsers(ctx, users)
	if err != nil {
		return nil, fmt.Errorf("find active users: %w", err)
	}
	var reached []string
	seen := map[string]bool{}
	for _, user := range users {
		if active[user] && !seen[user] {
			seen[user] = true
			reached = append(reached, user)
		}
	}
	return reached, nil
}

func (s *Service) Acknowledge(ctx context.Context, id string) (_ *murphy.Incident, err error) {
	ctx, span := tracer.Span(ctx)
	ctx = span.Set(ctx, telemetry.WithField("incident.id", id))
	defer span.End(&err)

	return s.change(ctx, id, murphy.IncidentVerb_INCIDENT_VERB_ACKNOWLEDGED, func(incident *murphy.Incident, now *timestamppb.Timestamp) *murphy.TimelineEntry {
		if incident.GetStatus() != murphy.IncidentStatus_INCIDENT_STATUS_TRIGGERED {
			return nil
		}
		incident.Status, incident.AcknowledgedAt = murphy.IncidentStatus_INCIDENT_STATUS_ACKNOWLEDGED, now
		return note(incident, murphy.TimelineEntryType_TIMELINE_ENTRY_TYPE_ACKNOWLEDGED, "Acknowledged through the API", now)
	})
}

func (s *Service) Resolve(ctx context.Context, id string) (_ *murphy.Incident, err error) {
	ctx, span := tracer.Span(ctx)
	ctx = span.Set(ctx, telemetry.WithField("incident.id", id))
	defer span.End(&err)

	return s.change(ctx, id, murphy.IncidentVerb_INCIDENT_VERB_RESOLVED, func(incident *murphy.Incident, now *timestamppb.Timestamp) *murphy.TimelineEntry {
		if !incident.Resolve(now) {
			return nil
		}
		return note(incident, murphy.TimelineEntryType_TIMELINE_ENTRY_TYPE_RESOLVED, "Resolved through the API", now)
	})
}

func (s *Service) change(ctx context.Context, id string, verb murphy.IncidentVerb, apply func(*murphy.Incident, *timestamppb.Timestamp) *murphy.TimelineEntry) (*murphy.Incident, error) {
	if err := murphy.LookupID("incident", id); err != nil {
		return nil, err
	}
	for attempt := 1; ; attempt++ {
		incident, err := s.repo.GetIncident(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("get incident: %w", err)
		}
		previous := clone(incident)
		now := murphy.Now()
		entry := apply(incident, now)
		if entry == nil {
			return previous, nil
		}
		incident.Version, incident.UpdatedAt = incident.GetVersion()+1, now
		change := &Change{
			IncidentID:   id,
			Updated:      incident,
			CancelTimers: true,
			Timeline:     []*murphy.TimelineEntry{entry},
			Messages:     s.messages(ctx, incident, verb, previous, nil),
		}
		_, err = s.repo.Record(ctx, change)
		if errors.Is(err, murphy.ErrConflict) && attempt < changeAttempts {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("record %s: %w", verb, err)
		}
		return incident, nil
	}
}

func (s *Service) messages(ctx context.Context, incident *murphy.Incident, verb murphy.IncidentVerb, previous *murphy.Incident, more func() ([]*ScheduledMessage, error)) func() ([]*ScheduledMessage, error) {
	return func() ([]*ScheduledMessage, error) {
		event, err := scheduled(ctx, incident.GetId(), MessageEvent, incident.GetUpdatedAt().AsTime(), &murphy.Envelope{
			Event: &murphy.Envelope_Incident{Incident: &murphy.IncidentEvent{Verb: verb, Previous: previous, Current: clone(incident)}},
		})
		if err != nil {
			return nil, err
		}
		list := []*ScheduledMessage{event}
		if more != nil {
			extra, err := more()
			if err != nil {
				return nil, err
			}
			list = append(list, extra...)
		}
		return list, nil
	}
}

func (s *Service) timer(ctx context.Context, incident *murphy.Incident, level, round int32, due time.Time) (*ScheduledMessage, error) {
	return scheduled(ctx, incident.GetId(), MessageTimer, due, &murphy.Envelope{
		Event: &murphy.Envelope_Timer{Timer: &murphy.TimerEvent{
			Verb: murphy.TimerVerb_TIMER_VERB_EXPIRED,
			Current: &murphy.Timer{
				Id: murphy.NewID(), IncidentId: incident.GetId(), EscalationLevel: level, EscalationRound: round, DueAt: timestamppb.New(due),
			},
		}},
	})
}

func scheduled(ctx context.Context, incidentID string, kind MessageKind, due time.Time, envelope *murphy.Envelope) (*ScheduledMessage, error) {
	envelope.Id, envelope.OccurredAt, envelope.TraceContext = murphy.NewID(), murphy.Now(), tracer.Inject(ctx)
	data, err := proto.Marshal(envelope)
	if err != nil {
		return nil, fmt.Errorf("encode message: %w", err)
	}
	return &ScheduledMessage{ID: envelope.GetId(), IncidentID: incidentID, Kind: kind, DueAt: due, Envelope: data}, nil
}

func note(incident *murphy.Incident, kind murphy.TimelineEntryType, summary string, at *timestamppb.Timestamp) *murphy.TimelineEntry {
	return &murphy.TimelineEntry{Id: murphy.NewID(), IncidentId: incident.GetId(), Type: kind, Summary: summary, At: at}
}

func clone(incident *murphy.Incident) *murphy.Incident {
	return proto.Clone(incident).(*murphy.Incident)
}
