//go:build integration

package integration

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/client"
	migrateapp "github.com/murphyhq/server/cmd/migrate/app"
	"github.com/murphyhq/server/internal/messaging"
	brokerpkg "github.com/murphyhq/server/internal/messaging/broker"
	"github.com/murphyhq/server/services/catalog"
	catalogdb "github.com/murphyhq/server/services/catalog/db"
	"github.com/murphyhq/server/services/escalation"
	escalationdb "github.com/murphyhq/server/services/escalation/db"
	"github.com/murphyhq/server/services/incidents"
	incidentsdb "github.com/murphyhq/server/services/incidents/db"
	"github.com/murphyhq/server/tests/testenv"
)

func integrationKey(t *testing.T, api *client.ClientWithResponses) (serviceID, integrationID, key string) {
	t.Helper()
	return integrationOf(t, api, client.Generic)
}

func integrationOf(t *testing.T, api *client.ClientWithResponses, kind client.IntegrationType) (serviceID, integrationID, key string) {
	t.Helper()
	ctx := t.Context()
	suffix := murphy.NewID()[24:]
	team := createTeam(t, api, "payments-"+suffix)
	user := createUser(t, api, "ana-"+suffix+"@example.com")
	policy, err := api.CreateEscalationPolicyWithResponse(ctx, nil, policyInput("payments-"+suffix, team, client.EscalationTarget{Type: client.EscalationTargetTypeUser, Id: user}))
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, policy.StatusCode())
	service, err := api.CreateServiceWithResponse(ctx, nil, client.ServiceInput{Name: "checkout-api-" + suffix, EscalationPolicy: client.Reference{Id: policy.JSON201.Id}})
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, service.StatusCode())
	integration, err := api.CreateIntegrationWithResponse(ctx, service.JSON201.Id, nil, client.IntegrationCreate{Name: string(kind), Type: kind})
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, integration.StatusCode())
	return service.JSON201.Id, integration.JSON201.Id, integration.JSON201.Key
}

func alertBody(dedupKey, status string) map[string]any {
	return map[string]any{"dedup_key": dedupKey, "status": status, "summary": dedupKey + " is " + status}
}

func send(t *testing.T, stack testenv.Stack, key string, body map[string]any) {
	t.Helper()
	resp, _ := postAlert(t, stack.IngestURL, "Bearer "+key, body)
	require.Equal(t, http.StatusAccepted, resp.StatusCode)
}

func incidentsOf(t *testing.T, api *client.ClientWithResponses) []client.Incident {
	t.Helper()
	list, err := api.ListIncidentsWithResponse(t.Context(), nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, list.StatusCode())
	return list.JSON200.Items
}

func alertEntries(t *testing.T, api *client.ClientWithResponses, id string) []client.TimelineEntryType {
	t.Helper()
	got, err := api.GetIncidentWithResponse(t.Context(), id)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, got.StatusCode())
	var types []client.TimelineEntryType
	for _, entry := range got.JSON200.Timeline {
		if entry.Escalation == nil && entry.Notification == nil && entry.Type != client.TimelineEntryTypeEscalationExhausted {
			types = append(types, entry.Type)
		}
	}
	return types
}

const settle = 15 * time.Second

func TestAlertsOpenJoinAndResolveIncidents(t *testing.T) {
	testenv.EachDatabase(t, func(t *testing.T, engine testenv.DatabaseEngine) {
		for name, broker := range brokers {
			t.Run(name, func(t *testing.T) {
				stack := testenv.Murphy(t, broker(t), engine)
				_, _, key := integrationKey(t, stack.API)

				send(t, stack, key, alertBody("disk-full", "firing"))
				send(t, stack, key, alertBody("cpu-high", "firing"))
				require.EventuallyWithT(t, func(c *assert.CollectT) {
					assert.Len(c, incidentsOf(t, stack.API), 2)
				}, settle, 100*time.Millisecond)

				var diskID string
				for _, incident := range incidentsOf(t, stack.API) {
					if incident.DedupKey == "disk-full" {
						diskID = incident.Id
					}
				}
				send(t, stack, key, alertBody("disk-full", "firing"))
				require.EventuallyWithT(t, func(c *assert.CollectT) {
					assert.Len(c, alertEntries(t, stack.API, diskID), 2)
				}, settle, 100*time.Millisecond)
				time.Sleep(1100 * time.Millisecond)
				send(t, stack, key, alertBody("disk-full", "resolved"))

				var disk client.Incident
				require.EventuallyWithT(t, func(c *assert.CollectT) {
					for _, incident := range incidentsOf(t, stack.API) {
						if incident.DedupKey == "disk-full" {
							disk = incident
						}
					}
					assert.Equal(c, client.IncidentStatusResolved, disk.Status)
				}, settle, 100*time.Millisecond)

				assert.Equal(t, []client.TimelineEntryType{client.TimelineEntryTypeTriggered, client.TimelineEntryTypeAlerted, client.TimelineEntryTypeResolved}, alertEntries(t, stack.API, disk.Id))
				assert.NotNil(t, disk.ResolvedAt)
				assert.Equal(t, client.Urgency("high"), disk.Urgency)

				open, err := stack.API.ListIncidentsWithResponse(t.Context(), &client.ListIncidentsParams{Status: &[]client.IncidentStatus{client.IncidentStatusTriggered}})
				require.NoError(t, err)
				require.Len(t, open.JSON200.Items, 1)
				assert.Equal(t, "cpu-high", open.JSON200.Items[0].DedupKey)
				assert.NotEqual(t, disk.Number, open.JSON200.Items[0].Number)
			})
		}
	})
}

