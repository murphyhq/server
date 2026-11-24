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

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/client"
	notifierhandlers "github.com/murphyhq/server/cmd/notifier/handlers"
	"github.com/murphyhq/server/internal/messaging"
	brokerpkg "github.com/murphyhq/server/internal/messaging/broker"
	"github.com/murphyhq/server/internal/pagination"
	"github.com/murphyhq/server/services/incidents"
	"github.com/murphyhq/server/services/notifications"
	"github.com/murphyhq/server/services/notifications/webhook"
	"github.com/murphyhq/server/tests/testenv"
)

type pagedService struct {
	user          string
	contactMethod string
	key           string
}

func pagedServiceWith(t *testing.T, stack testenv.Stack, methods ...client.ContactMethodInput) pagedService {
	t.Helper()
	ctx := t.Context()
	team := createTeam(t, stack.API, "payments")
	user := createUser(t, stack.API, "ana@example.com")
	out := pagedService{user: user}
	for _, method := range methods {
		created, err := stack.API.CreateContactMethodWithResponse(ctx, user, nil, method)
		require.NoError(t, err)
		require.Equal(t, http.StatusCreated, created.StatusCode())
		if out.contactMethod == "" {
			out.contactMethod = created.JSON201.Id
		}
	}
	policy, err := stack.API.CreateEscalationPolicyWithResponse(ctx, nil, policyInput("payments", team, client.EscalationTarget{Type: client.EscalationTargetTypeUser, Id: user}))
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, policy.StatusCode())
	service, err := stack.API.CreateServiceWithResponse(ctx, nil, client.ServiceInput{Name: "checkout-api", EscalationPolicy: client.Reference{Id: policy.JSON201.Id}})
	require.NoError(t, err)
	integration, err := stack.API.CreateIntegrationWithResponse(ctx, service.JSON201.Id, nil, client.IntegrationCreate{Name: "generic", Type: client.Generic})
	require.NoError(t, err)
	out.key = integration.JSON201.Key
	return out
}

func notificationEntries(t *testing.T, c *assert.CollectT, api *client.ClientWithResponses, id string) []client.TimelineEntry {
	got, err := api.GetIncidentWithResponse(t.Context(), id)
	require.NoError(c, err)
	var entries []client.TimelineEntry
	for _, entry := range got.JSON200.Timeline {
		if entry.Notification != nil {
			entries = append(entries, entry)
		}
	}
	return entries
}

func openIncident(t *testing.T, stack testenv.Stack, key string) client.Incident {
	t.Helper()
	send(t, stack, key, alertBody("disk-full", "firing"))
	var incident client.Incident
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		list := incidentsOf(t, stack.API)
		if assert.Len(c, list, 1) {
			incident = list[0]
		}
	}, settle, 100*time.Millisecond)
	return incident
}

func TestAPageReachesEveryContactMethodAndTheTimeline(t *testing.T) {
	testenv.EachDatabase(t, func(t *testing.T, engine testenv.DatabaseEngine) {
		for name, broker := range brokers {
			t.Run(name, func(t *testing.T) {
				stack := testenv.Murphy(t, broker(t), engine)
				receiver := testenv.NewWebhookReceiver(t, testenv.WebhookPublicKey)
				paged := pagedServiceWith(t, stack,
					client.ContactMethodInput{Type: client.Webhook, Address: receiver.URL + "/ana"},
					client.ContactMethodInput{Type: client.Email, Address: "ana@example.com"},
				)

				incident := openIncident(t, stack, paged.key)

				require.EventuallyWithT(t, func(c *assert.CollectT) {
					received := receiver.Received()
					if !assert.Len(c, received, 1) {
						return
					}
					assert.True(c, received[0].Verified, "the signature checks out with the public key")
					body := received[0].Body
					assert.Equal(c, webhook.IncidentPaged, body.Type)
					assert.Equal(c, incident.Id, body.Data.Incident.Id)
					assert.Equal(c, paged.user, body.Data.User.Id)
					assert.Equal(c, paged.contactMethod, body.Data.ContactMethod.Id)
					if assert.NotNil(c, body.Data.Escalation) {
						assert.Equal(c, int32(1), body.Data.Escalation.Level)
					}

					entries := notificationEntries(t, c, stack.API, incident.Id)
					types := map[client.TimelineEntryType]string{}
					for _, entry := range entries {
						types[entry.Type] = string(*entry.Notification.Channel)
					}
					assert.Equal(c, map[client.TimelineEntryType]string{
						client.TimelineEntryTypeNotified:           "webhook",
						client.TimelineEntryTypeNotificationFailed: "email",
					}, types, "the webhook is sent and email, which Murphy can't send yet, fails")
				}, settle, 100*time.Millisecond)
			})
		}
	})
}

