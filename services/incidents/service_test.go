package incidents_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
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
	policyGone  bool
	recorded    *incidents.Change
}

const (
	schedule = "0195f3a0-0000-7000-8000-00000000000a"
	bo       = "0195f3a0-0000-7000-8000-0000000000b1"
)

func setup(t *testing.T) *fixture {
	t.Helper()
	repo, catalog := NewMockRepository(t), NewMockCatalog(t)
	policies := NewMockPolicies(t)
	f := &fixture{
		service:  incidents.New(repo, catalog, policies),
		repo:     repo,
		policies: policies,
		policy: &murphy.EscalationPolicy{
			Id: murphy.NewID(), Version: 3, RepeatCount: 1,
			Levels: []*murphy.EscalationLevel{
				{Delay: durationpb.New(5 * time.Minute), Targets: []*murphy.EscalationTarget{{Type: murphy.EscalationTargetType_ESCALATION_TARGET_TYPE_SCHEDULE, Id: schedule}}},
				{Delay: durationpb.New(10 * time.Minute), Targets: []*murphy.EscalationTarget{{Type: murphy.EscalationTargetType_ESCALATION_TARGET_TYPE_USER, Id: bo}}},
			},
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
		if f.policyGone {
			return 0, &murphy.NotFoundError{Entity: "escalation policy"}
		}
		return f.policy.GetVersion(), nil
	}).Maybe()
	policies.EXPECT().GetPolicyVersion(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(context.Context, string, int32) (*murphy.EscalationPolicy, error) {
		return f.policy, nil
	}).Maybe()
	repo.EXPECT().Record(mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, outcome *incidents.Change) (bool, error) {
		f.recorded = outcome
		return true, nil
	}).Maybe()
	return f
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

func TestAFiringAlertOpensAnIncidentOnTheServicesCurrentPolicy(t *testing.T) {
	f := setup(t)
	f.noneOpen()
	alert := f.alert(murphy.AlertStatus_ALERT_STATUS_FIRING)

	require.NoError(t, f.service.AlertReceived(t.Context(), "message-1", alert))

	assert.Equal(t, "message-1", f.recorded.MessageID)
	assert.Equal(t, alert, f.recorded.Alert)
	require.True(t, f.recorded.Decision.Created)
	opened := f.recorded.Decision.GetIncident()
	assert.Equal(t, opened.GetId(), f.recorded.IncidentID)
	assert.Equal(t, int32(3), opened.GetEscalationPolicyVersion())
	assert.Equal(t, murphy.Urgency_URGENCY_LOW, opened.GetUrgency())
}

func TestAnIncidentOpensUnpagedWhenItsPolicyIsGone(t *testing.T) {
	f := setup(t)
	f.noneOpen()
	f.policyGone = true

	require.NoError(t, f.service.AlertReceived(t.Context(), "message-1", f.alert(murphy.AlertStatus_ALERT_STATUS_FIRING)))

	assert.True(t, f.recorded.Decision.GetIncident().GetEscalationExhausted())
}

func TestAFiringAlertJoinsTheOpenIncident(t *testing.T) {
	f := setup(t)
	open := &murphy.Incident{Id: murphy.NewID(), Status: murphy.IncidentStatus_INCIDENT_STATUS_ACKNOWLEDGED}
	f.open(open)

	require.NoError(t, f.service.AlertReceived(t.Context(), "message-1", f.alert(murphy.AlertStatus_ALERT_STATUS_FIRING)))

	assert.False(t, f.recorded.Decision.Created)
	assert.Equal(t, open, f.recorded.Decision.GetIncident())
	assert.Equal(t, murphy.IncidentStatus_INCIDENT_STATUS_ACKNOWLEDGED, open.GetStatus())
	assert.Equal(t, open.GetId(), f.recorded.IncidentID)
	require.Len(t, f.recorded.Decision.GetTimeline(), 1)
	assert.Equal(t, murphy.TimelineEntryType_TIMELINE_ENTRY_TYPE_ALERTED, f.recorded.Decision.GetTimeline()[0].GetType())
}

