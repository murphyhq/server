package notifications_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/services/notifications"
)

type fixture struct {
	sender    *notifications.Sender
	provider  *MockProvider
	published []*murphy.Envelope
}

func setup(t *testing.T) *fixture {
	t.Helper()
	provider, publisher := NewMockProvider(t), NewMockPublisher(t)
	f := &fixture{provider: provider}
	publisher.EXPECT().Publish(mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, envelope *murphy.Envelope) error {
		f.published = append(f.published, envelope)
		return nil
	}).Maybe()
	f.sender = notifications.NewSender(publisher, map[murphy.ContactMethodType]notifications.Provider{
		murphy.ContactMethodType_CONTACT_METHOD_TYPE_WEBHOOK: provider,
	})
	return f
}

func notification(channel murphy.ContactMethodType, age time.Duration) *murphy.Notification {
	return &murphy.Notification{
		Id:            murphy.NewID(),
		Reason:        murphy.NotificationReason_NOTIFICATION_REASON_PAGED,
		Incident:      &murphy.Incident{Id: murphy.NewID()},
		ContactMethod: &murphy.ContactMethod{Id: murphy.NewID(), Type: channel, Address: "https://example.com/hook"},
		CreatedAt:     timestamppb.New(time.Now().Add(-age)),
		Deadline:      timestamppb.New(time.Now().Add(-age).Add(10 * time.Minute)),
	}
}

func TestASentNotificationIsAnnounced(t *testing.T) {
	f := setup(t)
	f.provider.EXPECT().Send(mock.Anything, mock.Anything).Return(nil)

	require.NoError(t, f.sender.Send(t.Context(), "message-1", notification(murphy.ContactMethodType_CONTACT_METHOD_TYPE_WEBHOOK, 0)))

	require.Len(t, f.published, 1)
	event := f.published[0].GetNotification()
	assert.Equal(t, murphy.NotificationVerb_NOTIFICATION_VERB_SENT, event.GetVerb())
	assert.NotNil(t, event.GetCurrent().GetSentAt())
	assert.Empty(t, event.GetCurrent().GetFailure())
}

func TestAnOutcomeKeepsItsIDWhenTheMessageIsRetried(t *testing.T) {
	f := setup(t)
	f.provider.EXPECT().Send(mock.Anything, mock.Anything).Return(nil)
	sent := notification(murphy.ContactMethodType_CONTACT_METHOD_TYPE_WEBHOOK, 0)

	require.NoError(t, f.sender.Send(t.Context(), "message-1", sent))
	require.NoError(t, f.sender.Send(t.Context(), "message-1", sent))
	require.NoError(t, f.sender.Send(t.Context(), "message-2", sent))

	require.Len(t, f.published, 3)
	assert.Equal(t, f.published[0].GetId(), f.published[1].GetId(), "so the broker and engine drop the duplicate")
	assert.NotEqual(t, f.published[0].GetId(), f.published[2].GetId())
}

func TestARefusedNotificationFailsAtOnce(t *testing.T) {
	f := setup(t)
	f.provider.EXPECT().Send(mock.Anything, mock.Anything).Return(&notifications.RefusedError{Reason: "the receiver answered 410"})

	require.NoError(t, f.sender.Send(t.Context(), "message-1", notification(murphy.ContactMethodType_CONTACT_METHOD_TYPE_WEBHOOK, 0)))

	require.Len(t, f.published, 1)
	event := f.published[0].GetNotification()
	assert.Equal(t, murphy.NotificationVerb_NOTIFICATION_VERB_FAILED, event.GetVerb())
	assert.Equal(t, "the receiver answered 410", event.GetCurrent().GetFailure())
}

func TestAFailureThatMayPassIsRetriedByTheBroker(t *testing.T) {
	f := setup(t)
	f.provider.EXPECT().Send(mock.Anything, mock.Anything).Return(errors.New("the receiver answered 503"))

	err := f.sender.Send(t.Context(), "message-1", notification(murphy.ContactMethodType_CONTACT_METHOD_TYPE_WEBHOOK, time.Minute))

	assert.Error(t, err)
	assert.Empty(t, f.published)
}

func TestANotificationStillFailingAtItsDeadlineFails(t *testing.T) {
	f := setup(t)
	f.provider.EXPECT().Send(mock.Anything, mock.Anything).Return(errors.New("the receiver answered 503"))

	require.NoError(t, f.sender.Send(t.Context(), "message-1", notification(murphy.ContactMethodType_CONTACT_METHOD_TYPE_WEBHOOK, 10*time.Minute)))

	require.Len(t, f.published, 1)
	assert.Equal(t, murphy.NotificationVerb_NOTIFICATION_VERB_FAILED, f.published[0].GetNotification().GetVerb())
	assert.Equal(t, "still failing after 10m0s: the receiver answered 503", f.published[0].GetNotification().GetCurrent().GetFailure())
}

func TestAChannelWithoutAProviderFails(t *testing.T) {
	f := setup(t)

	require.NoError(t, f.sender.Send(t.Context(), "message-1", notification(murphy.ContactMethodType_CONTACT_METHOD_TYPE_SMS, 0)))

	require.Len(t, f.published, 1)
	assert.Equal(t, murphy.NotificationVerb_NOTIFICATION_VERB_FAILED, f.published[0].GetNotification().GetVerb())
	assert.Equal(t, "Murphy can't send by sms yet", f.published[0].GetNotification().GetCurrent().GetFailure())
}
