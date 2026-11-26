package push_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/internal/relay"
	"github.com/murphyhq/server/services/notifications"
	"github.com/murphyhq/server/services/notifications/providers/push"
)

type relayAnswer struct {
	status   int
	received map[string]any
}

func pushesThrough(t *testing.T, answer *relayAnswer) *push.Pushes {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&answer.received)
		answer.received["authorization"] = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(answer.status)
		_, _ = w.Write([]byte(`<html>Bad Gateway</html>`))
	}))
	t.Cleanup(server.Close)
	client, err := relay.New(server.URL, "mik_install-key")
	require.NoError(t, err)
	return push.New(client, "https://murphy.example.com")
}

func notification(reason murphy.NotificationReason) *murphy.Notification {
	return &murphy.Notification{
		Id: "notification-1", Reason: reason, Incident: &murphy.Incident{Id: "incident-1", Title: "Disk full on db-1"},
		ContactMethod: &murphy.ContactMethod{Type: murphy.ContactMethodType_CONTACT_METHOD_TYPE_PUSH, Address: "apns-token", PushPlatform: murphy.PushPlatform_PUSH_PLATFORM_IOS},
		Deadline:      timestamppb.New(time.Now().Add(time.Minute)),
	}
}

func TestAPushCarriesTheEventTheServerAndTheIncidentOnly(t *testing.T) {
	answer := &relayAnswer{status: http.StatusOK}

	err := pushesThrough(t, answer).Send(t.Context(), notification(murphy.NotificationReason_NOTIFICATION_REASON_PAGED))

	require.NoError(t, err)
	assert.Equal(t, map[string]any{
		"id": "notification-1", "platform": "ios", "token": "apns-token", "category": "page", "server": "https://murphy.example.com",
		"subject": map[string]any{"type": "incident", "id": "incident-1"}, "authorization": "Bearer mik_install-key",
	}, answer.received, "no incident title or other incident data")
}

func TestATestPagePushIsAboutTheTestPage(t *testing.T) {
	answer := &relayAnswer{status: http.StatusOK}
	test := notification(murphy.NotificationReason_NOTIFICATION_REASON_TEST)
	test.Incident, test.TestId = nil, "test-1"

	err := pushesThrough(t, answer).Send(t.Context(), test)

	require.NoError(t, err)
	assert.Equal(t, "test", answer.received["category"])
	assert.Equal(t, map[string]any{"type": "test", "id": "test-1"}, answer.received["subject"])
}

func TestAPushCarriesTheTraceToThePhone(t *testing.T) {
	previousProvider, previousPropagator := otel.GetTracerProvider(), otel.GetTextMapPropagator()
	otel.SetTracerProvider(sdktrace.NewTracerProvider())
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() {
		otel.SetTracerProvider(previousProvider)
		otel.SetTextMapPropagator(previousPropagator)
	})
	ctx, span := otel.Tracer("push_test").Start(t.Context(), "page")
	defer span.End()
	answer := &relayAnswer{status: http.StatusOK}

	err := pushesThrough(t, answer).Send(ctx, notification(murphy.NotificationReason_NOTIFICATION_REASON_PAGED))

	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(answer.received["traceparent"].(string), "00-"+span.SpanContext().TraceID().String()+"-"), "the phone continues the page's trace")
}

func TestAnAcknowledgementIsPushedAsAnUpdateAboutTheIncident(t *testing.T) {
	answer := &relayAnswer{status: http.StatusOK}

	err := pushesThrough(t, answer).Send(t.Context(), notification(murphy.NotificationReason_NOTIFICATION_REASON_ACKNOWLEDGED))

	require.NoError(t, err)
	assert.Equal(t, "update", answer.received["category"])
	assert.Equal(t, map[string]any{"type": "incident", "id": "incident-1"}, answer.received["subject"], "it replaces the page on the lock screen")
}

func TestAPhoneTheRelayForgotFailsForGood(t *testing.T) {
	answer := &relayAnswer{status: http.StatusGone}

	err := pushesThrough(t, answer).Send(t.Context(), notification(murphy.NotificationReason_NOTIFICATION_REASON_PAGED))

	var refused *notifications.RefusedError
	require.True(t, errors.As(err, &refused), "%v", err)
	assert.Equal(t, "the phone is no longer registered for pushes, so it's disabled until the app registers again", refused.Reason)
	assert.True(t, refused.Gone, "the token never works again")
}

func TestARelayThatIsDownIsRetried(t *testing.T) {
	answer := &relayAnswer{status: http.StatusBadGateway}

	err := pushesThrough(t, answer).Send(t.Context(), notification(murphy.NotificationReason_NOTIFICATION_REASON_PAGED))

	var refused *notifications.RefusedError
	require.Error(t, err)
	assert.False(t, errors.As(err, &refused), "not refused, so the sender retries it")
}
