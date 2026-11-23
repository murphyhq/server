package httpserver

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const traceHeaders = "X-Trace-Id, traceresponse"

type CORS struct {
	Origins       []string
	Methods       []string
	AllowHeaders  []string
	ExposeHeaders []string
}

func validateOrigins(origins []string) error {
	for i, origin := range origins {
		u, err := url.Parse(origin)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" ||
			u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
			return errors.New("entry " + strconv.Itoa(i+1) + " is not an origin of the form scheme://host[:port]")
		}
	}
	return nil
}

func cors(c CORS) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if len(c.Origins) == 0 {
			return next
		}
		allowed := make(map[string]bool, len(c.Origins))
		for _, origin := range c.Origins {
			allowed[strings.ToLower(origin)] = true
		}
		expose := strings.Join(append([]string{traceHeaders}, c.ExposeHeaders...), ", ")
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
			h.Set("Access-Control-Expose-Headers", expose)
			if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
				h.Set("Access-Control-Allow-Methods", strings.Join(c.Methods, ", "))
				h.Set("Access-Control-Allow-Headers", strings.Join(c.AllowHeaders, ", "))
				h.Set("Access-Control-Max-Age", "600")
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
