package httpapi

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"max-carreer-bot/internal/auth"
	"max-carreer-bot/internal/questionnaire"
)

const maxFixture = "auth_date=1771409719&query_id=4c0ab423-342b-4e45-aea4-2747dbc500cd&user=%7B%22id%22%3A67890%2C%22first_name%22%3A%22Max%20%2B%20%D0%A2%D0%B5%D1%81%D1%82%22%7D&hash=d7f1f5337cbc7ad50544fdb236e6141e124f46fdf0f1be803ea49754ecc2c8d2"

type fakeSessionRepository struct {
	sessions map[[32]byte]time.Time
}

func (f *fakeSessionRepository) Exchange(_ context.Context, maxID int64, hash [32]byte, expires time.Time) (string, error) {
	if maxID != 67890 {
		panic("wrong MAX ID")
	}
	f.sessions[hash] = expires
	return "6d305789-d179-4c87-9d96-695dd88891f1", nil
}

func (f *fakeSessionRepository) Lookup(_ context.Context, hash [32]byte, now time.Time) (string, bool, error) {
	expires, ok := f.sessions[hash]
	return "6d305789-d179-4c87-9d96-695dd88891f1", ok && expires.After(now), nil
}

func (f *fakeSessionRepository) Revoke(_ context.Context, hash [32]byte, _ time.Time) error {
	delete(f.sessions, hash)
	return nil
}

func (f *fakeSessionRepository) Ready(context.Context) error { return nil }

func TestAuthSessionAndProtectedPreview(t *testing.T) {
	bank, err := questionnaire.Load(filepath.Join("..", "..", "data", "question-bank.v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	validator, err := auth.NewValidator("test-bot-token")
	if err != nil {
		t.Fatal(err)
	}
	store := &fakeSessionRepository{sessions: map[[32]byte]time.Time{}}
	handler, err := NewAuthHandler(validator, store, bank, func() time.Time { return time.Unix(1771409719+60, 0) })
	if err != nil {
		t.Fatal(err)
	}
	request := func(method, path, body, bearer string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		return res
	}
	previewBody := `{"directions":["backend"]}`
	if res := request("POST", "/api/v1/questionnaire/preview", previewBody, ""); res.Code != 401 {
		t.Fatalf("anonymous preview: %d", res.Code)
	}
	loginBody, err := json.Marshal(map[string]string{"init_data": maxFixture})
	if err != nil {
		t.Fatal(err)
	}
	login := request("POST", "/api/v1/auth/max", string(loginBody), "")
	if login.Code != 200 {
		t.Fatalf("login: %d %s", login.Code, login.Body.String())
	}
	var session struct {
		Token  string `json:"token"`
		UserID string `json:"user_id"`
	}
	if err := json.Unmarshal(login.Body.Bytes(), &session); err != nil || session.Token == "" {
		t.Fatalf("invalid session response: %v", err)
	}
	if session.UserID != "6d305789-d179-4c87-9d96-695dd88891f1" {
		t.Fatalf("wrong user %s", session.UserID)
	}
	if res := request("POST", "/api/v1/questionnaire/preview", previewBody, session.Token); res.Code != 200 {
		t.Fatalf("authenticated preview: %d %s", res.Code, res.Body.String())
	}
	if res := request("DELETE", "/api/v1/auth/session", "", session.Token); res.Code != 204 || res.Body.Len() != 0 {
		t.Fatalf("logout: %d %s", res.Code, res.Body.String())
	}
	if res := request("POST", "/api/v1/questionnaire/preview", previewBody, session.Token); res.Code != 401 {
		t.Fatalf("revoked token accepted: %d", res.Code)
	}
	if res := request("POST", "/api/v1/auth/max", `{"init_data":"bad"}`, ""); res.Code != 401 {
		t.Fatalf("bad initData: %d", res.Code)
	}
	if res := request("POST", "/api/v1/auth/max", `{"init_data":"bad","user_id":"other"}`, ""); res.Code != 400 {
		t.Fatalf("client user_id accepted: %d", res.Code)
	}
}
