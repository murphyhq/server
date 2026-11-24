package murphytest

import (
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
	"uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wiremock/go-wiremock"

	"github.com/murphyhq/server/services/notifications/providers/webhook"
)

type Receiver struct {
	t      T
	client *wiremock.Client
	from   string
	keys   func() []ed25519.PublicKey
}

func NewReceiver(t T, reachableAt, publicKeys string) *Receiver {
	t.Helper()
	keys, err := webhook.ParsePublicKeys(publicKeys)
	require.NoError(t, err)
	return &Receiver{t: t, client: wiremock.NewClient(receiverURL), from: reachableAt, keys: func() []ed25519.PublicKey { return keys }}
}

type WebhookOption func(*webhookSpec)

type webhookSpec struct {
	failures []int
	refusal  int
}

func FailsWith(statuses ...int) WebhookOption {
	return func(s *webhookSpec) { s.failures = append(s.failures, statuses...) }
}

func Refuses(status int) WebhookOption {
	return func(s *webhookSpec) { s.refusal = status }
}

type Webhook struct {
	ContactMethodID string
	Address         string
	path            string
	receiver        *Receiver
}

type ReceivedWebhook struct {
	ID         string
	Verified   bool
	Body       webhook.Webhook
	timestamp  string
	signatures string
	raw        []byte
}

func (r ReceivedWebhook) VerifiesWith(publicKey string) bool {
	keys, err := webhook.ParsePublicKeys(publicKey)
	return err == nil && webhook.Verify(keys, r.ID, r.timestamp, r.signatures, r.raw, time.Now(), 5*time.Minute) == nil
}

func (r ReceivedWebhook) Signatures() int {
	return len(strings.Fields(r.signatures))
}

func (r *Receiver) Webhook(options ...WebhookOption) *Webhook {
	r.t.Helper()
	spec := webhookSpec{refusal: http.StatusNoContent}
	for _, option := range options {
		option(&spec)
	}
	path := "/hooks/" + uuid.NewV4().String()
	state := wiremock.ScenarioStateStarted
	for i, status := range spec.failures {
		next := fmt.Sprintf("failed %d", i+1)
		r.stub(path, state, status, next)
		state = next
	}
	r.stub(path, state, spec.refusal, "")
	return &Webhook{Address: r.from + path, path: path, receiver: r}
}

func (r *Receiver) stub(path, state string, status int, next string) {
	r.t.Helper()
	rule := wiremock.Post(wiremock.URLPathEqualTo(path)).
		InScenario(path).
		WhenScenarioStateIs(state).
		WillReturnResponse(wiremock.NewResponse().WithStatus(int64(status)))
	if next != "" {
		rule = rule.WillSetStateTo(next)
	}
	require.NoError(r.t, r.client.StubFor(rule), "set up the webhook receiver")
}

type Phone struct {
	Handle   string
	receiver *Receiver
}

type ReceivedPush struct {
	ID         string `json:"id"`
	Handle     string `json:"handle"`
	Event      string `json:"event"`
	Server     string `json:"server"`
	IncidentID string `json:"incident_id"`
}

func (p *Phone) pushes() *wiremock.Request {
	return wiremock.NewRequest(http.MethodPost, wiremock.URLPathEqualTo("/relay/v1/pushes")).
		WithBodyPattern(wiremock.MatchingJsonPath("$[?(@.handle == '" + p.Handle + "')]"))
}

func (p *Phone) WaitForPushes(count int) []ReceivedPush {
	p.receiver.t.Helper()
	require.EventuallyWithT(p.receiver.t, func(c *assert.CollectT) {
		got, err := p.receiver.client.GetCountRequests(p.pushes())
		assert.NoError(c, err)
		assert.Equal(c, int64(count), got)
	}, Settle, tick)
	found, err := p.receiver.client.FindRequestsByCriteria(p.pushes())
	require.NoError(p.receiver.t, err, "read what the relay received")
	received := make([]ReceivedPush, len(found.Requests))
	for i, request := range found.Requests {
		require.NoError(p.receiver.t, json.Unmarshal([]byte(request.Body), &received[i]))
	}
	return received
}

func (w *Webhook) Received() []ReceivedWebhook {
	w.receiver.t.Helper()
	found, err := w.receiver.client.FindRequestsByCriteria(wiremock.NewRequest(http.MethodPost, wiremock.URLPathEqualTo(w.path)))
	require.NoError(w.receiver.t, err, "read what the webhook receiver got")
	keys := w.receiver.keys()
	received := make([]ReceivedWebhook, len(found.Requests))
	for i, request := range found.Requests {
		headers := map[string]string{}
		for name, value := range request.Headers {
			headers[strings.ToLower(name)] = value
		}
		received[i].ID = headers[webhook.HeaderID]
		received[i].timestamp, received[i].signatures, received[i].raw = headers[webhook.HeaderTimestamp], headers[webhook.HeaderSignature], []byte(request.Body)
		received[i].Verified = webhook.Verify(keys, received[i].ID, headers[webhook.HeaderTimestamp], headers[webhook.HeaderSignature], []byte(request.Body), time.Now(), 5*time.Minute) == nil
		_ = json.Unmarshal([]byte(request.Body), &received[i].Body)
	}
	return received
}

func (w *Webhook) ReceivesNothing() {
	w.receiver.t.Helper()
	request := wiremock.NewRequest(http.MethodPost, wiremock.URLPathEqualTo(w.path))
	require.Never(w.receiver.t, func() bool {
		got, err := w.receiver.client.GetCountRequests(request)
		return err != nil || got > 0
	}, 3*time.Second, tick, "the webhook receives nothing")
}

func (w *Webhook) WaitForWebhooks(count int) []ReceivedWebhook {
	w.receiver.t.Helper()
	request := wiremock.NewRequest(http.MethodPost, wiremock.URLPathEqualTo(w.path))
	require.EventuallyWithT(w.receiver.t, func(c *assert.CollectT) {
		got, err := w.receiver.client.GetCountRequests(request)
		assert.NoError(c, err)
		assert.Equal(c, int64(count), got)
	}, Settle, tick)
	return w.Received()
}
