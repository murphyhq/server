package postgres

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/murphyhq/server"
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

	return storeError("service", r.queries.CreateService(ctx, CreateServiceParams{
		ID:                 service.GetId(),
		Name:               service.GetName(),
		Description:        service.GetDescription(),
		TeamID:             service.GetTeamId(),
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
		return nil, storeError("service", err)
	}
	return toService(row), nil
}

func (r *Repository) ListServices(ctx context.Context, before string, limit int) (_ []*murphy.Service, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	rows, err := r.queries.ListServices(ctx, ListServicesParams{Before: before, MaxRows: int32(limit)})
	if err != nil {
		return nil, storeError("service", err)
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
		TeamID:             service.GetTeamId(),
		EscalationPolicyID: service.GetEscalationPolicyId(),
		Urgency:            service.GetUrgency().Text(),
		Status:             service.GetStatus().Text(),
		UpdatedAt:          service.GetUpdatedAt().AsTime(),
	})
	return changed("service", rows, err)
}

func (r *Repository) DeleteService(ctx context.Context, id string) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	rows, err := r.queries.DeleteService(ctx, id)
	return changed("service", rows, err)
}

func (r *Repository) CreateIntegration(ctx context.Context, integration *murphy.Integration) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	return storeError("integration", r.queries.CreateIntegration(ctx, CreateIntegrationParams{
		ID:                    integration.GetId(),
		ServiceID:             integration.GetServiceId(),
		Name:                  integration.GetName(),
		Type:                  integration.GetType().Text(),
		KeyVersion:            integration.GetKeyVersion(),
		PreviousKeyValidUntil: fromTimestamp(integration.GetPreviousKeyValidUntil()),
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
		return nil, storeError("integration", err)
	}
	return toIntegration(row), nil
}

func (r *Repository) ListIntegrations(ctx context.Context, serviceID, before string, limit int) (_ []*murphy.Integration, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	rows, err := r.queries.ListIntegrations(ctx, ListIntegrationsParams{ServiceID: serviceID, Before: before, MaxRows: int32(limit)})
	if err != nil {
		return nil, storeError("integration", err)
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
	return changed("integration", rows, err)
}

func (r *Repository) DeleteIntegration(ctx context.Context, id string) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	rows, err := r.queries.DeleteIntegration(ctx, id)
	return changed("integration", rows, err)
}

func toService(row Service) *murphy.Service {
	return &murphy.Service{
		Id:                 row.ID,
		Name:               row.Name,
		Description:        row.Description,
		TeamId:             row.TeamID,
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
		PreviousKeyValidUntil: toTimestamp(row.PreviousKeyValidUntil),
		Status:                murphy.IntegrationStatusFromText(row.Status),
		CreatedAt:             timestamppb.New(row.CreatedAt),
		UpdatedAt:             timestamppb.New(row.UpdatedAt),
	}
}

func fromTimestamp(ts *timestamppb.Timestamp) *time.Time {
	if ts == nil {
		return nil
	}
	t := ts.AsTime()
	return &t
}

func toTimestamp(t *time.Time) *timestamppb.Timestamp {
	if t == nil {
		return nil
	}
	return timestamppb.New(*t)
}

func storeError(entity string, err error) error {
	var pgErr *pgconn.PgError
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return &murphy.NotFoundError{Entity: entity}
	case errors.As(err, &pgErr) && pgErr.Code == "23505":
		return murphy.ErrConflict
	}
	return err
}

func changed(entity string, rows int64, err error) error {
	if err != nil {
		return storeError(entity, err)
	}
	if rows == 0 {
		return &murphy.NotFoundError{Entity: entity}
	}
	return nil
}
