package murphytest

import (
	"crypto/x509"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/murphyhq/server/client"
)

const MailPort = 1025

type Mail struct {
	Subject string
	From    struct{ Address string }
	To      []struct{ Address string }
}

type Mailbox struct {
	t       T
	address string
}

func NewMailbox(t T, address string) *Mailbox {
	return &Mailbox{t: t, address: address}
}

func (b *Mailbox) WaitForMail(count int) []Mail {
	b.t.Helper()
	var found []Mail
	require.EventuallyWithT(b.t, func(c *assert.CollectT) {
		found = b.mail(c)
		assert.Len(c, found, count, "mail to %s", b.address)
	}, Settle, tick)
	return found
}

func (b *Mailbox) mail(c *assert.CollectT) []Mail {
	response, err := http.Get(mailpitURL + "/api/v1/search?query=" + url.QueryEscape(`to:"`+b.address+`"`))
	if !assert.NoError(c, err) {
		return nil
	}
	defer func() { _ = response.Body.Close() }()
	var page struct{ Messages []Mail }
	if !assert.NoError(c, json.NewDecoder(response.Body).Decode(&page)) {
		return nil
	}
	return page.Messages
}

func MailCA(t T) *x509.CertPool {
	t.Helper()
	_, here, _, _ := runtime.Caller(0)
	pem, err := os.ReadFile(filepath.Join(filepath.Dir(here), "..", "..", ".murphy", "mail", "ca.crt"))
	require.NoError(t, err, "the local mail CA, made by compose's mail-certs")
	roots := x509.NewCertPool()
	require.True(t, roots.AppendCertsFromPEM(pem))
	return roots
}

func Mailpit(secret string) client.EmailProviderInput {
	return client.EmailProviderInput{
		Type: client.Smtp, From: "Murphy <murphy@murphy.test>", Host: MailServerFromMurphy, Port: MailPort,
		Username: new("murphy"), Secret: &secret,
	}
}

func (m *Murphy) EmailThroughMailpit() {
	m.t.Helper()
	m.SetEmailProvider(Mailpit("env://MURPHY_SECRET_SMTP_PASSWORD"))
	m.WaitForEmailProvider(client.Ready)
}

func (m *Murphy) SetEmailProvider(input client.EmailProviderInput) {
	m.t.Helper()
	set, err := m.API.SetEmailProviderWithResponse(m.t.Context(), input)
	m.ok(set, err, "set the email provider")
}

func (m *Murphy) WaitForEmailProvider(status client.EmailProviderStatus) client.EmailProvider {
	m.t.Helper()
	var last client.EmailProvider
	require.EventuallyWithT(m.t, func(c *assert.CollectT) {
		got, err := m.API.GetEmailProviderWithResponse(m.t.Context())
		if assert.NoError(c, err) && assert.NotNil(c, got.JSON200) {
			last = *got.JSON200
			assert.Equal(c, status, last.Status, last.Problem)
		}
	}, Settle, tick)
	return last
}
