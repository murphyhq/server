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

		fields := append([]telemetry.Field{
			telemetry.Standard("http.request.method", r.Method),
			telemetry.Standard("http.response.status_code", recorder.status),
			telemetry.WithField("duration_ms", time.Since(start).Milliseconds()),
		}, clientFields(r)...)
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

func clientFields(r *http.Request) []telemetry.Field {
	return []telemetry.Field{
		telemetry.WithField("murphy.client.app.version", clean(r.Header.Get("Murphy-App-Version"))),
		telemetry.WithField("murphy.client.app.build", clean(r.Header.Get("Murphy-App-Build"))),
		telemetry.Standard("os.name", clean(r.Header.Get("Murphy-OS"))),
		telemetry.Standard("os.version", clean(r.Header.Get("Murphy-OS-Version"))),
		telemetry.Standard("device.model.name", clean(r.Header.Get("Murphy-Device-Model"))),
		telemetry.WithField("murphy.client.locale", clean(r.Header.Get("Murphy-Locale"))),
	}
}

func clean(value string) string {
	runes := []rune(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, strings.TrimSpace(value)))
	if len(runes) > 64 {
		runes = runes[:64]
	}
	return string(runes)
}
