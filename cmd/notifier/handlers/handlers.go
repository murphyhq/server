package handlers

import (
	"github.com/murphyhq/server"
	"github.com/murphyhq/server/internal/messaging"
	"github.com/murphyhq/server/services/notifications"
)

type Handlers struct {
	notifications *notifications.Service
}

func New(notifications *notifications.Service) messaging.Handlers {
	h := &Handlers{notifications: notifications}
	return messaging.Handlers{
		messaging.On(murphy.NotificationVerb_NOTIFICATION_VERB_CREATED, h.NotificationCreated),
	}
}
