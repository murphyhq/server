package incidents

import (
	"fmt"
	"slices"
	"strings"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/murphyhq/server"
)

type Decision struct {
	Incident     *murphy.Incident
	Created      bool
	Announce     []Announcement
	Timeline     []*murphy.TimelineEntry
	NextTimer    *murphy.Timer
	CancelTimers bool
}

type Announcement struct {
	Verb     murphy.IncidentVerb
	Previous *murphy.Incident
}

func OpenIncident(alert *murphy.Alert, service *murphy.Service, policy *murphy.EscalationPolicy, now *timestamppb.Timestamp) *Decision {
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
	}
	decision := &Decision{
		Incident: incident,
		Created:  true,
		Announce: []Announcement{{Verb: murphy.IncidentVerb_INCIDENT_VERB_CREATED}},
		Timeline: []*murphy.TimelineEntry{alertEntry(incident, murphy.TimelineEntryType_TIMELINE_ENTRY_TYPE_TRIGGERED, alert, now)},
	}
	if policy == nil {
		incident.EscalationExhausted = true
		decision.Timeline = append(decision.Timeline, note(incident, murphy.TimelineEntryType_TIMELINE_ENTRY_TYPE_ESCALATION_EXHAUSTED, "Not paged: the service's escalation policy no longer exists", now))
		return decision
	}
	incident.EscalationPolicyVersion = policy.GetVersion()
	decision.escalate(1, 1, policy, now)
	return decision
}

func ReceiveAlert(incident *murphy.Incident, alert *murphy.Alert, stillFiring bool, now *timestamppb.Timestamp) *Decision {
	decision, previous := change(incident, now)
	if stillFiring {
		decision.Announce = []Announcement{{Verb: murphy.IncidentVerb_INCIDENT_VERB_UPDATED, Previous: previous}}
		decision.Timeline = []*murphy.TimelineEntry{alertEntry(incident, murphy.TimelineEntryType_TIMELINE_ENTRY_TYPE_ALERTED, alert, now)}
		return decision
	}
	incident.Status, incident.ResolvedAt = murphy.IncidentStatus_INCIDENT_STATUS_RESOLVED, now
	decision.Announce = []Announcement{{Verb: murphy.IncidentVerb_INCIDENT_VERB_RESOLVED, Previous: previous}}
	decision.CancelTimers = true
	decision.Timeline = []*murphy.TimelineEntry{alertEntry(incident, murphy.TimelineEntryType_TIMELINE_ENTRY_TYPE_RESOLVED, alert, now)}
	return decision
}

func Escalating(incident *murphy.Incident) bool {
	return incident.GetStatus() == murphy.IncidentStatus_INCIDENT_STATUS_TRIGGERED && !incident.GetEscalationExhausted()
}

func ExpireTimer(incident *murphy.Incident, timer *murphy.Timer, policy *murphy.EscalationPolicy, now *timestamppb.Timestamp) *Decision {
	level, round := nextStep(incident.GetEscalationLevel(), incident.GetEscalationRound(), policy)
	if !Escalating(incident) || policy == nil || timer.GetEscalationLevel() != level || timer.GetEscalationRound() != round {
		return nil
	}
	return moveOn(incident, policy, now)
}

func RecordPage(incident *murphy.Incident, page *murphy.Page, policy *murphy.EscalationPolicy, now *timestamppb.Timestamp) *Decision {
	timeline := pageEntries(incident, page, murphy.Or(page.GetCreatedAt(), now))
	current := incident.GetEscalationLevel() == page.GetEscalationLevel() && incident.GetEscalationRound() == page.GetEscalationRound()
	if len(page.GetReached()) > 0 || !Escalating(incident) || policy == nil || !current {
		return &Decision{Timeline: timeline}
	}
	decision := moveOn(incident, policy, now)
	decision.Timeline = append(timeline, decision.Timeline...)
	decision.CancelTimers = true
	return decision
}

func AcknowledgeIncident(incident *murphy.Incident, actorID string, now *timestamppb.Timestamp) *Decision {
	if !incident.Open() {
		return nil
	}
	decision, previous := change(incident, now)
	acknowledged := note(incident, murphy.TimelineEntryType_TIMELINE_ENTRY_TYPE_ACKNOWLEDGED, "Acknowledged through the API", now)
	acknowledged.ActorUserId = actorID
	decision.Timeline = []*murphy.TimelineEntry{acknowledged}
	if previous.GetStatus() == murphy.IncidentStatus_INCIDENT_STATUS_ACKNOWLEDGED {
		return decision
	}
	incident.Status, incident.AcknowledgedAt = murphy.IncidentStatus_INCIDENT_STATUS_ACKNOWLEDGED, now
	decision.Announce = []Announcement{{Verb: murphy.IncidentVerb_INCIDENT_VERB_ACKNOWLEDGED, Previous: previous}}
	decision.CancelTimers = true
	return decision
}

