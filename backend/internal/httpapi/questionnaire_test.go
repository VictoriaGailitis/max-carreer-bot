package httpapi

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"max-carreer-bot/internal/questionnaire"
)

func TestPreviewHTTP(t *testing.T) {
	bank, err := questionnaire.Load(filepath.Join("..", "..", "data", "question-bank.v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	handler := QuestionnaireHandler(bank)
	tests := []struct {
		body, contentType string
		status            int
		contains          string
	}{
		{`{"directions":["backend","analytics"]}`, "application/json", 200, `"question_count":12`},
		{`{"directions":["bad"]}`, "application/json", 422, `"fields"`},
		{`{"directions":["backend"],"user_id":"x"}`, "application/json", 400, `MALFORMED_JSON`},
		{`{"directions":["backend"]} {}`, "application/json", 400, `MALFORMED_JSON`},
		{`{"directions":["backend"]}`, "text/plain", 415, `UNSUPPORTED_MEDIA_TYPE`},
		{strings.Repeat("x", maxPreviewBody+1), "application/json", 413, `BODY_TOO_LARGE`},
	}
	for _, tt := range tests {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/questionnaire/preview", strings.NewReader(tt.body))
		req.Header.Set("Content-Type", tt.contentType)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != tt.status || !strings.Contains(rec.Body.String(), tt.contains) {
			t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
		}
		if rec.Header().Get("X-Request-ID") == "" {
			t.Fatal("missing request ID")
		}
	}
}
