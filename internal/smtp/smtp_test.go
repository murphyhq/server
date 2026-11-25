//go:build integration

package smtp_test

import (
	"crypto/x509"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/internal/smtp"
	"github.com/murphyhq/server/tests/murphytest"
)

func mailpit(roots *x509.CertPool) smtp.Server {
	return smtp.Server{Host: murphytest.MailServer, Port: murphytest.MailPort, Username: "murphy", Password: "any", Roots: roots}
}

func TestAMailServerThatOffersTLSAndSignInPassesTheCheck(t *testing.T) {
	err := mailpit(murphytest.MailCA(t)).Check(t.Context())

	require.NoError(t, err)
}

func TestAMailServerWithAnUntrustedCertificateFailsTheCheck(t *testing.T) {
	err := mailpit(x509.NewCertPool()).Check(t.Context())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "start TLS")
}

func TestAMessageReachesTheMailbox(t *testing.T) {
	to := murphy.NewID() + "@example.com"

	err := mailpit(murphytest.MailCA(t)).Send(t.Context(), "murphy@example.com", to, []byte("To: "+to+"\r\nSubject: Disk full\r\n\r\nDisk full on db-1.\r\n"))

	require.NoError(t, err)
	assert.Equal(t, "Disk full", murphytest.NewMailbox(t, to).WaitForMail(1)[0].Subject)
}
