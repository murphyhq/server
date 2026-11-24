//go:build integration

package testenv

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/murphyhq/server/services/notifications/providers/webhook"
)

type ReceivedWebhook struct {
	ID       string
	Verified bool
	Body     webhook.Webhook
}

type WebhookReceiver struct {
	URL string

	mu       sync.Mutex
	received []ReceivedWebhook
	failures []int
}

func NewWebhookReceiver(t *testing.T, publicKeys string) *WebhookReceiver {
	t.Helper()
	keys, err := webhook.ParsePublicKeys(publicKeys)
	require.NoError(t, err)

	r := &WebhookReceiver{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		data, err := io.ReadAll(req.Body)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		received := ReceivedWebhook{ID: req.Header.Get(webhook.HeaderID)}
		received.Verified = webhook.Verify(keys, received.ID, req.Header.Get(webhook.HeaderTimestamp), req.Header.Get(webhook.HeaderSignature), data, time.Now(), 5*time.Minute) == nil
		_ = json.Unmarshal(data, &received.Body)

		r.mu.Lock()
		defer r.mu.Unlock()
		if len(r.failures) > 0 {
			status := r.failures[0]
			r.failures = r.failures[1:]
			w.WriteHeader(status)
			return
		}
		r.received = append(r.received, received)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)
	r.URL = server.URL
	return r
}

func (r *WebhookReceiver) FailWith(statuses ...int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.failures = append(r.failures, statuses...)
}

func (r *WebhookReceiver) Received() []ReceivedWebhook {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]ReceivedWebhook(nil), r.received...)
}