func TestConcurrentAlertsOpenOneIncident(t *testing.T) {
	testenv.EachDatabase(t, func(t *testing.T, engine testenv.DatabaseEngine) {
		stack := testenv.Murphy(t, testenv.NATS(), engine)
		_, _, key := integrationKey(t, stack.API)

		var wg sync.WaitGroup
		for range 10 {
			wg.Go(func() { send(t, stack, key, alertBody("disk-full", "firing")) })
		}
		wg.Wait()

		require.EventuallyWithT(t, func(c *assert.CollectT) {
			list := incidentsOf(t, stack.API)
			if assert.Len(c, list, 1) {
				assert.Len(c, alertEntries(t, stack.API, list[0].Id), 10)
			}
		}, settle, 100*time.Millisecond)
	})
}

func TestAResolveRacingARefireNeverClosesTheIncident(t *testing.T) {
	testenv.EachDatabase(t, func(t *testing.T, engine testenv.DatabaseEngine) {
		stack := testenv.Murphy(t, testenv.NATS(), engine)
		_, _, key := integrationKey(t, stack.API)
		send(t, stack, key, alertBody("disk-full", "firing"))
		require.EventuallyWithT(t, func(c *assert.CollectT) {
			assert.Len(c, incidentsOf(t, stack.API), 1)
		}, settle, 100*time.Millisecond)

		var wg sync.WaitGroup
		wg.Go(func() { send(t, stack, key, alertBody("disk-full", "resolved")) })
		wg.Go(func() { send(t, stack, key, alertBody("disk-full", "firing")) })
		wg.Wait()

		require.EventuallyWithT(t, func(c *assert.CollectT) {
			total := 0
			for _, incident := range incidentsOf(t, stack.API) {
				total += len(alertEntries(t, stack.API, incident.Id))
			}
			assert.Equal(c, 3, total)
		}, settle, 100*time.Millisecond)
		open, err := stack.API.ListIncidentsWithResponse(t.Context(), &client.ListIncidentsParams{Status: &[]client.IncidentStatus{client.IncidentStatusTriggered}})
		require.NoError(t, err)
		assert.Len(t, open.JSON200.Items, 1)
	})
}

func TestAlertsForADisabledIntegrationOpenNothing(t *testing.T) {
	testenv.EachDatabase(t, func(t *testing.T, engine testenv.DatabaseEngine) {
		stack := testenv.Murphy(t, testenv.NATS(), engine)
		_, integrationID, key := integrationKey(t, stack.API)
		disabled := client.IntegrationStatusDisabled
		updated, err := stack.API.UpdateIntegrationWithResponse(t.Context(), integrationID, client.IntegrationUpdate{Name: "generic", Status: &disabled})
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, updated.StatusCode())

		send(t, stack, key, alertBody("disk-full", "firing"))

		time.Sleep(2 * time.Second)
		assert.Empty(t, incidentsOf(t, stack.API))
	})
}

func TestARedeliveredAlertTakesEffectOnce(t *testing.T) {
	testenv.EachDatabase(t, func(t *testing.T, engine testenv.DatabaseEngine) {
		ctx := t.Context()
		databases := testenv.Databases(t, engine)
		api, _ := testenv.APIWith(t, databases)
		serviceID, integrationID, _ := integrationKey(t, api)

		service, storage := incidentsFor(t, databases)

		alert := &murphy.Alert{
			Id:            murphy.NewID(),
			IntegrationId: integrationID,
			ServiceId:     serviceID,
			DedupKey:      "disk-full",
			Status:        murphy.AlertStatus_ALERT_STATUS_FIRING,
			Summary:       "Disk almost full",
			KeyVersion:    1,
			ReceivedAt:    murphy.Now(),
		}
		messageID := murphy.NewID()
		require.NoError(t, service.AlertReceived(ctx, messageID, alert))
		require.NoError(t, service.AlertReceived(ctx, messageID, alert))

		list := incidentsOf(t, api)
		require.Len(t, list, 1)
		assert.Len(t, alertEntries(t, api, list[0].Id), 1)

		purged, err := storage.PurgeHandledMessages(ctx, time.Now().Add(time.Hour))
		require.NoError(t, err)
		assert.Equal(t, int64(1), purged)
	})
}

