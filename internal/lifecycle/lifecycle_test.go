package lifecycle_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/murphyhq/server/internal/lifecycle"
)

var cfg = lifecycle.Config{HealthAddr: "127.0.0.1:0", ShutdownTimeout: 200 * time.Millisecond}

func TestRunReturnsWhatRunReturns(t *testing.T) {
	assert.NoError(t, lifecycle.Run(context.Background(), "test", cfg, func(context.Context) error { return nil }))

	boom := errors.New("boom")
	assert.ErrorIs(t, lifecycle.Run(context.Background(), "test", cfg, func(context.Context) error { return boom }), boom)
}

func TestRunStopsWhenTheContextEnds(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		result <- lifecycle.Run(ctx, "test", cfg, lifecycle.UntilStopped)
	}()

	cancel()

	select {
	case err := <-result:
		assert.NoError(t, err)
	case <-time.After(time.Second):
		require.Fail(t, "Run did not stop")
	}
}

func TestRunGivesUpAfterTheShutdownTimeout(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	stuck := make(chan struct{})
	t.Cleanup(func() { close(stuck) })

	result := make(chan error, 1)
	go func() {
		result <- lifecycle.Run(ctx, "test", cfg, func(context.Context) error {
			<-stuck
			return nil
		})
	}()

	cancel()

	select {
	case err := <-result:
		assert.ErrorContains(t, err, "did not stop within")
	case <-time.After(time.Second):
		require.Fail(t, "Run did not give up")
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

			assert.Equal(t, tt.want, rec.Code)
			assert.NotContains(t, rec.Body.String(), "database unreachable")
		})
	}
}
