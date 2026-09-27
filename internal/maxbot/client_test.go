package maxbot

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestClientSendsOpenAppAndHandlesRetry(t *testing.T) {
	count := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count++
		if r.Method != "POST" || r.URL.String() != "/messages?user_id=42" || r.Header.Get("Authorization") != "secret-token" {
			t.Errorf("bad request: %s %s", r.Method, r.URL)
		}
		var body struct {
			Text        string `json:"text"`
			Attachments []struct {
				Type    string `json:"type"`
				Payload struct {
					Buttons [][]struct {
						Type   string `json:"type"`
						WebApp string `json:"web_app"`
					} `json:"buttons"`
				} `json:"payload"`
			} `json:"attachments"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body.Text == "" || len(body.Attachments) != 1 || len(body.Attachments[0].Payload.Buttons) != 1 || body.Attachments[0].Payload.Buttons[0][0].Type != "open_app" || body.Attachments[0].Payload.Buttons[0][0].WebApp != "test_bot" {
			t.Errorf("bad body: %+v", body)
		}
		if count == 1 {
			w.Header().Set("Retry-After", "7")
			w.WriteHeader(http.StatusTooManyRequests)
		} else {
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()
	client, err := NewClient("secret-token", server.URL, "test_bot", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.Send(context.Background(), 42)
	if err == nil || !result.Retryable || result.RetryAfter != 7*time.Second {
		t.Fatalf("retry: %+v %v", result, err)
	}
	result, err = client.Send(context.Background(), 42)
	if err != nil || result.Retryable {
		t.Fatalf("success: %+v %v", result, err)
	}
}

func TestClientUsesConfiguredCA(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "ca.pem")
	cert := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	if err := os.WriteFile(path, cert, 0600); err != nil {
		t.Fatal(err)
	}
	httpClient, err := NewHTTPClient(path)
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewClient("token", server.URL, "test_bot", httpClient)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Send(context.Background(), 42); err != nil {
		t.Fatal(err)
	}
}
