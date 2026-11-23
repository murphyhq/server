package httpserver

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const (
	corsMethods = "GET, HEAD, POST, PUT, PATCH, DELETE"
	corsHeaders = "Authorization, Content-Type, Idempotency-Key"
	corsExposed = "X-Trace-Id, traceresponse"
)

func ValidateOrigins(origins []string) error {
	for i, origin := range origins {
		u, err := url.Parse(origin)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" ||
			u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
			return errors.New("entry " + strconv.Itoa(i+1) + " is not an origin of the form scheme://host[:port]")
		}
	}
	return nil
}

func CORS(origins []string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if len(origins) == 0 {
			return next
		}
		allowed := make(map[string]bool, len(origins))
		for _, origin := range origins {
			allowed[strings.ToLower(origin)] = true
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin == "" {
				next.ServeHTTP(w, r)
				return
			}
			w.Header().Add("Vary", "Origin")
			if !allowed[strings.ToLower(origin)] {
				next.ServeHTTP(w, r)
				return
			}

			h := w.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			h.Set("Access-Control-Expose-Headers", corsExposed)
			if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
				h.Set("Access-Control-Allow-Methods", corsMethods)
				h.Set("Access-Control-Allow-Headers", corsHeaders)
				h.Set("Access-Control-Max-Age", "600")
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
