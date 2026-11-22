//go:build integration

package integration

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/murphyhq/server/services/catalog"
	catalogdb "github.com/murphyhq/server/services/catalog/db"
	catalogstoragetest "github.com/murphyhq/server/services/catalog/db/storagetest"
	"github.com/murphyhq/server/services/incidents"
	incidentsdb "github.com/murphyhq/server/services/incidents/db"
	"github.com/murphyhq/server/services/incidents/db/storagetest"
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
