package incidents_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/services/incidents"
)

type fixture struct {
	service     *incidents.Service
	repo        *MockRepository
	policies    *MockPolicies
	integration *murphy.Integration
	policy      *murphy.EscalationPolicy
	onCall      map[string]string
	active      map[string]bool
	methods     map[string][]*murphy.ContactMethod
	timeline    []*murphy.TimelineEntry
	recorded    *incidents.Change
}

const (
	schedule = "0195f3a0-0000-7000-8000-00000000000a"
	ana      = "0195f3a0-0000-7000-8000-0000000000a1"
	bo       = "0195f3a0-0000-7000-8000-0000000000b1"
)

func setup(t *testing.T) *fixture {
	t.Helper()
	repo, catalog := NewMockRepository(t), NewMockCatalog(t)
	policies, oncall, people := NewMockPolicies(t), NewMockOnCall(t), NewMockPeople(t)
	f := &fixture{
		service:  incidents.New(repo, catalog, policies, oncall, people),
		repo:     repo,
		policies: policies,
		policy: &murphy.EscalationPolicy{
			Id: murphy.NewID(), Version: 3, RepeatCount: 1,
			Levels: []*murphy.EscalationLevel{
				{Delay: durationpb.New(5 * time.Minute), Targets: []*murphy.EscalationTarget{{Type: murphy.EscalationTargetType_ESCALATION_TARGET_TYPE_SCHEDULE, Id: schedule}}},
				{Delay: durationpb.New(10 * time.Minute), Targets: []*murphy.EscalationTarget{{Type: murphy.EscalationTargetType_ESCALATION_TARGET_TYPE_USER, Id: bo}}},
			},
		},
		onCall: map[string]string{schedule: ana},
		active: map[string]bool{ana: true, bo: true},
		methods: map[string][]*murphy.ContactMethod{
			ana: {webhookOf(ana)},
			bo:  {webhookOf(bo)},
		},
		integration: &murphy.Integration{
			Id:         murphy.NewID(),
			ServiceId:  murphy.NewID(),
			KeyVersion: 2,
			Type:       murphy.IntegrationType_INTEGRATION_TYPE_GENERIC,
			Status:     murphy.IntegrationStatus_INTEGRATION_STATUS_ACTIVE,
		},
	}
	catalog.EXPECT().GetIntegrationAndService(mock.Anything, mock.Anything).RunAndReturn(func(context.Context, string) (*murphy.Integration, *murphy.Service, error) {
		return f.integration, &murphy.Service{
			Id:                 f.integration.GetServiceId(),
			Urgency:            murphy.Urgency_URGENCY_LOW,
			Status:             murphy.ServiceStatus_SERVICE_STATUS_ACTIVE,
			EscalationPolicyId: f.policy.GetId(),
		}, nil
	}).Maybe()
	policies.EXPECT().CurrentVersion(mock.Anything, mock.Anything).RunAndReturn(func(context.Context, string) (int32, error) {
		return f.policy.GetVersion(), nil
	}).Maybe()
	policies.EXPECT().GetPolicyVersion(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(context.Context, string, int32) (*murphy.EscalationPolicy, error) {
		return f.policy, nil
	}).Maybe()
	oncall.EXPECT().WhoIsOnCall(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, ids []string, _ time.Time) (map[string]string, error) {
		found := map[string]string{}
		for _, id := range ids {
			if user, ok := f.onCall[id]; ok {
				found[id] = user
			}
		}
		return found, nil
	}).Maybe()
	people.EXPECT().ActiveUsers(mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, ids []string) (map[string]bool, error) {
		found := map[string]bool{}
		for _, id := range ids {
			found[id] = f.active[id]
		}
		return found, nil
	}).Maybe()
	people.EXPECT().ContactMethodsOfUsers(mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, ids []string) (map[string][]*murphy.ContactMethod, error) {
		found := map[string][]*murphy.ContactMethod{}
		for _, id := range ids {
			if methods, ok := f.methods[id]; ok {
				found[id] = methods
			}
		}
		return found, nil
	}).Maybe()
	repo.EXPECT().ListTimeline(mock.Anything, mock.Anything).RunAndReturn(func(context.Context, string) ([]*murphy.TimelineEntry, error) {
		return f.timeline, nil
	}).Maybe()
	repo.EXPECT().Record(mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, outcome *incidents.Change) (bool, error) {
		f.recorded = outcome
		return true, nil
	}).Maybe()
	return f
}

