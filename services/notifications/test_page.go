package notifications

import (
	"time"

	"github.com/murphyhq/server"
)

type TestPage struct {
	ID         string
	UserID     string
	CreatedAt  time.Time
	Deliveries []*TestDelivery
}

type DeliveryStatus string

const (
	DeliveryPending DeliveryStatus = "pending"
	DeliverySent    DeliveryStatus = "sent"
	DeliveryFailed  DeliveryStatus = "failed"
)

type TestDelivery struct {
	ContactMethodID string
	Channel         murphy.ContactMethodType
	Status          DeliveryStatus
	Failure         string
}

func SubjectOf(notification *murphy.Notification) (subject, id string) {
	if id := notification.GetTestId(); id != "" {
		return "test", id
	}
	return "incident", notification.GetIncident().GetId()
}
