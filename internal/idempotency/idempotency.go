package idempotency

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"time"

	"github.com/murphyhq/server/internal/httpserver"
	"github.com/murphyhq/server/internal/logger"
)

const (
	header       = "Idempotency-Key"
	maxKeyLength = 255
	maxBodySize  = 1 << 20
)

type Record struct {
	Fingerprint string `json:"fingerprint"`
	Status      int    `json:"status,omitempty"`
	ContentType string `json:"content_type,omitempty"`
	Body        []byte `json:"body,omitempty"`
}

func (r *Record) completed() bool {
	return r.Status != 0
}

type Store interface {
	Reserve(ctx context.Context, key string, record Record, ttl time.Duration) (*Record, error)
	Complete(ctx context.Context, key string, record Record) error
	Release(ctx context.Context, key string) error
}

func Middleware(store Store, ttl time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := r.Header.Get(header)
			if key == "" || r.Method != http.MethodPost {
				next.ServeHTTP(w, r)
				return
			}
			if len(key) > maxKeyLength {
				httpserver.WriteProblem(w, http.StatusBadRequest, "Idempotency-Key must be at most 255 characters")
				return
			}

			body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodySize))
			if err != nil {
				httpserver.WriteProblem(w, http.StatusBadRequest, "request body could not be read")
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
			fingerprint := fingerprintOf(r, body)

			existing, err := store.Reserve(r.Context(), key, Record{Fingerprint: fingerprint}, ttl)
			if err != nil {
				httpserver.WriteError(w, r, err)
				return
			}
			if existing != nil {
				replay(w, existing, fingerprint)
				return
			}

			recorder := &recorder{ResponseWriter: w, status: http.StatusOK}
			finished := false
			defer func() {
				if !finished {
					release(r.Context(), store, key)
				}
			}()
			next.ServeHTTP(recorder, r)
			finished = true

			if recorder.status >= http.StatusInternalServerError {
				release(r.Context(), store, key)
				return
			}
			record := Record{Fingerprint: fingerprint, Status: recorder.status, ContentType: recorder.Header().Get("Content-Type"), Body: recorder.body.Bytes()}
			if err := store.Complete(context.WithoutCancel(r.Context()), key, record); err != nil {
				logger.Error(r.Context(), "store idempotent response", logger.WithError(err))
				release(r.Context(), store, key)
			}
		})
	}
}

func replay(w http.ResponseWriter, existing *Record, fingerprint string) {
	switch {
	case existing.Fingerprint != fingerprint:
		httpserver.WriteProblem(w, http.StatusUnprocessableEntity, "Idempotency-Key was already used for a different request")
	case !existing.completed():
		httpserver.WriteProblem(w, http.StatusConflict, "a request with this Idempotency-Key is still in progress")
	default:
		if existing.ContentType != "" {
			w.Header().Set("Content-Type", existing.ContentType)
		}
		w.Header().Set("Idempotent-Replayed", "true")
		w.WriteHeader(existing.Status)
		_, _ = w.Write(existing.Body)
	}
}

func release(ctx context.Context, store Store, key string) {
	if err := store.Release(context.WithoutCancel(ctx), key); err != nil {
		logger.Error(ctx, "release idempotency key", logger.WithError(err))
	}
}

func fingerprintOf(r *http.Request, body []byte) string {
	hash := sha256.New()
	hash.Write([]byte(r.Method + " " + r.URL.RequestURI() + "\n"))
	hash.Write(body)
	return hex.EncodeToString(hash.Sum(nil))
}

type recorder struct {
	http.ResponseWriter
	status int
	body   bytes.Buffer
}

func (r *recorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *recorder) Write(p []byte) (int, error) {
	r.body.Write(p)
	return r.ResponseWriter.Write(p)
}
