package router

import (
	"context"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/internal/telemetry"
	"github.com/murphyhq/server/internal/tracer"
)

func (s *Server) ReceiveAlert(ctx context.Context, request ReceiveAlertRequestObject) (_ ReceiveAlertResponseObject, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	key := keyFrom(ctx)
	ctx = span.Set(ctx,
		telemetry.WithField("integration.id", key.GetIntegrationId()),
		telemetry.WithField("service.id", key.GetServiceId()),
	)

	now := murphy.Now()
	alert := &murphy.Alert{
		Id:            murphy.NewID(),
		IntegrationId: key.GetIntegrationId(),
		ServiceId:     key.GetServiceId(),
		DedupKey:      request.Body.DedupKey,
		Status:        murphy.AlertStatusFromText(string(request.Body.Status)),
		Summary:       request.Body.Summary,
		ReceivedAt:    now,
		KeyVersion:    key.GetKeyVersion(),
	}
	if request.Body.Labels != nil {
		alert.Labels = *request.Body.Labels
	}
	if err := alert.Validate(); err != nil {
		return nil, err
	}
	ctx = span.Set(ctx, telemetry.WithField("alert.id", alert.GetId()))

	envelope := &murphy.Envelope{
		Id:         murphy.NewID(),
		OccurredAt: now,
		Event: &murphy.Envelope_Alert{Alert: &murphy.AlertEvent{
			Verb:    murphy.AlertVerb_ALERT_VERB_RECEIVED,
			Current: alert,
		}},
	}
	if err := s.publisher.Publish(ctx, envelope); err != nil {
		return nil, err
	}
	return ReceiveAlert202JSONResponse{Id: envelope.GetId()}, nil
}
