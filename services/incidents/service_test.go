package incidents_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/services/incidents"
)

type fixture struct {
	service     *incidents.Service
	repo        *MockRepository
	integration *murphy.Integration
	recorded    *incidents.AlertOutcome
}

func setup(t *testing.T) *fixture {
	t.Helper()
	repo, catalog := NewMockRepository(t), NewMockCatalog(t)
	f := &fixture{
		service: incidents.New(repo, catalog),
		repo:    repo,
		integration: &murphy.Integration{
			Id:         murphy.NewID(),
			ServiceId:  murphy.NewID(),
			KeyVersion: 2,
			Status:     murphy.IntegrationStatus_INTEGRATION_STATUS_ACTIVE,
		},
	}
	catalog.EXPECT().GetIntegration(mock.Anything, mock.Anything).RunAndReturn(func(context.Context, string) (*murphy.Integration, error) {
		return f.integration, nil
	}).Maybe()
	catalog.EXPECT().GetService(mock.Anything, mock.Anything).Return(&murphy.Service{
		Urgency: murphy.Urgency_URGENCY_LOW,
		Status:  murphy.ServiceStatus_SERVICE_STATUS_ACTIVE,
	}, nil).Maybe()
	repo.EXPECT().RecordAlert(mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, outcome *incidents.AlertOutcome) (bool, error) {
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
	assert.Equal(t, int32(3), resolved.GetVersion())
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

func TestAResolvedAlertWithoutAnOpenIncidentIsOnlyRecorded(t *testing.T) {
	f := setup(t)
	f.noneOpen()

	require.NoError(t, f.service.AlertReceived(t.Context(), "message-1", f.alert(murphy.AlertStatus_ALERT_STATUS_RESOLVED)))

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
