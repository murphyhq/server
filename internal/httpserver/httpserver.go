package httpserver

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/murphyhq/server/internal/logger"
	"github.com/murphyhq/server/internal/telemetry"
	"github.com/murphyhq/server/internal/tracer"
)

type Config struct {
	Addr            string
	ShutdownTimeout time.Duration
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

func Serve(ctx context.Context, cfg Config, handler http.Handler) error {
	listener, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return fmt.Errorf("listen for HTTP on %s: %w", cfg.Addr, err)
	}
	server := &http.Server{Handler: Handler(handler), ReadHeaderTimeout: 10 * time.Second}
	errs := make(chan error, 1)
	go func() { errs <- server.Serve(listener) }()
	logger.Info(ctx, "serving HTTP", telemetry.WithField("http_addr", listener.Addr().String()))

	select {
	case err := <-errs:
		return fmt.Errorf("serve HTTP: %w", err)
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cfg.ShutdownTimeout)
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

		recorder := &recorder{ResponseWriter: w, request: r, status: http.StatusOK}
		next.ServeHTTP(recorder, r)

		fields := []telemetry.Field{
			telemetry.Standard("http.request.method", r.Method),
			telemetry.Standard("http.response.status_code", recorder.status),
			telemetry.WithField("duration_ms", time.Since(start).Milliseconds()),
		}
		if _, route, ok := strings.Cut(r.Pattern, " "); ok {
			trace.SpanFromContext(ctx).SetAttributes(attribute.String("http.route", route))
			if labeler, ok := otelhttp.LabelerFromContext(ctx); ok {
				labeler.Add(attribute.String("http.route", route))
			}
			fields = append(fields, telemetry.Standard("http.route", route))
		}
		logger.Info(ctx, "request handled", fields...)
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
