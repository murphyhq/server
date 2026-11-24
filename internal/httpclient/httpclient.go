package httpclient

import (
	"net/http"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/murphyhq/server/internal/telemetry"
)

func New(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout, Transport: Instrument(http.DefaultTransport)}
}

func Instrument(transport http.RoundTripper) http.RoundTripper {
	return labeled{next: otelhttp.NewTransport(transport)}
}

type labeled struct {
	next http.RoundTripper
}

func (l labeled) RoundTrip(r *http.Request) (*http.Response, error) {
	if component := telemetry.ComponentAttributes(r.Context()); len(component) > 0 {
		labeler := &otelhttp.Labeler{}
		labeler.Add(component...)
		r = r.WithContext(otelhttp.ContextWithClientLabeler(r.Context(), labeler))
	}
	return l.next.RoundTrip(r)
}