func webhookOf(user string) *murphy.ContactMethod {
	return &murphy.ContactMethod{Id: murphy.NewID(), UserId: user, Type: murphy.ContactMethodType_CONTACT_METHOD_TYPE_WEBHOOK, Address: "https://example.com/" + user}
}

func (f *fixture) alert(status murphy.AlertStatus) *murphy.Alert {
	return &murphy.Alert{
		Id:            murphy.NewID(),
		IntegrationId: f.integration.GetId(),
		ServiceId:     f.integration.GetServiceId(),
		DedupKey:      "disk-full",
		Status:        status,
		Summary:       "Disk almost full",
		KeyVersion:    f.integration.GetKeyVersion(),
		ReceivedAt:    murphy.Now(),
	}
}

func (f *fixture) open(incident *murphy.Incident) {
	f.repo.EXPECT().FindOpenIncident(mock.Anything, mock.Anything, mock.Anything).Return(incident, nil)
}

func (f *fixture) noneOpen() {
	f.repo.EXPECT().FindOpenIncident(mock.Anything, mock.Anything, mock.Anything).Return(nil, &murphy.NotFoundError{Entity: "incident"})
}

func TestAFiringAlertOpensAnIncident(t *testing.T) {
	f := setup(t)
	f.noneOpen()

	require.NoError(t, f.service.AlertReceived(t.Context(), "message-1", f.alert(murphy.AlertStatus_ALERT_STATUS_FIRING)))

	opened := f.recorded.Created
	require.NotNil(t, opened)
	assert.Equal(t, "message-1", f.recorded.MessageID)
	assert.Equal(t, murphy.IncidentStatus_INCIDENT_STATUS_TRIGGERED, opened.GetStatus())
	assert.Equal(t, murphy.Urgency_URGENCY_LOW, opened.GetUrgency())
	assert.Equal(t, "Disk almost full", opened.GetTitle())
	assert.Equal(t, opened.GetId(), f.recorded.IncidentID)
	require.Len(t, f.recorded.Timeline, 1)
	assert.Equal(t, murphy.TimelineEntryType_TIMELINE_ENTRY_TYPE_TRIGGERED, f.recorded.Timeline[0].GetType())
	assert.Equal(t, f.policy.GetId(), opened.GetEscalationPolicyId())
	assert.Equal(t, int32(3), opened.GetEscalationPolicyVersion())

	messages := f.messages(t)
	require.Len(t, messages, 2)
	assert.Equal(t, murphy.IncidentVerb_INCIDENT_VERB_CREATED, messages[0].envelope.GetIncident().GetVerb())
	assert.Equal(t, opened.GetId(), messages[0].envelope.GetIncident().GetCurrent().GetId())
	timer := messages[1].envelope.GetTimer().GetCurrent()
	assert.Equal(t, incidents.MessageTimer, messages[1].Kind)
	assert.Equal(t, int32(1), timer.GetEscalationLevel())
	assert.Equal(t, int32(1), timer.GetEscalationRound())
	assert.WithinDuration(t, time.Now(), messages[1].DueAt, time.Second)
}