func TestAcknowledgingAndResolvingTellEveryonePaged(t *testing.T) {
	testenv.EachDatabase(t, func(t *testing.T, engine testenv.DatabaseEngine) {
		for name, broker := range brokers {
			t.Run(name, func(t *testing.T) {
				stack := testenv.Murphy(t, broker(t), engine)
				receiver := testenv.NewWebhookReceiver(t, testenv.WebhookPublicKey)
				paged := pagedServiceWith(t, stack, client.ContactMethodInput{Type: client.Webhook, Address: receiver.URL})
				incident := openIncident(t, stack, paged.key)
				require.EventuallyWithT(t, func(c *assert.CollectT) {
					assert.Len(c, receiver.Received(), 1)
				}, settle, 100*time.Millisecond)

				acknowledged, err := stack.API.AcknowledgeIncidentWithResponse(t.Context(), incident.Id, nil)
				require.NoError(t, err)
				require.Equal(t, http.StatusOK, acknowledged.StatusCode())
				resolved, err := stack.API.ResolveIncidentWithResponse(t.Context(), incident.Id, nil)
				require.NoError(t, err)
				require.Equal(t, http.StatusOK, resolved.StatusCode())

				require.EventuallyWithT(t, func(c *assert.CollectT) {
					var types []webhook.WebhookType
					for _, received := range receiver.Received() {
						assert.True(c, received.Verified)
						types = append(types, received.Body.Type)
					}
					assert.ElementsMatch(c, []webhook.WebhookType{webhook.IncidentPaged, webhook.IncidentAcknowledged, webhook.IncidentResolved}, types)
				}, settle, 100*time.Millisecond)
			})
		}
	})
}

func TestAFailingWebhookIsRetried(t *testing.T) {
	testenv.EachDatabase(t, func(t *testing.T, engine testenv.DatabaseEngine) {
		stack := testenv.Murphy(t, testenv.NATS(), engine)
		receiver := testenv.NewWebhookReceiver(t, testenv.WebhookPublicKey)
		receiver.FailWith(http.StatusServiceUnavailable)
		paged := pagedServiceWith(t, stack, client.ContactMethodInput{Type: client.Webhook, Address: receiver.URL})
		incident := openIncident(t, stack, paged.key)

		require.EventuallyWithT(t, func(c *assert.CollectT) {
			assert.Len(c, receiver.Received(), 1)
			entries := notificationEntries(t, c, stack.API, incident.Id)
			if assert.Len(c, entries, 1) {
				assert.Equal(c, client.TimelineEntryTypeNotified, entries[0].Type)
			}
		}, settle, 100*time.Millisecond)
	})
}

func TestARefusedWebhookFailsAtOnce(t *testing.T) {
	testenv.EachDatabase(t, func(t *testing.T, engine testenv.DatabaseEngine) {
		stack := testenv.Murphy(t, testenv.NATS(), engine)
		receiver := testenv.NewWebhookReceiver(t, testenv.WebhookPublicKey)
		receiver.FailWith(http.StatusGone, http.StatusGone)
		paged := pagedServiceWith(t, stack, client.ContactMethodInput{Type: client.Webhook, Address: receiver.URL})
		incident := openIncident(t, stack, paged.key)

		require.EventuallyWithT(t, func(c *assert.CollectT) {
			entries := notificationEntries(t, c, stack.API, incident.Id)
			if assert.Len(c, entries, 1) {
				assert.Equal(c, client.TimelineEntryTypeNotificationFailed, entries[0].Type)
				assert.Equal(c, "the receiver answered 410", *entries[0].Notification.Failure)
			}
		}, settle, 100*time.Millisecond)
		assert.Empty(t, receiver.Received(), "a refused webhook is not sent again")
	})
}

