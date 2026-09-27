package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCORS(t *testing.T) {
	for _, test := range []struct {
		name, method, origin, requestedMethod, headers string
		status                                         int
		called, permitted                              bool
	}{
		{"preflight", "OPTIONS", "http://localhost:5173", "PUT", "authorization, Content-Type, Idempotency-Key", 204, false, true},
		{"foreign preflight", "OPTIONS", "https://foreign.test", "PUT", "", 403, false, false},
		{"method rejected", "OPTIONS", "http://localhost:5173", "PATCH", "", 403, false, true},
		{"header rejected", "OPTIONS", "http://localhost:5173", "POST", "X-Max-Bot-Api-Secret", 403, false, true},
		{"authenticated request reaches API", "GET", "http://localhost:5173", "", "", 401, true, true},
		{"foreign origin receives no permission", "GET", "https://foreign.test", "", "", 401, true, false},
		{"server request", "GET", "", "", "", 401, true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			called := false
			handler, err := WithCORS(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				w.WriteHeader(http.StatusUnauthorized)
			}), "http://localhost:5173, http://127.0.0.1:5173", false)
			if err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest(test.method, "/api/v1/me", nil)
			r.Header.Set("Origin", test.origin)
			r.Header.Set("Access-Control-Request-Method", test.requestedMethod)
			r.Header.Set("Access-Control-Request-Headers", test.headers)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != test.status || called != test.called {
				t.Fatalf("status=%d called=%v", w.Code, called)
			}
			if (w.Header().Get("Access-Control-Allow-Origin") == test.origin && test.origin != "") != test.permitted {
				t.Fatal("incorrect allowed origin")
			}
			if w.Header().Get("Access-Control-Allow-Credentials") != "" {
				t.Fatal("cookie credentials enabled")
			}
		})
	}
}

func TestCORSConfiguration(t *testing.T) {
	for _, origin := range []string{"*", "null", "https://*.test", "https://app.test/", "https://user@app.test", "https://app.test?", "https://app.test#fragment", "https://app.test,", "http://app.test"} {
		if _, err := WithCORS(http.NotFoundHandler(), origin, true); err == nil {
			t.Fatalf("accepted %q", origin)
		}
	}
	if _, err := WithCORS(http.NotFoundHandler(), "https://app.test, https://other.test:8443", true); err != nil {
		t.Fatal(err)
	}
	if _, err := WithCORS(http.NotFoundHandler(), "", true); err != nil {
		t.Fatal(err)
	}
}
