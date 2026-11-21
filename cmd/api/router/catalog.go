package router

import (
	"context"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/internal/pagination"
)

func (s *Server) ListServices(ctx context.Context, request ListServicesRequestObject) (_ ListServicesResponseObject, err error) {
	services, next, err := s.catalog.ListServices(ctx, page(request.Params.Limit, request.Params.Cursor))
	if err != nil {
		return nil, err
	}
	items := make([]Service, len(services))
	for i, service := range services {
		items[i] = toService(service)
	}
	return ListServices200JSONResponse{Items: items, NextCursor: next}, nil
}

func (s *Server) CreateService(ctx context.Context, request CreateServiceRequestObject) (_ CreateServiceResponseObject, err error) {
	service, err := s.catalog.CreateService(ctx, fromServiceInput("", request.Body))
	if err != nil {
		return nil, err
	}
	return CreateService201JSONResponse(toService(service)), nil
}

func (s *Server) GetService(ctx context.Context, request GetServiceRequestObject) (_ GetServiceResponseObject, err error) {
	service, err := s.catalog.GetService(ctx, request.Id)
	if err != nil {
		return nil, err
	}
	return GetService200JSONResponse(toService(service)), nil
}

func (s *Server) UpdateService(ctx context.Context, request UpdateServiceRequestObject) (_ UpdateServiceResponseObject, err error) {
	service, err := s.catalog.UpdateService(ctx, fromServiceInput(request.Id, request.Body))
	if err != nil {
		return nil, err
	}
	return UpdateService200JSONResponse(toService(service)), nil
}

func (s *Server) DeleteService(ctx context.Context, request DeleteServiceRequestObject) (_ DeleteServiceResponseObject, err error) {
	if err := s.catalog.DeleteService(ctx, request.Id); err != nil {
		return nil, err
	}
	return DeleteService204Response{}, nil
}

func (s *Server) ListIntegrations(ctx context.Context, request ListIntegrationsRequestObject) (_ ListIntegrationsResponseObject, err error) {
	integrations, next, err := s.catalog.ListIntegrations(ctx, request.Id, page(request.Params.Limit, request.Params.Cursor))
	if err != nil {
		return nil, err
	}
	items := make([]Integration, len(integrations))
	for i, integration := range integrations {
		items[i] = toIntegration(integration)
	}
	return ListIntegrations200JSONResponse{Items: items, NextCursor: next}, nil
}

func (s *Server) CreateIntegration(ctx context.Context, request CreateIntegrationRequestObject) (_ CreateIntegrationResponseObject, err error) {
	integration, err := s.catalog.CreateIntegration(ctx, &murphy.Integration{
		ServiceId: request.Id,
		Name:      request.Body.Name,
		Type:      murphy.IntegrationTypeFromText(string(request.Body.Type)),
		Status:    murphy.IntegrationStatusFromText(text(request.Body.Status)),
	})
	if err != nil {
		return nil, err
	}
	return CreateIntegration201JSONResponse(toIntegration(integration)), nil
}

func (s *Server) GetIntegration(ctx context.Context, request GetIntegrationRequestObject) (_ GetIntegrationResponseObject, err error) {
	integration, err := s.catalog.GetIntegration(ctx, request.Id)
	if err != nil {
		return nil, err
	}
	return GetIntegration200JSONResponse(toIntegration(integration)), nil
}

func (s *Server) UpdateIntegration(ctx context.Context, request UpdateIntegrationRequestObject) (_ UpdateIntegrationResponseObject, err error) {
	integration, err := s.catalog.UpdateIntegration(ctx, &murphy.Integration{
		Id:     request.Id,
		Name:   request.Body.Name,
		Status: murphy.IntegrationStatusFromText(text(request.Body.Status)),
	})
	if err != nil {
		return nil, err
	}
	return UpdateIntegration200JSONResponse(toIntegration(integration)), nil
}

func (s *Server) DeleteIntegration(ctx context.Context, request DeleteIntegrationRequestObject) (_ DeleteIntegrationResponseObject, err error) {
	if err := s.catalog.DeleteIntegration(ctx, request.Id); err != nil {
		return nil, err
	}
	return DeleteIntegration204Response{}, nil
}

func fromServiceInput(id string, in *ServiceInput) *murphy.Service {
	return &murphy.Service{
		Id:                 id,
		Name:               in.Name,
		Description:        text(in.Description),
		TeamId:             in.TeamId,
		EscalationPolicyId: in.EscalationPolicyId,
		Urgency:            murphy.UrgencyFromText(text(in.Urgency)),
		Status:             murphy.ServiceStatusFromText(text(in.Status)),
	}
}

func toService(service *murphy.Service) Service {
	return Service{
		Id:                 service.GetId(),
		Name:               service.GetName(),
		Description:        service.GetDescription(),
		TeamId:             service.GetTeamId(),
		EscalationPolicyId: service.GetEscalationPolicyId(),
		Urgency:            Urgency(service.GetUrgency().Text()),
		Status:             ServiceStatus(service.GetStatus().Text()),
		CreatedAt:          service.GetCreatedAt().AsTime(),
		UpdatedAt:          service.GetUpdatedAt().AsTime(),
	}
}

func toIntegration(integration *murphy.Integration) Integration {
	out := Integration{
		Id:         integration.GetId(),
		ServiceId:  integration.GetServiceId(),
		Name:       integration.GetName(),
		Type:       IntegrationType(integration.GetType().Text()),
		KeyVersion: integration.GetKeyVersion(),
		Status:     IntegrationStatus(integration.GetStatus().Text()),
		CreatedAt:  integration.GetCreatedAt().AsTime(),
		UpdatedAt:  integration.GetUpdatedAt().AsTime(),
	}
	if until := integration.GetPreviousKeyValidUntil(); until != nil {
		t := until.AsTime()
		out.PreviousKeyValidUntil = &t
	}
	return out
}

func page(limit *Limit, cursor *Cursor) pagination.Page {
	var p pagination.Page
	if limit != nil {
		p.Limit = *limit
	}
	if cursor != nil {
		p.Cursor = *cursor
	}
	return p
}

func text[T ~string](value *T) string {
	if value == nil {
		return ""
	}
	return string(*value)
}
