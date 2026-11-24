package httpclient

import (
	"net/http"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"

	"github.com/murphyhq/server/internal/telemetry"
)

func New(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout, Transport: Instrument(http.DefaultTransport)}
}

func Instrument(transport http.RoundTripper) http.RoundTripper {
	return otelhttp.NewTransport(transport, otelhttp.WithMetricAttributesFn(func(r *http.Request) []attribute.KeyValue {
		return telemetry.ComponentAttributes(r.Context())
	}))
}
