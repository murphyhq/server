package catalog

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/murphyhq/server"
)

const (
	defaultLimit = 50
	maxLimit     = 100
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
	DeleteIntegration(ctx context.Context, id string) error
}

type Page struct {
	Cursor string
	Limit  int
}

type Service struct {
	repo Repository
}

func New(repo Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) CreateService(ctx context.Context, in *murphy.Service) (*murphy.Service, error) {
	now := timestamppb.New(now())
	service := &murphy.Service{
		Id:                 uuid.NewV7().String(),
		Name:               in.GetName(),
		Description:        in.GetDescription(),
		TeamId:             in.GetTeamId(),
		EscalationPolicyId: in.GetEscalationPolicyId(),
		Urgency:            or(in.GetUrgency(), murphy.Urgency_URGENCY_HIGH),
		Status:             or(in.GetStatus(), murphy.ServiceStatus_SERVICE_STATUS_ACTIVE),
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	if err := service.Validate(); err != nil {
		return nil, err
	}
	if err := s.repo.CreateService(ctx, service); err != nil {
		return nil, fmt.Errorf("create service: %w", err)
	}
	return service, nil
}

func (s *Service) GetService(ctx context.Context, id string) (*murphy.Service, error) {
	if err := exists(id, "service"); err != nil {
		return nil, err
	}
	service, err := s.repo.GetService(ctx, id)
	if err != nil {
		return nil, failed("get service", "service", err)
	}
	return service, nil
}

func (s *Service) ListServices(ctx context.Context, page Page) ([]*murphy.Service, string, error) {
	before, limit, err := page.resolve()
	if err != nil {
		return nil, "", err
	}
	services, err := s.repo.ListServices(ctx, before, limit+1)
	if err != nil {
		return nil, "", fmt.Errorf("list services: %w", err)
	}
	items, next := paginate(services, limit)
	return items, next, nil
}

func (s *Service) UpdateService(ctx context.Context, in *murphy.Service) (*murphy.Service, error) {
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
		Urgency:            or(in.GetUrgency(), current.GetUrgency()),
		Status:             or(in.GetStatus(), current.GetStatus()),
		CreatedAt:          current.GetCreatedAt(),
		UpdatedAt:          timestamppb.New(now()),
	}
	if err := service.Validate(); err != nil {
		return nil, err
	}
	if err := s.repo.UpdateService(ctx, service); err != nil {
		return nil, failed("update service", "service", err)
	}
	return service, nil
}

func (s *Service) DeleteService(ctx context.Context, id string) error {
	if err := exists(id, "service"); err != nil {
		return err
	}
	if err := s.repo.DeleteService(ctx, id); err != nil {
		return failed("delete service", "service", err)
	}
	return nil
}

func (s *Service) CreateIntegration(ctx context.Context, in *murphy.Integration) (*murphy.Integration, error) {
	now := timestamppb.New(now())
	integration := &murphy.Integration{
		Id:         uuid.NewV7().String(),
		ServiceId:  in.GetServiceId(),
		Name:       in.GetName(),
		Type:       in.GetType(),
		KeyVersion: 1,
		Status:     or(in.GetStatus(), murphy.IntegrationStatus_INTEGRATION_STATUS_ACTIVE),
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := integration.Validate(); err != nil {
		return nil, err
	}
	if _, err := s.GetService(ctx, integration.GetServiceId()); err != nil {
		return nil, err
	}
	if err := s.repo.CreateIntegration(ctx, integration); err != nil {
		return nil, failed("create integration", "service", err)
	}
	return integration, nil
}

func (s *Service) GetIntegration(ctx context.Context, id string) (*murphy.Integration, error) {
	if err := exists(id, "integration"); err != nil {
		return nil, err
	}
	integration, err := s.repo.GetIntegration(ctx, id)
	if err != nil {
		return nil, failed("get integration", "integration", err)
	}
	return integration, nil
}

func (s *Service) ListIntegrations(ctx context.Context, serviceID string, page Page) ([]*murphy.Integration, string, error) {
	before, limit, err := page.resolve()
	if err != nil {
		return nil, "", err
	}
	if _, err := s.GetService(ctx, serviceID); err != nil {
		return nil, "", err
	}
	integrations, err := s.repo.ListIntegrations(ctx, serviceID, before, limit+1)
	if err != nil {
		return nil, "", fmt.Errorf("list integrations: %w", err)
	}
	items, next := paginate(integrations, limit)
	return items, next, nil
}

func (s *Service) UpdateIntegration(ctx context.Context, in *murphy.Integration) (*murphy.Integration, error) {
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
		Status:                or(in.GetStatus(), current.GetStatus()),
		CreatedAt:             current.GetCreatedAt(),
		UpdatedAt:             timestamppb.New(now()),
	}
	if err := integration.Validate(); err != nil {
		return nil, err
	}
	if err := s.repo.UpdateIntegration(ctx, integration); err != nil {
		return nil, failed("update integration", "integration", err)
	}
	return integration, nil
}

func (s *Service) DeleteIntegration(ctx context.Context, id string) error {
	if err := exists(id, "integration"); err != nil {
		return err
	}
	if err := s.repo.DeleteIntegration(ctx, id); err != nil {
		return failed("delete integration", "integration", err)
	}
	return nil
}

func (p Page) resolve() (before string, limit int, err error) {
	switch {
	case p.Limit < 0:
		return "", 0, &murphy.FieldError{Field: "limit", Reason: "must not be negative"}
	case p.Limit == 0:
		limit = defaultLimit
	default:
		limit = min(p.Limit, maxLimit)
	}
	if p.Cursor == "" {
		return uuid.Max().String(), limit, nil
	}
	if _, err := uuid.Parse(p.Cursor); err != nil {
		return "", 0, &murphy.FieldError{Field: "cursor", Reason: "is not valid"}
	}
	return p.Cursor, limit, nil
}

func paginate[T interface{ GetId() string }](items []T, limit int) ([]T, string) {
	if len(items) <= limit {
		return items, ""
	}
	items = items[:limit]
	return items, items[limit-1].GetId()
}

func exists(id, entity string) error {
	if _, err := uuid.Parse(id); err != nil {
		return &murphy.NotFoundError{Entity: entity}
	}
	return nil
}

func failed(action, entity string, err error) error {
	if errors.Is(err, murphy.ErrNotFound) {
		return &murphy.NotFoundError{Entity: entity}
	}
	return fmt.Errorf("%s: %w", action, err)
}

func or[T comparable](value, fallback T) T {
	var zero T
	if value == zero {
		return fallback
	}
	return value
}

func now() time.Time {
	return time.Now().UTC().Truncate(time.Microsecond)
}