func TestSomeonePagedWithoutContactMethodsIsOnTheTimeline(t *testing.T) {
	testenv.EachDatabase(t, func(t *testing.T, engine testenv.DatabaseEngine) {
		stack := testenv.Murphy(t, testenv.NATS(), engine)
		paged := pagedServiceWith(t, stack)
		incident := openIncident(t, stack, paged.key)

		require.EventuallyWithT(t, func(c *assert.CollectT) {
			entries := notificationEntries(t, c, stack.API, incident.Id)
			if assert.Len(c, entries, 1) {
				assert.Equal(c, client.TimelineEntryTypeNotificationFailed, entries[0].Type)
				assert.Equal(c, paged.user, entries[0].Notification.User.Id)
				assert.Nil(c, entries[0].Notification.ContactMethod)
			}
		}, settle, 100*time.Millisecond)
	})
}

func TestTheAPIServesThePublicSigningKeys(t *testing.T) {
	api, _ := testenv.API(t, testenv.PostgresDatabase)

	got, err := api.GetSigningKeysWithResponse(t.Context())

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, got.StatusCode())
	assert.Equal(t, []client.SigningKey{{PublicKey: testenv.WebhookPublicKey}}, got.JSON200.Keys)
}

func TestARedeliveredNotificationOutcomeIsRecordedOnce(t *testing.T) {
	testenv.EachDatabase(t, func(t *testing.T, engine testenv.DatabaseEngine) {
		ctx := t.Context()
		databases := testenv.Databases(t, engine)
		api, _ := testenv.APIWith(t, databases)
		serviceID, integrationID, _ := integrationKey(t, api)
		service, _ := incidentsFor(t, databases)
		require.NoError(t, service.AlertReceived(ctx, murphy.NewID(), firingAlert(serviceID, integrationID)))
		opened, _, err := service.ListIncidents(ctx, incidents.Filter{}, pagination.Page{})
		require.NoError(t, err)
		require.Len(t, opened, 1)
		sent := &murphy.Notification{
			Id: murphy.NewID(), Reason: murphy.NotificationReason_NOTIFICATION_REASON_PAGED, Incident: opened[0], UserId: murphy.NewID(),
			ContactMethod: &murphy.ContactMethod{Id: murphy.NewID(), Type: murphy.ContactMethodType_CONTACT_METHOD_TYPE_WEBHOOK}, SentAt: murphy.Now(),
		}

		messageID := murphy.NewID()
		require.NoError(t, service.NotificationSent(ctx, messageID, sent))
		require.NoError(t, service.NotificationSent(ctx, messageID, sent))

		_, timeline, err := service.GetIncident(ctx, opened[0].GetId())
		require.NoError(t, err)
		notified := 0
		for _, entry := range timeline {
			if entry.GetType() == murphy.TimelineEntryType_TIMELINE_ENTRY_TYPE_NOTIFIED {
				notified++
			}
		}
		assert.Equal(t, 1, notified)
	})
}

type crashingPublisher struct {
	publisher *messaging.Publisher
	after     bool
	crashed   atomic.Bool
}

func (p *crashingPublisher) Publish(ctx context.Context, envelope *murphy.Envelope) error {
	if !p.after && p.crashed.CompareAndSwap(false, true) {
		return errors.New("notifier crashed before announcing the outcome")
	}
	if err := p.publisher.Publish(ctx, envelope); err != nil {
		return err
	}
	if p.after && p.crashed.CompareAndSwap(false, true) {
		return errors.New("notifier crashed after announcing the outcome, before acknowledging")
	}
	return nil
}

