package notifications_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/internal/secrets"
	"github.com/murphyhq/server/services/notifications"
)

type emailSetup struct {
	email    *notifications.Email
	repo     *MockRepository
	secrets  *MockSecrets
	sender   *MockEmailSender
	recorded *murphy.EmailProvider
}

func newEmailSetup(t *testing.T, provider *murphy.EmailProvider) *emailSetup {
	e := &emailSetup{repo: NewMockRepository(t), secrets: NewMockSecrets(t), sender: NewMockEmailSender(t)}
	e.email = notifications.NewEmail(e.repo, e.secrets, e.sender)
	e.repo.EXPECT().GetEmailProvider(mock.Anything).Return(provider, nil).Once()
	e.repo.EXPECT().RecordEmailProviderCheck(mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, checked *murphy.EmailProvider) error {
		e.recorded = checked
		return nil
	}).Maybe()
	return e
}

func TestAProviderThatSignsInIsReady(t *testing.T) {
	e := newEmailSetup(t, smtpProvider())
	e.secrets.EXPECT().Read(mock.Anything, "env://SMTP_PASSWORD").Return("hunter2", nil).Once()
	e.sender.EXPECT().Use(mock.Anything, mock.Anything, "hunter2").Return(nil).Once()

	require.NoError(t, e.email.Refresh(t.Context()))

	assert.Equal(t, murphy.EmailProviderStatus_EMAIL_PROVIDER_STATUS_READY, e.recorded.GetStatus())
	assert.Empty(t, e.recorded.GetProblem())
	assert.NotNil(t, e.recorded.GetCheckedAt())
}

func TestAPasswordThatCantBeReadFailsTheProviderWithoutTryingIt(t *testing.T) {
	e := newEmailSetup(t, smtpProvider())
	e.secrets.EXPECT().Read(mock.Anything, mock.Anything).Return("", errors.Join(secrets.ErrMissing, errors.New("SMTP_PASSWORD isn't set"))).Once()

	require.NoError(t, e.email.Refresh(t.Context()))

	assert.Equal(t, murphy.EmailProviderStatus_EMAIL_PROVIDER_STATUS_FAILING, e.recorded.GetStatus())
	assert.Contains(t, e.recorded.GetProblem(), "couldn't read the password")
	assert.Contains(t, e.recorded.GetProblem(), "SMTP_PASSWORD isn't set")
}

func TestAMailServerThatRefusesTheSignInFailsTheProvider(t *testing.T) {
	e := newEmailSetup(t, smtpProvider())
	e.secrets.EXPECT().Read(mock.Anything, mock.Anything).Return("wrong", nil).Once()
	e.sender.EXPECT().Use(mock.Anything, mock.Anything, "wrong").Return(errors.New("sign in to the mail server: 535 authentication failed")).Once()

	require.NoError(t, e.email.Refresh(t.Context()))

	assert.Equal(t, murphy.EmailProviderStatus_EMAIL_PROVIDER_STATUS_FAILING, e.recorded.GetStatus())
	assert.Equal(t, "sign in to the mail server: 535 authentication failed", e.recorded.GetProblem())
}

func TestWithoutAProviderNothingIsSent(t *testing.T) {
	repo, sender := NewMockRepository(t), NewMockEmailSender(t)
	repo.EXPECT().GetEmailProvider(mock.Anything).Return(nil, &murphy.NotFoundError{Entity: "email provider"}).Once()
	sender.EXPECT().Stop().Once()

	err := notifications.NewEmail(repo, NewMockSecrets(t), sender).Refresh(t.Context())

	require.NoError(t, err)
}