func TestAResolvedAlertResolvesTheOpenIncident(t *testing.T) {
	f := setup(t)
	open := &murphy.Incident{Id: murphy.NewID(), Status: murphy.IncidentStatus_INCIDENT_STATUS_TRIGGERED, Version: 3}
	f.open(open)
	f.repo.EXPECT().LatestFiringAt(mock.Anything, open.GetId()).Return(time.Now().Add(-time.Minute), nil)

	require.NoError(t, f.service.AlertReceived(t.Context(), "message-1", f.alert(murphy.AlertStatus_ALERT_STATUS_RESOLVED)))

	resolved := f.recorded.Decision.GetIncident()
	require.NotNil(t, resolved)
	assert.Equal(t, murphy.IncidentStatus_INCIDENT_STATUS_RESOLVED, resolved.GetStatus())
	assert.NotNil(t, resolved.GetResolvedAt())
	assert.Equal(t, int32(4), resolved.GetVersion())
	assert.True(t, f.recorded.Decision.GetCancelTimers())
	assert.Equal(t, murphy.IncidentVerb_INCIDENT_VERB_RESOLVED, f.recorded.Decision.GetAnnounce()[0].Verb)
	require.Len(t, f.recorded.Decision.GetTimeline(), 1)
	assert.Equal(t, murphy.TimelineEntryType_TIMELINE_ENTRY_TYPE_RESOLVED, f.recorded.Decision.GetTimeline()[0].GetType())
}

func TestAResolvedAlertNotClearlyNewerThanTheLatestFiringKeepsTheIncidentOpen(t *testing.T) {
	f := setup(t)
	open := &murphy.Incident{Id: murphy.NewID(), Status: murphy.IncidentStatus_INCIDENT_STATUS_TRIGGERED, Version: 3}
	f.open(open)
	alert := f.alert(murphy.AlertStatus_ALERT_STATUS_RESOLVED)
	f.repo.EXPECT().LatestFiringAt(mock.Anything, open.GetId()).Return(alert.GetReceivedAt().AsTime().Add(-500*time.Millisecond), nil)

	require.NoError(t, f.service.AlertReceived(t.Context(), "message-1", alert))

	assert.Equal(t, open, f.recorded.Decision.GetIncident())
	assert.Equal(t, murphy.IncidentStatus_INCIDENT_STATUS_TRIGGERED, open.GetStatus())
	require.Len(t, f.recorded.Decision.GetTimeline(), 1)
	assert.Equal(t, murphy.TimelineEntryType_TIMELINE_ENTRY_TYPE_ALERTED, f.recorded.Decision.GetTimeline()[0].GetType())
}

func TestAResolveForOneInstanceKeepsTheIncidentOpenWhileAnotherFires(t *testing.T) {
	f := setup(t)
	open := &murphy.Incident{Id: murphy.NewID(), Status: murphy.IncidentStatus_INCIDENT_STATUS_TRIGGERED, Version: 2}
	f.open(open)
	f.repo.EXPECT().OpenInstances(mock.Anything, open.GetId()).Return([]string{"a", "b"}, nil)
	alert := f.alert(murphy.AlertStatus_ALERT_STATUS_RESOLVED)
	alert.Instance = "a"

	require.NoError(t, f.service.AlertReceived(t.Context(), "message-1", alert))

	assert.Equal(t, murphy.IncidentStatus_INCIDENT_STATUS_TRIGGERED, f.recorded.Decision.GetIncident().GetStatus())
	assert.Equal(t, murphy.TimelineEntryType_TIMELINE_ENTRY_TYPE_ALERTED, f.recorded.Decision.GetTimeline()[0].GetType())
}

func TestTheResolveForTheLastInstanceResolvesTheIncident(t *testing.T) {
	f := setup(t)
	open := &murphy.Incident{Id: murphy.NewID(), Status: murphy.IncidentStatus_INCIDENT_STATUS_TRIGGERED, Version: 2}
	f.open(open)
	f.repo.EXPECT().OpenInstances(mock.Anything, open.GetId()).Return([]string{"b"}, nil)
	alert := f.alert(murphy.AlertStatus_ALERT_STATUS_RESOLVED)
	alert.Instance = "b"

	require.NoError(t, f.service.AlertReceived(t.Context(), "message-1", alert))

	assert.Equal(t, murphy.IncidentStatus_INCIDENT_STATUS_RESOLVED, f.recorded.Decision.GetIncident().GetStatus())
	assert.Equal(t, murphy.TimelineEntryType_TIMELINE_ENTRY_TYPE_RESOLVED, f.recorded.Decision.GetTimeline()[0].GetType())
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
	assert.Nil(t, f.recorded.Decision.GetIncident())
	assert.Empty(t, f.recorded.Decision.GetTimeline())
}