func TestAFiringAlertJoinsTheOpenIncident(t *testing.T) {
	f := setup(t)
	open := &murphy.Incident{Id: murphy.NewID(), Status: murphy.IncidentStatus_INCIDENT_STATUS_ACKNOWLEDGED}
	f.open(open)

	require.NoError(t, f.service.AlertReceived(t.Context(), "message-1", f.alert(murphy.AlertStatus_ALERT_STATUS_FIRING)))

	assert.Nil(t, f.recorded.Created)
	assert.Equal(t, open, f.recorded.Updated)
	assert.Equal(t, murphy.IncidentStatus_INCIDENT_STATUS_ACKNOWLEDGED, open.GetStatus())
	assert.Equal(t, open.GetId(), f.recorded.IncidentID)
	require.Len(t, f.recorded.Timeline, 1)
	assert.Equal(t, murphy.TimelineEntryType_TIMELINE_ENTRY_TYPE_ALERTED, f.recorded.Timeline[0].GetType())
}

func TestAResolvedAlertResolvesTheOpenIncident(t *testing.T) {
	f := setup(t)
	open := &murphy.Incident{Id: murphy.NewID(), Status: murphy.IncidentStatus_INCIDENT_STATUS_TRIGGERED, Version: 3}
	f.open(open)
	f.repo.EXPECT().LatestFiringAt(mock.Anything, open.GetId()).Return(time.Now().Add(-time.Minute), nil)

	require.NoError(t, f.service.AlertReceived(t.Context(), "message-1", f.alert(murphy.AlertStatus_ALERT_STATUS_RESOLVED)))

	resolved := f.recorded.Updated
	require.NotNil(t, resolved)
	assert.Equal(t, murphy.IncidentStatus_INCIDENT_STATUS_RESOLVED, resolved.GetStatus())
	assert.NotNil(t, resolved.GetResolvedAt())
	assert.Equal(t, int32(4), resolved.GetVersion())
	assert.True(t, f.recorded.CancelTimers)
	assert.Equal(t, murphy.IncidentVerb_INCIDENT_VERB_RESOLVED, f.messages(t)[0].envelope.GetIncident().GetVerb())
	require.Len(t, f.recorded.Timeline, 1)
	assert.Equal(t, murphy.TimelineEntryType_TIMELINE_ENTRY_TYPE_RESOLVED, f.recorded.Timeline[0].GetType())
}

func TestAResolvedAlertNotClearlyNewerThanTheLatestFiringKeepsTheIncidentOpen(t *testing.T) {
	f := setup(t)
	open := &murphy.Incident{Id: murphy.NewID(), Status: murphy.IncidentStatus_INCIDENT_STATUS_TRIGGERED, Version: 3}
	f.open(open)
	alert := f.alert(murphy.AlertStatus_ALERT_STATUS_RESOLVED)
	f.repo.EXPECT().LatestFiringAt(mock.Anything, open.GetId()).Return(alert.GetReceivedAt().AsTime().Add(-500*time.Millisecond), nil)

	require.NoError(t, f.service.AlertReceived(t.Context(), "message-1", alert))

	assert.Equal(t, open, f.recorded.Updated)
	assert.Equal(t, murphy.IncidentStatus_INCIDENT_STATUS_TRIGGERED, open.GetStatus())
	require.Len(t, f.recorded.Timeline, 1)
	assert.Equal(t, murphy.TimelineEntryType_TIMELINE_ENTRY_TYPE_ALERTED, f.recorded.Timeline[0].GetType())
}

func TestAResolveForOneInstanceKeepsTheIncidentOpenWhileAnotherFires(t *testing.T) {
	f := setup(t)
	open := &murphy.Incident{Id: murphy.NewID(), Status: murphy.IncidentStatus_INCIDENT_STATUS_TRIGGERED, Version: 2}
	f.open(open)
	f.repo.EXPECT().OpenInstances(mock.Anything, open.GetId()).Return([]string{"a", "b"}, nil)
	alert := f.alert(murphy.AlertStatus_ALERT_STATUS_RESOLVED)
	alert.Instance = "a"

	require.NoError(t, f.service.AlertReceived(t.Context(), "message-1", alert))

	assert.Equal(t, murphy.IncidentStatus_INCIDENT_STATUS_TRIGGERED, f.recorded.Updated.GetStatus())
	assert.Equal(t, murphy.TimelineEntryType_TIMELINE_ENTRY_TYPE_ALERTED, f.recorded.Timeline[0].GetType())
}