func incidentsFor(t *testing.T, databases migrateapp.Settings) (*incidents.Service, incidentsdb.Storage) {
	t.Helper()
	ctx := t.Context()
	catalogStorage, err := catalogdb.Open(ctx, databases.CatalogDatabaseURL)
	require.NoError(t, err)
	t.Cleanup(catalogStorage.Close)
	escalationStorage, err := escalationdb.Open(ctx, databases.EscalationDatabaseURL)
	require.NoError(t, err)
	t.Cleanup(escalationStorage.Close)
	storage, err := incidentsdb.Open(ctx, databases.IncidentsDatabaseURL)
	require.NoError(t, err)
	t.Cleanup(storage.Close)

	return incidents.New(storage, catalog.New(catalogStorage, nil), escalation.New(escalationStorage, nil, nil, nil)), storage
}

func TestAnIncidentPagesItsFirstLevelUntilAcknowledged(t *testing.T) {
	testenv.EachDatabase(t, func(t *testing.T, engine testenv.DatabaseEngine) {
		for name, broker := range brokers {
			t.Run(name, func(t *testing.T) {
				stack := testenv.Murphy(t, broker(t), engine)
				ctx := t.Context()
				team := createTeam(t, stack.API, "payments")
				ana := createUser(t, stack.API, "ana@example.com")
				schedule := createSchedule(t, stack.API, "primary", []string{team}, ana)
				policy, err := stack.API.CreateEscalationPolicyWithResponse(ctx, nil, policyInput("payments", team,
					client.EscalationTarget{Type: client.EscalationTargetTypeSchedule, Id: schedule}))
				require.NoError(t, err)
				require.Equal(t, http.StatusCreated, policy.StatusCode())
				service, err := stack.API.CreateServiceWithResponse(ctx, nil, client.ServiceInput{Name: "checkout-api", EscalationPolicy: client.Reference{Id: policy.JSON201.Id}})
				require.NoError(t, err)
				integration, err := stack.API.CreateIntegrationWithResponse(ctx, service.JSON201.Id, nil, client.IntegrationCreate{Name: "generic", Type: client.Generic})
				require.NoError(t, err)

				send(t, stack, integration.JSON201.Key, alertBody("disk-full", "firing"))

				var incident client.Incident
				require.EventuallyWithT(t, func(c *assert.CollectT) {
					list := incidentsOf(t, stack.API)
					if !assert.Len(c, list, 1) {
						return
					}
					incident = list[0]
					got, err := stack.API.GetIncidentWithResponse(ctx, incident.Id)
					require.NoError(c, err)
					var paged []client.Page
					for _, entry := range got.JSON200.Timeline {
						if entry.Type == client.TimelineEntryTypeEscalated {
							paged = append(paged, entry.Escalation.Paged...)
						}
					}
					if assert.Len(c, paged, 1) {
						assert.Equal(c, ana, paged[0].User.Id)
						assert.Equal(c, schedule, paged[0].Via.Id)
					}
				}, settle, 100*time.Millisecond)

				acknowledged, err := stack.API.AcknowledgeIncidentWithResponse(ctx, incident.Id, nil)
				require.NoError(t, err)
				require.Equal(t, http.StatusOK, acknowledged.StatusCode())
				assert.Equal(t, client.IncidentStatusAcknowledged, acknowledged.JSON200.Status)
				require.NotNil(t, acknowledged.JSON200.Escalation)
				assert.Equal(t, int32(1), acknowledged.JSON200.Escalation.Level)
				assert.Equal(t, policy.JSON201.Version, acknowledged.JSON200.Escalation.Policy.Version)

				again, err := stack.API.AcknowledgeIncidentWithResponse(ctx, incident.Id, nil)
				require.NoError(t, err)
				assert.Equal(t, acknowledged.JSON200.UpdatedAt, again.JSON200.UpdatedAt, "acknowledging again changes nothing")

				resolved, err := stack.API.ResolveIncidentWithResponse(ctx, incident.Id, nil)
				require.NoError(t, err)
				assert.Equal(t, client.IncidentStatusResolved, resolved.JSON200.Status)
			})
		}
	})
}

func firingAlert(serviceID, integrationID string) *murphy.Alert {
	return &murphy.Alert{
		Id: murphy.NewID(), IntegrationId: integrationID, ServiceId: serviceID, DedupKey: "disk-full",
		Status: murphy.AlertStatus_ALERT_STATUS_FIRING, Summary: "Disk almost full", KeyVersion: 1, ReceivedAt: murphy.Now(),
	}
}

