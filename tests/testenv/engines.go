//go:build integration

package testenv

import "testing"

type DatabaseEngine func(t *testing.T) string

var DatabaseEngines = map[string]DatabaseEngine{
	"postgres": PostgresDatabase,
	"mysql":    MySQLDatabase,
}

func EachDatabase(t *testing.T, test func(t *testing.T, engine DatabaseEngine)) {
	t.Helper()
	for name, engine := range DatabaseEngines {
		t.Run(name, func(t *testing.T) {
			test(t, engine)
		})
	}
}
