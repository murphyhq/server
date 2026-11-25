package incidents_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/services/incidents"
)

const (
	primary = "0195f3a0-0000-7000-8000-00000000000a"
	ana     = "0195f3a0-0000-7000-8000-0000000000a1"
)

func twoLevels() *murphy.EscalationPolicy {
	return &murphy.EscalationPolicy{
		Id: murphy.NewID(), Version: 3, RepeatCount: 1,
		Levels: []*murphy.EscalationLevel{
			{Delay: durationpb.New(5 * time.Minute), Targets: []*murphy.EscalationTarget{{Type: murphy.EscalationTargetType_ESCALATION_TARGET_TYPE_SCHEDULE, Id: primary}}},
			{Delay: durationpb.New(10 * time.Minute), Targets: []*murphy.EscalationTarget{{Type: murphy.EscalationTargetType_ESCALATION_TARGET_TYPE_USER, Id: bo}}},
		},
	}
}

func incidentAt(level, round int32) *murphy.Incident {
	return &murphy.Incident{
		Id: murphy.NewID(), Status: murphy.IncidentStatus_INCIDENT_STATUS_TRIGGERED, Version: 2,
		EscalationPolicyVersion: 3, EscalationLevel: level, EscalationRound: round,
	}
}

func firing() *murphy.Alert {
	return &murphy.Alert{Id: murphy.NewID(), ServiceId: murphy.NewID(), DedupKey: "disk-full", Status: murphy.AlertStatus_ALERT_STATUS_FIRING, Summary: "Disk almost full"}
}

func verbs(decision *incidents.Decision) []murphy.IncidentVerb {
	out := make([]murphy.IncidentVerb, len(decision.Announce))
	for i, announcement := range decision.Announce {
		out[i] = announcement.Verb
	}
	return out
}

func entries(decision *incidents.Decision) []murphy.TimelineEntryType {
	out := make([]murphy.TimelineEntryType, len(decision.Timeline))
	for i, entry := range decision.Timeline {
		out[i] = entry.GetType()
	}
	return out
}

func TestAnIncidentOpensOnItsFirstLevel(t *testing.T) {
	policy := twoLevels()
	service := &murphy.Service{Urgency: murphy.Urgency_URGENCY_LOW, EscalationPolicyId: policy.GetId()}

	decision := incidents.OpenIncident(firing(), service, policy, murphy.Now())

	opened := decision.Incident
	assert.True(t, decision.Created)
	assert.Equal(t, murphy.IncidentStatus_INCIDENT_STATUS_TRIGGERED, opened.GetStatus())
	assert.Equal(t, murphy.Urgency_URGENCY_LOW, opened.GetUrgency())
	assert.Equal(t, "Disk almost full", opened.GetTitle())
	assert.Equal(t, int32(3), opened.GetEscalationPolicyVersion())
	assert.Equal(t, []int32{1, 1}, []int32{opened.GetEscalationLevel(), opened.GetEscalationRound()})
	assert.Equal(t, []murphy.IncidentVerb{murphy.IncidentVerb_INCIDENT_VERB_CREATED, murphy.IncidentVerb_INCIDENT_VERB_ESCALATED}, verbs(decision))
	assert.Equal(t, []murphy.TimelineEntryType{murphy.TimelineEntryType_TIMELINE_ENTRY_TYPE_TRIGGERED}, entries(decision))
	assert.Equal(t, []int32{2, 1}, []int32{decision.NextTimer.GetEscalationLevel(), decision.NextTimer.GetEscalationRound()}, "the next level")
	assert.WithinDuration(t, time.Now().Add(5*time.Minute), decision.NextTimer.GetDueAt().AsTime(), time.Second, "after the first level's delay")
}

func TestAnIncidentWhosePolicyIsGoneOpensWithoutPaging(t *testing.T) {
	decision := incidents.OpenIncident(firing(), &murphy.Service{}, nil, murphy.Now())

	assert.True(t, decision.Incident.GetEscalationExhausted())
	assert.Nil(t, decision.NextTimer)
	assert.Equal(t, []murphy.IncidentVerb{murphy.IncidentVerb_INCIDENT_VERB_CREATED}, verbs(decision))
	assert.Equal(t, []murphy.TimelineEntryType{murphy.TimelineEntryType_TIMELINE_ENTRY_TYPE_TRIGGERED, murphy.TimelineEntryType_TIMELINE_ENTRY_TYPE_ESCALATION_EXHAUSTED}, entries(decision))
}

