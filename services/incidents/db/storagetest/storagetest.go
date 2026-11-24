package storagetest

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/services/incidents"
)

func Run(t *testing.T, open func(t *testing.T) incidents.Repository) {
	t.Run("stores an opened incident with a number", func(t *testing.T) {
		repo := open(t)
		outcome := opened(murphy.NewID(), "disk-full")

		recorded, err := repo.Record(t.Context(), outcome)

		require.NoError(t, err)
		assert.True(t, recorded)
		assert.Positive(t, outcome.Decision.Incident.GetNumber())
		got, err := repo.GetIncident(t.Context(), outcome.IncidentID)
		require.NoError(t, err)
		assert.Equal(t, outcome.Decision.Incident.GetNumber(), got.GetNumber())
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
		_, err := repo.Record(t.Context(), first)
		require.NoError(t, err)
		_, err = repo.Record(t.Context(), second)
		require.NoError(t, err)

		assert.Greater(t, second.Decision.Incident.GetNumber(), first.Decision.Incident.GetNumber())
	})

	t.Run("remembers which policy version an incident follows", func(t *testing.T) {
		repo := open(t)
		outcome := opened(murphy.NewID(), "disk-full")
		_, err := repo.Record(t.Context(), outcome)
		require.NoError(t, err)

		got, err := repo.GetIncident(t.Context(), outcome.IncidentID)

		require.NoError(t, err)
		assert.Equal(t, outcome.Decision.Incident.GetEscalationPolicyId(), got.GetEscalationPolicyId())
		assert.Equal(t, int32(3), got.GetEscalationPolicyVersion())
	})

	t.Run("records escalation progress and the people paged", func(t *testing.T) {
		repo := open(t)
		outcome := opened(murphy.NewID(), "disk-full")
		_, err := repo.Record(t.Context(), outcome)
		require.NoError(t, err)

		ana, bo := murphy.NewID(), murphy.NewID()
		escalated := joined(outcome.Decision.Incident, murphy.NewID())
		escalated.Alert = nil
		escalated.Decision.Incident.EscalationLevel, escalated.Decision.Incident.EscalationRound = 2, 1
		escalated.Decision.Timeline = []*murphy.TimelineEntry{{
			Id: murphy.NewID(), IncidentId: outcome.IncidentID, Type: murphy.TimelineEntryType_TIMELINE_ENTRY_TYPE_ESCALATED,
			Summary: "level 2", Escalation: &murphy.TimelineEscalation{Level: 2, Round: 1, Paged: []*murphy.TimelinePage{
				{UserId: bo, Via: &murphy.EscalationTarget{Type: murphy.EscalationTargetType_ESCALATION_TARGET_TYPE_USER, Id: bo}},
				{UserId: ana, Via: &murphy.EscalationTarget{Type: murphy.EscalationTargetType_ESCALATION_TARGET_TYPE_SCHEDULE, Id: murphy.NewID()}},
			}}, At: murphy.Now(),
		}}
		_, err = repo.Record(t.Context(), escalated)
		require.NoError(t, err)

		got, err := repo.GetIncident(t.Context(), outcome.IncidentID)
		require.NoError(t, err)
		assert.Equal(t, int32(2), got.GetEscalationLevel())
		assert.Equal(t, int32(1), got.GetEscalationRound())
		timeline, err := repo.ListTimeline(t.Context(), outcome.IncidentID)
		require.NoError(t, err)
		require.Len(t, timeline, 2)
		assert.Equal(t, int32(2), timeline[1].GetEscalation().GetLevel())
		paged := timeline[1].GetEscalation().GetPaged()
		require.Len(t, paged, 2)
		assert.Equal(t, bo, paged[0].GetUserId())
		assert.Equal(t, ana, paged[1].GetUserId())
		assert.Equal(t, murphy.EscalationTargetType_ESCALATION_TARGET_TYPE_SCHEDULE, paged[1].GetVia().GetType())
		assert.Equal(t, outcome.Alert.GetId(), timeline[0].GetAlertId())
	})

	t.Run("records a change without a message, every time", func(t *testing.T) {
		repo := open(t)
		outcome := opened(murphy.NewID(), "disk-full")
		_, err := repo.Record(t.Context(), outcome)
		require.NoError(t, err)

		acknowledged := joined(outcome.Decision.Incident, "")
		acknowledged.Alert = nil
		acknowledged.Decision.Incident.Status = murphy.IncidentStatus_INCIDENT_STATUS_ACKNOWLEDGED
		acknowledged.Decision.Incident.AcknowledgedAt = murphy.Now()
		recorded, err := repo.Record(t.Context(), acknowledged)

		require.NoError(t, err)
		assert.True(t, recorded)
		got, err := repo.GetIncident(t.Context(), outcome.IncidentID)
		require.NoError(t, err)
		assert.Equal(t, murphy.IncidentStatus_INCIDENT_STATUS_ACKNOWLEDGED, got.GetStatus())
		assert.NotNil(t, got.GetAcknowledgedAt())
	})

	t.Run("hands out due messages once, until their claim expires", func(t *testing.T) {
		repo := open(t)
		outcome := opened(murphy.NewID(), "disk-full")
		now := time.Now().UTC()
		outcome.Decision.NextTimer = timer(outcome.IncidentID, now.Add(time.Hour))
		_, err := repo.Record(t.Context(), outcome)
		require.NoError(t, err)

		claimed, err := repo.ClaimDueMessages(t.Context(), now.Add(time.Second), 10, time.Minute)
		require.NoError(t, err)
		require.Len(t, claimed, 1, "the announcement is due now, the timer later")
		assert.Equal(t, incidents.MessageEvent, claimed[0].Kind)
		announced := &murphy.Envelope{}
		require.NoError(t, proto.Unmarshal(claimed[0].Envelope, announced))
		assert.Equal(t, murphy.IncidentVerb_INCIDENT_VERB_CREATED, announced.GetIncident().GetVerb())
		assert.Positive(t, announced.GetIncident().GetCurrent().GetNumber(), "the announcement carries the number the insert gave")

		again, err := repo.ClaimDueMessages(t.Context(), now.Add(2*time.Second), 10, time.Minute)
		require.NoError(t, err)
		assert.Empty(t, again)

		expired, err := repo.ClaimDueMessages(t.Context(), now.Add(2*time.Minute), 10, time.Minute)
		require.NoError(t, err)
		require.Len(t, expired, 1)
		assert.Equal(t, claimed[0].ID, expired[0].ID)

		require.NoError(t, repo.DeleteMessages(t.Context(), []string{claimed[0].ID}))
		later, err := repo.ClaimDueMessages(t.Context(), now.Add(2*time.Hour), 10, time.Minute)
		require.NoError(t, err)
		require.Len(t, later, 1)
		assert.Equal(t, incidents.MessageTimer, later[0].Kind)
	})

	t.Run("cancelling timers keeps the incident's events", func(t *testing.T) {
		repo := open(t)
		outcome := opened(murphy.NewID(), "disk-full")
		now := time.Now().UTC()
		outcome.Decision.NextTimer = timer(outcome.IncidentID, now)
		_, err := repo.Record(t.Context(), outcome)
		require.NoError(t, err)

		acknowledged := joined(outcome.Decision.Incident, "")
		acknowledged.Alert = nil
		acknowledged.Decision.Incident.Status = murphy.IncidentStatus_INCIDENT_STATUS_ACKNOWLEDGED
		acknowledged.Decision.CancelTimers = true
		acknowledged.Decision.Announce = []incidents.Announcement{{Verb: murphy.IncidentVerb_INCIDENT_VERB_ACKNOWLEDGED}}
		_, err = repo.Record(t.Context(), acknowledged)
		require.NoError(t, err)

		claimed, err := repo.ClaimDueMessages(t.Context(), now.Add(time.Second), 10, time.Minute)
		require.NoError(t, err)
		kinds := make([]incidents.MessageKind, len(claimed))
		for i, message := range claimed {
			kinds[i] = message.Kind
		}
		assert.Equal(t, []incidents.MessageKind{incidents.MessageEvent, incidents.MessageEvent}, kinds)
	})

	t.Run("records a message only once", func(t *testing.T) {
		repo := open(t)
		outcome := opened(murphy.NewID(), "disk-full")
		_, err := repo.Record(t.Context(), outcome)
		require.NoError(t, err)

		again := joined(outcome.Decision.Incident, outcome.MessageID)
		recorded, err := repo.Record(t.Context(), again)

		require.NoError(t, err)
		assert.False(t, recorded)
		timeline, err := repo.ListTimeline(t.Context(), outcome.IncidentID)
		require.NoError(t, err)
		assert.Len(t, timeline, 1)
	})

	t.Run("refuses a second open incident for the same key", func(t *testing.T) {
		repo := open(t)
		first := opened(murphy.NewID(), "disk-full")
		_, err := repo.Record(t.Context(), first)
		require.NoError(t, err)

		second := opened(first.Decision.Incident.GetServiceId(), "disk-full")
		_, err = repo.Record(t.Context(), second)

		assert.ErrorIs(t, err, murphy.ErrConflict)
	})

	t.Run("allows a new incident once the open one is resolved", func(t *testing.T) {
		repo := open(t)
		first := opened(murphy.NewID(), "disk-full")
		_, err := repo.Record(t.Context(), first)
		require.NoError(t, err)
		_, err = repo.Record(t.Context(), resolved(first.Decision.Incident))
		require.NoError(t, err)

		_, err = repo.Record(t.Context(), opened(first.Decision.Incident.GetServiceId(), "disk-full"))

		assert.NoError(t, err)
	})

	t.Run("refuses an update from a stale version and keeps nothing of it", func(t *testing.T) {
		repo := open(t)
		outcome := opened(murphy.NewID(), "disk-full")
		_, err := repo.Record(t.Context(), outcome)
		require.NoError(t, err)
		_, err = repo.Record(t.Context(), joined(outcome.Decision.Incident, murphy.NewID()))
		require.NoError(t, err)

		stale := resolved(outcome.Decision.Incident)
		stale.Decision.Incident.Version = 1
		_, err = repo.Record(t.Context(), stale)

		assert.ErrorIs(t, err, murphy.ErrConflict)
		got, err := repo.GetIncident(t.Context(), outcome.IncidentID)
		require.NoError(t, err)
		assert.Equal(t, murphy.IncidentStatus_INCIDENT_STATUS_TRIGGERED, got.GetStatus())
		assert.Equal(t, int32(2), got.GetVersion())
		timeline, err := repo.ListTimeline(t.Context(), outcome.IncidentID)
		require.NoError(t, err)
		assert.Len(t, timeline, 2)
		retried, err := repo.Record(t.Context(), joined(got, stale.MessageID))
		require.NoError(t, err)
		assert.True(t, retried, "a refused message must not count as handled")
	})

	t.Run("finds only the open incident for a key", func(t *testing.T) {
		repo := open(t)
		outcome := opened(murphy.NewID(), "disk-full")
		serviceID := outcome.Decision.Incident.GetServiceId()
		_, err := repo.FindOpenIncident(t.Context(), serviceID, "disk-full")
		assert.ErrorIs(t, err, murphy.ErrNotFound)

		_, err = repo.Record(t.Context(), outcome)
		require.NoError(t, err)
		found, err := repo.FindOpenIncident(t.Context(), serviceID, "disk-full")
		require.NoError(t, err)
		assert.Equal(t, outcome.IncidentID, found.GetId())

		_, err = repo.Record(t.Context(), resolved(found))
		require.NoError(t, err)
		_, err = repo.FindOpenIncident(t.Context(), serviceID, "disk-full")
		assert.ErrorIs(t, err, murphy.ErrNotFound)
	})

	t.Run("lists incidents newest first, filtered by status and service", func(t *testing.T) {
		repo := open(t)
		first, second := opened(murphy.NewID(), "a"), opened(murphy.NewID(), "b")
		_, err := repo.Record(t.Context(), first)
		require.NoError(t, err)
		_, err = repo.Record(t.Context(), second)
		require.NoError(t, err)
		_, err = repo.Record(t.Context(), resolved(first.Decision.Incident))
		require.NoError(t, err)

		all, err := repo.ListIncidents(t.Context(), incidents.Filter{}, maxID, 10)
		require.NoError(t, err)
		assert.Equal(t, []string{second.IncidentID, first.IncidentID}, ids(all))

		open, err := repo.ListIncidents(t.Context(), incidents.Filter{Statuses: []murphy.IncidentStatus{murphy.IncidentStatus_INCIDENT_STATUS_TRIGGERED}}, maxID, 10)
		require.NoError(t, err)
		assert.Equal(t, []string{second.IncidentID}, ids(open))

		ofService, err := repo.ListIncidents(t.Context(), incidents.Filter{ServiceIDs: []string{first.Decision.Incident.GetServiceId()}}, maxID, 10)
		require.NoError(t, err)
		assert.Equal(t, []string{first.IncidentID}, ids(ofService))

		page, err := repo.ListIncidents(t.Context(), incidents.Filter{}, second.IncidentID, 10)
		require.NoError(t, err)
		assert.Equal(t, []string{first.IncidentID}, ids(page))
	})

	t.Run("tells when the latest firing alert arrived", func(t *testing.T) {
		repo := open(t)
		outcome := opened(murphy.NewID(), "disk-full")
		_, err := repo.Record(t.Context(), outcome)
		require.NoError(t, err)
		later := joined(outcome.Decision.Incident, murphy.NewID())
		later.Alert.ReceivedAt = timestamppb.New(outcome.Alert.GetReceivedAt().AsTime().Add(time.Minute))
		_, err = repo.Record(t.Context(), later)
		require.NoError(t, err)

		latest, err := repo.LatestFiringAt(t.Context(), outcome.IncidentID)

		require.NoError(t, err)
		assert.WithinDuration(t, later.Alert.GetReceivedAt().AsTime(), latest, time.Millisecond)
	})

	t.Run("forgets handled messages older than the purge", func(t *testing.T) {
		repo := open(t)
		outcome := opened(murphy.NewID(), "disk-full")
		_, err := repo.Record(t.Context(), outcome)
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

func opened(serviceID, dedupKey string) *incidents.Change {
	now := murphy.Now()
	a := alert(serviceID, dedupKey, murphy.AlertStatus_ALERT_STATUS_FIRING)
	incident := &murphy.Incident{
		Id: murphy.NewID(), ServiceId: serviceID, DedupKey: dedupKey, Title: dedupKey,
		Status: murphy.IncidentStatus_INCIDENT_STATUS_TRIGGERED, Urgency: murphy.Urgency_URGENCY_HIGH,
		Version: 1, CreatedAt: now, UpdatedAt: now,
		EscalationPolicyId: murphy.NewID(), EscalationPolicyVersion: 3,
	}
	return &incidents.Change{
		MessageID: murphy.NewID(), Alert: a, IncidentID: incident.GetId(),
		Decision: &incidents.Decision{
			Incident: incident, Created: true,
			Announce: []incidents.Announcement{{Verb: murphy.IncidentVerb_INCIDENT_VERB_CREATED}},
			Timeline: []*murphy.TimelineEntry{entry(incident.GetId(), murphy.TimelineEntryType_TIMELINE_ENTRY_TYPE_TRIGGERED, a)},
		},
	}
}

func joined(incident *murphy.Incident, messageID string) *incidents.Change {
	a := alert(incident.GetServiceId(), incident.GetDedupKey(), murphy.AlertStatus_ALERT_STATUS_FIRING)
	updated := &murphy.Incident{
		Id: incident.GetId(), ServiceId: incident.GetServiceId(), DedupKey: incident.GetDedupKey(), Title: incident.GetTitle(),
		Status: incident.GetStatus(), Urgency: incident.GetUrgency(), Version: incident.GetVersion() + 1,
		CreatedAt: incident.GetCreatedAt(), UpdatedAt: murphy.Now(),
		EscalationPolicyId: incident.GetEscalationPolicyId(), EscalationPolicyVersion: incident.GetEscalationPolicyVersion(),
		EscalationLevel: incident.GetEscalationLevel(), EscalationRound: incident.GetEscalationRound(),
	}
	return &incidents.Change{
		MessageID: messageID, Alert: a, IncidentID: incident.GetId(),
		Decision: &incidents.Decision{
			Incident: updated,
			Timeline: []*murphy.TimelineEntry{entry(incident.GetId(), murphy.TimelineEntryType_TIMELINE_ENTRY_TYPE_ALERTED, a)},
		},
	}
}

func resolved(incident *murphy.Incident) *incidents.Change {
	outcome := joined(incident, murphy.NewID())
	outcome.Alert.Status = murphy.AlertStatus_ALERT_STATUS_RESOLVED
	outcome.Decision.Incident.Status, outcome.Decision.Incident.ResolvedAt = murphy.IncidentStatus_INCIDENT_STATUS_RESOLVED, murphy.Now()
	outcome.Decision.Timeline[0].Type = murphy.TimelineEntryType_TIMELINE_ENTRY_TYPE_RESOLVED
	return outcome
}

func ids(list []*murphy.Incident) []string {
	out := make([]string, len(list))
	for i, incident := range list {
		out[i] = incident.GetId()
	}
	return out
}

func timer(incidentID string, due time.Time) *murphy.Timer {
	return &murphy.Timer{Id: murphy.NewID(), IncidentId: incidentID, EscalationLevel: 2, EscalationRound: 1, DueAt: timestamppb.New(due)}
}