func ResolveIncident(incident *murphy.Incident, actorID string, now *timestamppb.Timestamp) *Decision {
	if !incident.Open() {
		return nil
	}
	decision, previous := change(incident, now)
	incident.Status, incident.ResolvedAt = murphy.IncidentStatus_INCIDENT_STATUS_RESOLVED, now
	decision.Announce = []Announcement{{Verb: murphy.IncidentVerb_INCIDENT_VERB_RESOLVED, Previous: previous}}
	decision.CancelTimers = true
	resolved := note(incident, murphy.TimelineEntryType_TIMELINE_ENTRY_TYPE_RESOLVED, "Resolved through the API", now)
	resolved.ActorUserId = actorID
	decision.Timeline = []*murphy.TimelineEntry{resolved}
	return decision
}

func NotificationOutcome(notification *murphy.Notification, failed bool) *Decision {
	if !failed && notification.GetReason() != murphy.NotificationReason_NOTIFICATION_REASON_PAGED {
		return nil
	}
	channel := notification.GetContactMethod().GetType()
	kind := murphy.TimelineEntryType_TIMELINE_ENTRY_TYPE_NOTIFIED
	summary := fmt.Sprintf("%s sent by %s", outcomeNoun(notification.GetReason()), channel.Text())
	if failed {
		kind = murphy.TimelineEntryType_TIMELINE_ENTRY_TYPE_NOTIFICATION_FAILED
		summary = fmt.Sprintf("%s by %s failed: %s", outcomeNoun(notification.GetReason()), channel.Text(), notification.GetFailure())
	}
	return &Decision{Timeline: []*murphy.TimelineEntry{{
		Id: murphy.NewID(), IncidentId: notification.GetIncident().GetId(), Type: kind, Summary: summary, At: murphy.Or(notification.GetSentAt(), murphy.Now()),
		Notification: &murphy.TimelineNotification{
			NotificationId:  notification.GetId(),
			Reason:          notification.GetReason(),
			UserId:          notification.GetUserId(),
			ContactMethodId: notification.GetContactMethod().GetId(),
			Channel:         channel,
			Failure:         notification.GetFailure(),
		},
	}}}
}

func change(incident *murphy.Incident, now *timestamppb.Timestamp) (*Decision, *murphy.Incident) {
	previous := proto.CloneOf(incident)
	incident.Version, incident.UpdatedAt = incident.GetVersion()+1, now
	return &Decision{Incident: incident}, previous
}

func moveOn(incident *murphy.Incident, policy *murphy.EscalationPolicy, now *timestamppb.Timestamp) *Decision {
	decision, previous := change(incident, now)
	level, round := nextStep(incident.GetEscalationLevel(), incident.GetEscalationRound(), policy)
	if level == 0 {
		incident.EscalationExhausted = true
		decision.CancelTimers = true
		decision.Announce = []Announcement{{Verb: murphy.IncidentVerb_INCIDENT_VERB_UPDATED, Previous: previous}}
		decision.Timeline = []*murphy.TimelineEntry{note(incident, murphy.TimelineEntryType_TIMELINE_ENTRY_TYPE_ESCALATION_EXHAUSTED, "Escalation is exhausted: every level and repeat was paged", now)}
		return decision
	}
	decision.escalate(level, round, policy, now)
	return decision
}

func (d *Decision) escalate(level, round int32, policy *murphy.EscalationPolicy, now *timestamppb.Timestamp) {
	incident := d.Incident
	previous := proto.CloneOf(incident)
	incident.EscalationLevel, incident.EscalationRound = level, round
	d.Announce = append(d.Announce, Announcement{Verb: murphy.IncidentVerb_INCIDENT_VERB_ESCALATED, Previous: previous})
	followingLevel, followingRound := nextStep(level, round, policy)
	d.NextTimer = &murphy.Timer{
		Id: murphy.NewID(), IncidentId: incident.GetId(), EscalationLevel: followingLevel, EscalationRound: followingRound,
		DueAt: timestamppb.New(now.AsTime().Add(policy.GetLevels()[level-1].GetDelay().AsDuration())),
	}
}

func nextStep(level, round int32, policy *murphy.EscalationPolicy) (int32, int32) {
	switch {
	case int(level) < len(policy.GetLevels()):
		return level + 1, round
	case round <= policy.GetRepeatCount():
		return 1, round + 1
	default:
		return 0, 0
	}
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
			failed := note(incident, murphy.TimelineEntryType_TIMELINE_ENTRY_TYPE_NOTIFICATION_FAILED, "Not paged: the user has no working contact methods", now)
			failed.Notification = &murphy.TimelineNotification{Reason: murphy.NotificationReason_NOTIFICATION_REASON_PAGED, UserId: recipient.GetUserId(), Failure: "the user has no working contact methods"}
			entries = append(entries, failed)
		}
	}
	return entries
}

