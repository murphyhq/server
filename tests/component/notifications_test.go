//go:build integration

package component

import (
	"context"
	"net/netip"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/client"
	migrateapp "github.com/murphyhq/server/cmd/migrate/app"
	"github.com/murphyhq/server/internal/messaging"
	"github.com/murphyhq/server/services/escalation"
	escalationdb "github.com/murphyhq/server/services/escalation/db"
	"github.com/murphyhq/server/services/notifications"
	notificationsdb "github.com/murphyhq/server/services/notifications/db"
	"github.com/murphyhq/server/services/notifications/providers/webhook"
	"github.com/murphyhq/server/services/oncall"
	oncalldb "github.com/murphyhq/server/services/oncall/db"
	"github.com/murphyhq/server/services/people"
	peopledb "github.com/murphyhq/server/services/people/db"
	"github.com/murphyhq/server/tests/murphytest"
	"github.com/murphyhq/server/tests/testenv"
)

func notificationsOn(t *testing.T, databases migrateapp.Settings) (*notifications.Service, notificationsdb.Storage) {
	t.Helper()
	storage, err := notificationsdb.Open(t.Context(), databases.NotificationsDatabaseURL)
	require.NoError(t, err)
	t.Cleanup(storage.Close)
	peopleStorage, err := peopledb.Open(t.Context(), databases.PeopleDatabaseURL)
	require.NoError(t, err)
	t.Cleanup(peopleStorage.Close)
	oncallStorage, err := oncalldb.Open(t.Context(), databases.OnCallDatabaseURL)
	require.NoError(t, err)
	t.Cleanup(oncallStorage.Close)
	escalationStorage, err := escalationdb.Open(t.Context(), databases.EscalationDatabaseURL)
	require.NoError(t, err)
	t.Cleanup(escalationStorage.Close)
	peopleService := people.New(peopleStorage)
	return notifications.New(storage, escalation.New(escalationStorage, nil, nil, nil, 0), oncall.New(oncallStorage, peopleService), peopleService), storage
}

func webhookTo(address string) *murphy.ContactMethod {
	return &murphy.ContactMethod{Id: murphy.NewID(), Type: murphy.ContactMethodType_CONTACT_METHOD_TYPE_WEBHOOK, Address: address}
}

func TestARedeliveredNotificationOutcomeIsRecordedOnce(t *testing.T) {
	testenv.EachDatabase(t, func(t *testing.T, engine testenv.DatabaseEngine) {
		databases := testenv.Databases(t, engine)
		service := murphyOn(t, databases).Service()
		incidents, _ := incidentsOn(t, databases)
		require.NoError(t, incidents.AlertReceived(t.Context(), murphy.NewID(), firingAlert(service)))
		incident := service.WaitForIncident()
		sent := &murphy.Notification{
			Id: murphy.NewID(), Reason: murphy.NotificationReason_NOTIFICATION_REASON_PAGED, Incident: &murphy.Incident{Id: incident.ID},
			UserId: murphy.NewID(), ContactMethod: webhookTo("https://example.com/hook"), SentAt: murphy.Now(),
		}
		messageID := murphy.NewID()

		require.NoError(t, incidents.NotificationSent(t.Context(), messageID, sent))
		require.NoError(t, incidents.NotificationSent(t.Context(), messageID, sent))

		incident.WaitFor(murphytest.Entries(client.TimelineEntryTypeNotified))
	})
}

