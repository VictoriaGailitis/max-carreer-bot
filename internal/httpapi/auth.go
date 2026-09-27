package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"max-carreer-bot/internal/auth"
	"max-carreer-bot/internal/catalog"
	"max-carreer-bot/internal/profile"
	"max-carreer-bot/internal/questionnaire"
	"max-carreer-bot/internal/storage"
)

const maxAuthBody = 64 << 10

type SessionRepository interface {
	Exchange(context.Context, int64, [32]byte, time.Time) (string, error)
	Lookup(context.Context, [32]byte, time.Time) (string, bool, error)
	Revoke(context.Context, [32]byte, time.Time) error
	Ready(context.Context) error
}

type sessionContextKey struct{}
type sessionContext struct {
	UserID    string
	TokenHash [32]byte
}

type authHandler struct {
	validator *auth.Validator
	repo      SessionRepository
	state     UserStateRepository
	catalog   CatalogRepository
	bank      *questionnaire.Bank
	now       func() time.Time
}

type UserStateRepository interface {
	Get(context.Context, string) (storage.State, error)
	PutDraft(context.Context, string, int64, profile.Document) (storage.State, error)
	DeleteDraft(context.Context, string) error
	Complete(context.Context, string, int64, string, time.Time) (storage.State, error)
	DeleteData(context.Context, string) error
	GetFavorites(context.Context, string) (storage.FavoritesState, error)
	MutateFavorite(context.Context, string, string, bool) (storage.FavoritesState, error)
}

type CatalogRepository interface {
	Active(context.Context, bool) (catalog.Stored, error)
	Ready(context.Context) error
}

// NewAuthHandler exposes only the session-backed slice of the API. No caller
// supplied user identifier is accepted on authenticated endpoints.
func NewAuthHandler(validator *auth.Validator, repo SessionRepository, bank *questionnaire.Bank, now func() time.Time) (http.Handler, error) {
	return newHandler(validator, repo, nil, nil, bank, now)
}

func NewAPIHandler(validator *auth.Validator, repo SessionRepository, state UserStateRepository, bank *questionnaire.Bank, now func() time.Time) (http.Handler, error) {
	if state == nil {
		return nil, errors.New("missing user state dependency")
	}
	return newHandler(validator, repo, state, nil, bank, now)
}

func NewCatalogAPIHandler(validator *auth.Validator, repo SessionRepository, state UserStateRepository, catalogRepo CatalogRepository, bank *questionnaire.Bank, now func() time.Time) (http.Handler, error) {
	if state == nil || catalogRepo == nil {
		return nil, errors.New("missing catalog API dependency")
	}
	return newHandler(validator, repo, state, catalogRepo, bank, now)
}

func newHandler(validator *auth.Validator, repo SessionRepository, state UserStateRepository, catalogRepo CatalogRepository, bank *questionnaire.Bank, now func() time.Time) (http.Handler, error) {
	if validator == nil || repo == nil || bank == nil || now == nil {
		return nil, errors.New("missing auth API dependency")
	}
	a := &authHandler{validator: validator, repo: repo, state: state, catalog: catalogRepo, bank: bank, now: now}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/auth/max", a.exchange)
	mux.Handle("DELETE /api/v1/auth/session", a.requireSession(http.HandlerFunc(a.revoke)))
	mux.Handle("/api/v1/questionnaire/preview", a.requireSession(QuestionnaireHandler(bank)))
	if state != nil {
		mux.Handle("GET /api/v1/questionnaire", a.requireSession(http.HandlerFunc(a.questionnaire)))
		mux.Handle("GET /api/v1/me", a.requireSession(http.HandlerFunc(a.me)))
		mux.Handle("GET /api/v1/me/draft", a.requireSession(http.HandlerFunc(a.getDraft)))
		mux.Handle("PUT /api/v1/me/draft", a.requireSession(http.HandlerFunc(a.putDraft)))
		mux.Handle("DELETE /api/v1/me/draft", a.requireSession(http.HandlerFunc(a.deleteDraft)))
		mux.Handle("POST /api/v1/me/onboarding/complete", a.requireSession(http.HandlerFunc(a.complete)))
		mux.Handle("DELETE /api/v1/me/data", a.requireSession(http.HandlerFunc(a.deleteData)))
	}
	if catalogRepo != nil {
		mux.Handle("GET /api/v1/recommendations", a.requireSession(http.HandlerFunc(a.recommendations)))
		mux.Handle("GET /api/v1/opportunities", a.requireSession(http.HandlerFunc(a.opportunities)))
		mux.Handle("GET /api/v1/opportunities/{id}", a.requireSession(http.HandlerFunc(a.opportunity)))
		mux.Handle("GET /api/v1/me/favorites", a.requireSession(http.HandlerFunc(a.favorites)))
		mux.Handle("PUT /api/v1/me/favorites/{id}", a.requireSession(http.HandlerFunc(a.putFavorite)))
		mux.Handle("DELETE /api/v1/me/favorites/{id}", a.requireSession(http.HandlerFunc(a.deleteFavorite)))
	}
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /readyz", a.ready)
	return mux, nil
}

