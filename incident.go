package murphy

import (
	"fmt"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type IncidentDecision struct {
	Incident     *Incident
	Created      bool
	Announce     []Announcement
	Timeline     []*TimelineEntry
	NextTimer    *Timer
	CancelTimers bool
}

type Announcement struct {
	Verb     IncidentVerb
	Previous *Incident
}

func OpenIncident(alert *Alert, service *Service, policy *EscalationPolicy, now *timestamppb.Timestamp) *IncidentDecision {
	incident := &Incident{
		Id:                 NewID(),
		ServiceId:          alert.GetServiceId(),
		DedupKey:           alert.GetDedupKey(),
		Title:              alert.GetSummary(),
		Status:             IncidentStatus_INCIDENT_STATUS_TRIGGERED,
		Urgency:            service.GetUrgency(),
		Version:            1,
		CreatedAt:          now,
		UpdatedAt:          now,
		EscalationPolicyId: service.GetEscalationPolicyId(),
	}
	decision := &IncidentDecision{
		Incident: incident,
		Created:  true,
		Announce: []Announcement{{Verb: IncidentVerb_INCIDENT_VERB_CREATED}},
		Timeline: []*TimelineEntry{alertEntry(incident, TimelineEntryType_TIMELINE_ENTRY_TYPE_TRIGGERED, alert, now)},
	}
	if policy == nil {
		incident.EscalationExhausted = true
		decision.Timeline = append(decision.Timeline, note(incident, TimelineEntryType_TIMELINE_ENTRY_TYPE_ESCALATION_EXHAUSTED, "Not paged: the service's escalation policy no longer exists", now))
		return decision
	}
	incident.EscalationPolicyVersion = policy.GetVersion()
	decision.escalate(1, 1, policy, now)
	return decision
}

func (i *Incident) Receive(alert *Alert, stillFiring bool, now *timestamppb.Timestamp) *IncidentDecision {
	decision, previous := i.change(now)
	if stillFiring {
		decision.Announce = []Announcement{{Verb: IncidentVerb_INCIDENT_VERB_UPDATED, Previous: previous}}
		decision.Timeline = []*TimelineEntry{alertEntry(i, TimelineEntryType_TIMELINE_ENTRY_TYPE_ALERTED, alert, now)}
		return decision
	}
	decision.Announce = []Announcement{{Verb: IncidentVerb_INCIDENT_VERB_RESOLVED, Previous: previous}}
	i.Status, i.ResolvedAt = IncidentStatus_INCIDENT_STATUS_RESOLVED, now
	decision.CancelTimers = true
	decision.Timeline = []*TimelineEntry{alertEntry(i, TimelineEntryType_TIMELINE_ENTRY_TYPE_RESOLVED, alert, now)}
	return decision
}

func (i *Incident) Escalating() bool {
	return i.GetStatus() == IncidentStatus_INCIDENT_STATUS_TRIGGERED && !i.GetEscalationExhausted()
}

func (i *Incident) TimerExpired(timer *Timer, policy *EscalationPolicy, now *timestamppb.Timestamp) *IncidentDecision {
	level, round := nextStep(i.GetEscalationLevel(), i.GetEscalationRound(), policy)
	if !i.Escalating() || policy == nil || timer.GetEscalationLevel() != level || timer.GetEscalationRound() != round {
		return nil
	}
	return i.moveOn(policy, now)
}

func (i *Incident) PageCreated(page *Page, policy *EscalationPolicy, now *timestamppb.Timestamp) *IncidentDecision {
	timeline := pageEntries(i, page, now)
	current := i.GetEscalationLevel() == page.GetEscalationLevel() && i.GetEscalationRound() == page.GetEscalationRound()
	if len(page.GetReached()) > 0 || !i.Escalating() || policy == nil || !current {
		return &IncidentDecision{Timeline: timeline}
	}
	decision := i.moveOn(policy, now)
	decision.Timeline = append(timeline, decision.Timeline...)
	decision.CancelTimers = true
	return decision
}

func (i *Incident) Acknowledge(now *timestamppb.Timestamp) *IncidentDecision {
	if i.GetStatus() != IncidentStatus_INCIDENT_STATUS_TRIGGERED {
		return nil
	}
	decision, previous := i.change(now)
	decision.Announce = []Announcement{{Verb: IncidentVerb_INCIDENT_VERB_ACKNOWLEDGED, Previous: previous}}
	i.Status, i.AcknowledgedAt = IncidentStatus_INCIDENT_STATUS_ACKNOWLEDGED, now
	decision.CancelTimers = true
	decision.Timeline = []*TimelineEntry{note(i, TimelineEntryType_TIMELINE_ENTRY_TYPE_ACKNOWLEDGED, "Acknowledged through the API", now)}
	return decision
}

func (i *Incident) Resolve(now *timestamppb.Timestamp) *IncidentDecision {
	if !i.Open() {
		return nil
	}
	decision, previous := i.change(now)
	decision.Announce = []Announcement{{Verb: IncidentVerb_INCIDENT_VERB_RESOLVED, Previous: previous}}
	i.Status, i.ResolvedAt = IncidentStatus_INCIDENT_STATUS_RESOLVED, now
	decision.CancelTimers = true
	decision.Timeline = []*TimelineEntry{note(i, TimelineEntryType_TIMELINE_ENTRY_TYPE_RESOLVED, "Resolved through the API", now)}
	return decision
}

func NotificationOutcome(notification *Notification, failed bool) *IncidentDecision {
	channel := notification.GetContactMethod().GetType()
	kind := TimelineEntryType_TIMELINE_ENTRY_TYPE_NOTIFIED
	summary := fmt.Sprintf("%s sent by %s", outcomeNoun(notification.GetReason()), channel.Text())
	if failed {
		kind = TimelineEntryType_TIMELINE_ENTRY_TYPE_NOTIFICATION_FAILED
		summary = fmt.Sprintf("%s by %s failed: %s", outcomeNoun(notification.GetReason()), channel.Text(), notification.GetFailure())
	}
	return &IncidentDecision{Timeline: []*TimelineEntry{{
		Id: NewID(), IncidentId: notification.GetIncident().GetId(), Type: kind, Summary: summary, At: Or(notification.GetSentAt(), Now()),
		Notification: &TimelineNotification{
			NotificationId:  notification.GetId(),
			Reason:          notification.GetReason(),
			UserId:          notification.GetUserId(),
			ContactMethodId: notification.GetContactMethod().GetId(),
			Channel:         channel,
			Failure:         notification.GetFailure(),
		},
	}}}
}

func (i *Incident) Open() bool {
	return i.GetStatus() != IncidentStatus_INCIDENT_STATUS_RESOLVED
}

func (i *Incident) change(now *timestamppb.Timestamp) (*IncidentDecision, *Incident) {
	previous := proto.CloneOf(i)
	i.Version, i.UpdatedAt = i.GetVersion()+1, now
	return &IncidentDecision{Incident: i}, previous
}

func (i *Incident) moveOn(policy *EscalationPolicy, now *timestamppb.Timestamp) *IncidentDecision {
	decision, previous := i.change(now)
	level, round := nextStep(i.GetEscalationLevel(), i.GetEscalationRound(), policy)
	if level == 0 {
		i.EscalationExhausted = true
		decision.CancelTimers = true
		decision.Announce = []Announcement{{Verb: IncidentVerb_INCIDENT_VERB_UPDATED, Previous: previous}}
		decision.Timeline = []*TimelineEntry{note(i, TimelineEntryType_TIMELINE_ENTRY_TYPE_ESCALATION_EXHAUSTED, "Escalation is exhausted: every level and repeat was paged", now)}
		return decision
	}
	decision.escalate(level, round, policy, now)
	return decision
}

func (d *IncidentDecision) escalate(level, round int32, policy *EscalationPolicy, now *timestamppb.Timestamp) {
	incident := d.Incident
	previous := proto.CloneOf(incident)
	incident.EscalationLevel, incident.EscalationRound = level, round
	d.Announce = append(d.Announce, Announcement{Verb: IncidentVerb_INCIDENT_VERB_ESCALATED, Previous: previous})
	followingLevel, followingRound := nextStep(level, round, policy)
	d.NextTimer = &Timer{
		Id: NewID(), IncidentId: incident.GetId(), EscalationLevel: followingLevel, EscalationRound: followingRound,
		DueAt: timestamppb.New(now.AsTime().Add(policy.GetLevels()[level-1].GetDelay().AsDuration())),
	}
}

func nextStep(level, round int32, policy *EscalationPolicy) (int32, int32) {
	switch {
	case int(level) < len(policy.GetLevels()):
		return level + 1, round
	case round <= policy.GetRepeatCount():
		return 1, round + 1
	default:
		return 0, 0
	}
}

func pageEntries(incident *Incident, page *Page, now *timestamppb.Timestamp) []*TimelineEntry {
	level, round := page.GetEscalationLevel(), page.GetEscalationRound()
	if len(page.GetReached()) == 0 {
		entry := note(incident, TimelineEntryType_TIMELINE_ENTRY_TYPE_ESCALATION_SKIPPED, fmt.Sprintf("Level %d reached nobody", level), now)
		entry.Escalation = &TimelineEscalation{Level: level, Round: round, ReachedNobody: page.GetUnreached()}
		return []*TimelineEntry{entry}
	}
	paged := make([]*TimelinePage, len(page.GetReached()))
	for i, recipient := range page.GetReached() {
		paged[i] = &TimelinePage{UserId: recipient.GetUserId(), Via: recipient.GetVia()}
	}
	entry := note(incident, TimelineEntryType_TIMELINE_ENTRY_TYPE_ESCALATED, fmt.Sprintf("Level %d paged", level), now)
	entry.Escalation = &TimelineEscalation{Level: level, Round: round, Paged: paged, ReachedNobody: page.GetUnreached()}
	entries := []*TimelineEntry{entry}
	for _, recipient := range page.GetReached() {
		if recipient.GetContactMethods() == 0 {
			failed := note(incident, TimelineEntryType_TIMELINE_ENTRY_TYPE_NOTIFICATION_FAILED, "Not paged: the user has no contact methods", now)
			failed.Notification = &TimelineNotification{Reason: NotificationReason_NOTIFICATION_REASON_PAGED, UserId: recipient.GetUserId(), Failure: "the user has no contact methods"}
			entries = append(entries, failed)
		}
	}
	return entries
}

func alertEntry(incident *Incident, kind TimelineEntryType, alert *Alert, at *timestamppb.Timestamp) *TimelineEntry {
	return &TimelineEntry{Id: NewID(), IncidentId: incident.GetId(), Type: kind, AlertId: alert.GetId(), Summary: alert.GetSummary(), At: at}
}

func note(incident *Incident, kind TimelineEntryType, summary string, at *timestamppb.Timestamp) *TimelineEntry {
	return &TimelineEntry{Id: NewID(), IncidentId: incident.GetId(), Type: kind, Summary: summary, At: at}
}

func outcomeNoun(reason NotificationReason) string {
	switch reason {
	case NotificationReason_NOTIFICATION_REASON_ACKNOWLEDGED:
		return "Acknowledgement"
	case NotificationReason_NOTIFICATION_REASON_RESOLVED:
		return "Resolution"
	default:
		return "Page"
	}
}

func (s IncidentStatus) Text() string {
	return enumText(s, "INCIDENT_STATUS_")
}

func IncidentStatusFromText(text string) IncidentStatus {
	return enumFromText[IncidentStatus](IncidentStatus_value, "INCIDENT_STATUS_", text)
}

func (t TimelineEntryType) Text() string {
	return enumText(t, "TIMELINE_ENTRY_TYPE_")
}

func TimelineEntryTypeFromText(text string) TimelineEntryType {
	return enumFromText[TimelineEntryType](TimelineEntryType_value, "TIMELINE_ENTRY_TYPE_", text)
}

func (d *IncidentDecision) GetIncident() *Incident {
	if d == nil {
		return nil
	}
	return d.Incident
}

func (d *IncidentDecision) GetAnnounce() []Announcement {
	if d == nil {
		return nil
	}
	return d.Announce
}

func (d *IncidentDecision) GetTimeline() []*TimelineEntry {
	if d == nil {
		return nil
	}
	return d.Timeline
}

func (d *IncidentDecision) GetNextTimer() *Timer {
	if d == nil {
		return nil
	}
	return d.NextTimer
}

func (d *IncidentDecision) GetCancelTimers() bool {
	return d != nil && d.CancelTimers
}
