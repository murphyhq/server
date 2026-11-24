package murphytest

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"regexp"
	"strings"
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
	identityProvider   = setting("MURPHY_TEST_IDENTITY_PROVIDER_URL", "http://localhost:5556/dex")
)

const (
	Ana = "ana@murphy.test"
	Bo  = "bo@murphy.test"

	testPassword     = "murphy"
	testClientID     = "murphy"
	testClientSecret = "murphy-local-secret"
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
	session  string
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
	if m.apiURL != "" {
		m.session = m.SessionAs(Ana)
		m.API = m.ClientWith(m.session)
	}
	return m
}

func SignIn(ctx context.Context, apiURL, identityProviderURL, email string) (string, error) {
	idToken, err := identityToken(ctx, identityProviderURL, email)
	if err != nil {
		return "", err
	}
	api, err := client.NewClientWithResponses(apiURL)
	if err != nil {
		return "", err
	}
	got, err := api.CreateSessionWithResponse(ctx, client.CreateSessionJSONRequestBody{IdToken: idToken})
	if err != nil {
		return "", fmt.Errorf("create a session: %w", err)
	}
	if got.JSON201 == nil {
		return "", fmt.Errorf("create a session: %s %s", got.Status(), got.Body)
	}
	return got.JSON201.Token, nil
}

func Authenticated(apiURL, token string) (*client.ClientWithResponses, error) {
	return client.NewClientWithResponses(apiURL, client.WithRequestEditorFn(func(_ context.Context, request *http.Request) error {
		request.Header.Set("Authorization", "Bearer "+token)
		return nil
	}))
}

func identityToken(ctx context.Context, identityProviderURL, email string) (string, error) {
	form := url.Values{"grant_type": {"password"}, "username": {email}, "password": {testPassword}, "scope": {"openid email profile"}}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, identityProviderURL+"/token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	request.SetBasicAuth(testClientID, testClientSecret)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return "", fmt.Errorf("sign in to the identity provider: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	var answer struct {
		IDToken string `json:"id_token"`
	}
	if err := json.NewDecoder(response.Body).Decode(&answer); err != nil || answer.IDToken == "" {
		return "", fmt.Errorf("sign in to the identity provider as %s: %s", email, response.Status)
	}
	return answer.IDToken, nil
}

func (m *Murphy) SessionAs(email string) string {
	m.t.Helper()
	token, err := SignIn(m.t.Context(), m.apiURL, identityProvider, email)
	require.NoError(m.t, err)
	return token
}

func (m *Murphy) ClientWith(token string) *client.ClientWithResponses {
	m.t.Helper()
	api, err := Authenticated(m.apiURL, token)
	require.NoError(m.t, err)
	return api
}

func (m *Murphy) Anonymous() *client.ClientWithResponses {
	m.t.Helper()
	api, err := client.NewClientWithResponses(m.apiURL)
	require.NoError(m.t, err)
	return api
}

func (m *Murphy) SignInThroughBrowser(email string) (*client.ClientWithResponses, *http.Response) {
	m.t.Helper()
	jar, err := cookiejar.New(nil)
	require.NoError(m.t, err)
	browser := &http.Client{Jar: jar, Timeout: 10 * time.Second}
	loginPage, err := browser.Get(m.apiURL + "/v1/auth/sign-in")
	require.NoError(m.t, err, "open the sign-in page")
	page, err := io.ReadAll(loginPage.Body)
	_ = loginPage.Body.Close()
	require.NoError(m.t, err)
	action := loginForm.FindSubmatch(page)
	require.NotNil(m.t, action, "the identity provider shows a login form")
	submit, err := loginPage.Request.URL.Parse(html.UnescapeString(string(action[1])))
	require.NoError(m.t, err)
	signedIn, err := browser.PostForm(submit.String(), url.Values{"login": {email}, "password": {testPassword}})
	require.NoError(m.t, err, "submit the login form")
	_ = signedIn.Body.Close()
	api, err := client.NewClientWithResponses(m.apiURL, client.WithHTTPClient(browser))
	require.NoError(m.t, err)
	return api, signedIn
}

var loginForm = regexp.MustCompile(`<form[^>]*action="([^"]+)"`)

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

func (m *Murphy) SigningKeys() []string {
	m.t.Helper()
	got, err := m.API.GetSigningKeysWithResponse(m.t.Context())
	m.ok(got, err, "get the public signing keys")
	keys := make([]string, len(got.JSON200.Keys))
	for i, key := range got.JSON200.Keys {
		keys[i] = key.PublicKey
	}
	return keys
}

func (m *Murphy) Session() string {
	return m.session
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
