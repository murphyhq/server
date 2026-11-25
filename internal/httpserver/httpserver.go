package httpserver

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
	"unicode"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/murphyhq/server/internal/logger"
	"github.com/murphyhq/server/internal/metrics"
	"github.com/murphyhq/server/internal/telemetry"
	"github.com/murphyhq/server/internal/tracer"
)

type Config struct {
	Addr            string
	ShutdownTimeout time.Duration
	CORS            CORS
}

type Server struct {
	cfg     Config
	handler http.Handler
}

func New(cfg Config, handler http.Handler) *Server {
	return &Server{cfg: cfg, handler: cors(cfg.CORS)(Handler(handler))}
}

func (s *Server) Handler() http.Handler {
	return s.handler
}

type router interface {
	Handler(r *http.Request) (http.Handler, string)
}

func Handler(handler http.Handler) http.Handler {
	return otelhttp.NewHandler(observe(handler), "http.server",
		otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
			if mux, ok := handler.(router); ok {
				if _, pattern := mux.Handler(r); pattern != "" {
					if _, route, found := strings.Cut(pattern, " "); found {
						return r.Method + " " + route
					}
				}
			}
			return r.Method
		}),
	)
}

func (s *Server) Serve(ctx context.Context) error {
	listener, err := net.Listen("tcp", s.cfg.Addr)
	if err != nil {
		return fmt.Errorf("listen for HTTP on %s: %w", s.cfg.Addr, err)
	}
	base := context.WithoutCancel(ctx)
	server := &http.Server{Handler: s.handler, ReadHeaderTimeout: 10 * time.Second, BaseContext: func(net.Listener) context.Context { return base }}
	errs := make(chan error, 1)
	go func() { errs <- server.Serve(listener) }()
	logger.Info(ctx, "serving HTTP", telemetry.WithField("http_addr", listener.Addr().String()))

	select {
	case err := <-errs:
		return fmt.Errorf("serve HTTP: %w", err)
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.cfg.ShutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("stop HTTP server: %w", err)
	}
	return nil
}

func observe(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ctx := r.Context()
		if id := tracer.TraceID(ctx); id != "" {
			w.Header().Set("X-Trace-Id", id)
			w.Header().Set("traceresponse", tracer.TraceResponse(ctx))
		}

		ctx, scope := metrics.StartScope(telemetry.Collecting(ctx))
		r = r.WithContext(ctx)
		recorder := &recorder{ResponseWriter: w, request: r, status: http.StatusOK}
		next.ServeHTTP(recorder, r)

		client := clientValues(r)
		trace.SpanFromContext(ctx).SetAttributes(clientAttributes(client)...)
		fields := append([]telemetry.Field{
			telemetry.Standard("http.request.method", r.Method),
			telemetry.Standard("http.response.status_code", recorder.status),
			telemetry.WithField("duration_ms", time.Since(start).Milliseconds()),
		}, clientFields(client)...)
		if _, route, ok := strings.Cut(r.Pattern, " "); ok {
			trace.SpanFromContext(ctx).SetAttributes(attribute.String("http.route", route))
			if labeler, ok := otelhttp.LabelerFromContext(ctx); ok {
				labeler.Add(attribute.String("http.route", route))
				if component := telemetry.Component(ctx); component != "" {
					labeler.Add(attribute.String("murphy.component", component))
				}
			}
			fields = append(fields, telemetry.Standard("http.route", route))
			scope.Record(ctx, "http.request", metrics.Dimension{Name: "http.request.method", Value: r.Method}, metrics.Dimension{Name: "http.route", Value: route})
		}
		logger.Info(ctx, "request handled", append(fields, telemetry.Collected(ctx)...)...)
	})
}

type recorder struct {
	http.ResponseWriter
	request  *http.Request
	status   int
	replaced bool
}

func (r *recorder) WriteHeader(status int) {
	r.status = status
	if r.request.Pattern == "" && (status == http.StatusNotFound || status == http.StatusMethodNotAllowed) {
		r.replaced = true
		WriteProblem(r.ResponseWriter, r.request, status, "")
		return
	}
	r.ResponseWriter.WriteHeader(status)
}

func (r *recorder) Write(p []byte) (int, error) {
	if r.replaced {
		return len(p), nil
	}
	return r.ResponseWriter.Write(p)
}

func (r *recorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

const maxClientValue = 64

var clientHeaders = []struct {
	header   string
	name     string
	standard bool
}{
	{"Murphy-App-Version", "murphy.client.app.version", false},
	{"Murphy-App-Build", "murphy.client.app.build", false},
	{"Murphy-OS", "os.name", true},
	{"Murphy-OS-Version", "os.version", true},
	{"Murphy-Device-Model", "device.model.name", true},
	{"Murphy-Locale", "murphy.client.locale", false},
}

type clientValue struct {
	name     string
	value    string
	standard bool
}

func clientValues(r *http.Request) []clientValue {
	values := make([]clientValue, 0, len(clientHeaders))
	for _, c := range clientHeaders {
		if value := cleanClientValue(r.Header.Get(c.header)); value != "" {
			values = append(values, clientValue{name: c.name, value: value, standard: c.standard})
		}
	}
	return values
}

func clientFields(values []clientValue) []telemetry.Field {
	fields := make([]telemetry.Field, 0, len(values))
	for _, v := range values {
		if v.standard {
			fields = append(fields, telemetry.Standard(v.name, v.value))
			continue
		}
		fields = append(fields, telemetry.WithField(v.name, v.value))
	}
	return fields
}

func clientAttributes(values []clientValue) []attribute.KeyValue {
	attributes := make([]attribute.KeyValue, 0, len(values))
	for _, v := range values {
		attributes = append(attributes, attribute.String(v.name, v.value))
	}
	return attributes
}

func cleanClientValue(value string) string {
	cleaned := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, value)
	runes := []rune(strings.TrimSpace(cleaned))
	if len(runes) > maxClientValue {
		runes = runes[:maxClientValue]
	}
	return string(runes)
}
