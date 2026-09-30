package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"max-carreer-bot/internal/assessment"
)

func (a *authHandler) results(w http.ResponseWriter, r *http.Request) {
	requestID := w.Header().Get("X-Request-ID")
	if r.URL.RawQuery != "" {
		writeError(w, 400, "INVALID_QUERY", "Неизвестные параметры", nil, requestID)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	state, err := a.state.Get(ctx, currentUser(r))
	if err != nil {
		writeStateError(w, err)
		return
	}
	if state.Profile == nil {
		writeError(w, 409, "PROFILE_REQUIRED", "Сначала завершите опросник", nil, requestID)
		return
	}
	result, err := assessment.Build(a.bank, *state.Profile, state.ProfileRevision, a.now())
	if errors.Is(err, assessment.ErrOutdated) {
		writeError(w, 409, "PROFILE_OUTDATED", "Опросник изменился: обновите профиль", nil, requestID)
		return
	}
	if err != nil {
		writeError(w, 503, "RESULTS_UNAVAILABLE", "Не удалось получить результаты", nil, requestID)
		return
	}
	writeJSON(w, 200, result)
}
