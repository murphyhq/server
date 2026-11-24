package router

import (
	"context"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/internal/pagination"
	"github.com/murphyhq/server/internal/tracer"
)

func (s *Server) ListServices(ctx context.Context, request ListServicesRequestObject) (_ ListServicesResponseObject, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	n := newNames()
	services, next, err := s.catalog.ListServices(ctx, page(request.Params.Limit, request.Params.Cursor))
	if err != nil {
		return nil, err
	}
	items := make([]Service, len(services))
	for i, service := range services {
		items[i] = toService(service, n)
	}
	return ListServices200JSONResponse{Items: items, NextCursor: next}, s.name(ctx, n)
}

func (s *Server) CreateService(ctx context.Context, request CreateServiceRequestObject) (_ CreateServiceResponseObject, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	n := newNames()
	service, err := s.catalog.CreateService(ctx, fromServiceInput("", request.Body))
	if err != nil {
		return nil, err
	}
	return CreateService201JSONResponse(toService(service, n)), s.name(ctx, n)
}

func (s *Server) GetService(ctx context.Context, request GetServiceRequestObject) (_ GetServiceResponseObject, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	n := newNames()
	service, err := s.catalog.GetService(ctx, request.Id)
	if err != nil {
		return nil, err
	}
	return GetService200JSONResponse(toService(service, n)), s.name(ctx, n)
}

func (s *Server) UpdateService(ctx context.Context, request UpdateServiceRequestObject) (_ UpdateServiceResponseObject, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	n := newNames()
	service, err := s.catalog.UpdateService(ctx, fromServiceInput(request.Id, request.Body))
	if err != nil {
		return nil, err
	}
	return UpdateService200JSONResponse(toService(service, n)), s.name(ctx, n)
}

func (s *Server) DeleteService(ctx context.Context, request DeleteServiceRequestObject) (_ DeleteServiceResponseObject, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	if err := s.catalog.DeleteService(ctx, request.Id); err != nil {
		return nil, err
	}
	return DeleteService204Response{}, nil
}

func (s *Server) ListIntegrations(ctx context.Context, request ListIntegrationsRequestObject) (_ ListIntegrationsResponseObject, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	n := newNames()
	integrations, next, err := s.catalog.ListIntegrations(ctx, request.Id, page(request.Params.Limit, request.Params.Cursor))
	if err != nil {
		return nil, err
	}
	items := make([]Integration, len(integrations))
	for i, integration := range integrations {
		items[i] = toIntegration(integration, n)
	}
	return ListIntegrations200JSONResponse{Items: items, NextCursor: next}, s.name(ctx, n)
}

func (s *Server) CreateIntegration(ctx context.Context, request CreateIntegrationRequestObject) (_ CreateIntegrationResponseObject, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	n := newNames()
	integration, key, err := s.catalog.CreateIntegration(ctx, &murphy.Integration{
		ServiceId: request.Id,
		Name:      request.Body.Name,
		Type:      murphy.IntegrationTypeFromText(string(request.Body.Type)),
		Status:    murphy.IntegrationStatusFromText(text(request.Body.Status)),
	})
	if err != nil {
		return nil, err
	}
	return CreateIntegration201JSONResponse(withKey(integration, key, n)), s.name(ctx, n)
}

func (s *Server) RotateIntegrationKey(ctx context.Context, request RotateIntegrationKeyRequestObject) (_ RotateIntegrationKeyResponseObject, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	n := newNames()
	integration, key, err := s.catalog.RotateIntegrationKey(ctx, request.Id)
	if err != nil {
		return nil, err
	}
	return RotateIntegrationKey200JSONResponse(withKey(integration, key, n)), s.name(ctx, n)
}

func (s *Server) GetIntegration(ctx context.Context, request GetIntegrationRequestObject) (_ GetIntegrationResponseObject, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	n := newNames()
	integration, err := s.catalog.GetIntegration(ctx, request.Id)
	if err != nil {
		return nil, err
	}
	return GetIntegration200JSONResponse(toIntegration(integration, n)), s.name(ctx, n)
}

func (s *Server) UpdateIntegration(ctx context.Context, request UpdateIntegrationRequestObject) (_ UpdateIntegrationResponseObject, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	n := newNames()
	integration, err := s.catalog.UpdateIntegration(ctx, &murphy.Integration{
		Id:     request.Id,
		Name:   request.Body.Name,
		Status: murphy.IntegrationStatusFromText(text(request.Body.Status)),
	})
	if err != nil {
		return nil, err
	}
	return UpdateIntegration200JSONResponse(toIntegration(integration, n)), s.name(ctx, n)
}

func (s *Server) DeleteIntegration(ctx context.Context, request DeleteIntegrationRequestObject) (_ DeleteIntegrationResponseObject, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

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
		EscalationPolicyId: in.EscalationPolicy.Id,
		Urgency:            murphy.UrgencyFromText(text(in.Urgency)),
		Status:             murphy.ServiceStatusFromText(text(in.Status)),
	}
}

func toService(service *murphy.Service, n *names) Service {
	return Service{
		Id:               service.GetId(),
		Name:             service.GetName(),
		Description:      service.GetDescription(),
		EscalationPolicy: n.policy(service.GetEscalationPolicyId()),
		Urgency:          Urgency(service.GetUrgency().Text()),
		Status:           ServiceStatus(service.GetStatus().Text()),
		CreatedAt:        service.GetCreatedAt().AsTime(),
		UpdatedAt:        service.GetUpdatedAt().AsTime(),
	}
}

func toIntegration(integration *murphy.Integration, n *names) Integration {
	out := Integration{
		Id:         integration.GetId(),
		Service:    n.service(integration.GetServiceId()),
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

func withKey(integration *murphy.Integration, key string, n *names) IntegrationWithKey {
	out := toIntegration(integration, n)
	return IntegrationWithKey{
		Id:                    out.Id,
		Service:               out.Service,
		Name:                  out.Name,
		Type:                  out.Type,
		KeyVersion:            out.KeyVersion,
		PreviousKeyValidUntil: out.PreviousKeyValidUntil,
		Status:                out.Status,
		CreatedAt:             out.CreatedAt,
		UpdatedAt:             out.UpdatedAt,
		Key:                   key,
	}
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
