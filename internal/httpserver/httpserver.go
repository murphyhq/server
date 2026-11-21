package httpserver

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/murphyhq/server/internal/logger"
	"github.com/murphyhq/server/internal/metrics"
	"github.com/murphyhq/server/internal/tracer"
)

type Config struct {
	Addr            string
	ShutdownTimeout time.Duration
}

func Serve(ctx context.Context, cfg Config, handler http.Handler) error {
	duration, err := metrics.NewHistogram("http.server.request.duration", "Time taken to answer HTTP requests.", "s")
	if err != nil {
		return err
	}

	listener, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return fmt.Errorf("listen for HTTP on %s: %w", cfg.Addr, err)
	}
	server := &http.Server{Handler: tracer.ContinueHTTP(measure(duration, problemsForUnmatched(handler))), ReadHeaderTimeout: 10 * time.Second}
	errs := make(chan error, 1)
	go func() { errs <- server.Serve(listener) }()
	logger.Info(ctx, "serving HTTP", logger.WithField("http_addr", listener.Addr().String()))

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

func measure(duration *metrics.Histogram, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r)

		route := r.Pattern
		if route == "" {
			route = "unmatched"
		}
		duration.Record(r.Context(), time.Since(start).Seconds(),
			metrics.Dimension{Name: "http.route", Value: route},
			metrics.Dimension{Name: "http.status", Value: strconv.Itoa(recorder.status)},
		)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}
