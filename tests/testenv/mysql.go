//go:build integration

package testenv

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"
	"uuid"

	"github.com/stretchr/testify/require"

	murphymysql "github.com/murphyhq/server/internal/mysql"
	"github.com/murphyhq/server/tests/provision"
)

func MySQLDatabase(t *testing.T) string {
	t.Helper()
	admin := os.Getenv("MURPHY_TEST_MYSQL_URL")
	if admin == "" {
		admin = "mysql://root:murphy@localhost:3306/mysql"
	}
	name := "test_" + strings.ReplaceAll(uuid.NewV4().String(), "-", "")

	require.NoError(t, provision.MySQLDatabase(t.Context(), admin, name, ""))
	t.Cleanup(func() {
		ctx := context.Background()
		db, err := murphymysql.Open(ctx, murphymysql.Config{URL: admin})
		if err != nil {
			return
		}
		_, _ = db.ExecContext(ctx, "DROP DATABASE IF EXISTS `"+name+"`")
		_ = db.Close()
	})

	u, err := url.Parse(admin)
	require.NoError(t, err)
	u.Path = "/" + name
	return u.String()
}