func TestARedeliveredEscalationPagesOnce(t *testing.T) {
	testenv.EachDatabase(t, func(t *testing.T, engine testenv.DatabaseEngine) {
		databases := testenv.Databases(t, engine)
		m := murphyOn(t, databases)
		ana := m.User(murphytest.WithPhone())
		policy := m.Policy(murphytest.Pages(ana.Target()))
		notifications, storage := notificationsOn(t, databases)
		incident := &murphy.Incident{
			Id: murphy.NewID(), Status: murphy.IncidentStatus_INCIDENT_STATUS_TRIGGERED,
			EscalationPolicyId: policy.ID, EscalationPolicyVersion: policy.Version, EscalationLevel: 1, EscalationRound: 1,
		}
		messageID := murphy.NewID()

		require.NoError(t, notifications.IncidentEscalated(t.Context(), messageID, incident))
		require.NoError(t, notifications.IncidentEscalated(t.Context(), messageID, incident))

		due, err := storage.ClaimDueMessages(t.Context(), time.Now().Add(time.Second), 10, time.Minute)
		require.NoError(t, err)
		assert.Len(t, due, 2, "one page and one notification, not two of each")
		users, err := storage.RecipientsOf(t.Context(), incident.GetId())
		require.NoError(t, err)
		assert.Equal(t, []string{ana.ID}, users)
	})
}

func TestANotifierCrashSendsTheWebhookAgainWithTheSameID(t *testing.T) {
	keys, err := webhook.ParseSigningKeys(testenv.WebhookSigningKey)
	require.NoError(t, err)
	provider := webhook.New(keys, []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8"), netip.MustParsePrefix("::1/128")})
	crashes := map[string]func(t *testing.T, sender *notifications.Sender, created *murphy.Envelope){
		"after sending, before announcing": func(t *testing.T, _ *notifications.Sender, created *murphy.Envelope) {
			require.NoError(t, provider.Send(t.Context(), created.GetNotification().GetCurrent()))
		},
		"after announcing, before acknowledging": func(t *testing.T, sender *notifications.Sender, created *murphy.Envelope) {
			require.NoError(t, sender.Send(t.Context(), created.GetId(), created.GetNotification().GetCurrent()))
		},
	}
	for name, broker := range brokers {
		for crash, crashed := range crashes {
			t.Run(name+"/"+crash, func(t *testing.T) {
				bus := testenv.NewBus(t, broker(t), messaging.Topology{
					"notifier": {"murphy.v1.notification": {"created"}},
					"engine":   {"murphy.v1.notification": {"sent"}},
				})
				outcomes := bus.Collect("engine", murphy.NotificationVerb_NOTIFICATION_VERB_SENT)
				sender := notifications.NewSender(bus.Publisher, map[murphy.ContactMethodType]notifications.Provider{
					murphy.ContactMethodType_CONTACT_METHOD_TYPE_WEBHOOK: provider,
				})
				hook := murphytest.NewReceiver(t, "http://localhost:8092", testenv.WebhookPublicKey).Webhook()
				created := notificationCreated(hook.Address)
				crashed(t, sender, created)

				bus.Consume("notifier", messaging.Handlers{
					messaging.On(murphy.NotificationVerb_NOTIFICATION_VERB_CREATED, func(ctx context.Context, envelope *murphy.Envelope) error {
						return sender.Send(ctx, envelope.GetId(), envelope.GetNotification().GetCurrent())
					}),
				})
				require.NoError(t, bus.Publisher.Publish(t.Context(), created))

				received := hook.WaitForWebhooks(2)
				id := created.GetNotification().GetCurrent().GetId()
				assert.Equal(t, []string{id, id}, []string{received[0].ID, received[1].ID}, "sent again on redelivery, with the same webhook-id")
				require.EventuallyWithT(t, func(c *assert.CollectT) {
					assert.Len(c, slices.Compact(outcomes.IDs()), 1, "every announcement of the outcome has the same ID, so engine records it once")
				}, murphytest.Settle, tick)
			})
		}
	}
}

func notificationCreated(address string) *murphy.Envelope {
	return &murphy.Envelope{
		Id: murphy.NewID(), OccurredAt: murphy.Now(),
		Event: &murphy.Envelope_Notification{Notification: &murphy.NotificationEvent{
			Verb: murphy.NotificationVerb_NOTIFICATION_VERB_CREATED,
			Current: &murphy.Notification{
				Id: murphy.NewID(), Reason: murphy.NotificationReason_NOTIFICATION_REASON_PAGED, Incident: &murphy.Incident{Id: murphy.NewID()},
				UserId: murphy.NewID(), ContactMethod: webhookTo(address), CreatedAt: murphy.Now(),
			},
		}},
	}
}
