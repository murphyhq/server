package app

import (
	"context"
	"fmt"
	"io"

	"github.com/murphyhq/server/services/people"
	peopledb "github.com/murphyhq/server/services/people/db"
)

type Settings struct {
	PeopleDatabaseURL string `env:"PEOPLE_DATABASE_URL" required:"true"`
	PublicURL         string `env:"PUBLIC_URL" required:"true" validate:"base-url"`
	InitialAdminEmail string `env:"INITIAL_ADMIN_EMAIL" required:"true"`
	OIDCIssuer        string `env:"OIDC_ISSUER" validate:"http-url"`
	AllowPasswords    bool   `env:"ALLOW_PASSWORDS"`
}

func Recover(ctx context.Context, s Settings, out io.Writer) error {
	storage, err := peopledb.Open(ctx, s.PeopleDatabaseURL)
	if err != nil {
		return fmt.Errorf("MURPHY_PEOPLE_DATABASE_URL: %w", err)
	}
	defer storage.Close()

	auth := people.NewAuth(storage, nil, nil, people.AuthSettings{
		InitialAdminEmail: s.InitialAdminEmail,
		PublicURL:         s.PublicURL,
		PasswordsAllowed:  s.OIDCIssuer == "" || s.AllowPasswords,
	})
	link, err := auth.RecoveryLink(ctx)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(out, "Open this link to set a password for %s, the admin. It works once, until %s.\n\n%s\n",
		s.InitialAdminEmail, link.ExpiresAt.Format("2006-01-02 15:04 MST"), link.Link)
	return err
}
