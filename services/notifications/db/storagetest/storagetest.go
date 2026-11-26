package storagetest

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/services/notifications"
)

func Run(t *testing.T, open func(t *testing.T) notifications.Repository) {
	t.Run("records a page, its notifications and messages once per message", func(t *testing.T) {
		repo := open(t)
		incident := &murphy.Incident{Id: murphy.NewID()}
		ana, bo := murphy.NewID(), murphy.NewID()
		page := newPage(incident, ana, bo)
		change := &notifications.Change{
			MessageID:     murphy.NewID(),
			Page:          page,
			Notifications: []*murphy.Notification{newNotification(incident, page, ana), newNotification(incident, page, bo)},
			Messages:      []*notifications.ScheduledMessage{{ID: murphy.NewID(), DueAt: time.Now(), Envelope: []byte("page created")}},
		}

		first, err := repo.Record(t.Context(), change)
		require.NoError(t, err)
		again, err := repo.Record(t.Context(), change)
		require.NoError(t, err)

		assert.True(t, first)
		assert.False(t, again, "the same message is recorded once")
		due, err := repo.ClaimDueMessages(t.Context(), time.Now().Add(time.Second), 10, time.Minute)
		require.NoError(t, err)
		assert.Len(t, due, 1)
	})

	t.Run("stops an incident's waiting steps, but nothing already due or another incident's", func(t *testing.T) {
		repo := open(t)
		now := time.Now().UTC()
		incident, other := &murphy.Incident{Id: murphy.NewID()}, &murphy.Incident{Id: murphy.NewID()}
		ana := murphy.NewID()
		page, otherPage := newPage(incident, ana), newPage(other, ana)
		atOnce, later, elsewhere := newNotification(incident, page, ana), newNotification(incident, page, ana), newNotification(other, otherPage, ana)
		messages := map[string]*notifications.ScheduledMessage{
			"at once":   {ID: murphy.NewID(), DueAt: now, Envelope: []byte("at once")},
			"later":     {ID: murphy.NewID(), DueAt: now.Add(2 * time.Minute), Envelope: []byte("later"), Waiting: later},
			"elsewhere": {ID: murphy.NewID(), DueAt: now.Add(2 * time.Minute), Envelope: []byte("elsewhere"), Waiting: elsewhere},
		}
		_, err := repo.Record(t.Context(), &notifications.Change{MessageID: murphy.NewID(), Page: page, Notifications: []*murphy.Notification{atOnce, later},
			Messages: []*notifications.ScheduledMessage{messages["at once"], messages["later"]}})
		require.NoError(t, err)
		_, err = repo.Record(t.Context(), &notifications.Change{MessageID: murphy.NewID(), Page: otherPage, Notifications: []*murphy.Notification{elsewhere},
			Messages: []*notifications.ScheduledMessage{messages["elsewhere"]}})
		require.NoError(t, err)

		_, err = repo.Record(t.Context(), &notifications.Change{MessageID: murphy.NewID(), StopWaiting: incident.GetId()})
		require.NoError(t, err)

		due, err := repo.ClaimDueMessages(t.Context(), now.Add(3*time.Minute), 10, time.Minute)
		require.NoError(t, err)
		envelopes := make([]string, len(due))
		for i, message := range due {
			envelopes[i] = string(message.Envelope)
		}
		assert.ElementsMatch(t, []string{"at once", "elsewhere"}, envelopes)
	})

	t.Run("keeps one email provider, replaced by each set", func(t *testing.T) {
		repo := open(t)
		first := &murphy.EmailProvider{Type: murphy.EmailProviderType_EMAIL_PROVIDER_TYPE_SMTP, From: "murphy@example.com", Host: "smtp.example.com", Port: 587, UpdatedAt: murphy.Now()}
		second := &murphy.EmailProvider{
			Type: murphy.EmailProviderType_EMAIL_PROVIDER_TYPE_SMTP, From: "Murphy <pager@example.com>", Host: "mail.example.com", Port: 465,
			Username: "murphy", Secret: "env://SMTP_PASSWORD", UpdatedAt: murphy.Now(),
		}

		require.NoError(t, repo.SetEmailProvider(t.Context(), first))
		require.NoError(t, repo.SetEmailProvider(t.Context(), second))
		got, err := repo.GetEmailProvider(t.Context())

		require.NoError(t, err)
		assert.Equal(t, second.GetFrom(), got.GetFrom())
		assert.Equal(t, second.GetHost(), got.GetHost())
		assert.Equal(t, second.GetPort(), got.GetPort())
		assert.Equal(t, second.GetUsername(), got.GetUsername())
		assert.Equal(t, second.GetSecret(), got.GetSecret())
		assert.Equal(t, second.GetType(), got.GetType())
		assert.WithinDuration(t, second.GetUpdatedAt().AsTime(), got.GetUpdatedAt().AsTime(), time.Millisecond)
	})

	t.Run("records a check only for the provider it checked, and a new set starts pending", func(t *testing.T) {
		repo := open(t)
		checked := &murphy.EmailProvider{Type: murphy.EmailProviderType_EMAIL_PROVIDER_TYPE_SMTP, From: "murphy@example.com", Host: "smtp.example.com", Port: 587, UpdatedAt: timestamppb.New(time.Now().Add(-time.Minute))}
		require.NoError(t, repo.SetEmailProvider(t.Context(), checked))
		checked.Status, checked.Problem, checked.CheckedAt = murphy.EmailProviderStatus_EMAIL_PROVIDER_STATUS_FAILING, "start TLS: certificate signed by unknown authority", murphy.Now()
		require.NoError(t, repo.RecordEmailProviderCheck(t.Context(), checked))
		failing, err := repo.GetEmailProvider(t.Context())
		require.NoError(t, err)
		replaced := &murphy.EmailProvider{Type: murphy.EmailProviderType_EMAIL_PROVIDER_TYPE_SMTP, From: "murphy@example.com", Host: "mail.example.com", Port: 587, UpdatedAt: murphy.Now()}
		require.NoError(t, repo.SetEmailProvider(t.Context(), replaced))

		checked.Status, checked.Problem = murphy.EmailProviderStatus_EMAIL_PROVIDER_STATUS_READY, ""
		require.NoError(t, repo.RecordEmailProviderCheck(t.Context(), checked))
		got, err := repo.GetEmailProvider(t.Context())

		require.NoError(t, err)
		assert.Equal(t, murphy.EmailProviderStatus_EMAIL_PROVIDER_STATUS_FAILING, failing.GetStatus())
		assert.Equal(t, "start TLS: certificate signed by unknown authority", failing.GetProblem())
		assert.NotNil(t, failing.GetCheckedAt())
		assert.Equal(t, murphy.EmailProviderStatus_EMAIL_PROVIDER_STATUS_PENDING, got.GetStatus(), "a check of the old settings doesn't apply to the new ones")
		assert.Empty(t, got.GetProblem())
		assert.Nil(t, got.GetCheckedAt())
	})

	t.Run("has no email provider until one is set, and none once it's removed", func(t *testing.T) {
		repo := open(t)
		_, before := repo.GetEmailProvider(t.Context())
		require.NoError(t, repo.SetEmailProvider(t.Context(), &murphy.EmailProvider{Type: murphy.EmailProviderType_EMAIL_PROVIDER_TYPE_SMTP, From: "murphy@example.com", Host: "smtp.example.com", Port: 587, UpdatedAt: murphy.Now()}))

		require.NoError(t, repo.DeleteEmailProvider(t.Context()))
		_, after := repo.GetEmailProvider(t.Context())

		assert.ErrorIs(t, before, murphy.ErrNotFound)
		assert.ErrorIs(t, after, murphy.ErrNotFound)
	})

	t.Run("knows everyone an incident's pages reached, once each", func(t *testing.T) {
		repo := open(t)
		incident, other := &murphy.Incident{Id: murphy.NewID()}, &murphy.Incident{Id: murphy.NewID()}
		ana, bo, cy := murphy.NewID(), murphy.NewID(), murphy.NewID()
		for _, page := range []*murphy.Page{newPage(incident, ana), newPage(incident, bo, ana), newPage(other, cy), newPage(incident)} {
			_, err := repo.Record(t.Context(), &notifications.Change{MessageID: murphy.NewID(), Page: page})
			require.NoError(t, err)
		}

		users, err := repo.RecipientsOf(t.Context(), incident.GetId())

		require.NoError(t, err)
		assert.ElementsMatch(t, []string{ana, bo}, users)
	})

	t.Run("records follow-ups and outcomes", func(t *testing.T) {
		repo := open(t)
		incident := &murphy.Incident{Id: murphy.NewID()}
		followUp := newNotification(incident, nil, murphy.NewID())
		followUp.Reason = murphy.NotificationReason_NOTIFICATION_REASON_ACKNOWLEDGED
		_, err := repo.Record(t.Context(), &notifications.Change{MessageID: murphy.NewID(), Notifications: []*murphy.Notification{followUp}})
		require.NoError(t, err)

		sent := copyOf(followUp)
		sent.SentAt = murphy.Now()
		failed := copyOf(followUp)
		failed.Failure = "the receiver answered 410"
		for _, outcome := range []*murphy.Notification{sent, failed} {
			recorded, err := repo.Record(t.Context(), &notifications.Change{MessageID: murphy.NewID(), Outcome: outcome})
			require.NoError(t, err)
			assert.True(t, recorded)
		}
	})

	t.Run("keeps a test page's deliveries for the person who sent it, and counts their tests", func(t *testing.T) {
		repo := open(t)
		ana, test := murphy.NewID(), murphy.NewID()
		hook, inbox := newNotification(nil, nil, ana), newNotification(nil, nil, ana)
		inbox.ContactMethod.Type = murphy.ContactMethodType_CONTACT_METHOD_TYPE_EMAIL
		for _, delivery := range []*murphy.Notification{hook, inbox} {
			delivery.Reason, delivery.TestId = murphy.NotificationReason_NOTIFICATION_REASON_TEST, test
		}
		_, err := repo.Record(t.Context(), &notifications.Change{Notifications: []*murphy.Notification{hook, inbox}})
		require.NoError(t, err)
		failed := copyOf(inbox)
		failed.TestId, failed.Failure = test, "Murphy has no email provider"
		_, err = repo.Record(t.Context(), &notifications.Change{MessageID: murphy.NewID(), Outcome: failed})
		require.NoError(t, err)

		page, err := repo.GetTestPage(t.Context(), test, ana)
		require.NoError(t, err)
		_, someoneElse := repo.GetTestPage(t.Context(), test, murphy.NewID())

		require.Len(t, page.Deliveries, 2)
		assert.Equal(t, notifications.DeliveryPending, page.Deliveries[0].Status)
		assert.Equal(t, notifications.DeliveryFailed, page.Deliveries[1].Status)
		assert.Equal(t, "Murphy has no email provider", page.Deliveries[1].Failure)
		assert.Equal(t, murphy.ContactMethodType_CONTACT_METHOD_TYPE_EMAIL, page.Deliveries[1].Channel)
		assert.ErrorIs(t, someoneElse, murphy.ErrNotFound, "a test page belongs to the person who sent it")
	})

	t.Run("records an outcome once per message", func(t *testing.T) {
		repo := open(t)
		incident := &murphy.Incident{Id: murphy.NewID()}
		page := newPage(incident, murphy.NewID())
		notification := newNotification(incident, page, page.GetReached()[0].GetUserId())
		_, err := repo.Record(t.Context(), &notifications.Change{MessageID: murphy.NewID(), Page: page, Notifications: []*murphy.Notification{notification}})
		require.NoError(t, err)
		sent := copyOf(notification)
		sent.SentAt = murphy.Now()
		outcome := &notifications.Change{MessageID: murphy.NewID(), Outcome: sent}

		first, err := repo.Record(t.Context(), outcome)
		require.NoError(t, err)
		again, err := repo.Record(t.Context(), outcome)
		require.NoError(t, err)

		assert.True(t, first)
		assert.False(t, again, "a redelivered outcome changes nothing")
	})

	t.Run("hands out each due message once until its claim expires", func(t *testing.T) {
		repo := open(t)
		now := time.Now().UTC()
		message := &notifications.ScheduledMessage{ID: murphy.NewID(), DueAt: now, Envelope: []byte("notification created")}
		_, err := repo.Record(t.Context(), &notifications.Change{MessageID: murphy.NewID(), Messages: []*notifications.ScheduledMessage{message}})
		require.NoError(t, err)

		claimed, err := repo.ClaimDueMessages(t.Context(), now.Add(time.Second), 10, time.Minute)
		require.NoError(t, err)
		require.Len(t, claimed, 1)
		assert.Equal(t, []byte("notification created"), claimed[0].Envelope)
		again, err := repo.ClaimDueMessages(t.Context(), now.Add(2*time.Second), 10, time.Minute)
		require.NoError(t, err)
		assert.Empty(t, again, "claimed by someone else")
		expired, err := repo.ClaimDueMessages(t.Context(), now.Add(2*time.Minute), 10, time.Minute)
		require.NoError(t, err)
		assert.Len(t, expired, 1, "the claim expired, so it is handed out again")

		require.NoError(t, repo.DeleteMessages(t.Context(), []string{message.ID}))
		gone, err := repo.ClaimDueMessages(t.Context(), now.Add(5*time.Minute), 10, time.Minute)
		require.NoError(t, err)
		assert.Empty(t, gone)
	})

	t.Run("forgets handled messages after their retention", func(t *testing.T) {
		repo := open(t)
		_, err := repo.Record(t.Context(), &notifications.Change{MessageID: murphy.NewID()})
		require.NoError(t, err)

		purged, err := repo.PurgeHandledMessages(t.Context(), time.Now().Add(time.Minute))

		require.NoError(t, err)
		assert.Equal(t, int64(1), purged)
	})
}

