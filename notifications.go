package murphy

import (
	"errors"
	"net/mail"
)

func (p *EmailProvider) Validate() error {
	var errs []error
	if p.GetType() != EmailProviderType_EMAIL_PROVIDER_TYPE_SMTP {
		errs = append(errs, invalid("type", "must be smtp"))
	}
	if _, err := mail.ParseAddress(p.GetFrom()); err != nil {
		errs = append(errs, invalid("from", "must be an email address, as in Murphy <murphy@example.com>"))
	}
	if p.GetHost() == "" {
		errs = append(errs, invalid("host", "is required"))
	}
	if p.GetPort() < 1 || p.GetPort() > 65535 {
		errs = append(errs, invalid("port", "must be between 1 and 65535"))
	}
	if p.GetSecret() != "" {
		errs = append(errs, validSecretReference("secret", p.GetSecret()))
	}
	if p.GetSecret() != "" && p.GetUsername() == "" {
		errs = append(errs, invalid("username", "is required with a secret"))
	}
	return errors.Join(errs...)
}

func (t EmailProviderType) Text() string {
	return enumText(t, "EMAIL_PROVIDER_TYPE_")
}

func EmailProviderTypeFromText(text string) EmailProviderType {
	return enumFromText[EmailProviderType](EmailProviderType_value, "EMAIL_PROVIDER_TYPE_", text)
}
