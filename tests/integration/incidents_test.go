//go:build integration

package integration

import (
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/client"
	migrateapp "github.com/murphyhq/server/cmd/migrate/app"
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
	service, err := api.CreateServiceWithResponse(ctx, nil, client.ServiceInput{Name: "checkout-api-" + suffix, EscalationPolicyId: policy.JSON201.Id})
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
		if entry.Escalation == nil && entry.Type != client.TimelineEntryTypeEscalationExhausted {
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

		catalogStorage, err := catalogdb.Open(ctx, databases.CatalogDatabaseURL)
		require.NoError(t, err)
		t.Cleanup(catalogStorage.Close)
		storage, err := incidentsdb.Open(ctx, databases.IncidentsDatabaseURL)
		require.NoError(t, err)
		t.Cleanup(storage.Close)
		service := incidents.New(storage, catalog.New(catalogStorage, nil), escalationFor(t, databases), nil, nil)

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

func escalationFor(t *testing.T, databases migrateapp.Settings) *escalation.Service {
	t.Helper()
	storage, err := escalationdb.Open(t.Context(), databases.EscalationDatabaseURL)
	require.NoError(t, err)
	t.Cleanup(storage.Close)
	return escalation.New(storage, nil, nil, nil)
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
				service, err := stack.API.CreateServiceWithResponse(ctx, nil, client.ServiceInput{Name: "checkout-api", EscalationPolicyId: policy.JSON201.Id})
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