func (a *authHandler) exchange(w http.ResponseWriter, r *http.Request) {
	requestID := newRequestID()
	w.Header().Set("X-Request-ID", requestID)
	w.Header().Set("Cache-Control", "no-store")
	mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" || (params["charset"] != "" && !strings.EqualFold(params["charset"], "utf-8")) {
		writeError(w, http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE", "Ожидается JSON", nil, requestID)
		return
	}
	if r.ContentLength > maxAuthBody {
		writeError(w, http.StatusRequestEntityTooLarge, "BODY_TOO_LARGE", "Слишком большой запрос", nil, requestID)
		return
	}
	defer r.Body.Close()
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxAuthBody))
	decoder.DisallowUnknownFields()
	var input struct {
		InitData string `json:"init_data"`
	}
	if err := decoder.Decode(&input); err != nil {
		writeDecodeError(w, err, requestID)
		return
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		writeDecodeError(w, err, requestID)
		return
	}
	now := a.now()
	identity, err := a.validator.Validate(input.InitData, now)
	if err != nil {
		code := "INVALID_INIT_DATA"
		if errors.Is(err, auth.ErrExpiredInitData) {
			code = "INIT_DATA_EXPIRED"
		}
		writeError(w, http.StatusUnauthorized, code, "Не удалось подтвердить вход MAX", nil, requestID)
		return
	}
	session, err := auth.IssueSession(now)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Ошибка сервера", nil, requestID)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	userID, err := a.repo.Exchange(ctx, identity.MaxID, session.TokenHash, session.ExpiresAt)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "STORAGE_UNAVAILABLE", "Сервис временно недоступен", nil, requestID)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
		UserID    string    `json:"user_id"`
	}{session.Token, session.ExpiresAt, userID})
}

func (a *authHandler) requireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := newRequestID()
		w.Header().Set("X-Request-ID", requestID)
		w.Header().Set("Cache-Control", "no-store")
		hash, err := auth.HashBearer(r.Header.Get("Authorization"))
		if err != nil {
			writeError(w, http.StatusUnauthorized, "INVALID_SESSION", "Требуется вход", nil, requestID)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		userID, valid, err := a.repo.Lookup(ctx, hash, a.now())
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "STORAGE_UNAVAILABLE", "Сервис временно недоступен", nil, requestID)
			return
		}
		if !valid {
			writeError(w, http.StatusUnauthorized, "INVALID_SESSION", "Требуется вход", nil, requestID)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), sessionContextKey{}, sessionContext{UserID: userID, TokenHash: hash})))
	})
}

func (a *authHandler) revoke(w http.ResponseWriter, r *http.Request) {
	current := r.Context().Value(sessionContextKey{}).(sessionContext)
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	if err := a.repo.Revoke(ctx, current.TokenHash, a.now()); err != nil {
		writeError(w, http.StatusServiceUnavailable, "STORAGE_UNAVAILABLE", "Сервис временно недоступен", nil, w.Header().Get("X-Request-ID"))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *authHandler) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	if err := a.repo.Ready(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "not_ready"})
		return
	}
	if a.catalog != nil {
		if err := a.catalog.Ready(ctx); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "not_ready"})
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}
