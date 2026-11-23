package storagetest

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/services/incidents"
)

func Run(t *testing.T, open func(t *testing.T) incidents.Repository) {
	t.Run("stores an opened incident with a number", func(t *testing.T) {
		repo := open(t)
		outcome := opened(murphy.NewID(), "disk-full")

		recorded, err := repo.RecordAlert(t.Context(), outcome)

		require.NoError(t, err)
		assert.True(t, recorded)
		assert.Positive(t, outcome.Created.GetNumber())
		got, err := repo.GetIncident(t.Context(), outcome.IncidentID)
		require.NoError(t, err)
		assert.Equal(t, outcome.Created.GetNumber(), got.GetNumber())
		assert.Equal(t, murphy.IncidentStatus_INCIDENT_STATUS_TRIGGERED, got.GetStatus())
		assert.Equal(t, int32(1), got.GetVersion())
		timeline, err := repo.ListTimeline(t.Context(), outcome.IncidentID)
		require.NoError(t, err)
		require.Len(t, timeline, 1)
		assert.Equal(t, outcome.Alert.GetId(), timeline[0].GetAlertId())
	})

	t.Run("numbers incidents in the order they were opened", func(t *testing.T) {
		repo := open(t)
		first, second := opened(murphy.NewID(), "a"), opened(murphy.NewID(), "b")
		_, err := repo.RecordAlert(t.Context(), first)
		require.NoError(t, err)
		_, err = repo.RecordAlert(t.Context(), second)
		require.NoError(t, err)

		assert.Greater(t, second.Created.GetNumber(), first.Created.GetNumber())
	})

	t.Run("records a message only once", func(t *testing.T) {
		repo := open(t)
		outcome := opened(murphy.NewID(), "disk-full")
		_, err := repo.RecordAlert(t.Context(), outcome)
		require.NoError(t, err)

		again := joined(outcome.Created, outcome.MessageID)
		recorded, err := repo.RecordAlert(t.Context(), again)

		require.NoError(t, err)
		assert.False(t, recorded)
		timeline, err := repo.ListTimeline(t.Context(), outcome.IncidentID)
		require.NoError(t, err)
		assert.Len(t, timeline, 1)
	})

	t.Run("refuses a second open incident for the same key", func(t *testing.T) {
		repo := open(t)
		first := opened(murphy.NewID(), "disk-full")
		_, err := repo.RecordAlert(t.Context(), first)
		require.NoError(t, err)

		second := opened(first.Created.GetServiceId(), "disk-full")
		_, err = repo.RecordAlert(t.Context(), second)

		assert.ErrorIs(t, err, murphy.ErrConflict)
	})

	t.Run("allows a new incident once the open one is resolved", func(t *testing.T) {
		repo := open(t)
		first := opened(murphy.NewID(), "disk-full")
		_, err := repo.RecordAlert(t.Context(), first)
		require.NoError(t, err)
		_, err = repo.RecordAlert(t.Context(), resolved(first.Created))
		require.NoError(t, err)

		_, err = repo.RecordAlert(t.Context(), opened(first.Created.GetServiceId(), "disk-full"))

		assert.NoError(t, err)
	})

	t.Run("refuses an update from a stale version and keeps nothing of it", func(t *testing.T) {
		repo := open(t)
		outcome := opened(murphy.NewID(), "disk-full")
		_, err := repo.RecordAlert(t.Context(), outcome)
		require.NoError(t, err)
		_, err = repo.RecordAlert(t.Context(), joined(outcome.Created, murphy.NewID()))
		require.NoError(t, err)

		stale := resolved(outcome.Created)
		stale.Updated.Version = 1
		_, err = repo.RecordAlert(t.Context(), stale)

		assert.ErrorIs(t, err, murphy.ErrConflict)
		got, err := repo.GetIncident(t.Context(), outcome.IncidentID)
		require.NoError(t, err)
		assert.Equal(t, murphy.IncidentStatus_INCIDENT_STATUS_TRIGGERED, got.GetStatus())
		assert.Equal(t, int32(2), got.GetVersion())
		timeline, err := repo.ListTimeline(t.Context(), outcome.IncidentID)
		require.NoError(t, err)
		assert.Len(t, timeline, 2)
		retried, err := repo.RecordAlert(t.Context(), joined(got, stale.MessageID))
		require.NoError(t, err)
		assert.True(t, retried, "a refused message must not count as handled")
	})

	t.Run("finds only the open incident for a key", func(t *testing.T) {
		repo := open(t)
		outcome := opened(murphy.NewID(), "disk-full")
		serviceID := outcome.Created.GetServiceId()
		_, err := repo.FindOpenIncident(t.Context(), serviceID, "disk-full")
		assert.ErrorIs(t, err, murphy.ErrNotFound)

		_, err = repo.RecordAlert(t.Context(), outcome)
		require.NoError(t, err)
		found, err := repo.FindOpenIncident(t.Context(), serviceID, "disk-full")
		require.NoError(t, err)
		assert.Equal(t, outcome.IncidentID, found.GetId())

		_, err = repo.RecordAlert(t.Context(), resolved(found))
		require.NoError(t, err)
		_, err = repo.FindOpenIncident(t.Context(), serviceID, "disk-full")
		assert.ErrorIs(t, err, murphy.ErrNotFound)
	})

	t.Run("lists incidents newest first, filtered by status and service", func(t *testing.T) {
		repo := open(t)
		first, second := opened(murphy.NewID(), "a"), opened(murphy.NewID(), "b")
		_, err := repo.RecordAlert(t.Context(), first)
		require.NoError(t, err)
		_, err = repo.RecordAlert(t.Context(), second)
		require.NoError(t, err)
		_, err = repo.RecordAlert(t.Context(), resolved(first.Created))
		require.NoError(t, err)

		all, err := repo.ListIncidents(t.Context(), incidents.Filter{}, maxID, 10)
		require.NoError(t, err)
		assert.Equal(t, []string{second.IncidentID, first.IncidentID}, ids(all))

		open, err := repo.ListIncidents(t.Context(), incidents.Filter{Statuses: []murphy.IncidentStatus{murphy.IncidentStatus_INCIDENT_STATUS_TRIGGERED}}, maxID, 10)
		require.NoError(t, err)
		assert.Equal(t, []string{second.IncidentID}, ids(open))

		ofService, err := repo.ListIncidents(t.Context(), incidents.Filter{ServiceIDs: []string{first.Created.GetServiceId()}}, maxID, 10)
		require.NoError(t, err)
		assert.Equal(t, []string{first.IncidentID}, ids(ofService))

		page, err := repo.ListIncidents(t.Context(), incidents.Filter{}, second.IncidentID, 10)
		require.NoError(t, err)
		assert.Equal(t, []string{first.IncidentID}, ids(page))
	})

	t.Run("tells when the latest firing alert arrived", func(t *testing.T) {
		repo := open(t)
		outcome := opened(murphy.NewID(), "disk-full")
		_, err := repo.RecordAlert(t.Context(), outcome)
		require.NoError(t, err)
		later := joined(outcome.Created, murphy.NewID())
		later.Alert.ReceivedAt = timestamppb.New(outcome.Alert.GetReceivedAt().AsTime().Add(time.Minute))
		_, err = repo.RecordAlert(t.Context(), later)
		require.NoError(t, err)

		latest, err := repo.LatestFiringAt(t.Context(), outcome.IncidentID)

		require.NoError(t, err)
		assert.WithinDuration(t, later.Alert.GetReceivedAt().AsTime(), latest, time.Millisecond)
	})

	t.Run("forgets handled messages older than the purge", func(t *testing.T) {
		repo := open(t)
		outcome := opened(murphy.NewID(), "disk-full")
		_, err := repo.RecordAlert(t.Context(), outcome)
		require.NoError(t, err)

		purged, err := repo.PurgeHandledMessages(t.Context(), time.Now().Add(-time.Hour))
		require.NoError(t, err)
		assert.Zero(t, purged)
		purged, err = repo.PurgeHandledMessages(t.Context(), time.Now().Add(time.Hour))
		require.NoError(t, err)
		assert.Equal(t, int64(1), purged)
	})
}

