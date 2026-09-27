package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"max-carreer-bot/internal/catalog"
	"max-carreer-bot/internal/profile"
)

func (a *authHandler) catalogContext(w http.ResponseWriter, r *http.Request) (profile.Document, int64, catalog.Stored, bool) {
	requestID := w.Header().Get("X-Request-ID")
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	state, err := a.state.Get(ctx, currentUser(r))
	if err != nil {
		writeStateError(w, err)
		return profile.Document{}, 0, catalog.Stored{}, false
	}
	if state.Profile == nil {
		writeError(w, 409, "PROFILE_REQUIRED", "Сначала завершите опросник", nil, requestID)
		return profile.Document{}, 0, catalog.Stored{}, false
	}
	if state.Profile.QuestionnaireVersion != a.bank.QuestionnaireVersion {
		writeError(w, 409, "PROFILE_OUTDATED", "Опросник изменился: обновите профиль", nil, requestID)
		return profile.Document{}, 0, catalog.Stored{}, false
	}
	stored, err := a.catalog.Active(ctx, false)
	if err != nil {
		code := "STORAGE_UNAVAILABLE"
		if errors.Is(err, catalog.ErrNoActiveCatalog) {
			code = "CATALOG_UNAVAILABLE"
		}
		writeError(w, 503, code, "Каталог временно недоступен", nil, requestID)
		return profile.Document{}, 0, catalog.Stored{}, false
	}
	return *state.Profile, state.ProfileRevision, stored, true
}

func (a *authHandler) recommendations(w http.ResponseWriter, r *http.Request) {
	if r.URL.RawQuery != "" {
		writeError(w, 400, "INVALID_QUERY", "Неизвестные параметры", nil, w.Header().Get("X-Request-ID"))
		return
	}
	p, revision, stored, ok := a.catalogContext(w, r)
	if !ok {
		return
	}
	writeJSON(w, 200, catalog.BuildResult(stored, p, revision, a.now(), catalog.Filters{}))
}

func (a *authHandler) opportunities(w http.ResponseWriter, r *http.Request) {
	query, parseErr := url.ParseQuery(r.URL.RawQuery)
	if parseErr != nil {
		writeError(w, 400, "INVALID_QUERY", "Некорректные параметры", nil, w.Header().Get("X-Request-ID"))
		return
	}
	for key, values := range query {
		if !slices.Contains([]string{"type", "direction", "format"}, key) || len(values) != 1 || values[0] == "" {
			writeError(w, 400, "INVALID_QUERY", "Некорректный фильтр", nil, w.Header().Get("X-Request-ID"))
			return
		}
	}
	filters := catalog.Filters{Type: query.Get("type"), Direction: query.Get("direction"), Format: query.Get("format")}
	if filters.Type != "" && !slices.Contains([]string{"course", "internship", "event"}, filters.Type) ||
		filters.Format != "" && !slices.Contains([]string{"online", "offline", "hybrid", "unknown"}, filters.Format) {
		writeError(w, 400, "INVALID_QUERY", "Некорректный фильтр", nil, w.Header().Get("X-Request-ID"))
		return
	}
	if filters.Direction != "" {
		known := false
		for _, direction := range a.bank.Directions {
			if direction.ID == filters.Direction {
				known = true
				break
			}
		}
		if !known {
			writeError(w, 400, "INVALID_QUERY", "Неизвестное направление", nil, w.Header().Get("X-Request-ID"))
			return
		}
	}
	p, revision, stored, ok := a.catalogContext(w, r)
	if !ok {
		return
	}
	writeJSON(w, 200, catalog.BuildResult(stored, p, revision, a.now(), filters))
}

func (a *authHandler) opportunity(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" || len(id) > 64 || strings.Contains(id, "/") {
		writeError(w, 404, "NOT_FOUND", "Карточка не найдена", nil, w.Header().Get("X-Request-ID"))
		return
	}
	p, revision, stored, ok := a.catalogContext(w, r)
	if !ok {
		return
	}
	now := a.now()
	for _, item := range stored.Items {
		if item.ID != id || item.IsDemo || item.PublicationStatus != "published" {
			continue
		}
		status, hint := catalog.EffectiveStatus(item, now)
		score := catalog.Rank(p, item)
		writeJSON(w, 200, struct {
			CatalogVersion  string             `json:"catalog_version"`
			ProfileRevision int64              `json:"profile_revision"`
			RankingVersion  int                `json:"ranking_version"`
			EvaluatedAt     time.Time          `json:"evaluated_at"`
			Item            catalog.ResultItem `json:"item"`
		}{stored.Version, revision, catalog.RankingVersion, now, catalog.ResultItem{Opportunity: item, EffectiveStatus: status, AvailabilityHint: hint, Reasons: score.Reasons}})
		return
	}
	writeError(w, 404, "NOT_FOUND", "Карточка не найдена", nil, w.Header().Get("X-Request-ID"))
}