func TestAnAlertJoinsAnIncidentWhileAnythingStillFires(t *testing.T) {
	incident := incidentAt(1, 1)

	decision := incidents.ReceiveAlert(incident, firing(), true, murphy.Now())

	assert.Equal(t, murphy.IncidentStatus_INCIDENT_STATUS_TRIGGERED, decision.Incident.GetStatus())
	assert.Equal(t, int32(3), decision.Incident.GetVersion())
	assert.False(t, decision.CancelTimers)
	assert.Equal(t, []murphy.IncidentVerb{murphy.IncidentVerb_INCIDENT_VERB_UPDATED}, verbs(decision))
	assert.Equal(t, []murphy.TimelineEntryType{murphy.TimelineEntryType_TIMELINE_ENTRY_TYPE_ALERTED}, entries(decision))
}

func TestTheLastResolveResolvesTheIncident(t *testing.T) {
	incident := incidentAt(1, 1)

	decision := incidents.ReceiveAlert(incident, firing(), false, murphy.Now())

	assert.Equal(t, murphy.IncidentStatus_INCIDENT_STATUS_RESOLVED, decision.Incident.GetStatus())
	assert.NotNil(t, decision.Incident.GetResolvedAt())
	assert.True(t, decision.CancelTimers)
	assert.Equal(t, []murphy.IncidentVerb{murphy.IncidentVerb_INCIDENT_VERB_RESOLVED}, verbs(decision))
	assert.Equal(t, murphy.IncidentStatus_INCIDENT_STATUS_TRIGGERED, decision.Announce[0].Previous.GetStatus())
}

func TestATimerMovesTheIncidentToTheNextLevel(t *testing.T) {
	incident := incidentAt(1, 1)

	decision := incidents.ExpireTimer(incident, &murphy.Timer{EscalationLevel: 2, EscalationRound: 1}, twoLevels(), murphy.Now())

	assert.Equal(t, []int32{2, 1}, []int32{decision.Incident.GetEscalationLevel(), decision.Incident.GetEscalationRound()})
	assert.Empty(t, decision.Timeline, "whom the level reached is recorded when its page comes back")
	assert.Equal(t, []murphy.IncidentVerb{murphy.IncidentVerb_INCIDENT_VERB_ESCALATED}, verbs(decision))
	assert.Equal(t, []int32{1, 2}, []int32{decision.NextTimer.GetEscalationLevel(), decision.NextTimer.GetEscalationRound()}, "after the last level, back to the first")
	assert.WithinDuration(t, time.Now().Add(10*time.Minute), decision.NextTimer.GetDueAt().AsTime(), time.Second)
}

func TestEscalationIsExhaustedAfterItsRepeats(t *testing.T) {
	incident := incidentAt(2, 2)

	decision := incidents.ExpireTimer(incident, &murphy.Timer{}, twoLevels(), murphy.Now())

	assert.True(t, decision.Incident.GetEscalationExhausted())
	assert.True(t, decision.CancelTimers)
	assert.Nil(t, decision.NextTimer)
	assert.Equal(t, []murphy.TimelineEntryType{murphy.TimelineEntryType_TIMELINE_ENTRY_TYPE_ESCALATION_EXHAUSTED}, entries(decision))
}

func TestATimerThatIsNoLongerNeededDecidesNothing(t *testing.T) {
	acknowledged := incidentAt(1, 1)
	acknowledged.Status = murphy.IncidentStatus_INCIDENT_STATUS_ACKNOWLEDGED
	for name, test := range map[string]struct {
		incident *murphy.Incident
		timer    *murphy.Timer
	}{
		"incident acknowledged": {acknowledged, &murphy.Timer{EscalationLevel: 2, EscalationRound: 1}},
		"step already taken":    {incidentAt(2, 1), &murphy.Timer{EscalationLevel: 2, EscalationRound: 1}},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Nil(t, incidents.ExpireTimer(test.incident, test.timer, twoLevels(), murphy.Now()))
		})
	}
}

func page(level, round int32, reached ...*murphy.PageRecipient) *murphy.Page {
	return &murphy.Page{EscalationLevel: level, EscalationRound: round, Reached: reached}
}

