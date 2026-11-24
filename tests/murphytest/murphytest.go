package murphytest

import (
	"context"
	"crypto/ed25519"
	"net/http"
	"os"
	"time"
	"uuid"

	"github.com/stretchr/testify/require"
	"github.com/wiremock/go-wiremock"

	"github.com/murphyhq/server/client"
	"github.com/murphyhq/server/services/notifications/providers/webhook"
)

type T interface {
	Helper()
	Context() context.Context
	Cleanup(func())
	Errorf(format string, args ...any)
	FailNow()
}

const (
	Settle = 30 * time.Second
	tick   = 100 * time.Millisecond
)

var (
	apiURL             = setting("MURPHY_TEST_API_URL", "http://localhost:8080")
	ingestURL          = setting("MURPHY_TEST_INGEST_URL", "http://localhost:8081")
	receiverURL        = setting("MURPHY_TEST_RECEIVER_URL", "http://localhost:8092")
	receiverFromMurphy = setting("MURPHY_TEST_RECEIVER_URL_FROM_MURPHY", "http://webhook-receiver:8080")
)

func setting(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

type Murphy struct {
	t        T
	API      *client.ClientWithResponses
	apiURL   string
	ingest   string
	patience time.Duration
	run      string
	receiver *Receiver
}

type Option func(*Murphy)

func At(api, ingest string) Option {
	return func(m *Murphy) { m.apiURL, m.ingest = api, ingest }
}

func Patience(wait time.Duration) Option {
	return func(m *Murphy) { m.patience = wait }
}

func New(t T, options ...Option) *Murphy {
	t.Helper()
	m := &Murphy{t: t, apiURL: apiURL, ingest: ingestURL, patience: Settle, run: uuid.NewV4().String()[:8]}
	for _, option := range options {
		option(m)
	}
	api, err := client.NewClientWithResponses(m.apiURL)
	require.NoError(t, err)
	m.API = api
	return m
}

func (m *Murphy) Receiver() *Receiver {
	if m.receiver == nil {
		m.receiver = &Receiver{t: m.t, client: wiremock.NewClient(receiverURL), from: receiverFromMurphy, keys: m.publicKeys}
	}
	return m.receiver
}

func (m *Murphy) publicKeys() []ed25519.PublicKey {
	m.t.Helper()
	got, err := m.API.GetSigningKeysWithResponse(m.t.Context())
	m.ok(got, err, "get the public signing keys")
	var keys []ed25519.PublicKey
	for _, key := range got.JSON200.Keys {
		parsed, err := webhook.ParsePublicKeys(key.PublicKey)
		require.NoError(m.t, err)
		keys = append(keys, parsed...)
	}
	return keys
}

func (m *Murphy) APIURL() string {
	return m.apiURL
}

func (m *Murphy) Name(prefix string) string {
	return prefix + "-" + m.run + "-" + uuid.NewV4().String()[:4]
}

type response interface {
	StatusCode() int
}

func (m *Murphy) expect(got response, err error, status int, doing string) {
	m.t.Helper()
	require.NoError(m.t, err, doing)
	require.Equal(m.t, status, got.StatusCode(), doing)
}

func (m *Murphy) ok(got response, err error, doing string) {
	m.t.Helper()
	m.expect(got, err, http.StatusOK, doing)
}

func (m *Murphy) created(got response, err error, doing string) {
	m.t.Helper()
	m.expect(got, err, http.StatusCreated, doing)
}
