package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"max-carreer-bot/internal/catalog"
	"max-carreer-bot/internal/storage"
)

type favoriteItem struct {
	ID              string              `json:"id"`
	EffectiveStatus string              `json:"effective_status"`
	Item            *catalog.ResultItem `json:"item"`
}

func (a *authHandler) favorites(w http.ResponseWriter, r *http.Request) {
	p, revision, stored, ok := a.catalogContext(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	saved, err := a.state.GetFavorites(ctx, currentUser(r))
	if err != nil {
		writeStateError(w, err)
		return
	}
	now := a.now()
	items := make([]favoriteItem, 0, len(saved.IDs))
	byID := map[string]catalog.Opportunity{}
	for _, item := range stored.Items {
		if item.PublicationStatus == "published" && !item.IsDemo {
			byID[item.ID] = item
		}
	}
	for _, id := range saved.IDs {
		out := favoriteItem{ID: id, EffectiveStatus: "unavailable"}
		if opportunity, found := byID[id]; found {
			status, hint := catalog.EffectiveStatus(opportunity, now)
			score := catalog.Rank(p, opportunity)
			out.EffectiveStatus = status
			out.Item = &catalog.ResultItem{Opportunity: opportunity, EffectiveStatus: status, AvailabilityHint: hint, Reasons: score.Reasons}
		}
		items = append(items, out)
	}
	writeJSON(w, 200, struct {
		CatalogVersion    string         `json:"catalog_version"`
		ProfileRevision   int64          `json:"profile_revision"`
		FavoritesRevision int64          `json:"favorites_revision"`
		EvaluatedAt       time.Time      `json:"evaluated_at"`
		Items             []favoriteItem `json:"items"`
	}{stored.Version, revision, saved.Revision, now, items})
}

func (a *authHandler) putFavorite(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	requestID := w.Header().Get("X-Request-ID")
	if !catalog.ValidID(id) {
		writeError(w, 404, "NOT_FOUND", "Карточка не найдена", nil, requestID)
		return
	}
	_, _, stored, ok := a.catalogContext(w, r)
	if !ok {
		return
	}
	now := a.now()
	found := false
	for _, item := range stored.Items {
		if item.ID != id || item.IsDemo || item.PublicationStatus != "published" {
			continue
		}
		found = true
		status, _ := catalog.EffectiveStatus(item, now)
		if status != "active" {
			writeError(w, 409, "OPPORTUNITY_UNAVAILABLE", "Карточка недоступна для добавления", nil, requestID)
			return
		}
		break
	}
	if !found {
		writeError(w, 404, "NOT_FOUND", "Карточка не найдена", nil, requestID)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	if _, err := a.state.MutateFavorite(ctx, currentUser(r), id, true); err != nil {
		if errors.Is(err, storage.ErrFavoritesLimit) {
			writeError(w, 422, "FAVORITES_LIMIT", "Слишком много избранного", nil, requestID)
		} else {
			writeStateError(w, err)
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *authHandler) deleteFavorite(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	requestID := w.Header().Get("X-Request-ID")
	if !catalog.ValidID(id) {
		writeError(w, 404, "NOT_FOUND", "Карточка не найдена", nil, requestID)
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
	if _, err := a.state.MutateFavorite(ctx, currentUser(r), id, false); err != nil {
		writeStateError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
