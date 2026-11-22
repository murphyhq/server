package broker

import (
	"context"
	"errors"
	"strings"

	"github.com/murphyhq/server/internal/messaging"
	"github.com/murphyhq/server/internal/messaging/broker/nats"
	"github.com/murphyhq/server/internal/messaging/broker/sqs"
)

func Open(ctx context.Context, url string) (messaging.Broker, error) {
	scheme, _, _ := strings.Cut(url, "://")
	switch scheme {
	case "nats", "tls":
		return nats.Open(ctx, url)
	case "sqs":
		return sqs.Open(ctx, url)
	default:
		return nil, errors.New("unsupported broker")
	}
}
