package postgres

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/internal/database"
	murphypostgres "github.com/murphyhq/server/internal/postgres"
	"github.com/murphyhq/server/internal/tracer"
)

//go:embed migrations/*.sql
var migrations embed.FS

type Repository struct {
	pool    *pgxpool.Pool
	queries *Queries
}

func Open(ctx context.Context, url string) (*Repository, error) {
	pool, err := murphypostgres.Open(ctx, murphypostgres.Config{URL: url})
	if err != nil {
		return nil, err
	}
	return &Repository{pool: pool, queries: New(pool)}, nil
}

func (r *Repository) Migrate(ctx context.Context) error {
	files, err := fs.Sub(migrations, "migrations")
	if err != nil {
		return fmt.Errorf("load catalog migrations: %w", err)
	}
	return murphypostgres.Migrate(ctx, r.pool, murphypostgres.Migrations{Service: "catalog", Files: files})
}

func (r *Repository) Ping(ctx context.Context) error {
	return r.pool.Ping(ctx)
}

func (r *Repository) Close() {
	r.pool.Close()
}

func (r *Repository) CreateService(ctx context.Context, service *murphy.Service) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	return murphypostgres.Error("service", r.queries.CreateService(ctx, CreateServiceParams{
		ID:                 service.GetId(),
		Name:               service.GetName(),
		Description:        service.GetDescription(),
		EscalationPolicyID: service.GetEscalationPolicyId(),
		Urgency:            service.GetUrgency().Text(),
		Status:             service.GetStatus().Text(),
		CreatedAt:          service.GetCreatedAt().AsTime(),
		UpdatedAt:          service.GetUpdatedAt().AsTime(),
	}))
}

func (r *Repository) GetService(ctx context.Context, id string) (_ *murphy.Service, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	row, err := r.queries.GetService(ctx, id)
	if err != nil {
		return nil, murphypostgres.Error("service", err)
	}
	return toService(row), nil
}

func (r *Repository) ListServices(ctx context.Context, before string, limit int) (_ []*murphy.Service, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	rows, err := r.queries.ListServices(ctx, ListServicesParams{Before: before, MaxRows: int32(limit)})
	if err != nil {
		return nil, murphypostgres.Error("service", err)
	}
	services := make([]*murphy.Service, len(rows))
	for i, row := range rows {
		services[i] = toService(row)
	}
	return services, nil
}

func (r *Repository) UpdateService(ctx context.Context, service *murphy.Service) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	rows, err := r.queries.UpdateService(ctx, UpdateServiceParams{
		ID:                 service.GetId(),
		Name:               service.GetName(),
		Description:        service.GetDescription(),
		EscalationPolicyID: service.GetEscalationPolicyId(),
		Urgency:            service.GetUrgency().Text(),
		Status:             service.GetStatus().Text(),
		UpdatedAt:          service.GetUpdatedAt().AsTime(),
	})
	return murphypostgres.Changed("service", rows, err)
}

func (r *Repository) DeleteService(ctx context.Context, id string) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	rows, err := r.queries.DeleteService(ctx, id)
	return murphypostgres.Changed("service", rows, err)
}

func (r *Repository) CreateIntegration(ctx context.Context, integration *murphy.Integration) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	return murphypostgres.Error("integration", r.queries.CreateIntegration(ctx, CreateIntegrationParams{
		ID:                    integration.GetId(),
		ServiceID:             integration.GetServiceId(),
		Name:                  integration.GetName(),
		Type:                  integration.GetType().Text(),
		KeyVersion:            integration.GetKeyVersion(),
		PreviousKeyValidUntil: database.Time(integration.GetPreviousKeyValidUntil()),
		Status:                integration.GetStatus().Text(),
		CreatedAt:             integration.GetCreatedAt().AsTime(),
		UpdatedAt:             integration.GetUpdatedAt().AsTime(),
	}))
}

func (r *Repository) GetIntegration(ctx context.Context, id string) (_ *murphy.Integration, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	row, err := r.queries.GetIntegration(ctx, id)
	if err != nil {
		return nil, murphypostgres.Error("integration", err)
	}
	return toIntegration(row), nil
}

func (r *Repository) ListIntegrations(ctx context.Context, serviceID, before string, limit int) (_ []*murphy.Integration, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	rows, err := r.queries.ListIntegrations(ctx, ListIntegrationsParams{ServiceID: serviceID, Before: before, MaxRows: int32(limit)})
	if err != nil {
		return nil, murphypostgres.Error("integration", err)
	}
	integrations := make([]*murphy.Integration, len(rows))
	for i, row := range rows {
		integrations[i] = toIntegration(row)
	}
	return integrations, nil
}

func (r *Repository) UpdateIntegration(ctx context.Context, integration *murphy.Integration) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	rows, err := r.queries.UpdateIntegration(ctx, UpdateIntegrationParams{
		ID:        integration.GetId(),
		Name:      integration.GetName(),
		Status:    integration.GetStatus().Text(),
		UpdatedAt: integration.GetUpdatedAt().AsTime(),
	})
	return murphypostgres.Changed("integration", rows, err)
}

func (r *Repository) Transaction(ctx context.Context, work func(tx *Tx) error) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	if err := work(&Tx{queries: r.queries.WithTx(tx)}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return murphypostgres.Error("integration", err)
	}
	return nil
}

type Tx struct {
	queries *Queries
}

func (t *Tx) RotateIntegrationKeyRow(ctx context.Context, id string, previousValidUntil, updatedAt time.Time) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	rows, err := t.queries.RotateIntegrationKey(ctx, RotateIntegrationKeyParams{
		ID:                    id,
		PreviousKeyValidUntil: &previousValidUntil,
		UpdatedAt:             updatedAt,
	})
	return murphypostgres.Changed("integration", rows, err)
}

func (t *Tx) GetIntegration(ctx context.Context, id string) (_ *murphy.Integration, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	row, err := t.queries.GetIntegration(ctx, id)
	if err != nil {
		return nil, murphypostgres.Error("integration", err)
	}
	return toIntegration(row), nil
}

func (r *Repository) DeleteIntegration(ctx context.Context, id string) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	rows, err := r.queries.DeleteIntegration(ctx, id)
	return murphypostgres.Changed("integration", rows, err)
}

func toService(row Service) *murphy.Service {
	return &murphy.Service{
		Id:                 row.ID,
		Name:               row.Name,
		Description:        row.Description,
		EscalationPolicyId: row.EscalationPolicyID,
		Urgency:            murphy.UrgencyFromText(row.Urgency),
		Status:             murphy.ServiceStatusFromText(row.Status),
		CreatedAt:          timestamppb.New(row.CreatedAt),
		UpdatedAt:          timestamppb.New(row.UpdatedAt),
	}
}

func toIntegration(row Integration) *murphy.Integration {
	return &murphy.Integration{
		Id:                    row.ID,
		ServiceId:             row.ServiceID,
		Name:                  row.Name,
		Type:                  murphy.IntegrationTypeFromText(row.Type),
		KeyVersion:            row.KeyVersion,
		PreviousKeyValidUntil: database.Timestamp(row.PreviousKeyValidUntil),
		Status:                murphy.IntegrationStatusFromText(row.Status),
		CreatedAt:             timestamppb.New(row.CreatedAt),
		UpdatedAt:             timestamppb.New(row.UpdatedAt),
	}
}
