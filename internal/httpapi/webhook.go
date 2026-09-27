package httpapi

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"max-carreer-bot/internal/maxbot"
)

type BotInbox interface {
	Enqueue(context.Context, maxbot.Task, time.Time) error
}

type Webhook struct {
	secretHash [32]byte
	parser     *maxbot.Parser
	inbox      BotInbox
	now        func() time.Time
}

func NewWebhook(secret string, parser *maxbot.Parser, inbox BotInbox, now func() time.Time) (*Webhook, error) {
	if len(secret) < 5 || len(secret) > 256 || parser == nil || inbox == nil || now == nil {
		return nil, errors.New("invalid webhook configuration")
	}
	for _, c := range secret {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return nil, errors.New("invalid webhook secret")
		}
	}
	return &Webhook{secretHash: sha256.Sum256([]byte(secret)), parser: parser, inbox: inbox, now: now}, nil
}

func (h *Webhook) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	provided := sha256.Sum256([]byte(r.Header.Get("X-Max-Bot-Api-Secret")))
	if subtle.ConstantTimeCompare(h.secretHash[:], provided[:]) != 1 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" || (params["charset"] != "" && !strings.EqualFold(params["charset"], "utf-8")) {
		http.Error(w, "JSON required", http.StatusUnsupportedMediaType)
		return
	}
	if r.ContentLength > maxbot.MaxWebhookBytes {
		http.Error(w, "body too large", http.StatusRequestEntityTooLarge)
		return
	}
	defer r.Body.Close()
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxbot.MaxWebhookBytes))
	if err != nil {
		http.Error(w, "body too large", http.StatusRequestEntityTooLarge)
		return
	}
	task, ok, err := h.parser.Parse(body)
	if err != nil {
		http.Error(w, "invalid update", http.StatusBadRequest)
		return
	}
	if ok {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		if err := h.inbox.Enqueue(ctx, task, h.now()); err != nil {
			http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
			return
		}
	}
	w.WriteHeader(http.StatusOK)
}
