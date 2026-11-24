//go:build integration

package component

import (
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/murphyhq/server"
	migrateapp "github.com/murphyhq/server/cmd/migrate/app"
	"github.com/murphyhq/server/internal/messaging"
	"github.com/murphyhq/server/internal/messaging/brokertest"
	"github.com/murphyhq/server/services/catalog"
	catalogdb "github.com/murphyhq/server/services/catalog/db"
	"github.com/murphyhq/server/services/escalation"
	escalationdb "github.com/murphyhq/server/services/escalation/db"
	"github.com/murphyhq/server/services/incidents"
	incidentsdb "github.com/murphyhq/server/services/incidents/db"
	"github.com/murphyhq/server/tests/murphytest"
	"github.com/murphyhq/server/tests/testenv"
)

var brokers = map[string]func(t *testing.T) brokertest.Environment{
	"nats": func(*testing.T) brokertest.Environment { return testenv.NATS() },
	"sqs":  testenv.SQS,
}

func murphyOn(t *testing.T, databases migrateapp.Settings) *murphytest.Murphy {
	t.Helper()
	_, api := testenv.APIWith(t, databases)
	return murphytest.New(t, murphytest.At(api, ""))
}

func incidentsOn(t *testing.T, databases migrateapp.Settings) (*incidents.Service, incidentsdb.Storage) {
	t.Helper()
	catalogStorage, err := catalogdb.Open(t.Context(), databases.CatalogDatabaseURL)
	require.NoError(t, err)
	t.Cleanup(catalogStorage.Close)
	escalationStorage, err := escalationdb.Open(t.Context(), databases.EscalationDatabaseURL)
	require.NoError(t, err)
	t.Cleanup(escalationStorage.Close)
	storage, err := incidentsdb.Open(t.Context(), databases.IncidentsDatabaseURL)
	require.NoError(t, err)
	t.Cleanup(storage.Close)
	return incidents.New(storage, catalog.New(catalogStorage, nil), escalation.New(escalationStorage, nil, nil, nil)), storage
}

func firingAlert(service *murphytest.Service) *murphy.Alert {
	return &murphy.Alert{
		Id: murphy.NewID(), IntegrationId: service.IntegrationID, ServiceId: service.ID, DedupKey: "disk-full",
		Status: murphy.AlertStatus_ALERT_STATUS_FIRING, Summary: "Disk almost full", KeyVersion: 1, ReceivedAt: murphy.Now(),
	}
}

func scheduledTimer(t *testing.T, storage incidentsdb.Storage) *murphy.Envelope {
	t.Helper()
	due, err := storage.ClaimDueMessages(t.Context(), time.Now().Add(24*time.Hour), 10, time.Minute)
	require.NoError(t, err)
	envelopes := make([]*murphy.Envelope, len(due))
	for i, message := range due {
		envelopes[i] = &murphy.Envelope{}
		require.NoError(t, proto.Unmarshal(message.Envelope, envelopes[i]))
	}
	timer := slices.IndexFunc(envelopes, func(envelope *murphy.Envelope) bool { return envelope.GetTimer() != nil })
	require.NotEqual(t, -1, timer, "opening the incident scheduled a timer")
	return envelopes[timer]
}

func schedulerCrashesAfterPublishing(t *testing.T, source messaging.ScheduledMessages, publisher *messaging.Publisher) {
	t.Helper()
	claimed, err := source.ClaimDueMessages(t.Context(), 10, 300*time.Millisecond)
	require.NoError(t, err)
	for _, message := range claimed {
		envelope := &murphy.Envelope{}
		require.NoError(t, proto.Unmarshal(message.Envelope, envelope))
		require.NoError(t, publisher.Publish(t.Context(), envelope))
	}
}

func TestARedeliveredAlertTakesEffectOnce(t *testing.T) {
	testenv.EachDatabase(t, func(t *testing.T, engine testenv.DatabaseEngine) {
		databases := testenv.Databases(t, engine)
		service := murphyOn(t, databases).Service()
		incidents, storage := incidentsOn(t, databases)
		alert, messageID := firingAlert(service), murphy.NewID()

		require.NoError(t, incidents.AlertReceived(t.Context(), messageID, alert))
		require.NoError(t, incidents.AlertReceived(t.Context(), messageID, alert))

		service.WaitForIncident().WaitFor(murphytest.Alerts(1))
		purged, err := storage.PurgeHandledMessages(t.Context(), time.Now().Add(time.Hour))
		require.NoError(t, err)
		assert.Equal(t, int64(1), purged, "the handled message is forgotten after its retention")
	})
}

func TestARedeliveredTimerTakesEffectOnce(t *testing.T) {
	testenv.EachDatabase(t, func(t *testing.T, engine testenv.DatabaseEngine) {
		databases := testenv.Databases(t, engine)
		service := murphyOn(t, databases).Service()
		incidents, storage := incidentsOn(t, databases)
		require.NoError(t, incidents.AlertReceived(t.Context(), murphy.NewID(), firingAlert(service)))
		expired := scheduledTimer(t, storage)

		require.NoError(t, incidents.TimerExpired(t.Context(), expired.GetId(), expired.GetTimer().GetCurrent()))
		require.NoError(t, incidents.TimerExpired(t.Context(), expired.GetId(), expired.GetTimer().GetCurrent()))

		incident, _, err := incidents.GetIncident(t.Context(), expired.GetTimer().GetCurrent().GetIncidentId())
		require.NoError(t, err)
		assert.Equal(t, int32(2), incident.GetVersion(), "the timer moved the incident on once")
	})
}

func TestAMessagePublishedBeforeACrashIsPublishedAgain(t *testing.T) {
	testenv.EachDatabase(t, func(t *testing.T, engine testenv.DatabaseEngine) {
		for name, broker := range brokers {
			t.Run(name, func(t *testing.T) {
				databases := testenv.Databases(t, engine)
				service := murphyOn(t, databases).Service()
				incidents, storage := incidentsOn(t, databases)
				bus := testenv.NewBus(t, broker(t), messaging.Topology{"engine": {"murphy.v1.incident": {"created"}}})
				created := bus.Collect("engine", murphy.IncidentVerb_INCIDENT_VERB_CREATED)
				require.NoError(t, incidents.AlertReceived(t.Context(), murphy.NewID(), firingAlert(service)))
				schedulerCrashesAfterPublishing(t, incidents, bus.Publisher)

				testenv.RunRelay(t, "incidents", incidents, bus.Publisher)

				require.EventuallyWithT(t, func(c *assert.CollectT) {
					remaining, err := storage.ClaimDueMessages(t.Context(), time.Now(), 10, time.Millisecond)
					assert.NoError(c, err)
					assert.Empty(c, remaining, "published again once the crashed claim expired, then deleted")
				}, murphytest.Settle, tick)
				require.EventuallyWithT(t, func(c *assert.CollectT) {
					assert.Len(c, slices.Compact(created.IDs()), 1, "delivered under one ID: NATS drops the repeat, SQS delivers it for the consumer to drop")
				}, murphytest.Settle, 50*time.Millisecond)
			})
		}
	})
}

const tick = 50 * time.Millisecond