const maxID = "ffffffff-ffff-ffff-ffff-ffffffffffff"

func alert(serviceID, dedupKey string, status murphy.AlertStatus) *murphy.Alert {
	return &murphy.Alert{
		Id:            murphy.NewID(),
		IntegrationId: murphy.NewID(),
		ServiceId:     serviceID,
		DedupKey:      dedupKey,
		Status:        status,
		Summary:       dedupKey,
		Labels:        map[string]string{"host": "db-1"},
		ReceivedAt:    murphy.Now(),
	}
}

func entry(incidentID string, kind murphy.TimelineEntryType, a *murphy.Alert) *murphy.TimelineEntry {
	return &murphy.TimelineEntry{Id: murphy.NewID(), IncidentId: incidentID, Type: kind, AlertId: a.GetId(), Summary: a.GetSummary(), At: murphy.Now()}
}

func opened(serviceID, dedupKey string) *incidents.AlertOutcome {
	now := murphy.Now()
	a := alert(serviceID, dedupKey, murphy.AlertStatus_ALERT_STATUS_FIRING)
	incident := &murphy.Incident{
		Id: murphy.NewID(), ServiceId: serviceID, DedupKey: dedupKey, Title: dedupKey,
		Status: murphy.IncidentStatus_INCIDENT_STATUS_TRIGGERED, Urgency: murphy.Urgency_URGENCY_HIGH,
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	return &incidents.AlertOutcome{
		MessageID: murphy.NewID(), Alert: a, IncidentID: incident.GetId(), Created: incident,
		Timeline: []*murphy.TimelineEntry{entry(incident.GetId(), murphy.TimelineEntryType_TIMELINE_ENTRY_TYPE_TRIGGERED, a)},
	}
}

func joined(incident *murphy.Incident, messageID string) *incidents.AlertOutcome {
	a := alert(incident.GetServiceId(), incident.GetDedupKey(), murphy.AlertStatus_ALERT_STATUS_FIRING)
	updated := &murphy.Incident{
		Id: incident.GetId(), ServiceId: incident.GetServiceId(), DedupKey: incident.GetDedupKey(), Title: incident.GetTitle(),
		Status: incident.GetStatus(), Urgency: incident.GetUrgency(), Version: incident.GetVersion(),
		CreatedAt: incident.GetCreatedAt(), UpdatedAt: murphy.Now(),
	}
	return &incidents.AlertOutcome{
		MessageID: messageID, Alert: a, IncidentID: incident.GetId(), Updated: updated,
		Timeline: []*murphy.TimelineEntry{entry(incident.GetId(), murphy.TimelineEntryType_TIMELINE_ENTRY_TYPE_ALERTED, a)},
	}
}

func resolved(incident *murphy.Incident) *incidents.AlertOutcome {
	outcome := joined(incident, murphy.NewID())
	outcome.Alert.Status = murphy.AlertStatus_ALERT_STATUS_RESOLVED
	outcome.Updated.Resolve(murphy.Now())
	outcome.Timeline[0].Type = murphy.TimelineEntryType_TIMELINE_ENTRY_TYPE_RESOLVED
	return outcome
}

func ids(list []*murphy.Incident) []string {
	out := make([]string, len(list))
	for i, incident := range list {
		out[i] = incident.GetId()
	}
	return out
}
