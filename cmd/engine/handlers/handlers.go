package handlers

import (
	"github.com/murphyhq/server"
	"github.com/murphyhq/server/internal/messaging"
	"github.com/murphyhq/server/services/incidents"
)

type Handlers struct {
	incidents *incidents.Service
}

func New(incidents *incidents.Service) messaging.Handlers {
	h := &Handlers{incidents: incidents}
	return messaging.Handlers{
		messaging.On(murphy.AlertVerb_ALERT_VERB_RECEIVED, h.AlertReceived),
	}
}
