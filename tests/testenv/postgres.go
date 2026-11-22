//go:build integration

package testenv

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/murphyhq/server/tests/provision"
)

func Database(t *testing.T) string {
	t.Helper()
	ctx := t.Context()

	admin := os.Getenv("MURPHY_TEST_POSTGRES_URL")
	if admin == "" {
		admin = "postgres://murphy:murphy@localhost:5432/murphy"
	}
	name := "test_" + strings.ReplaceAll(uuid.NewV4().String(), "-", "")

	require.NoError(t, provision.Database(ctx, admin, name))
	t.Cleanup(func() {
		ctx := context.Background()
		conn, err := pgx.Connect(ctx, admin)
		if err != nil {
			return
		}
		_, _ = conn.Exec(ctx, "DROP DATABASE "+name+" WITH (FORCE)")
		_ = conn.Close(ctx)
	})

	u, err := url.Parse(admin)
	require.NoError(t, err)
	u.Path = "/" + name
	return u.String()
}
