package handlers

import (
	"context"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/internal/tracer"
)

func (h *Handlers) AlertReceived(ctx context.Context, envelope *murphy.Envelope) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	return h.incidents.AlertReceived(ctx, envelope.GetId(), envelope.GetAlert().GetCurrent())
}