func TestThePreviousKeyWorksDuringTheGracePeriod(t *testing.T) {
	f := setup(t)
	f.noneOpen()
	f.integration.PreviousKeyValidUntil = timestamppb.New(time.Now().Add(time.Hour))
	alert := f.alert(murphy.AlertStatus_ALERT_STATUS_FIRING)
	alert.KeyVersion = 1

	require.NoError(t, f.service.AlertReceived(t.Context(), "message-1", alert))
	assert.NotNil(t, f.recorded.Decision.GetIncident())
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

func (f *fixture) triggered(level, round int32) *murphy.Incident {
	incident := &murphy.Incident{
		Id: murphy.NewID(), Status: murphy.IncidentStatus_INCIDENT_STATUS_TRIGGERED, Version: 2,
		EscalationPolicyId: f.policy.GetId(), EscalationPolicyVersion: 3, EscalationLevel: level, EscalationRound: round,
	}
	f.repo.EXPECT().GetIncident(mock.Anything, incident.GetId()).Return(incident, nil).Maybe()
	return incident
}

func TestAcknowledgingRetriesAfterAConcurrentChange(t *testing.T) {
	f := setup(t)
	repo := NewMockRepository(t)
	f.service = incidents.New(repo, nil, nil)
	id := murphy.NewID()
	repo.EXPECT().GetIncident(mock.Anything, id).RunAndReturn(func(context.Context, string) (*murphy.Incident, error) {
		return &murphy.Incident{Id: id, Status: murphy.IncidentStatus_INCIDENT_STATUS_TRIGGERED, Version: 2}, nil
	}).Times(2)
	repo.EXPECT().Record(mock.Anything, mock.Anything).Return(false, murphy.ErrConflict).Once()
	repo.EXPECT().Record(mock.Anything, mock.Anything).Return(true, nil).Once()

	acknowledged, err := f.service.Acknowledge(t.Context(), id, murphy.NewID())

	require.NoError(t, err)
	assert.Equal(t, murphy.IncidentStatus_INCIDENT_STATUS_ACKNOWLEDGED, acknowledged.GetStatus())
}

func TestAcknowledgingAnAcknowledgedIncidentReadsOnlyItsAcknowledgements(t *testing.T) {
	f := setup(t)
	repo := NewMockRepository(t)
	f.service = incidents.New(repo, nil, nil)
	id := murphy.NewID()
	repo.EXPECT().GetIncident(mock.Anything, id).Return(&murphy.Incident{Id: id, Status: murphy.IncidentStatus_INCIDENT_STATUS_ACKNOWLEDGED, Version: 3}, nil).Once()
	repo.EXPECT().ListTimelineOfType(mock.Anything, id, murphy.TimelineEntryType_TIMELINE_ENTRY_TYPE_ACKNOWLEDGED).
		Return([]*murphy.TimelineEntry{{Type: murphy.TimelineEntryType_TIMELINE_ENTRY_TYPE_ACKNOWLEDGED, ActorUserId: ana}}, nil).Once()
	var recorded *incidents.Change
	repo.EXPECT().Record(mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, change *incidents.Change) (bool, error) {
		recorded = change
		return true, nil
	}).Once()

	_, err := f.service.Acknowledge(t.Context(), id, bo)

	require.NoError(t, err)
	assert.Equal(t, bo, recorded.Decision.Timeline[0].GetActorUserId())
}

func TestAPolicyVersionIsReadOnceAndReused(t *testing.T) {
	f := setup(t)
	first, second := f.triggered(1, 1), f.triggered(1, 1)

	require.NoError(t, f.service.TimerExpired(t.Context(), "message-1", &murphy.Timer{IncidentId: first.GetId(), EscalationLevel: 2, EscalationRound: 1}))
	require.NoError(t, f.service.TimerExpired(t.Context(), "message-2", &murphy.Timer{IncidentId: second.GetId(), EscalationLevel: 2, EscalationRound: 1}))

	f.policies.AssertNumberOfCalls(t, "GetPolicyVersion", 1)
}

func TestATimerForAnIncidentThatIsGoneOnlyMarksItsMessage(t *testing.T) {
	f := setup(t)
	f.repo.EXPECT().GetIncident(mock.Anything, mock.Anything).Return(nil, &murphy.NotFoundError{Entity: "incident"})

	require.NoError(t, f.service.TimerExpired(t.Context(), "message-1", &murphy.Timer{IncidentId: murphy.NewID(), EscalationLevel: 2, EscalationRound: 1}))

	assert.Equal(t, &incidents.Change{MessageID: "message-1"}, f.recorded)
}

func TestADeliveredFollowUpIsNotWrittenAnywhere(t *testing.T) {
	repo := NewMockRepository(t)
	service := incidents.New(repo, nil, nil)
	told := &murphy.Notification{
		Id: murphy.NewID(), Reason: murphy.NotificationReason_NOTIFICATION_REASON_ACKNOWLEDGED, Incident: &murphy.Incident{Id: murphy.NewID()},
		ContactMethod: &murphy.ContactMethod{Type: murphy.ContactMethodType_CONTACT_METHOD_TYPE_WEBHOOK},
	}

	err := service.NotificationSent(t.Context(), murphy.NewID(), told)

	require.NoError(t, err)
	repo.AssertNotCalled(t, "Record", mock.Anything, mock.Anything)
}
