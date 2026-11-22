package handlers

import (
	"github.com/murphyhq/server/internal/messaging"
	"github.com/murphyhq/server/services/incidents"
)

type Handlers struct {
	incidents *incidents.Service
}

func New(incidents *incidents.Service) messaging.Handlers {
	h := &Handlers{incidents: incidents}
	return messaging.Handlers{
		"murphy.v1.alert.received": h.AlertReceived,
	}
}
