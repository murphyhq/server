package storagetest

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