func TestTheResolveForTheLastInstanceResolvesTheIncident(t *testing.T) {
	f := setup(t)
	open := &murphy.Incident{Id: murphy.NewID(), Status: murphy.IncidentStatus_INCIDENT_STATUS_TRIGGERED, Version: 2}
	f.open(open)
	f.repo.EXPECT().OpenInstances(mock.Anything, open.GetId()).Return([]string{"b"}, nil)
	alert := f.alert(murphy.AlertStatus_ALERT_STATUS_RESOLVED)
	alert.Instance = "b"

	require.NoError(t, f.service.AlertReceived(t.Context(), "message-1", alert))

	assert.Equal(t, murphy.IncidentStatus_INCIDENT_STATUS_RESOLVED, f.recorded.Updated.GetStatus())
	assert.Equal(t, murphy.TimelineEntryType_TIMELINE_ENTRY_TYPE_RESOLVED, f.recorded.Timeline[0].GetType())
}

func TestARecentResolvedAlertWithoutAnOpenIncidentIsRetried(t *testing.T) {
	f := setup(t)
	f.noneOpen()

	err := f.service.AlertReceived(t.Context(), "message-1", f.alert(murphy.AlertStatus_ALERT_STATUS_RESOLVED))

	require.Error(t, err)
	assert.NotErrorIs(t, err, murphy.ErrInvalid, "a retry, not a rejection")
	assert.Nil(t, f.recorded)
}

func TestAnOldResolvedAlertWithoutAnOpenIncidentIsOnlyRecorded(t *testing.T) {
	f := setup(t)
	f.noneOpen()
	alert := f.alert(murphy.AlertStatus_ALERT_STATUS_RESOLVED)
	alert.ReceivedAt = timestamppb.New(time.Now().Add(-2 * time.Hour))

	require.NoError(t, f.service.AlertReceived(t.Context(), "message-1", alert))

	assert.Empty(t, f.recorded.IncidentID)
	assert.Nil(t, f.recorded.Created)
	assert.Empty(t, f.recorded.Timeline)
}

func TestThePreviousKeyWorksDuringTheGracePeriod(t *testing.T) {
	f := setup(t)
	f.noneOpen()
	f.integration.PreviousKeyValidUntil = timestamppb.New(time.Now().Add(time.Hour))
	alert := f.alert(murphy.AlertStatus_ALERT_STATUS_FIRING)
	alert.KeyVersion = 1

	require.NoError(t, f.service.AlertReceived(t.Context(), "message-1", alert))
	assert.NotNil(t, f.recorded.Created)
}

func TestKeysTheIntegrationNoLongerAcceptsAreRefused(t *testing.T) {
	for name, test := range map[string]func(f *fixture, alert *murphy.Alert){
		"previous version after the grace period": func(f *fixture, alert *murphy.Alert) {
			alert.KeyVersion = 1
			f.integration.PreviousKeyValidUntil = timestamppb.New(time.Now().Add(-time.Minute))
		},
		"older than the previous version": func(f *fixture, alert *murphy.Alert) {
			alert.KeyVersion = 0
			f.integration.PreviousKeyValidUntil = timestamppb.New(time.Now().Add(time.Hour))
		},
		"disabled integration": func(f *fixture, _ *murphy.Alert) {
			f.integration.Status = murphy.IntegrationStatus_INTEGRATION_STATUS_DISABLED
		},
		"key for another service": func(_ *fixture, alert *murphy.Alert) {
			alert.ServiceId = murphy.NewID()
		},
		"another format than the integration's type": func(_ *fixture, alert *murphy.Alert) {
			alert.Source = murphy.IntegrationType_INTEGRATION_TYPE_ALERTMANAGER
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := setup(t)
			alert := f.alert(murphy.AlertStatus_ALERT_STATUS_FIRING)
			test(f, alert)

			err := f.service.AlertReceived(t.Context(), "message-1", alert)

			assert.ErrorIs(t, err, murphy.ErrInvalid)
			assert.Nil(t, f.recorded)
		})
	}
}

