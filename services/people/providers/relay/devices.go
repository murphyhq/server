package relay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/internal/relay"
	"github.com/murphyhq/server/internal/tracer"
	"github.com/murphyhq/server/services/people"
)

type Devices struct {
	client *relay.ClientWithResponses
}

var _ people.Devices = (*Devices)(nil)

func New(client *relay.ClientWithResponses) *Devices {
	return &Devices{client: client}
}

func (d *Devices) Register(ctx context.Context, platform murphy.PushPlatform, token string) (_ string, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	response, err := d.client.RegisterDevice(ctx, relay.RegisterDeviceJSONRequestBody{Platform: relay.Platform(platform.Text()), Token: token})
	if err != nil {
		return "", fmt.Errorf("reach the relay: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	switch response.StatusCode {
	case http.StatusCreated, http.StatusOK:
		var registered relay.Device
		if err := json.NewDecoder(response.Body).Decode(&registered); err != nil || registered.Handle == "" {
			return "", errors.New("the relay answered without a handle")
		}
		return registered.Handle, nil
	case http.StatusBadRequest:
		return "", &murphy.FieldError{Field: "address", Reason: "was refused by the push relay as a device token"}
	default:
		return "", fmt.Errorf("the relay answered %s", response.Status)
	}
}

func (d *Devices) Forget(ctx context.Context, handle string) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	response, err := d.client.ForgetDevice(ctx, handle)
	if err != nil {
		return fmt.Errorf("reach the relay: %w", err)
	}
	_ = response.Body.Close()
	switch response.StatusCode {
	case http.StatusNoContent, http.StatusNotFound:
		return nil
	default:
		return fmt.Errorf("the relay answered %s", response.Status)
	}
}
