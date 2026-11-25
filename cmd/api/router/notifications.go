package router

import (
	"context"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/internal/tracer"
)

func (s *Server) GetEmailProvider(ctx context.Context, _ GetEmailProviderRequestObject) (_ GetEmailProviderResponseObject, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	provider, err := s.notifications.EmailProvider(ctx)
	if err != nil {
		return nil, err
	}
	return GetEmailProvider200JSONResponse(toEmailProvider(provider)), nil
}

func (s *Server) SetEmailProvider(ctx context.Context, request SetEmailProviderRequestObject) (_ SetEmailProviderResponseObject, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	in := request.Body
	provider, err := s.notifications.SetEmailProvider(ctx, &murphy.EmailProvider{
		Type: murphy.EmailProviderTypeFromText(string(in.Type)), From: in.From, Host: in.Host, Port: in.Port,
		Username: text(in.Username), Secret: text(in.Secret),
	})
	if err != nil {
		return nil, err
	}
	return SetEmailProvider200JSONResponse(toEmailProvider(provider)), nil
}

func (s *Server) RemoveEmailProvider(ctx context.Context, _ RemoveEmailProviderRequestObject) (_ RemoveEmailProviderResponseObject, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	if err := s.notifications.RemoveEmailProvider(ctx); err != nil {
		return nil, err
	}
	return RemoveEmailProvider204Response{}, nil
}

func toEmailProvider(provider *murphy.EmailProvider) EmailProvider {
	return EmailProvider{
		Type: EmailProviderType(provider.GetType().Text()), From: provider.GetFrom(), Host: provider.GetHost(), Port: provider.GetPort(),
		Username: provider.GetUsername(), Secret: provider.GetSecret(), UpdatedAt: provider.GetUpdatedAt().AsTime(),
	}
}
