package handlers

import (
	"context"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/internal/messaging"
	"github.com/murphyhq/server/internal/tracer"
)

type Handlers struct{}

func New() messaging.Handlers {
	h := &Handlers{}
	return messaging.Handlers{
		messaging.On(murphy.IncidentVerb_INCIDENT_VERB_CREATED, h.IncidentChanged),
		messaging.On(murphy.IncidentVerb_INCIDENT_VERB_UPDATED, h.IncidentChanged),
		messaging.On(murphy.IncidentVerb_INCIDENT_VERB_ESCALATED, h.IncidentChanged),
		messaging.On(murphy.IncidentVerb_INCIDENT_VERB_ACKNOWLEDGED, h.IncidentChanged),
		messaging.On(murphy.IncidentVerb_INCIDENT_VERB_RESOLVED, h.IncidentChanged),
	}
}

func (h *Handlers) IncidentChanged(ctx context.Context, _ *murphy.Envelope) (err error) {
	_, span := tracer.Span(ctx)
	defer span.End(&err)

	return nil
}