type message struct {
	*incidents.ScheduledMessage
	envelope *murphy.Envelope
}

func (f *fixture) messages(t *testing.T) []message {
	t.Helper()
	if f.recorded.Messages == nil {
		return nil
	}
	list, err := f.recorded.Messages()
	require.NoError(t, err)
	out := make([]message, len(list))
	for i, scheduled := range list {
		envelope := &murphy.Envelope{}
		require.NoError(t, proto.Unmarshal(scheduled.Envelope, envelope))
		out[i] = message{ScheduledMessage: scheduled, envelope: envelope}
	}
	return out
}

func (f *fixture) triggered(level, round int32) *murphy.Incident {
	incident := &murphy.Incident{
		Id: murphy.NewID(), Status: murphy.IncidentStatus_INCIDENT_STATUS_TRIGGERED, Version: 2,
		EscalationPolicyId: f.policy.GetId(), EscalationPolicyVersion: 3, EscalationLevel: level, EscalationRound: round,
	}
	f.repo.EXPECT().GetIncident(mock.Anything, incident.GetId()).Return(incident, nil).Maybe()
	return incident
}

func timer(incident *murphy.Incident, level, round int32) *murphy.Timer {
	return &murphy.Timer{Id: murphy.NewID(), IncidentId: incident.GetId(), EscalationLevel: level, EscalationRound: round}
}

func TestATimerPagesItsLevelAndSchedulesTheNext(t *testing.T) {
	f := setup(t)
	incident := f.triggered(0, 1)

	require.NoError(t, f.service.TimerExpired(t.Context(), "message-1", timer(incident, 1, 1)))

	escalated := f.recorded.Updated
	assert.Equal(t, int32(1), escalated.GetEscalationLevel())
	assert.Equal(t, int32(1), escalated.GetEscalationRound())
	assert.Equal(t, int32(3), escalated.GetVersion())
	require.Len(t, f.recorded.Timeline, 1)
	assert.Equal(t, murphy.TimelineEntryType_TIMELINE_ENTRY_TYPE_ESCALATED, f.recorded.Timeline[0].GetType())
	paged := f.recorded.Timeline[0].GetEscalation().GetPaged()
	require.Len(t, paged, 1)
	assert.Equal(t, ana, paged[0].GetUserId())
	assert.Equal(t, schedule, paged[0].GetVia().GetId(), "reached through the schedule she is on call for")

	messages := f.messages(t)
	require.Len(t, messages, 3)
	assert.Equal(t, murphy.IncidentVerb_INCIDENT_VERB_ESCALATED, messages[0].envelope.GetIncident().GetVerb())
	assert.Equal(t, murphy.NotificationVerb_NOTIFICATION_VERB_CREATED, messages[1].envelope.GetNotification().GetVerb())
	next := messages[2].envelope.GetTimer().GetCurrent()
	assert.Equal(t, int32(2), next.GetEscalationLevel())
	assert.Equal(t, int32(1), next.GetEscalationRound())
	assert.WithinDuration(t, time.Now().Add(5*time.Minute), messages[2].DueAt, time.Second)
}

func notifications(messages []message) []*murphy.Notification {
	var out []*murphy.Notification
	for _, m := range messages {
		if n := m.envelope.GetNotification().GetCurrent(); n != nil {
			out = append(out, n)
		}
	}
	return out
}

