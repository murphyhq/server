//go:build integration

package suites

import (
	"github.com/murphyhq/server/services/notifications"
	notificationsdb "github.com/murphyhq/server/services/notifications/db"
	notificationsstoragetest "github.com/murphyhq/server/services/notifications/db/storagetest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/murphyhq/server/services/catalog"
	catalogdb "github.com/murphyhq/server/services/catalog/db"
	catalogstoragetest "github.com/murphyhq/server/services/catalog/db/storagetest"
	"github.com/murphyhq/server/services/escalation"
	escalationdb "github.com/murphyhq/server/services/escalation/db"
	escalationstoragetest "github.com/murphyhq/server/services/escalation/db/storagetest"
	"github.com/murphyhq/server/services/incidents"
	incidentsdb "github.com/murphyhq/server/services/incidents/db"
	"github.com/murphyhq/server/services/incidents/db/storagetest"
	"github.com/murphyhq/server/services/oncall"
	oncalldb "github.com/murphyhq/server/services/oncall/db"
	oncallstoragetest "github.com/murphyhq/server/services/oncall/db/storagetest"
	peopledb "github.com/murphyhq/server/services/people/db"
	peoplestoragetest "github.com/murphyhq/server/services/people/db/storagetest"
	"github.com/murphyhq/server/tests/testenv"
)

func TestCatalogStorage(t *testing.T) {
	testenv.EachDatabase(t, func(t *testing.T, engine testenv.DatabaseEngine) {
		catalogstoragetest.Run(t, func(t *testing.T) catalog.Repository {
			storage, err := catalogdb.Open(t.Context(), testenv.Databases(t, engine).CatalogDatabaseURL)
			require.NoError(t, err)
			t.Cleanup(storage.Close)
			return storage
		})
	})
}

func TestIncidentsStorage(t *testing.T) {
	testenv.EachDatabase(t, func(t *testing.T, engine testenv.DatabaseEngine) {
		storagetest.Run(t, func(t *testing.T) incidents.Repository {
			storage, err := incidentsdb.Open(t.Context(), testenv.Databases(t, engine).IncidentsDatabaseURL)
			require.NoError(t, err)
			t.Cleanup(storage.Close)
			return storage
		})
	})
}

func TestPeopleStorage(t *testing.T) {
	testenv.EachDatabase(t, func(t *testing.T, engine testenv.DatabaseEngine) {
		peoplestoragetest.Run(t, func(t *testing.T) peoplestoragetest.Storage {
			storage, err := peopledb.Open(t.Context(), testenv.Databases(t, engine).PeopleDatabaseURL)
			require.NoError(t, err)
			t.Cleanup(storage.Close)
			return storage
		})
	})
}

func TestOnCallStorage(t *testing.T) {
	testenv.EachDatabase(t, func(t *testing.T, engine testenv.DatabaseEngine) {
		oncallstoragetest.Run(t, func(t *testing.T) oncall.Repository {
			storage, err := oncalldb.Open(t.Context(), testenv.Databases(t, engine).OnCallDatabaseURL)
			require.NoError(t, err)
			t.Cleanup(storage.Close)
			return storage
		})
	})
}

func TestEscalationStorage(t *testing.T) {
	testenv.EachDatabase(t, func(t *testing.T, engine testenv.DatabaseEngine) {
		escalationstoragetest.Run(t, func(t *testing.T) escalation.Repository {
			storage, err := escalationdb.Open(t.Context(), testenv.Databases(t, engine).EscalationDatabaseURL)
			require.NoError(t, err)
			t.Cleanup(storage.Close)
			return storage
		})
	})
}

func TestNotificationsStorage(t *testing.T) {
	testenv.EachDatabase(t, func(t *testing.T, engine testenv.DatabaseEngine) {
		notificationsstoragetest.Run(t, func(t *testing.T) notifications.Repository {
			storage, err := notificationsdb.Open(t.Context(), testenv.Databases(t, engine).NotificationsDatabaseURL)
			require.NoError(t, err)
			t.Cleanup(storage.Close)
			return storage
		})
	})
}
