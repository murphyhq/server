package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

type Config struct {
	HealthAddr      string        `env:"HEALTH_ADDR" default:":9000"`
	ShutdownTimeout time.Duration `env:"SHUTDOWN_TIMEOUT" default:"30s"`
}

type Check func(ctx context.Context) error

func Run(ctx context.Context, cfg Config, run func(context.Context) error, ready ...Check) error {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	listener, err := net.Listen("tcp", cfg.HealthAddr)
	if err != nil {
		return fmt.Errorf("listen for health checks on %s: %w", cfg.HealthAddr, err)
	}
	health := &http.Server{Handler: Handler(ready...), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		_ = health.Serve(listener)
	}()

	done := make(chan error, 1)
	go func() {
		done <- run(ctx)
	}()

	select {
	case err := <-done:
		return errors.Join(err, shutdown(health, cfg.ShutdownTimeout))
	case <-ctx.Done():
	}

	timer := time.NewTimer(cfg.ShutdownTimeout)
	defer timer.Stop()

	select {
	case err := <-done:
		return errors.Join(err, shutdown(health, cfg.ShutdownTimeout))
	case <-timer.C:
		return errors.Join(fmt.Errorf("did not stop within %s", cfg.ShutdownTimeout), shutdown(health, cfg.ShutdownTimeout))
	}
}

func UntilStopped(ctx context.Context) error {
	<-ctx.Done()
	return nil
}

func Handler(ready ...Check) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		for _, check := range ready {
			if err := check(r.Context()); err != nil {
				http.Error(w, "not ready", http.StatusServiceUnavailable)
				return
			}
		}
		w.WriteHeader(http.StatusOK)
	})
	return mux
}

func shutdown(server *http.Server, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		return fmt.Errorf("stop health server: %w", err)
	}
	return nil
}