func TestAPageNotifiesEveryContactMethodOfEveryonePaged(t *testing.T) {
	f := setup(t)
	email := &murphy.ContactMethod{Id: murphy.NewID(), UserId: ana, Type: murphy.ContactMethodType_CONTACT_METHOD_TYPE_EMAIL, Address: "ana@example.com"}
	f.methods[ana] = append(f.methods[ana], email)
	incident := f.triggered(0, 1)

	require.NoError(t, f.service.TimerExpired(t.Context(), "message-1", timer(incident, 1, 1)))

	sent := notifications(f.messages(t))
	require.Len(t, sent, 2)
	for i, method := range []*murphy.ContactMethod{f.methods[ana][0], email} {
		assert.Equal(t, method.GetId(), sent[i].GetContactMethod().GetId())
		assert.Equal(t, ana, sent[i].GetUserId())
		assert.Equal(t, murphy.NotificationReason_NOTIFICATION_REASON_PAGED, sent[i].GetReason())
		assert.Equal(t, int32(1), sent[i].GetEscalationLevel())
		assert.Equal(t, schedule, sent[i].GetVia().GetId())
		assert.Equal(t, incident.GetId(), sent[i].GetIncident().GetId())
	}
}

func TestSomeonePagedWithoutContactMethodsIsRecordedAsNotReached(t *testing.T) {
	f := setup(t)
	delete(f.methods, ana)
	incident := f.triggered(0, 1)

	require.NoError(t, f.service.TimerExpired(t.Context(), "message-1", timer(incident, 1, 1)))

	assert.Empty(t, notifications(f.messages(t)))
	require.Len(t, f.recorded.Timeline, 2)
	failed := f.recorded.Timeline[1]
	assert.Equal(t, murphy.TimelineEntryType_TIMELINE_ENTRY_TYPE_NOTIFICATION_FAILED, failed.GetType())
	assert.Equal(t, ana, failed.GetNotification().GetUserId())
}

func TestANotificationOutcomeIsRecordedOnTheTimeline(t *testing.T) {
	f := setup(t)
	notification := &murphy.Notification{
		Id: murphy.NewID(), Reason: murphy.NotificationReason_NOTIFICATION_REASON_PAGED, Incident: &murphy.Incident{Id: murphy.NewID()},
		UserId: ana, ContactMethod: webhookOf(ana), Failure: "the receiver answered 410",
	}

	require.NoError(t, f.service.NotificationFailed(t.Context(), "message-1", notification))

	assert.Equal(t, "message-1", f.recorded.MessageID)
	require.Len(t, f.recorded.Timeline, 1)
	entry := f.recorded.Timeline[0]
	assert.Equal(t, murphy.TimelineEntryType_TIMELINE_ENTRY_TYPE_NOTIFICATION_FAILED, entry.GetType())
	assert.Equal(t, notification.GetIncident().GetId(), entry.GetIncidentId())
	assert.Equal(t, "Page by webhook failed: the receiver answered 410", entry.GetSummary())
	assert.Equal(t, notification.GetContactMethod().GetId(), entry.GetNotification().GetContactMethodId())
	assert.Nil(t, f.recorded.Updated, "an outcome changes only the timeline")
}

func TestALevelThatReachesNobodyIsSkipped(t *testing.T) {
	f := setup(t)
	delete(f.onCall, schedule)
	incident := f.triggered(0, 1)

	require.NoError(t, f.service.TimerExpired(t.Context(), "message-1", timer(incident, 1, 1)))

	require.Len(t, f.recorded.Timeline, 2)
	assert.Equal(t, murphy.TimelineEntryType_TIMELINE_ENTRY_TYPE_ESCALATION_SKIPPED, f.recorded.Timeline[0].GetType())
	assert.Equal(t, murphy.TimelineEntryType_TIMELINE_ENTRY_TYPE_ESCALATED, f.recorded.Timeline[1].GetType())
	assert.Equal(t, []string{bo}, pagedUsers(f.recorded.Timeline[1]))
	assert.Equal(t, schedule, f.recorded.Timeline[0].GetEscalation().GetReachedNobody()[0].GetId())
	assert.Equal(t, int32(2), f.recorded.Updated.GetEscalationLevel())
	messages := f.messages(t)
	next := messages[len(messages)-1].envelope.GetTimer().GetCurrent()
	assert.Equal(t, int32(1), next.GetEscalationLevel(), "after the last level, back to the first")
	assert.Equal(t, int32(2), next.GetEscalationRound())
}

