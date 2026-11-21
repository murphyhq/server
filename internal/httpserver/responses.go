package httpserver

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/internal/logger"
)

type problem struct {
	Title  string `json:"title"`
	Status int    `json:"status"`
	Detail string `json:"detail,omitempty"`
}

func WriteProblem(w http.ResponseWriter, status int, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(problem{Title: http.StatusText(status), Status: status, Detail: detail})
}

func BadRequest(w http.ResponseWriter, _ *http.Request, err error) {
	WriteProblem(w, http.StatusBadRequest, err.Error())
}

func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, murphy.ErrInvalid):
		WriteProblem(w, http.StatusBadRequest, strings.ReplaceAll(err.Error(), "\n", "; "))
	case errors.Is(err, murphy.ErrNotFound):
		var notFound *murphy.NotFoundError
		detail := ""
		if errors.As(err, &notFound) {
			detail = notFound.Error()
		}
		WriteProblem(w, http.StatusNotFound, detail)
	case errors.Is(err, murphy.ErrConflict):
		WriteProblem(w, http.StatusConflict, "conflicts with an existing resource")
	default:
		logger.Error(r.Context(), "request failed", logger.WithError(err))
		WriteProblem(w, http.StatusInternalServerError, "")
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
					WriteProblem(w, http.StatusBadRequest, "unknown query parameter "+name)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

func problemsForUnmatched(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(&unmatchedWriter{ResponseWriter: w, request: r}, r)
	})
}

type unmatchedWriter struct {
	http.ResponseWriter
	request  *http.Request
	replaced bool
}

func (u *unmatchedWriter) WriteHeader(status int) {
	if u.request.Pattern == "" && (status == http.StatusNotFound || status == http.StatusMethodNotAllowed) {
		u.replaced = true
		WriteProblem(u.ResponseWriter, status, "")
		return
	}
	u.ResponseWriter.WriteHeader(status)
}

func (u *unmatchedWriter) Write(p []byte) (int, error) {
	if u.replaced {
		return len(p), nil
	}
	return u.ResponseWriter.Write(p)
}

func (u *unmatchedWriter) Unwrap() http.ResponseWriter {
	return u.ResponseWriter
}
