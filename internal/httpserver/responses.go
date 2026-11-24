package httpserver

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/internal/logger"
	"github.com/murphyhq/server/internal/telemetry"
	"github.com/murphyhq/server/internal/tracer"
)

type problem struct {
	Title   string `json:"title"`
	Status  int    `json:"status"`
	Detail  string `json:"detail,omitempty"`
	TraceID string `json:"trace_id,omitempty"`
}

func WriteProblem(w http.ResponseWriter, r *http.Request, status int, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(problem{
		Title:   http.StatusText(status),
		Status:  status,
		Detail:  detail,
		TraceID: tracer.TraceID(r.Context()),
	})
}

func BadRequest(w http.ResponseWriter, r *http.Request, err error) {
	WriteProblem(w, r, http.StatusBadRequest, err.Error())
}

func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, murphy.ErrInvalid):
		WriteProblem(w, r, http.StatusBadRequest, strings.ReplaceAll(err.Error(), "\n", "; "))
	case errors.Is(err, murphy.ErrNotFound):
		var notFound *murphy.NotFoundError
		detail := ""
		if errors.As(err, &notFound) {
			detail = notFound.Error()
		}
		WriteProblem(w, r, http.StatusNotFound, detail)
	case errors.Is(err, murphy.ErrConflict):
		WriteProblem(w, r, http.StatusConflict, "conflicts with an existing resource")
	case errors.Is(err, murphy.ErrUnauthenticated):
		WriteProblem(w, r, http.StatusUnauthorized, "sign in first")
	case errors.Is(err, murphy.ErrTooManyAttempts):
		WriteProblem(w, r, http.StatusTooManyRequests, "too many failed attempts; try again later")
	case errors.Is(err, murphy.ErrForbidden):
		var forbidden *murphy.ForbiddenError
		detail := "you aren't allowed to do this"
		if errors.As(err, &forbidden) {
			detail = forbidden.Reason
		}
		WriteProblem(w, r, http.StatusForbidden, detail)
	default:
		logger.Error(r.Context(), "request failed", telemetry.WithError(err))
		WriteProblem(w, r, http.StatusInternalServerError, "")
	}
}

func RejectUnknownParameters(spec *openapi3.T) func(http.Handler) http.Handler {
	routes := map[string]map[string]bool{}
	for path, item := range spec.Paths.Map() {
		for method, operation := range item.Operations() {
			names := map[string]bool{}
			for _, parameter := range append(item.Parameters, operation.Parameters...) {
				if parameter.Value.In == "query" {
					names[parameter.Value.Name] = true
				}
			}
			routes[method+" "+path] = names
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			allowed := routes[r.Pattern]
			for name := range r.URL.Query() {
				if !allowed[name] {
					WriteProblem(w, r, http.StatusBadRequest, "unknown query parameter "+name)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}