func TestAPageRecordsWhomItReached(t *testing.T) {
	incident := incidentAt(1, 1)
	via := &murphy.EscalationTarget{Type: murphy.EscalationTargetType_ESCALATION_TARGET_TYPE_SCHEDULE, Id: primary}

	decision := incidents.RecordPage(incident, page(1, 1, &murphy.PageRecipient{UserId: ana, Via: via, ContactMethods: 2}), twoLevels(), murphy.Now())

	assert.Nil(t, decision.Incident, "the incident stays on its level")
	require.Len(t, decision.Timeline, 1)
	assert.Equal(t, murphy.TimelineEntryType_TIMELINE_ENTRY_TYPE_ESCALATED, decision.Timeline[0].GetType())
	assert.Equal(t, ana, decision.Timeline[0].GetEscalation().GetPaged()[0].GetUserId())
	assert.Equal(t, primary, decision.Timeline[0].GetEscalation().GetPaged()[0].GetVia().GetId())
}

func TestAPageIsOnTheTimelineWhenItWasCreated(t *testing.T) {
	created := page(1, 1, &murphy.PageRecipient{UserId: ana})
	created.CreatedAt = murphy.Now()

	decision := incidents.RecordPage(incidentAt(1, 1), created, twoLevels(), timestamppb.New(created.GetCreatedAt().AsTime().Add(time.Second)))

	assert.Equal(t, created.GetCreatedAt().AsTime(), decision.Timeline[0].GetAt().AsTime(), "the level comes before what its notifications did")
	assert.Equal(t, created.GetCreatedAt().AsTime(), decision.Timeline[1].GetAt().AsTime())
}

func TestSomeoneReachedWithoutContactMethodsIsOnTheTimeline(t *testing.T) {
	decision := incidents.RecordPage(incidentAt(1, 1), page(1, 1, &murphy.PageRecipient{UserId: ana}), twoLevels(), murphy.Now())

	assert.Equal(t, []murphy.TimelineEntryType{murphy.TimelineEntryType_TIMELINE_ENTRY_TYPE_ESCALATED, murphy.TimelineEntryType_TIMELINE_ENTRY_TYPE_NOTIFICATION_FAILED}, entries(decision))
	assert.Equal(t, ana, decision.Timeline[1].GetNotification().GetUserId())
}

func TestAPageThatReachedNobodyMovesOnAtOnce(t *testing.T) {
	empty := page(1, 1)
	empty.Unreached = []*murphy.EscalationTarget{{Type: murphy.EscalationTargetType_ESCALATION_TARGET_TYPE_SCHEDULE, Id: primary}}

	decision := incidents.RecordPage(incidentAt(1, 1), empty, twoLevels(), murphy.Now())

	assert.Equal(t, []murphy.TimelineEntryType{murphy.TimelineEntryType_TIMELINE_ENTRY_TYPE_ESCALATION_SKIPPED}, entries(decision))
	assert.Equal(t, primary, decision.Timeline[0].GetEscalation().GetReachedNobody()[0].GetId())
	assert.Equal(t, int32(2), decision.Incident.GetEscalationLevel())
	assert.True(t, decision.CancelTimers, "the timer for the level it moved to is no longer needed")
	assert.Equal(t, []murphy.IncidentVerb{murphy.IncidentVerb_INCIDENT_VERB_ESCALATED}, verbs(decision))
	assert.Equal(t, int32(1), decision.NextTimer.GetEscalationLevel())
}

func TestAnEmptyPageForALevelAlreadyLeftOnlyRecordsIt(t *testing.T) {
	decision := incidents.RecordPage(incidentAt(2, 1), page(1, 1), twoLevels(), murphy.Now())

	assert.Nil(t, decision.Incident)
	assert.Equal(t, []murphy.TimelineEntryType{murphy.TimelineEntryType_TIMELINE_ENTRY_TYPE_ESCALATION_SKIPPED}, entries(decision))
}

