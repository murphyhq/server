package handlers

import (
	"context"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/internal/tracer"
)

func (h *Handlers) IncidentEscalated(ctx context.Context, envelope *murphy.Envelope) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	return h.notifications.IncidentEscalated(ctx, envelope.GetId(), envelope.GetIncident().GetCurrent())
}

func (h *Handlers) IncidentAcknowledged(ctx context.Context, envelope *murphy.Envelope) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	return h.notifications.IncidentAcknowledged(ctx, envelope.GetId(), envelope.GetIncident().GetCurrent())
}

func (h *Handlers) IncidentResolved(ctx context.Context, envelope *murphy.Envelope) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	return h.notifications.IncidentResolved(ctx, envelope.GetId(), envelope.GetIncident().GetCurrent())
}
