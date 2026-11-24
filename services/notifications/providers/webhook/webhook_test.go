package webhook_test

import (
	"crypto/ed25519"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/services/notifications"
	"github.com/murphyhq/server/services/notifications/providers/webhook"
)

func TestAWebhookToAnInternalAddressFailsForGood(t *testing.T) {
	var reached atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { reached.Store(true) }))
	t.Cleanup(server.Close)
	_, key, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	notification := &murphy.Notification{
		Id: murphy.NewID(), Incident: &murphy.Incident{Id: murphy.NewID()}, Deadline: timestamppb.New(time.Now().Add(time.Minute)),
		ContactMethod: &murphy.ContactMethod{Type: murphy.ContactMethodType_CONTACT_METHOD_TYPE_WEBHOOK, Address: server.URL},
	}

	err = webhook.New([]ed25519.PrivateKey{key}, nil).Send(t.Context(), notification)

	var refused *notifications.RefusedError
	require.True(t, errors.As(err, &refused), "%v", err)
	assert.Equal(t, "the webhook's address is on a network Murphy isn't allowed to reach", refused.Reason)
	assert.False(t, reached.Load(), "nothing was sent to it")
}
