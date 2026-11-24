package handlers

import (
	"context"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/internal/tracer"
)

func (h *Handlers) NotificationCreated(ctx context.Context, envelope *murphy.Envelope) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	return h.notifications.Send(ctx, envelope.GetId(), envelope.GetNotification().GetCurrent())
}
