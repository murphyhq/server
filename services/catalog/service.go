package catalog

import (
	"context"
	"fmt"
	"time"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/internal/pagination"
	"github.com/murphyhq/server/internal/telemetry"
	"github.com/murphyhq/server/internal/tracer"
)

type Repository interface {
	CreateService(ctx context.Context, service *murphy.Service) error
	GetService(ctx context.Context, id string) (*murphy.Service, error)
	ListServices(ctx context.Context, before string, limit int) ([]*murphy.Service, error)
	UpdateService(ctx context.Context, service *murphy.Service) error
	DeleteService(ctx context.Context, id string) error
	CreateIntegration(ctx context.Context, integration *murphy.Integration) error
	GetIntegration(ctx context.Context, id string) (*murphy.Integration, error)
	ListIntegrations(ctx context.Context, serviceID, before string, limit int) ([]*murphy.Integration, error)
	UpdateIntegration(ctx context.Context, integration *murphy.Integration) error
	RotateIntegrationKey(ctx context.Context, id string, previousValidUntil, updatedAt time.Time) (*murphy.Integration, error)
	DeleteIntegration(ctx context.Context, id string) error
}

const previousKeyGrace = 24 * time.Hour

type Service struct {
	repo Repository
	keys *Keyring
}

func New(repo Repository, keys *Keyring) *Service {
	return &Service{repo: repo, keys: keys}
}

