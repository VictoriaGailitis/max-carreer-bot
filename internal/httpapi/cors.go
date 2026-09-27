package httpapi

import (
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strings"
)

// WithCORS permits explicitly configured browser origins. Sessions use bearer
// tokens, so cookies and Access-Control-Allow-Credentials are not enabled.
func WithCORS(next http.Handler, origins string, production bool) (http.Handler, error) {
	allowed := map[string]bool{}
	if strings.TrimSpace(origins) != "" {
		for _, value := range strings.Split(origins, ",") {
			origin := strings.TrimSpace(value)
			u, err := url.Parse(origin)
			if err != nil || u.Hostname() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" || strings.Contains(origin, "*") || (u.Scheme != "http" && u.Scheme != "https") || (production && u.Scheme != "https") {
				return nil, errors.New("CORS_ALLOWED_ORIGINS must contain exact origins (HTTPS in production)")
			}
			allowed[origin] = true
		}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Vary", "Origin")
		origin := r.Header.Get("Origin")
		if !allowed[origin] {
			if r.Method == http.MethodOptions && origin != "" {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Expose-Headers", "X-Request-ID")
		if r.Method != http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Add("Vary", "Access-Control-Request-Method")
		w.Header().Add("Vary", "Access-Control-Request-Headers")
		if !slices.Contains([]string{"GET", "POST", "PUT", "DELETE"}, r.Header.Get("Access-Control-Request-Method")) {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		for _, header := range strings.Split(r.Header.Get("Access-Control-Request-Headers"), ",") {
			if header = strings.ToLower(strings.TrimSpace(header)); header != "" && !slices.Contains([]string{"authorization", "content-type", "idempotency-key"}, header) {
				w.WriteHeader(http.StatusForbidden)
				return
			}
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Idempotency-Key")
		w.Header().Set("Access-Control-Max-Age", "600")
		w.WriteHeader(http.StatusNoContent)
	}), nil
}