func TestAcknowledgingCancelsTimersAndAnnouncesIt(t *testing.T) {
	decision := incidents.AcknowledgeIncident(incidentAt(1, 1), ana, murphy.Now())

	assert.Equal(t, murphy.IncidentStatus_INCIDENT_STATUS_ACKNOWLEDGED, decision.Incident.GetStatus())
	assert.NotNil(t, decision.Incident.GetAcknowledgedAt())
	assert.True(t, decision.CancelTimers)
	assert.Equal(t, []murphy.IncidentVerb{murphy.IncidentVerb_INCIDENT_VERB_ACKNOWLEDGED}, verbs(decision))
	assert.Equal(t, murphy.IncidentStatus_INCIDENT_STATUS_TRIGGERED, decision.Announce[0].Previous.GetStatus())
}

func TestAcknowledgingOrResolvingTwiceDecidesNothing(t *testing.T) {
	acknowledged := incidentAt(1, 1)
	acknowledged.Status = murphy.IncidentStatus_INCIDENT_STATUS_ACKNOWLEDGED
	resolved := incidentAt(1, 1)
	resolved.Status = murphy.IncidentStatus_INCIDENT_STATUS_RESOLVED

	assert.Nil(t, incidents.AcknowledgeIncident(acknowledged, ana, murphy.Now()))
	assert.Nil(t, incidents.ResolveIncident(resolved, ana, murphy.Now()))
}

func TestWhoAcknowledgesOrResolvesGoesOnTheTimeline(t *testing.T) {
	acknowledged := incidents.AcknowledgeIncident(incidentAt(1, 1), ana, murphy.Now())
	resolved := incidents.ResolveIncident(incidentAt(1, 1), ana, murphy.Now())

	assert.Equal(t, ana, acknowledged.Timeline[0].GetActorUserId())
	assert.Equal(t, ana, resolved.Timeline[0].GetActorUserId())
}

func TestANotificationOutcomeGoesOnTheTimeline(t *testing.T) {
	notification := &murphy.Notification{
		Id: murphy.NewID(), Reason: murphy.NotificationReason_NOTIFICATION_REASON_PAGED, Incident: &murphy.Incident{Id: murphy.NewID()}, UserId: ana,
		ContactMethod: &murphy.ContactMethod{Id: murphy.NewID(), Type: murphy.ContactMethodType_CONTACT_METHOD_TYPE_WEBHOOK}, Failure: "the receiver answered 410",
	}

	decision := incidents.NotificationOutcome(notification, true)

	assert.Nil(t, decision.Incident, "an outcome changes only the timeline")
	require.Len(t, decision.Timeline, 1)
	entry := decision.Timeline[0]
	assert.Equal(t, murphy.TimelineEntryType_TIMELINE_ENTRY_TYPE_NOTIFICATION_FAILED, entry.GetType())
	assert.Equal(t, "Page by webhook failed: the receiver answered 410", entry.GetSummary())
	assert.Equal(t, notification.GetContactMethod().GetId(), entry.GetNotification().GetContactMethodId())
}

func TestTellingSomeoneItWasAcknowledgedOrResolvedStaysOffTheTimelineUnlessItFails(t *testing.T) {
	notification := func(reason murphy.NotificationReason) *murphy.Notification {
		return &murphy.Notification{
			Id: murphy.NewID(), Reason: reason, Incident: &murphy.Incident{Id: murphy.NewID()}, UserId: ana,
			ContactMethod: &murphy.ContactMethod{Id: murphy.NewID(), Type: murphy.ContactMethodType_CONTACT_METHOD_TYPE_WEBHOOK}, Failure: "the receiver answered 503",
		}
	}

	acknowledged := incidents.NotificationOutcome(notification(murphy.NotificationReason_NOTIFICATION_REASON_ACKNOWLEDGED), false)
	resolved := incidents.NotificationOutcome(notification(murphy.NotificationReason_NOTIFICATION_REASON_RESOLVED), false)
	failed := incidents.NotificationOutcome(notification(murphy.NotificationReason_NOTIFICATION_REASON_RESOLVED), true)
	paged := incidents.NotificationOutcome(notification(murphy.NotificationReason_NOTIFICATION_REASON_PAGED), false)

	assert.Nil(t, acknowledged)
	assert.Nil(t, resolved)
	assert.Equal(t, []murphy.TimelineEntryType{murphy.TimelineEntryType_TIMELINE_ENTRY_TYPE_NOTIFICATION_FAILED}, entries(failed), "a follow-up that couldn't be delivered is worth knowing")
	assert.Equal(t, []murphy.TimelineEntryType{murphy.TimelineEntryType_TIMELINE_ENTRY_TYPE_NOTIFIED}, entries(paged), "who a page reached always stays")
}