func TestANotifierCrashSendsTheWebhookAgainWithTheSameID(t *testing.T) {
	for name, broker := range brokers {
		for crash, after := range map[string]bool{"before announcing": false, "after announcing": true} {
			t.Run(name+"/"+crash, func(t *testing.T) {
				ctx := t.Context()
				endpoints := broker(t).Provision(t, messaging.Topology{
					"notifier": {"murphy.v1.notification": {"created"}},
					"engine":   {"murphy.v1.notification": {"sent"}},
				}, messaging.TopologyVersion)
				topic, err := brokerpkg.OpenTopic(ctx, endpoints.Topic)
				require.NoError(t, err)
				t.Cleanup(func() { _ = topic.Close() })
				publisher := messaging.NewPublisher(topic)

				keys, err := webhook.ParseSigningKeys(testenv.WebhookSigningKey)
				require.NoError(t, err)
				crashing := &crashingPublisher{publisher: publisher, after: after}
				service := notifications.New(crashing, map[murphy.ContactMethodType]notifications.Provider{
					murphy.ContactMethodType_CONTACT_METHOD_TYPE_WEBHOOK: webhook.New(keys),
				})
				notifierSubscription, err := brokerpkg.OpenSubscription(ctx, endpoints.Subscriptions["notifier"])
				require.NoError(t, err)
				t.Cleanup(func() { _ = notifierSubscription.Close() })
				notifier, err := messaging.Consume(ctx, notifierSubscription, "notifier", notifierhandlers.New(service))
				require.NoError(t, err)
				t.Cleanup(notifier.Stop)

				var mu sync.Mutex
				var outcomes []string
				engineSubscription, err := brokerpkg.OpenSubscription(ctx, endpoints.Subscriptions["engine"])
				require.NoError(t, err)
				t.Cleanup(func() { _ = engineSubscription.Close() })
				engine, err := messaging.Consume(ctx, engineSubscription, "engine", messaging.Handlers{
					messaging.On(murphy.NotificationVerb_NOTIFICATION_VERB_SENT, func(_ context.Context, envelope *murphy.Envelope) error {
						mu.Lock()
						defer mu.Unlock()
						outcomes = append(outcomes, envelope.GetId())
						return nil
					}),
				})
				require.NoError(t, err)
				t.Cleanup(engine.Stop)

				receiver := testenv.NewWebhookReceiver(t, testenv.WebhookPublicKey)
				created := &murphy.Notification{
					Id: murphy.NewID(), Reason: murphy.NotificationReason_NOTIFICATION_REASON_PAGED, Incident: &murphy.Incident{Id: murphy.NewID()}, UserId: murphy.NewID(),
					ContactMethod: &murphy.ContactMethod{Id: murphy.NewID(), Type: murphy.ContactMethodType_CONTACT_METHOD_TYPE_WEBHOOK, Address: receiver.URL}, CreatedAt: murphy.Now(),
				}
				require.NoError(t, publisher.Publish(ctx, &murphy.Envelope{
					Id: murphy.NewID(), OccurredAt: murphy.Now(),
					Event: &murphy.Envelope_Notification{Notification: &murphy.NotificationEvent{Verb: murphy.NotificationVerb_NOTIFICATION_VERB_CREATED, Current: created}},
				}))

				require.EventuallyWithT(t, func(c *assert.CollectT) {
					received := receiver.Received()
					if assert.Len(c, received, 2, "sent once before the crash and once on redelivery") {
						assert.Equal(c, created.GetId(), received[0].ID)
						assert.Equal(c, received[0].ID, received[1].ID, "so the receiver can drop the repeat")
					}
					mu.Lock()
					defer mu.Unlock()
					if assert.NotEmpty(c, outcomes) {
						for _, id := range outcomes {
							assert.Equal(c, outcomes[0], id, "every announcement of the outcome has the same ID, so engine records it once")
						}
					}
				}, settle, 100*time.Millisecond)
			})
		}
	}
}
