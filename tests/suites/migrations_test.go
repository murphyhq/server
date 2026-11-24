//go:build integration

package suites

import (
	"testing"

	"github.com/stretchr/testify/assert"

	migrateapp "github.com/murphyhq/server/cmd/migrate/app"
	"github.com/murphyhq/server/tests/testenv"
)

func TestSeveralCopiesMigratingAtOnceAllSucceed(t *testing.T) {
	testenv.EachDatabase(t, func(t *testing.T, engine testenv.DatabaseEngine) {
		databases := migrateapp.Settings{
			CatalogDatabaseURL:       engine(t),
			IncidentsDatabaseURL:     engine(t),
			PeopleDatabaseURL:        engine(t),
			OnCallDatabaseURL:        engine(t),
			EscalationDatabaseURL:    engine(t),
			NotificationsDatabaseURL: engine(t),
		}
		copies := 3
		results := make(chan error, copies)

		for range copies {
			go func() { results <- migrateapp.Migrate(t.Context(), databases) }()
		}

		for range copies {
			assert.NoError(t, <-results, "a copy that loses the race waits for the lock instead of failing")
		}
	})
}