func TestDeactivatedPeopleAreNotPaged(t *testing.T) {
	f := setup(t)
	f.active[ana] = false
	incident := f.triggered(0, 1)

	require.NoError(t, f.service.TimerExpired(t.Context(), "message-1", timer(incident, 1, 1)))

	assert.Equal(t, murphy.TimelineEntryType_TIMELINE_ENTRY_TYPE_ESCALATION_SKIPPED, f.recorded.Timeline[0].GetType())
	assert.Equal(t, []string{bo}, pagedUsers(f.recorded.Timeline[1]))
}

func TestEscalationIsExhaustedAfterItsRepeats(t *testing.T) {
	f := setup(t)
	incident := f.triggered(2, 2)

	require.NoError(t, f.service.TimerExpired(t.Context(), "message-1", timer(incident, 0, 0)))

	assert.True(t, f.recorded.Updated.GetEscalationExhausted())
	require.Len(t, f.recorded.Timeline, 1)
	assert.Equal(t, murphy.TimelineEntryType_TIMELINE_ENTRY_TYPE_ESCALATION_EXHAUSTED, f.recorded.Timeline[0].GetType())
	messages := f.messages(t)
	require.Len(t, messages, 1, "no timer after exhaustion")
	assert.True(t, messages[0].envelope.GetIncident().GetCurrent().GetEscalationExhausted())
}

func TestEveryLevelReachingNobodyExhaustsEscalation(t *testing.T) {
	f := setup(t)
	delete(f.onCall, schedule)
	f.active[bo] = false
	incident := f.triggered(0, 1)

	require.NoError(t, f.service.TimerExpired(t.Context(), "message-1", timer(incident, 1, 1)))

	assert.True(t, f.recorded.Updated.GetEscalationExhausted())
	require.Len(t, f.recorded.Timeline, 3)
	assert.Equal(t, murphy.TimelineEntryType_TIMELINE_ENTRY_TYPE_ESCALATION_EXHAUSTED, f.recorded.Timeline[2].GetType())
	assert.Len(t, f.messages(t), 1)
}

func TestATimerThatIsNoLongerNeededOnlyMarksItsMessage(t *testing.T) {
	for name, prepare := range map[string]func(f *fixture) (*murphy.Incident, *murphy.Timer){
		"incident acknowledged": func(f *fixture) (*murphy.Incident, *murphy.Timer) {
			incident := f.triggered(1, 1)
			incident.Status = murphy.IncidentStatus_INCIDENT_STATUS_ACKNOWLEDGED
			return incident, timer(incident, 2, 1)
		},
		"step already taken": func(f *fixture) (*murphy.Incident, *murphy.Timer) {
			incident := f.triggered(1, 1)
			return incident, timer(incident, 1, 1)
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := setup(t)
			_, expired := prepare(f)

			require.NoError(t, f.service.TimerExpired(t.Context(), "message-1", expired))

			assert.Equal(t, "message-1", f.recorded.MessageID)
			assert.Nil(t, f.recorded.Updated)
			assert.Empty(t, f.recorded.Timeline)
			assert.Nil(t, f.recorded.Messages)
		})
	}
}

func TestAcknowledgingCancelsTimersAndAnnouncesIt(t *testing.T) {
	f := setup(t)
	incident := f.triggered(1, 1)

	acknowledged, err := f.service.Acknowledge(t.Context(), incident.GetId())

	require.NoError(t, err)
	assert.Equal(t, murphy.IncidentStatus_INCIDENT_STATUS_ACKNOWLEDGED, acknowledged.GetStatus())
	assert.NotNil(t, acknowledged.GetAcknowledgedAt())
	assert.True(t, f.recorded.CancelTimers)
	assert.Empty(t, f.recorded.MessageID)
	event := f.messages(t)[0].envelope.GetIncident()
	assert.Equal(t, murphy.IncidentVerb_INCIDENT_VERB_ACKNOWLEDGED, event.GetVerb())
	assert.Equal(t, murphy.IncidentStatus_INCIDENT_STATUS_TRIGGERED, event.GetPrevious().GetStatus())
}

