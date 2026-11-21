package app

import (
	"context"
	"fmt"
	"net/http"

	"github.com/murphyhq/server/cmd/api/router"
	idempotencystore "github.com/murphyhq/server/internal/idempotency/store"
	"github.com/murphyhq/server/internal/lifecycle"
	"github.com/murphyhq/server/services/catalog"
	catalogdb "github.com/murphyhq/server/services/catalog/db"
)

type Settings struct {
	HTTPAddr            string `env:"HTTP_ADDR" default:":8080"`
	CatalogDatabaseURL  string `env:"CATALOG_DATABASE_URL" required:"true"`
	IdempotencyStoreURL string `env:"IDEMPOTENCY_STORE_URL" required:"true"`
}

type App struct {
	Handler http.Handler
	Ready   []lifecycle.Check
	closers []func()
}

func New(ctx context.Context, s Settings) (_ *App, err error) {
	a := &App{}
	defer func() {
		if err != nil {
			a.Close()
		}
	}()

	catalogStorage, err := catalogdb.Open(ctx, s.CatalogDatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("MURPHY_CATALOG_DATABASE_URL: %w", err)
	}
	a.closers = append(a.closers, catalogStorage.Close)

	keys, err := idempotencystore.Open(ctx, s.IdempotencyStoreURL)
	if err != nil {
		return nil, fmt.Errorf("MURPHY_IDEMPOTENCY_STORE_URL: %w", err)
	}
	a.closers = append(a.closers, func() { _ = keys.Close() })

	a.Handler, err = router.New(catalog.New(catalogStorage), keys)
	if err != nil {
		return nil, err
	}
	a.Ready = []lifecycle.Check{catalogStorage.Ping, keys.Ping}
	return a, nil
}

func (a *App) Close() {
	for i := len(a.closers) - 1; i >= 0; i-- {
		a.closers[i]()
	}
}
