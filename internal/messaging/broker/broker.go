package broker

import (
	"context"
	"errors"
	"strings"

	"github.com/murphyhq/server/internal/messaging"
	"github.com/murphyhq/server/internal/messaging/broker/nats"
	"github.com/murphyhq/server/internal/messaging/broker/snssqs"
)

func OpenTopic(ctx context.Context, url string) (messaging.Topic, error) {
	switch scheme(url) {
	case "nats", "tls":
		return nats.OpenTopic(ctx, url)
	case "awssns":
		return snssqs.OpenTopic(ctx, url)
	default:
		return nil, errors.New("unsupported topic URL")
	}
}

func OpenSubscription(ctx context.Context, url string) (messaging.Subscription, error) {
	switch scheme(url) {
	case "nats", "tls":
		return nats.OpenSubscription(ctx, url)
	case "awssqs":
		return snssqs.OpenSubscription(ctx, url)
	default:
		return nil, errors.New("unsupported subscription URL")
	}
}

func scheme(url string) string {
	scheme, _, _ := strings.Cut(url, "://")
	return scheme
}