func TestAcknowledgingTellsEveryonePagedOnEveryContactMethod(t *testing.T) {
	f := setup(t)
	incident := f.triggered(2, 1)
	f.timeline = []*murphy.TimelineEntry{
		{Type: murphy.TimelineEntryType_TIMELINE_ENTRY_TYPE_ESCALATED, Escalation: &murphy.TimelineEscalation{Level: 1, Paged: []*murphy.TimelinePage{{UserId: ana}}}},
		{Type: murphy.TimelineEntryType_TIMELINE_ENTRY_TYPE_ESCALATED, Escalation: &murphy.TimelineEscalation{Level: 2, Paged: []*murphy.TimelinePage{{UserId: bo}, {UserId: ana}}}},
	}

	_, err := f.service.Acknowledge(t.Context(), incident.GetId())

	require.NoError(t, err)
	sent := notifications(f.messages(t))
	require.Len(t, sent, 2, "once per contact method, however often someone was paged")
	var methods []string
	for _, notification := range sent {
		assert.Equal(t, murphy.NotificationReason_NOTIFICATION_REASON_ACKNOWLEDGED, notification.GetReason())
		assert.Equal(t, murphy.IncidentStatus_INCIDENT_STATUS_ACKNOWLEDGED, notification.GetIncident().GetStatus())
		methods = append(methods, notification.GetContactMethod().GetId())
	}
	assert.ElementsMatch(t, []string{f.methods[ana][0].GetId(), f.methods[bo][0].GetId()}, methods)
}

func TestAcknowledgingTwiceChangesNothing(t *testing.T) {
	f := setup(t)
	incident := f.triggered(1, 1)
	incident.Status = murphy.IncidentStatus_INCIDENT_STATUS_ACKNOWLEDGED

	got, err := f.service.Acknowledge(t.Context(), incident.GetId())

	require.NoError(t, err)
	assert.Equal(t, murphy.IncidentStatus_INCIDENT_STATUS_ACKNOWLEDGED, got.GetStatus())
	assert.Nil(t, f.recorded)
}

func TestAcknowledgingRetriesAfterAConcurrentChange(t *testing.T) {
	f := setup(t)
	repo := NewMockRepository(t)
	f.service = incidents.New(repo, nil, nil, nil, nil)
	id := murphy.NewID()
	repo.EXPECT().GetIncident(mock.Anything, id).RunAndReturn(func(context.Context, string) (*murphy.Incident, error) {
		return &murphy.Incident{Id: id, Status: murphy.IncidentStatus_INCIDENT_STATUS_TRIGGERED, Version: 2}, nil
	}).Times(2)
	repo.EXPECT().ListTimeline(mock.Anything, id).Return(nil, nil)
	repo.EXPECT().Record(mock.Anything, mock.Anything).Return(false, murphy.ErrConflict).Once()
	repo.EXPECT().Record(mock.Anything, mock.Anything).Return(true, nil).Once()

	acknowledged, err := f.service.Acknowledge(t.Context(), id)

	require.NoError(t, err)
	assert.Equal(t, murphy.IncidentStatus_INCIDENT_STATUS_ACKNOWLEDGED, acknowledged.GetStatus())
}

func pagedUsers(entry *murphy.TimelineEntry) []string {
	var users []string
	for _, page := range entry.GetEscalation().GetPaged() {
		users = append(users, page.GetUserId())
	}
	return users
}

func TestAPolicyVersionIsReadOnceAndReused(t *testing.T) {
	f := setup(t)
	first, second := f.triggered(0, 1), f.triggered(0, 1)

	require.NoError(t, f.service.TimerExpired(t.Context(), "message-1", timer(first, 1, 1)))
	require.NoError(t, f.service.TimerExpired(t.Context(), "message-2", timer(second, 1, 1)))

	f.policies.AssertNumberOfCalls(t, "GetPolicyVersion", 1)
}