func TestARedeliveredTimerTakesEffectOnce(t *testing.T) {
	testenv.EachDatabase(t, func(t *testing.T, engine testenv.DatabaseEngine) {
		ctx := t.Context()
		databases := testenv.Databases(t, engine)
		api, _ := testenv.APIWith(t, databases)
		serviceID, integrationID, _ := integrationKey(t, api)
		service, storage := incidentsFor(t, databases)
		require.NoError(t, service.AlertReceived(ctx, murphy.NewID(), firingAlert(serviceID, integrationID)))

		due, err := storage.ClaimDueMessages(ctx, time.Now().Add(24*time.Hour), 10, time.Minute)
		require.NoError(t, err)
		var expired *murphy.Envelope
		for _, message := range due {
			envelope := &murphy.Envelope{}
			require.NoError(t, proto.Unmarshal(message.Envelope, envelope))
			if envelope.GetTimer() != nil {
				expired = envelope
			}
		}
		require.NotNil(t, expired, "opening the incident scheduled the timer for after its first level")

		require.NoError(t, service.TimerExpired(ctx, expired.GetId(), expired.GetTimer().GetCurrent()))
		require.NoError(t, service.TimerExpired(ctx, expired.GetId(), expired.GetTimer().GetCurrent()))

		incident, _, err := service.GetIncident(ctx, expired.GetTimer().GetCurrent().GetIncidentId())
		require.NoError(t, err)
		assert.Equal(t, int32(2), incident.GetVersion(), "the timer moved the incident on once")
	})
}

type crashBeforeDeleting struct {
	messaging.ScheduledMessages
	crashed atomic.Bool
	deleted atomic.Bool
}

func (c *crashBeforeDeleting) MessagesSent(ctx context.Context, ids []string) error {
	if c.crashed.CompareAndSwap(false, true) {
		return errors.New("scheduler crashed before deleting what it published")
	}
	if err := c.ScheduledMessages.MessagesSent(ctx, ids); err != nil {
		return err
	}
	if len(ids) > 0 {
		c.deleted.Store(true)
	}
	return nil
}

func TestAMessagePublishedBeforeACrashIsPublishedAgain(t *testing.T) {
	testenv.EachDatabase(t, func(t *testing.T, engine testenv.DatabaseEngine) {
		for name, broker := range brokers {
			t.Run(name, func(t *testing.T) {
				ctx := t.Context()
				databases := testenv.Databases(t, engine)
				api, _ := testenv.APIWith(t, databases)
				serviceID, integrationID, _ := integrationKey(t, api)
				service, storage := incidentsFor(t, databases)
				endpoints := broker(t).Provision(t, messaging.Topology{"engine": {"murphy.v1.incident": {"created"}}}, messaging.TopologyVersion)
				topic, err := brokerpkg.OpenTopic(ctx, endpoints.Topic)
				require.NoError(t, err)
				t.Cleanup(func() { _ = topic.Close() })
				subscription, err := brokerpkg.OpenSubscription(ctx, endpoints.Subscriptions["engine"])
				require.NoError(t, err)
				t.Cleanup(func() { _ = subscription.Close() })

				var deliveries atomic.Int32
				var createdID atomic.Value
				consumer, err := messaging.Consume(ctx, subscription, "engine", messaging.Handlers{
					messaging.On(murphy.IncidentVerb_INCIDENT_VERB_CREATED, func(_ context.Context, envelope *murphy.Envelope) error {
						createdID.Store(envelope.GetId())
						deliveries.Add(1)
						return nil
					}),
				})
				require.NoError(t, err)
				t.Cleanup(consumer.Stop)

				require.NoError(t, service.AlertReceived(ctx, murphy.NewID(), firingAlert(serviceID, integrationID)))
				source := &crashBeforeDeleting{ScheduledMessages: service}
				relayCtx, stop := context.WithCancel(ctx)
				done := make(chan error, 1)
				go func() {
					done <- messaging.Relay("incidents", source, messaging.NewPublisher(topic),
						messaging.RelaySettings{PollInterval: 50 * time.Millisecond, BatchSize: 10, ClaimLease: 300 * time.Millisecond})(relayCtx)
				}()

				require.Eventually(t, source.deleted.Load, settle, 50*time.Millisecond, "published again once its claim expired, then deleted")
				stop()
				require.NoError(t, <-done)
				assert.True(t, source.crashed.Load())
				remaining, err := storage.ClaimDueMessages(ctx, time.Now().Add(time.Second), 10, time.Minute)
				require.NoError(t, err)
				assert.Empty(t, remaining)
				assert.GreaterOrEqual(t, deliveries.Load(), int32(1), "delivered: NATS drops the repeated publish by ID, SQS delivers it for the consumer to drop")
				assert.NotEmpty(t, createdID.Load())
			})
		}
	})
}
