package handlers

import (
	"github.com/murphyhq/server"
	"github.com/murphyhq/server/internal/messaging"
	"github.com/murphyhq/server/services/notifications"
)

type Handlers struct {
	notifications *notifications.Service
	sender        *notifications.Sender
}

func New(service *notifications.Service, sender *notifications.Sender) messaging.Handlers {
	h := &Handlers{notifications: service, sender: sender}
	return messaging.Handlers{
		messaging.On(murphy.IncidentVerb_INCIDENT_VERB_ESCALATED, h.IncidentEscalated),
		messaging.On(murphy.IncidentVerb_INCIDENT_VERB_ACKNOWLEDGED, h.IncidentAcknowledged),
		messaging.On(murphy.IncidentVerb_INCIDENT_VERB_RESOLVED, h.IncidentResolved),
		messaging.On(murphy.NotificationVerb_NOTIFICATION_VERB_CREATED, h.NotificationCreated),
		messaging.On(murphy.NotificationVerb_NOTIFICATION_VERB_SENT, h.NotificationSent),
		messaging.On(murphy.NotificationVerb_NOTIFICATION_VERB_FAILED, h.NotificationFailed),
	}
}