func newPage(incident *murphy.Incident, users ...string) *murphy.Page {
	page := &murphy.Page{Id: murphy.NewID(), Incident: incident, EscalationLevel: 1, EscalationRound: 1, CreatedAt: murphy.Now()}
	for _, user := range users {
		page.Reached = append(page.Reached, &murphy.PageRecipient{UserId: user, Via: &murphy.EscalationTarget{Type: murphy.EscalationTargetType_ESCALATION_TARGET_TYPE_USER, Id: user}})
	}
	return page
}

func newNotification(incident *murphy.Incident, page *murphy.Page, user string) *murphy.Notification {
	notification := &murphy.Notification{
		Id: murphy.NewID(), Reason: murphy.NotificationReason_NOTIFICATION_REASON_PAGED, Incident: incident, UserId: user,
		ContactMethod: &murphy.ContactMethod{Id: murphy.NewID(), UserId: user, Type: murphy.ContactMethodType_CONTACT_METHOD_TYPE_WEBHOOK},
		CreatedAt:     murphy.Now(),
	}
	if page != nil {
		notification.PageId = page.GetId()
	}
	return notification
}

func copyOf(notification *murphy.Notification) *murphy.Notification {
	return &murphy.Notification{Id: notification.GetId(), Reason: notification.GetReason(), Incident: notification.GetIncident(), UserId: notification.GetUserId(), ContactMethod: notification.GetContactMethod()}
}
