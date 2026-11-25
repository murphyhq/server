package email_test

import (
	"bytes"
	"io"
	"mime"
	"net/mail"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/services/notifications"
	"github.com/murphyhq/server/services/notifications/providers/email"
)

func page(title string, reason murphy.NotificationReason) *murphy.Notification {
	return &murphy.Notification{
		Id: "0195f3a0-0000-7000-8000-000000000001", Reason: reason,
		Incident: &murphy.Incident{
			Number: 14, Title: title, Status: murphy.IncidentStatus_INCIDENT_STATUS_TRIGGERED, Urgency: murphy.Urgency_URGENCY_HIGH,
		},
		ContactMethod: &murphy.ContactMethod{Type: murphy.ContactMethodType_CONTACT_METHOD_TYPE_EMAIL, Address: "ana@example.com"},
	}
}

func read(t *testing.T, notification *murphy.Notification) (*mail.Message, string) {
	t.Helper()
	from := &mail.Address{Name: "Murphy", Address: "murphy@example.com"}
	raw := email.Message(from, &mail.Address{Address: "ana@example.com"}, notification, time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC))
	message, err := mail.ReadMessage(bytes.NewReader(raw))
	require.NoError(t, err)
	subject, err := new(mime.WordDecoder).DecodeHeader(message.Header.Get("Subject"))
	require.NoError(t, err)
	return message, subject
}

func TestAPageIsAPlainTextEmailNamingTheIncident(t *testing.T) {
	message, subject := read(t, page("Resolver timeouts from eu-west-1 pods", murphy.NotificationReason_NOTIFICATION_REASON_PAGED))

	body, err := io.ReadAll(message.Body)
	require.NoError(t, err)
	assert.Equal(t, "[Murphy] #14 Resolver timeouts from eu-west-1 pods", subject)
	assert.Equal(t, `"Murphy" <murphy@example.com>`, message.Header.Get("From"))
	assert.Equal(t, "<ana@example.com>", message.Header.Get("To"))
	assert.Equal(t, "auto-generated", message.Header.Get("Auto-Submitted"), "out-of-office replies stay away")
	assert.Equal(t, "<0195f3a0-0000-7000-8000-000000000001@example.com>", message.Header.Get("Message-ID"))
	assert.Contains(t, string(body), "Urgency: high")
}

func TestFollowUpsSayWhatHappened(t *testing.T) {
	_, acknowledged := read(t, page("Disk full", murphy.NotificationReason_NOTIFICATION_REASON_ACKNOWLEDGED))
	_, resolved := read(t, page("Disk full", murphy.NotificationReason_NOTIFICATION_REASON_RESOLVED))

	assert.Equal(t, "[Murphy] Acknowledged: #14 Disk full", acknowledged)
	assert.Equal(t, "[Murphy] Resolved: #14 Disk full", resolved)
}

func TestATitleCantAddHeaders(t *testing.T) {
	message, subject := read(t, page("Disk full\r\nBcc: someone@example.org", murphy.NotificationReason_NOTIFICATION_REASON_PAGED))

	assert.Empty(t, message.Header.Get("Bcc"))
	assert.Equal(t, "[Murphy] #14 Disk full Bcc: someone@example.org", subject)
}

func TestWithoutAProviderAnEmailFailsAtOnce(t *testing.T) {
	err := email.New(nil).Send(t.Context(), page("Disk full", murphy.NotificationReason_NOTIFICATION_REASON_PAGED))

	var refused *notifications.RefusedError
	require.ErrorAs(t, err, &refused)
	assert.Equal(t, "Murphy has no email provider", refused.Reason)
}
