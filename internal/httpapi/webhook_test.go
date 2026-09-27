package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"max-carreer-bot/internal/maxbot"
)

type fakeInbox struct {
	count int
	fail  bool
}

func (f *fakeInbox) Enqueue(_ context.Context, task maxbot.Task, _ time.Time) error {
	f.count++
	if task.RecipientUserID != 42 {
		return errors.New("wrong recipient")
	}
	if f.fail {
		return errors.New("database unavailable")
	}
	return nil
}

func TestWebhookAcknowledgesOnlyStoredTasks(t *testing.T) {
	p, _ := maxbot.NewParser("test-token")
	store := &fakeInbox{}
	h, err := NewWebhook("abcde", p, store, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	valid := `{"update_type":"bot_started","timestamp":1780000000000,"chat_id":77,"user":{"user_id":42}}`
	request := func(body, secret string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/max", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Max-Bot-Api-Secret", secret)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if got := request(valid, "wrong").Code; got != 401 {
		t.Fatalf("auth: %d", got)
	}
	if store.count != 0 {
		t.Fatal("unauthorized write")
	}
	if got := request(valid, "abcde").Code; got != 200 || store.count != 1 {
		t.Fatalf("stored: %d %d", got, store.count)
	}
	store.fail = true
	if got := request(valid, "abcde").Code; got != 503 {
		t.Fatalf("storage: %d", got)
	}
	if got := request(`{"update_type":"unknown","timestamp":1780000000000}`, "abcde").Code; got != 200 {
		t.Fatalf("unknown: %d", got)
	}
	if got := request(`{`, "abcde").Code; got != 400 {
		t.Fatalf("malformed: %d", got)
	}
	if got := request(strings.Repeat("x", maxbot.MaxWebhookBytes+1), "abcde").Code; got != 413 {
		t.Fatalf("oversize: %d", got)
	}
}
