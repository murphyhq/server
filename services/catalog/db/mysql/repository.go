package mysql

import (
	"context"
	"embed"
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
	*murphymysql.Database[*Tx]
	queries *Queries
}

func Open(ctx context.Context, url string) (*Repository, error) {
	db, err := murphymysql.OpenDatabase(ctx, murphymysql.DatabaseConfig[*Tx]{
		URL:        url,
		Service:    "catalog",
		Migrations: migrations,
		Tx:         func(tx murphymysql.DBTX) *Tx { return &Tx{queries: New(tx)} },
	})
	if err != nil {
		return nil, err
	}
	return &Repository{Database: db, queries: New(db.DB())}, nil
}

func (r *Repository) CreateService(ctx context.Context, service *murphy.Service) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	return murphymysql.Error("service", r.queries.CreateService(ctx, CreateServiceParams{
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
	return murphymysql.Changed("integration", rows, err)
}

func (t *Tx) GetIntegration(ctx context.Context, id string) (_ *murphy.Integration, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	row, err := t.queries.GetIntegration(ctx, id)
	if err != nil {
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

func (r *Repository) PolicyInUse(ctx context.Context, policyID string) (_ bool, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	inUse, err := r.queries.PolicyInUse(ctx, policyID)
	if err != nil {
		return false, murphymysql.Error("service", err)
	}
	return inUse, nil
}

func (r *Repository) GetIntegrationAndService(ctx context.Context, integrationID string) (_ *murphy.Integration, _ *murphy.Service, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	row, err := r.queries.GetIntegrationAndService(ctx, integrationID)
	if err != nil {
		return nil, nil, murphymysql.Error("integration", err)
	}
	return toIntegration(row.Integration), toService(row.Service), nil
}

func (r *Repository) ServiceNames(ctx context.Context, ids []string) (_ map[string]string, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	rows, err := r.queries.ServiceNames(ctx, ids)
	if err != nil {
		return nil, murphymysql.Error("service", err)
	}
	names := make(map[string]string, len(rows))
	for _, row := range rows {
		names[row.ID] = row.Name
	}
	return names, nil
}
