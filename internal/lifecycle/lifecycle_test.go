package lifecycle_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/murphyhq/server/internal/lifecycle"
)

var cfg = lifecycle.Config{HealthAddr: "127.0.0.1:0", ShutdownTimeout: 200 * time.Millisecond}

func TestRunReturnsWhatRunReturns(t *testing.T) {
	if err := lifecycle.Run(context.Background(), cfg, func(context.Context) error { return nil }); err != nil {
		t.Errorf("Run = %v, want nil", err)
	}

	boom := errors.New("boom")
	if err := lifecycle.Run(context.Background(), cfg, func(context.Context) error { return boom }); !errors.Is(err, boom) {
		t.Errorf("Run = %v, want %v", err, boom)
	}
}

func TestRunStopsWhenTheContextEnds(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		result <- lifecycle.Run(ctx, cfg, lifecycle.UntilStopped)
	}()

	cancel()
	select {
	case err := <-result:
		if err != nil {
			t.Errorf("Run = %v, want nil", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run did not stop")
	}
}

func TestRunGivesUpAfterTheShutdownTimeout(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	stuck := make(chan struct{})
	t.Cleanup(func() { close(stuck) })

	result := make(chan error, 1)
	go func() {
		result <- lifecycle.Run(ctx, cfg, func(context.Context) error {
			<-stuck
			return nil
		})
	}()

	cancel()
	select {
	case err := <-result:
		if err == nil || !strings.Contains(err.Error(), "did not stop within") {
			t.Errorf("Run = %v, want a shutdown timeout error", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run did not give up")
	}
}

func TestHandler(t *testing.T) {
	failing := func(context.Context) error { return errors.New("database unreachable") }
	passing := func(context.Context) error { return nil }

	tests := []struct {
		name  string
		path  string
		ready []lifecycle.Check
		want  int
	}{
		{"alive", "/healthz", []lifecycle.Check{failing}, http.StatusOK},
		{"ready", "/readyz", []lifecycle.Check{passing}, http.StatusOK},
		{"not ready", "/readyz", []lifecycle.Check{passing, failing}, http.StatusServiceUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			lifecycle.Handler(tt.ready...).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tt.path, nil))
			if rec.Code != tt.want {
				t.Errorf("%s = %d, want %d", tt.path, rec.Code, tt.want)
			}
			if strings.Contains(rec.Body.String(), "database unreachable") {
				t.Error("readiness response leaks the check's error")
			}
		})
	}
}
