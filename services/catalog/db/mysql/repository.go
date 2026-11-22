package mysql

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/internal/database"
	murphymysql "github.com/murphyhq/server/internal/mysql"
	"github.com/murphyhq/server/internal/tracer"
)

//go:embed migrations/*.sql
var migrations embed.FS

type Repository struct {
	db      *sql.DB
	queries *Queries
}

func Open(ctx context.Context, url string) (*Repository, error) {
	db, err := murphymysql.Open(ctx, murphymysql.Config{URL: url})
	if err != nil {
		return nil, err
	}
	return &Repository{db: db, queries: New(db)}, nil
}

func (r *Repository) Migrate(ctx context.Context) error {
	files, err := fs.Sub(migrations, "migrations")
	if err != nil {
		return fmt.Errorf("load catalog migrations: %w", err)
	}
	return murphymysql.Migrate(ctx, r.db, murphymysql.Migrations{Service: "catalog", Files: files})
}

func (r *Repository) Ping(ctx context.Context) error {
	return r.db.PingContext(ctx)
}

func (r *Repository) Close() {
	_ = r.db.Close()
}

func (r *Repository) CreateService(ctx context.Context, service *murphy.Service) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	return murphymysql.Error("service", r.queries.CreateService(ctx, CreateServiceParams{
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
		return nil, murphymysql.Error("service", err)
	}
	return toService(row), nil
}

func (r *Repository) ListServices(ctx context.Context, before string, limit int) (_ []*murphy.Service, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	rows, err := r.queries.ListServices(ctx, ListServicesParams{Before: before, Limit: int32(limit)})
	if err != nil {
		return nil, murphymysql.Error("service", err)
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
	return murphymysql.Changed("service", rows, err)
}

func (r *Repository) DeleteService(ctx context.Context, id string) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	rows, err := r.queries.DeleteService(ctx, id)
	return murphymysql.Changed("service", rows, err)
}

func (r *Repository) CreateIntegration(ctx context.Context, integration *murphy.Integration) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	return murphymysql.Error("integration", r.queries.CreateIntegration(ctx, CreateIntegrationParams{
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
		return nil, murphymysql.Error("integration", err)
	}
	return toIntegration(row), nil
}

func (r *Repository) ListIntegrations(ctx context.Context, serviceID, before string, limit int) (_ []*murphy.Integration, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	rows, err := r.queries.ListIntegrations(ctx, ListIntegrationsParams{ServiceID: serviceID, Before: before, Limit: int32(limit)})
	if err != nil {
		return nil, murphymysql.Error("integration", err)
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
	return murphymysql.Changed("integration", rows, err)
}

func (r *Repository) RotateIntegrationKey(ctx context.Context, id string, previousValidUntil, updatedAt time.Time) (_ *murphy.Integration, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	q := r.queries.WithTx(tx)

	rows, err := q.RotateIntegrationKey(ctx, RotateIntegrationKeyParams{
		ID:                    id,
		PreviousKeyValidUntil: &previousValidUntil,
		UpdatedAt:             updatedAt,
	})
	if err := murphymysql.Changed("integration", rows, err); err != nil {
		return nil, err
	}
	row, err := q.GetIntegration(ctx, id)
	if err != nil {
		return nil, murphymysql.Error("integration", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, murphymysql.Error("integration", err)
	}
	return toIntegration(row), nil
}

func (r *Repository) DeleteIntegration(ctx context.Context, id string) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	rows, err := r.queries.DeleteIntegration(ctx, id)
	return murphymysql.Changed("integration", rows, err)
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
		PreviousKeyValidUntil: database.Timestamp(row.PreviousKeyValidUntil),
		Status:                murphy.IntegrationStatusFromText(row.Status),
		CreatedAt:             timestamppb.New(row.CreatedAt),
		UpdatedAt:             timestamppb.New(row.UpdatedAt),
	}
}