func alertEntry(incident *murphy.Incident, kind murphy.TimelineEntryType, alert *murphy.Alert, at *timestamppb.Timestamp) *murphy.TimelineEntry {
	return &murphy.TimelineEntry{Id: murphy.NewID(), IncidentId: incident.GetId(), Type: kind, AlertId: alert.GetId(), Summary: alert.GetSummary(), At: at}
}

func note(incident *murphy.Incident, kind murphy.TimelineEntryType, summary string, at *timestamppb.Timestamp) *murphy.TimelineEntry {
	return &murphy.TimelineEntry{Id: murphy.NewID(), IncidentId: incident.GetId(), Type: kind, Summary: summary, At: at}
}

func outcomeNoun(reason murphy.NotificationReason) string {
	switch reason {
	case murphy.NotificationReason_NOTIFICATION_REASON_ACKNOWLEDGED:
		return "Acknowledgement"
	case murphy.NotificationReason_NOTIFICATION_REASON_RESOLVED:
		return "Resolution"
	default:
		return "Page"
	}
}

func (d *Decision) GetIncident() *murphy.Incident {
	if d == nil {
		return nil
	}
	return d.Incident
}

func (d *Decision) GetAnnounce() []Announcement {
	if d == nil {
		return nil
	}
	return d.Announce
}

func (d *Decision) GetTimeline() []*murphy.TimelineEntry {
	if d == nil {
		return nil
	}
	return d.Timeline
}

func (d *Decision) GetNextTimer() *murphy.Timer {
	if d == nil {
		return nil
	}
	return d.NextTimer
}

func (d *Decision) GetCancelTimers() bool {
	return d != nil && d.CancelTimers
}

func Summary(entry *murphy.TimelineEntry, nameOf func(userID string) string) string {
	person := func(userID string) string { return murphy.Or(nameOf(userID), "someone since deleted") }
	switch {
	case entry.GetNotification() != nil:
		return notificationSummary(entry, person(entry.GetNotification().GetUserId()))
	case entry.GetType() == murphy.TimelineEntryType_TIMELINE_ENTRY_TYPE_ESCALATED && len(entry.GetEscalation().GetPaged()) > 0:
		return escalationSummary(entry.GetEscalation(), person)
	case entry.GetActorUserId() != "" && entry.GetType() == murphy.TimelineEntryType_TIMELINE_ENTRY_TYPE_ACKNOWLEDGED:
		return "Acknowledged by " + person(entry.GetActorUserId())
	case entry.GetActorUserId() != "" && entry.GetType() == murphy.TimelineEntryType_TIMELINE_ENTRY_TYPE_RESOLVED:
		return "Resolved by " + person(entry.GetActorUserId())
	default:
		return entry.GetSummary()
	}
}

func notificationSummary(entry *murphy.TimelineEntry, person string) string {
	notification := entry.GetNotification()
	channel := ""
	if notification.GetChannel() != murphy.ContactMethodType_CONTACT_METHOD_TYPE_UNSPECIFIED {
		channel = " by " + notification.GetChannel().Text()
	}
	verb, failed, about := "Paged ", "Couldn't page ", ""
	switch notification.GetReason() {
	case murphy.NotificationReason_NOTIFICATION_REASON_ACKNOWLEDGED:
		verb, failed, about = "Told ", "Couldn't tell ", " that it was acknowledged"
	case murphy.NotificationReason_NOTIFICATION_REASON_RESOLVED:
		verb, failed, about = "Told ", "Couldn't tell ", " that it was resolved"
	}
	if entry.GetType() == murphy.TimelineEntryType_TIMELINE_ENTRY_TYPE_NOTIFIED {
		return verb + person + channel + about
	}
	return failed + person + channel + about + ": " + notification.GetFailure()
}

func escalationSummary(escalation *murphy.TimelineEscalation, person func(string) string) string {
	var people []string
	for _, page := range escalation.GetPaged() {
		if name := person(page.GetUserId()); !slices.Contains(people, name) {
			people = append(people, name)
		}
	}
	summary := fmt.Sprintf("Level %d paged %s", escalation.GetLevel(), listOf(people))
	if escalation.GetRound() > 1 {
		summary += fmt.Sprintf(", round %d", escalation.GetRound())
	}
	return summary
}

func listOf(names []string) string {
	if len(names) < 2 {
		return strings.Join(names, "")
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}
