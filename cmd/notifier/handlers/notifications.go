package handlers

import (
	"context"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/internal/tracer"
)

func (h *Handlers) NotificationCreated(ctx context.Context, envelope *murphy.Envelope) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	return h.sender.Send(ctx, envelope.GetId(), envelope.GetNotification().GetCurrent())
}

func (h *Handlers) NotificationSent(ctx context.Context, envelope *murphy.Envelope) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	return h.notifications.NotificationSent(ctx, envelope.GetId(), envelope.GetNotification().GetCurrent())
}

func (h *Handlers) NotificationFailed(ctx context.Context, envelope *murphy.Envelope) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	return h.notifications.NotificationFailed(ctx, envelope.GetId(), envelope.GetNotification().GetCurrent())
}
