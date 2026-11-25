package router

import (
	"context"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/internal/tracer"
	"github.com/murphyhq/server/services/notifications"
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
	out := EmailProvider{
		Type: EmailProviderType(provider.GetType().Text()), From: provider.GetFrom(), Host: provider.GetHost(), Port: provider.GetPort(),
		Username: provider.GetUsername(), Secret: provider.GetSecret(), UpdatedAt: provider.GetUpdatedAt().AsTime(),
		Status: EmailProviderStatus(provider.GetStatus().Text()), Problem: provider.GetProblem(),
	}
	if at := provider.GetCheckedAt(); at != nil {
		checked := at.AsTime()
		out.CheckedAt = &checked
	}
	return out
}

func (s *Server) SendTestPage(ctx context.Context, request SendTestPageRequestObject) (_ SendTestPageResponseObject, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	var contactMethod string
	if request.Body != nil && request.Body.ContactMethod != nil {
		contactMethod = request.Body.ContactMethod.Id
	}
	page, err := s.notifications.SendTestPage(ctx, request.Id, contactMethod)
	if err != nil {
		return nil, err
	}
	return SendTestPage202JSONResponse(toTestPage(page)), nil
}

func (s *Server) GetTestPage(ctx context.Context, request GetTestPageRequestObject) (_ GetTestPageResponseObject, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	page, err := s.notifications.TestPage(ctx, request.Id, request.TestPageId)
	if err != nil {
		return nil, err
	}
	return GetTestPage200JSONResponse(toTestPage(page)), nil
}

func toTestPage(page *notifications.TestPage) TestPage {
	out := TestPage{Id: page.ID, CreatedAt: page.CreatedAt, Deliveries: make([]TestDelivery, len(page.Deliveries))}
	for i, delivery := range page.Deliveries {
		out.Deliveries[i] = TestDelivery{ContactMethod: ref(delivery.ContactMethodID), Channel: ContactMethodType(delivery.Channel.Text()), Status: TestDeliveryStatus(delivery.Status)}
		if delivery.Failure != "" {
			failure := delivery.Failure
			out.Deliveries[i].Failure = &failure
		}
	}
	return out
}
