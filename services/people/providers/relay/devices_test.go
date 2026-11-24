package relay_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/internal/relay"
	relaydevices "github.com/murphyhq/server/services/people/providers/relay"
)

func devicesAnswering(t *testing.T, status int, body string, received map[string]string) *relaydevices.Devices {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&received)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	client, err := relay.New(server.URL, "mik_install-key")
	require.NoError(t, err)
	return relaydevices.New(client)
}

func TestRegisteringAPhoneKeepsTheRelaysHandle(t *testing.T) {
	received := map[string]string{}
	devices := devicesAnswering(t, http.StatusCreated, `{"handle": "mdh_from-the-relay", "platform": "android"}`, received)

	handle, err := devices.Register(t.Context(), murphy.PushPlatform_PUSH_PLATFORM_ANDROID, "fcm-token")

	require.NoError(t, err)
	assert.Equal(t, "mdh_from-the-relay", handle)
	assert.Equal(t, map[string]string{"platform": "android", "token": "fcm-token"}, received)
}

func TestATokenTheRelayRefusesIsTheCallersMistake(t *testing.T) {
	devices := devicesAnswering(t, http.StatusBadRequest, `{"title": "Bad Request", "status": 400}`, map[string]string{})

	_, err := devices.Register(t.Context(), murphy.PushPlatform_PUSH_PLATFORM_IOS, "not-a-token")

	assert.ErrorIs(t, err, murphy.ErrInvalid)
}
