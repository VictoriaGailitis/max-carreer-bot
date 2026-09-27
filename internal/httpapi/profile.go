package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"regexp"
	"strings"
	"time"

	"max-carreer-bot/internal/profile"
	"max-carreer-bot/internal/questionnaire"
)

const maxProfileBody = 64 << 10

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func decodeProfileRequest(w http.ResponseWriter, r *http.Request, target any) error {
	media, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" || (params["charset"] != "" && !strings.EqualFold(params["charset"], "utf-8")) {
		writeError(w, http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE", "Ожидается JSON", nil, w.Header().Get("X-Request-ID"))
		return errors.New("response written")
	}
	defer r.Body.Close()
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxProfileBody))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeDecodeError(w, err, w.Header().Get("X-Request-ID"))
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		writeDecodeError(w, err, w.Header().Get("X-Request-ID"))
		return errors.New("trailing JSON")
	}
	return nil
}

func (a *authHandler) userState(ctx context.Context, userID string) (any, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return a.state.Get(ctx, userID)
}

func currentUser(r *http.Request) string {
	return r.Context().Value(sessionContextKey{}).(sessionContext).UserID
}

func (a *authHandler) me(w http.ResponseWriter, r *http.Request) {
	state, err := a.userState(r.Context(), currentUser(r))
	if err != nil {
		writeStateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, state)
}

func (a *authHandler) getDraft(w http.ResponseWriter, r *http.Request) {
	state, err := a.state.Get(r.Context(), currentUser(r))
	if err != nil {
		writeStateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Draft    *profile.Document `json:"draft"`
		Revision int64             `json:"revision"`
	}{state.Draft, state.DraftRevision})
}

func (a *authHandler) putDraft(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Revision int64            `json:"revision"`
		Draft    profile.Document `json:"draft"`
	}
	if err := decodeProfileRequest(w, r, &input); err != nil {
		return
	}
	if input.Revision < 0 {
		writeError(w, 422, "VALIDATION_ERROR", "Проверьте ревизию", map[string]string{"revision": "Неверная ревизия"}, w.Header().Get("X-Request-ID"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	state, err := a.state.PutDraft(ctx, currentUser(r), input.Revision, input.Draft)
	if err != nil {
		writeStateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Draft    *profile.Document `json:"draft"`
		Revision int64             `json:"revision"`
	}{state.Draft, state.DraftRevision})
}

func (a *authHandler) deleteDraft(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	if err := a.state.DeleteDraft(ctx, currentUser(r)); err != nil {
		writeStateError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *authHandler) complete(w http.ResponseWriter, r *http.Request) {
	key := r.Header.Get("Idempotency-Key")
	if !uuidPattern.MatchString(key) {
		writeError(w, 400, "INVALID_IDEMPOTENCY_KEY", "Требуется UUID в Idempotency-Key", nil, w.Header().Get("X-Request-ID"))
		return
	}
	var input struct {
		DraftRevision int64 `json:"draft_revision"`
	}
	if err := decodeProfileRequest(w, r, &input); err != nil {
		return
	}
	if input.DraftRevision <= 0 {
		writeError(w, 422, "VALIDATION_ERROR", "Проверьте ревизию", map[string]string{"draft_revision": "Неверная ревизия"}, w.Header().Get("X-Request-ID"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	state, err := a.state.Complete(ctx, currentUser(r), input.DraftRevision, key, a.now())
	if err != nil {
		writeStateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Profile  *profile.Document `json:"profile"`
		Revision int64             `json:"revision"`
	}{state.Profile, state.ProfileRevision})
}

func (a *authHandler) deleteData(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	if err := a.state.DeleteData(ctx, currentUser(r)); err != nil {
		writeStateError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeStateError(w http.ResponseWriter, err error) {
	requestID := w.Header().Get("X-Request-ID")
	if errors.Is(err, profile.ErrRevisionConflict) {
		writeError(w, 409, "REVISION_CONFLICT", "Состояние было изменено", nil, requestID)
		return
	}
	var profileValidation *profile.ValidationError
	if errors.As(err, &profileValidation) {
		writeError(w, 422, "VALIDATION_ERROR", "Проверьте профиль", profileValidation.Fields, requestID)
		return
	}
	var questionnaireValidation *questionnaire.ValidationError
	if errors.As(err, &questionnaireValidation) {
		writeError(w, 422, "VALIDATION_ERROR", "Проверьте ответы", questionnaireValidation.Fields, requestID)
		return
	}
	writeError(w, 503, "STORAGE_UNAVAILABLE", "Сервис временно недоступен", nil, requestID)
}
