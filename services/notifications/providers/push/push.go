package push

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/internal/relay"
	"github.com/murphyhq/server/internal/tracer"
	"github.com/murphyhq/server/services/notifications"
)

type Pushes struct {
	client *relay.ClientWithResponses
	server string
}

var _ notifications.Provider = (*Pushes)(nil)

func New(client *relay.ClientWithResponses, server string) *Pushes {
	return &Pushes{client: client, server: server}
}

func (p *Pushes) Send(ctx context.Context, notification *murphy.Notification) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	response, err := p.client.SendPush(ctx, relay.SendPushJSONRequestBody{
		Id:         notification.GetId(),
		Handle:     notification.GetContactMethod().GetAddress(),
		Event:      eventOf(notification.GetReason()),
		Server:     p.server,
		IncidentId: notification.GetIncident().GetId(),
	})
	if err != nil {
		return fmt.Errorf("couldn't reach the push relay: %w", err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
	switch response.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusGone, http.StatusNotFound:
		return &notifications.RefusedError{Reason: "the phone is no longer registered for pushes"}
	case http.StatusUnauthorized:
		return &notifications.RefusedError{Reason: "the push relay doesn't accept this install's key"}
	case http.StatusBadRequest:
		return &notifications.RefusedError{Reason: "the push relay refused the push"}
	default:
		return fmt.Errorf("the push relay answered %s", response.Status)
	}
}

func eventOf(reason murphy.NotificationReason) relay.PushInputEvent {
	switch reason {
	case murphy.NotificationReason_NOTIFICATION_REASON_ACKNOWLEDGED:
		return relay.Acknowledged
	case murphy.NotificationReason_NOTIFICATION_REASON_RESOLVED:
		return relay.Resolved
	default:
		return relay.Paged
	}
}
