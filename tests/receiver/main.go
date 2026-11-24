package main

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/murphyhq/server/internal/config"
	"github.com/murphyhq/server/internal/httpserver"
	"github.com/murphyhq/server/internal/lifecycle"
	"github.com/murphyhq/server/internal/logger"
	"github.com/murphyhq/server/internal/telemetry"
	"github.com/murphyhq/server/services/notifications/providers/webhook"
)

const kept = 100

type settings struct {
	Lifecycle  lifecycle.Config
	HTTPAddr   string `env:"RECEIVER_HTTP_ADDR" default:":8080"`
	PublicKeys string `env:"RECEIVER_PUBLIC_KEYS" required:"true"`
}

type received struct {
	Path     string          `json:"path"`
	ID       string          `json:"id"`
	Verified bool            `json:"verified"`
	At       time.Time       `json:"at"`
	Body     json.RawMessage `json:"body"`
}

type receiver struct {
	keys []ed25519.PublicKey
	mu   sync.Mutex
	last []received
}

func main() {
	ctx := context.Background()
	if err := run(ctx); err != nil {
		logger.Error(ctx, "webhook receiver stopped", telemetry.WithError(err))
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	var s settings
	if err := config.Load(&s); err != nil {
		return err
	}
	keys, err := webhook.ParsePublicKeys(s.PublicKeys)
	if err != nil {
		return err
	}
	r := &receiver{keys: keys}
	server := httpserver.New(httpserver.Config{Addr: s.HTTPAddr, ShutdownTimeout: s.Lifecycle.ShutdownTimeout}, r)
	return lifecycle.Run(ctx, "webhook-receiver", s.Lifecycle, server.Serve)
}

func (r *receiver) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if req.Method == http.MethodGet {
		r.mu.Lock()
		last := slices.Clone(r.last)
		r.mu.Unlock()
		slices.Reverse(last)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(last)
		return
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	got := received{Path: req.URL.Path, ID: req.Header.Get(webhook.HeaderID), At: time.Now().UTC(), Body: body}
	got.Verified = webhook.Verify(r.keys, got.ID, req.Header.Get(webhook.HeaderTimestamp), req.Header.Get(webhook.HeaderSignature), body, time.Now(), 5*time.Minute) == nil
	if !json.Valid(body) {
		got.Body = nil
	}

	r.mu.Lock()
	r.last = append(r.last, got)
	if len(r.last) > kept {
		r.last = r.last[len(r.last)-kept:]
	}
	r.mu.Unlock()

	logger.Info(req.Context(), "webhook received", telemetry.WithField("webhook.id", got.ID), telemetry.WithField("webhook.verified", got.Verified))
	if !got.Verified {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