func (s *Service) CreateService(ctx context.Context, in *murphy.Service) (_ *murphy.Service, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	now := murphy.Now()
	service := &murphy.Service{
		Id:                 murphy.NewID(),
		Name:               in.GetName(),
		Description:        in.GetDescription(),
		TeamId:             in.GetTeamId(),
		EscalationPolicyId: in.GetEscalationPolicyId(),
		Urgency:            murphy.Or(in.GetUrgency(), murphy.Urgency_URGENCY_HIGH),
		Status:             murphy.Or(in.GetStatus(), murphy.ServiceStatus_SERVICE_STATUS_ACTIVE),
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	ctx = span.Set(ctx, telemetry.WithField("service.id", service.GetId()))
	if err := service.Validate(); err != nil {
		return nil, err
	}
	if err := s.repo.CreateService(ctx, service); err != nil {
		return nil, fmt.Errorf("create service: %w", err)
	}
	return service, nil
}

func (s *Service) GetService(ctx context.Context, id string) (_ *murphy.Service, err error) {
	ctx, span := tracer.Span(ctx)
	ctx = span.Set(ctx, telemetry.WithField("service.id", id))
	defer span.End(&err)

	if err := murphy.LookupID("service", id); err != nil {
		return nil, err
	}
	service, err := s.repo.GetService(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get service: %w", err)
	}
	return service, nil
}

func (s *Service) ListServices(ctx context.Context, page pagination.Page) (_ []*murphy.Service, _ string, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	window, err := pagination.Resolve(page)
	if err != nil {
		return nil, "", err
	}
	services, err := s.repo.ListServices(ctx, window.Before, window.Rows())
	if err != nil {
		return nil, "", fmt.Errorf("list services: %w", err)
	}
	items, next := pagination.Trim(services, window)
	return items, next, nil
}

func (s *Service) UpdateService(ctx context.Context, in *murphy.Service) (_ *murphy.Service, err error) {
	ctx, span := tracer.Span(ctx)
	ctx = span.Set(ctx, telemetry.WithField("service.id", in.GetId()))
	defer span.End(&err)

	current, err := s.GetService(ctx, in.GetId())
	if err != nil {
		return nil, err
	}
	service := &murphy.Service{
		Id:                 current.GetId(),
		Name:               in.GetName(),
		Description:        in.GetDescription(),
		TeamId:             in.GetTeamId(),
		EscalationPolicyId: in.GetEscalationPolicyId(),
		Urgency:            murphy.Or(in.GetUrgency(), current.GetUrgency()),
		Status:             murphy.Or(in.GetStatus(), current.GetStatus()),
		CreatedAt:          current.GetCreatedAt(),
		UpdatedAt:          murphy.Now(),
	}
	if err := service.Validate(); err != nil {
		return nil, err
	}
	if err := s.repo.UpdateService(ctx, service); err != nil {
		return nil, fmt.Errorf("update service: %w", err)
	}
	return service, nil
}

func (s *Service) DeleteService(ctx context.Context, id string) (err error) {
	ctx, span := tracer.Span(ctx)
	ctx = span.Set(ctx, telemetry.WithField("service.id", id))
	defer span.End(&err)

	if err := murphy.LookupID("service", id); err != nil {
		return err
	}
	if err := s.repo.DeleteService(ctx, id); err != nil {
		return fmt.Errorf("delete service: %w", err)
	}
	return nil
}

func (s *Service) CreateIntegration(ctx context.Context, in *murphy.Integration) (_ *murphy.Integration, _ string, err error) {
	ctx, span := tracer.Span(ctx)
	ctx = span.Set(ctx, telemetry.WithField("service.id", in.GetServiceId()))
	defer span.End(&err)

	now := murphy.Now()
	integration := &murphy.Integration{
		Id:         murphy.NewID(),
		ServiceId:  in.GetServiceId(),
		Name:       in.GetName(),
		Type:       in.GetType(),
		KeyVersion: 1,
		Status:     murphy.Or(in.GetStatus(), murphy.IntegrationStatus_INTEGRATION_STATUS_ACTIVE),
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	ctx = span.Set(ctx, telemetry.WithField("integration.id", integration.GetId()))
	if err := integration.Validate(); err != nil {
		return nil, "", err
	}
	if _, err := s.GetService(ctx, integration.GetServiceId()); err != nil {
		return nil, "", err
	}
	key, err := s.keys.Sign(integration)
	if err != nil {
		return nil, "", err
	}
	if err := s.repo.CreateIntegration(ctx, integration); err != nil {
		return nil, "", fmt.Errorf("create integration: %w", err)
	}
	return integration, key, nil
}

func (s *Service) RotateIntegrationKey(ctx context.Context, id string) (_ *murphy.Integration, _ string, err error) {
	ctx, span := tracer.Span(ctx)
	ctx = span.Set(ctx, telemetry.WithField("integration.id", id))
	defer span.End(&err)

	if err := murphy.LookupID("integration", id); err != nil {
		return nil, "", err
	}
	now := murphy.Now()
	integration, err := s.repo.RotateIntegrationKey(ctx, id, now.AsTime().Add(previousKeyGrace), now.AsTime())
	if err != nil {
		return nil, "", fmt.Errorf("rotate integration key: %w", err)
	}
	key, err := s.keys.Sign(integration)
	if err != nil {
		return nil, "", err
	}
	return integration, key, nil
}

func (s *Service) GetIntegration(ctx context.Context, id string) (_ *murphy.Integration, err error) {
	ctx, span := tracer.Span(ctx)
	ctx = span.Set(ctx, telemetry.WithField("integration.id", id))
	defer span.End(&err)

	if err := murphy.LookupID("integration", id); err != nil {
		return nil, err
	}
	integration, err := s.repo.GetIntegration(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get integration: %w", err)
	}
	return integration, nil
}

func (s *Service) ListIntegrations(ctx context.Context, serviceID string, page pagination.Page) (_ []*murphy.Integration, _ string, err error) {
	ctx, span := tracer.Span(ctx)
	ctx = span.Set(ctx, telemetry.WithField("service.id", serviceID))
	defer span.End(&err)

	window, err := pagination.Resolve(page)
	if err != nil {
		return nil, "", err
	}
	if _, err := s.GetService(ctx, serviceID); err != nil {
		return nil, "", err
	}
	integrations, err := s.repo.ListIntegrations(ctx, serviceID, window.Before, window.Rows())
	if err != nil {
		return nil, "", fmt.Errorf("list integrations: %w", err)
	}
	items, next := pagination.Trim(integrations, window)
	return items, next, nil
}

func (s *Service) UpdateIntegration(ctx context.Context, in *murphy.Integration) (_ *murphy.Integration, err error) {
	ctx, span := tracer.Span(ctx)
	ctx = span.Set(ctx, telemetry.WithField("integration.id", in.GetId()))
	defer span.End(&err)

	current, err := s.GetIntegration(ctx, in.GetId())
	if err != nil {
		return nil, err
	}
	integration := &murphy.Integration{
		Id:                    current.GetId(),
		ServiceId:             current.GetServiceId(),
		Name:                  in.GetName(),
		Type:                  current.GetType(),
		KeyVersion:            current.GetKeyVersion(),
		PreviousKeyValidUntil: current.GetPreviousKeyValidUntil(),
		Status:                murphy.Or(in.GetStatus(), current.GetStatus()),
		CreatedAt:             current.GetCreatedAt(),
		UpdatedAt:             murphy.Now(),
	}
	if err := integration.Validate(); err != nil {
		return nil, err
	}
	if err := s.repo.UpdateIntegration(ctx, integration); err != nil {
		return nil, fmt.Errorf("update integration: %w", err)
	}
	return integration, nil
}

func (s *Service) DeleteIntegration(ctx context.Context, id string) (err error) {
	ctx, span := tracer.Span(ctx)
	ctx = span.Set(ctx, telemetry.WithField("integration.id", id))
	defer span.End(&err)

	if err := murphy.LookupID("integration", id); err != nil {
		return err
	}
	if err := s.repo.DeleteIntegration(ctx, id); err != nil {
		return fmt.Errorf("delete integration: %w", err)
	}
	return nil
}
