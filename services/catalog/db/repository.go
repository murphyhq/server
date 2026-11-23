package db

import (
	"context"
	"time"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/internal/tracer"
	"github.com/murphyhq/server/services/catalog"
)

type Tx interface {
	RotateIntegrationKeyRow(ctx context.Context, id string, previousValidUntil, updatedAt time.Time) error
	GetIntegration(ctx context.Context, id string) (*murphy.Integration, error)
}

type Engine[T Tx] interface {
	CreateService(ctx context.Context, service *murphy.Service) error
	GetService(ctx context.Context, id string) (*murphy.Service, error)
	ListServices(ctx context.Context, before string, limit int) ([]*murphy.Service, error)
	UpdateService(ctx context.Context, service *murphy.Service) error
	DeleteService(ctx context.Context, id string) error
	CreateIntegration(ctx context.Context, integration *murphy.Integration) error
	GetIntegration(ctx context.Context, id string) (*murphy.Integration, error)
	ListIntegrations(ctx context.Context, serviceID, before string, limit int) ([]*murphy.Integration, error)
	UpdateIntegration(ctx context.Context, integration *murphy.Integration) error
	DeleteIntegration(ctx context.Context, id string) error
	PolicyInUse(ctx context.Context, policyID string) (bool, error)
	GetIntegrationAndService(ctx context.Context, integrationID string) (*murphy.Integration, *murphy.Service, error)
	Transaction(ctx context.Context, work func(tx T) error) error
	Migrate(ctx context.Context) error
	Ping(ctx context.Context) error
	Close()
}

type Repository[T Tx] struct {
	Engine[T]
}

var _ catalog.Repository = (*Repository[Tx])(nil)

func (r *Repository[T]) RotateIntegrationKey(ctx context.Context, id string, previousValidUntil, updatedAt time.Time) (_ *murphy.Integration, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	var integration *murphy.Integration
	err = r.Transaction(ctx, func(tx T) error {
		if err := tx.RotateIntegrationKeyRow(ctx, id, previousValidUntil, updatedAt); err != nil {
			return err
		}
		integration, err = tx.GetIntegration(ctx, id)
		return err
	})
	return integration, err
}
