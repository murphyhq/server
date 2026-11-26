package webhook

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"time"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/internal/httpclient"
	"github.com/murphyhq/server/internal/tracer"
	"github.com/murphyhq/server/services/notifications"
)

const Timeout = 10 * time.Second

type Webhooks struct {
	client *http.Client
	keys   []ed25519.PrivateKey
}

var _ notifications.Provider = (*Webhooks)(nil)

func New(keys []ed25519.PrivateKey, allowed []netip.Prefix) *Webhooks {
	client := httpclient.Restricted(Timeout, allowed)
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Webhooks{client: client, keys: keys}
}

func (w *Webhooks) Send(ctx context.Context, notification *murphy.Notification) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	body, err := json.Marshal(payload(notification, time.Now().UTC()))
	if err != nil {
		return fmt.Errorf("encode webhook: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, notification.GetContactMethod().GetAddress(), bytes.NewReader(body))
	if err != nil {
		return &notifications.RefusedError{Reason: "the webhook's address is not a valid URL"}
	}
	now := time.Now()
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "Murphy")
	request.Header.Set(HeaderID, notification.GetId())
	request.Header.Set(HeaderTimestamp, fmt.Sprint(now.Unix()))
	request.Header.Set(HeaderSignature, sign(w.keys, notification.GetId(), now, body))

	response, err := w.client.Do(request)
	if err != nil {
		if errors.Is(err, httpclient.ErrForbiddenAddress) {
			return &notifications.RefusedError{Reason: "the webhook's address is on a network Murphy isn't allowed to reach"}
		}
		if errors.Is(err, context.DeadlineExceeded) || isTimeout(err) {
			return fmt.Errorf("the receiver didn't answer within %s", Timeout)
		}
		return errors.New("couldn't reach the receiver")
	}
	_ = response.Body.Close()

	switch status := response.StatusCode; {
	case status >= 200 && status < 300:
		return nil
	case status == http.StatusRequestTimeout, status == http.StatusTooManyRequests, status >= 500:
		return fmt.Errorf("the receiver answered %d", status)
	default:
		return &notifications.RefusedError{Reason: fmt.Sprintf("the receiver answered %d", status)}
	}
}

func isTimeout(err error) bool {
	var timeout interface{ Timeout() bool }
	return errors.As(err, &timeout) && timeout.Timeout()
}

func payload(notification *murphy.Notification, at time.Time) Webhook {
	if notification.GetReason() == murphy.NotificationReason_NOTIFICATION_REASON_TEST {
		return Webhook{Type: TestPage, Timestamp: at, Data: WebhookData{
			User: Reference{Id: notification.GetUserId()}, ContactMethod: Reference{Id: notification.GetContactMethod().GetId()},
		}}
	}
	incident := notification.GetIncident()
	data := WebhookData{
		Incident: &WebhookIncident{
			Id:        incident.GetId(),
			Number:    incident.GetNumber(),
			Service:   Reference{Id: incident.GetServiceId()},
			Title:     incident.GetTitle(),
			Status:    WebhookIncidentStatus(incident.GetStatus().Text()),
			Urgency:   WebhookIncidentUrgency(incident.GetUrgency().Text()),
			CreatedAt: incident.GetCreatedAt().AsTime(),
		},
		User:          Reference{Id: notification.GetUserId()},
		ContactMethod: Reference{Id: notification.GetContactMethod().GetId()},
	}
	kind := IncidentPaged
	switch notification.GetReason() {
	case murphy.NotificationReason_NOTIFICATION_REASON_ACKNOWLEDGED:
		kind = IncidentAcknowledged
	case murphy.NotificationReason_NOTIFICATION_REASON_RESOLVED:
		kind = IncidentResolved
	default:
		data.Escalation = &WebhookEscalation{
			Level: notification.GetEscalationLevel(),
			Round: notification.GetEscalationRound(),
			Via:   EscalationTarget{Type: EscalationTargetType(notification.GetVia().GetType().Text()), Id: notification.GetVia().GetId()},
		}
	}
	return Webhook{Type: kind, Timestamp: at, Data: data}
}
