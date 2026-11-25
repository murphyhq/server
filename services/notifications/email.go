package notifications

import (
	"context"
	"errors"
	"fmt"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/internal/telemetry"
	"github.com/murphyhq/server/internal/tracer"
)

type Secrets interface {
	Read(ctx context.Context, reference string) (string, error)
}

type EmailSender interface {
	Use(ctx context.Context, provider *murphy.EmailProvider, secret string) error
	Stop()
}

type EmailCheck struct {
	repo    Repository
	secrets Secrets
	sender  EmailSender
}

func NewEmailCheck(repo Repository, secrets Secrets, sender EmailSender) *EmailCheck {
	return &EmailCheck{repo: repo, secrets: secrets, sender: sender}
}

func (e *EmailCheck) Run(ctx context.Context) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	provider, err := e.repo.GetEmailProvider(ctx)
	if errors.Is(err, murphy.ErrNotFound) {
		e.sender.Stop()
		return nil
	}
	if err != nil {
		return fmt.Errorf("get email provider: %w", err)
	}
	provider.Status, provider.Problem, provider.CheckedAt = murphy.EmailProviderStatus_EMAIL_PROVIDER_STATUS_READY, "", murphy.Now()
	if problem := e.try(ctx, provider); problem != nil {
		provider.Status, provider.Problem = murphy.EmailProviderStatus_EMAIL_PROVIDER_STATUS_FAILING, problem.Error()
		span.Set(ctx, telemetry.WithField("email_provider.problem", provider.GetProblem()))
	}
	if err := e.repo.RecordEmailProviderCheck(ctx, provider); err != nil {
		return fmt.Errorf("record the email provider's check: %w", err)
	}
	return nil
}

func (e *EmailCheck) try(ctx context.Context, provider *murphy.EmailProvider) error {
	var secret string
	if provider.GetSecret() != "" {
		read, err := e.secrets.Read(ctx, provider.GetSecret())
		if err != nil {
			return fmt.Errorf("couldn't read the secret: %w", err)
		}
		secret = read
	}
	return e.sender.Use(ctx, provider, secret)
}