func named(userID string) string {
	return map[string]string{"ana": "Ana", "bo": "Bo", "cy": "Cy"}[userID]
}

func notifiedEntry(kind murphy.TimelineEntryType, reason murphy.NotificationReason, channel murphy.ContactMethodType, failure string) *murphy.TimelineEntry {
	return &murphy.TimelineEntry{Type: kind, Summary: "stored", Notification: &murphy.TimelineNotification{Reason: reason, UserId: "ana", Channel: channel, Failure: failure}}
}

func TestANotificationSaysWhoWasReachedAndHow(t *testing.T) {
	paged := notifiedEntry(murphy.TimelineEntryType_TIMELINE_ENTRY_TYPE_NOTIFIED, murphy.NotificationReason_NOTIFICATION_REASON_PAGED, murphy.ContactMethodType_CONTACT_METHOD_TYPE_WEBHOOK, "")
	told := notifiedEntry(murphy.TimelineEntryType_TIMELINE_ENTRY_TYPE_NOTIFIED, murphy.NotificationReason_NOTIFICATION_REASON_ACKNOWLEDGED, murphy.ContactMethodType_CONTACT_METHOD_TYPE_PUSH, "")
	failed := notifiedEntry(murphy.TimelineEntryType_TIMELINE_ENTRY_TYPE_NOTIFICATION_FAILED, murphy.NotificationReason_NOTIFICATION_REASON_PAGED, murphy.ContactMethodType_CONTACT_METHOD_TYPE_PUSH, "the phone is no longer registered for pushes")
	unreachable := notifiedEntry(murphy.TimelineEntryType_TIMELINE_ENTRY_TYPE_NOTIFICATION_FAILED, murphy.NotificationReason_NOTIFICATION_REASON_PAGED, murphy.ContactMethodType_CONTACT_METHOD_TYPE_UNSPECIFIED, "the user has no working contact methods")

	assert.Equal(t, "Paged Ana by webhook", incidents.Summary(paged, named))
	assert.Equal(t, "Told Ana by push that it was acknowledged", incidents.Summary(told, named))
	assert.Equal(t, "Couldn't page Ana by push: the phone is no longer registered for pushes", incidents.Summary(failed, named))
	assert.Equal(t, "Couldn't page Ana: the user has no working contact methods", incidents.Summary(unreachable, named))
}

func TestALevelNamesEveryoneItPagedOnce(t *testing.T) {
	escalated := &murphy.TimelineEntry{Type: murphy.TimelineEntryType_TIMELINE_ENTRY_TYPE_ESCALATED, Escalation: &murphy.TimelineEscalation{
		Level: 2, Round: 2, Paged: []*murphy.TimelinePage{{UserId: "ana"}, {UserId: "bo"}, {UserId: "ana"}, {UserId: "cy"}},
	}}

	assert.Equal(t, "Level 2 paged Ana, Bo and Cy, round 2", incidents.Summary(escalated, named))
}

func TestWhoActedAndSomeoneDeletedAreNamed(t *testing.T) {
	acknowledged := &murphy.TimelineEntry{Type: murphy.TimelineEntryType_TIMELINE_ENTRY_TYPE_ACKNOWLEDGED, ActorUserId: "bo"}
	resolved := &murphy.TimelineEntry{Type: murphy.TimelineEntryType_TIMELINE_ENTRY_TYPE_RESOLVED, ActorUserId: "someone-deleted"}

	assert.Equal(t, "Acknowledged by Bo", incidents.Summary(acknowledged, named))
	assert.Equal(t, "Resolved by someone since deleted", incidents.Summary(resolved, named))
}

func TestEntriesWithoutPeopleKeepTheirStoredSummary(t *testing.T) {
	exhausted := &murphy.TimelineEntry{Type: murphy.TimelineEntryType_TIMELINE_ENTRY_TYPE_ESCALATION_EXHAUSTED, Summary: "Escalation is exhausted: every level and repeat was paged"}

	assert.Equal(t, "Escalation is exhausted: every level and repeat was paged", incidents.Summary(exhausted, named))
}
