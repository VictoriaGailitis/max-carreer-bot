package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"max-carreer-bot/internal/assessment"
	"max-carreer-bot/internal/auth"
	"max-carreer-bot/internal/profile"
	"max-carreer-bot/internal/questionnaire"
	"max-carreer-bot/internal/storage"
)

type resultStateRepository struct {
	UserStateRepository // Other methods must never be called by a read-only results request.
	state               storage.State
	err                 error
	userID              string
}

func (s *resultStateRepository) Get(_ context.Context, userID string) (storage.State, error) {
	s.userID = userID
	return s.state, s.err
}

func TestResultsHTTPWithoutCatalog(t *testing.T) {
	bank, err := questionnaire.Load("../../data/question-bank.v1.json")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1800000000, 0).UTC()
	preview, err := bank.Preview(questionnaire.Selection{Directions: []string{"backend"}})
	if err != nil {
		t.Fatal(err)
	}
	p := profile.Document{QuestionnaireVersion: bank.QuestionnaireVersion, SelectionVersion: 1, Directions: preview.Directions, CompletedAt: &now, Skills: map[string]json.RawMessage{}}
	for _, q := range preview.Questions {
		p.QuestionIDs = append(p.QuestionIDs, q.ID)
		p.Skills[q.ID] = json.RawMessage("null")
	}
	state := &resultStateRepository{state: storage.State{Profile: &p, ProfileRevision: 8}}
	validator, err := auth.NewValidator("test-bot-token")
	if err != nil {
		t.Fatal(err)
	}
	session, err := auth.IssueSession(now)
	if err != nil {
		t.Fatal(err)
	}
	sessions := &fakeSessionRepository{sessions: map[[32]byte]time.Time{session.TokenHash: session.ExpiresAt}}
	handler, err := NewAPIHandler(validator, sessions, state, bank, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	request := func(path, token string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", path, nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		return res
	}
	if res := request("/api/v1/me/results", ""); res.Code != 401 {
		t.Fatalf("anonymous: %d", res.Code)
	}
	if state.userID != "" {
		t.Fatal("anonymous request read profile")
	}
	res := request("/api/v1/me/results", session.Token)
	if res.Code != 200 {
		t.Fatalf("%d %s", res.Code, res.Body.String())
	}
	if res.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("results must not be cached")
	}
	var result assessment.Result
	if err := json.Unmarshal(res.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.ProfileRevision != 8 || result.Directions[0].Level != "starter" || result.Directions[0].Basis != "no_self_assessment" || result.Directions[0].CoveragePercent != 0 {
		t.Fatal(result)
	}
	if state.userID != "6d305789-d179-4c87-9d96-695dd88891f1" {
		t.Fatal("did not use session identity")
	}
	if res := request("/api/v1/me/results?user_id=someone", session.Token); res.Code != 400 {
		t.Fatal("accepted caller identity")
	}
	// A draft does not replace the completed profile's results.
	draft := p
	draft.Skills = map[string]json.RawMessage{}
	for _, id := range p.QuestionIDs {
		draft.Skills[id] = json.RawMessage("3")
	}
	state.state.Draft = &draft
	res = request("/api/v1/me/results", session.Token)
	if err := json.Unmarshal(res.Body.Bytes(), &result); err != nil || result.Directions[0].ScorePercent != 0 {
		t.Fatal("draft leaked into results")
	}
	// Completing another revision updates results without persisted score columns.
	state.state.Profile = &draft
	state.state.ProfileRevision = 9
	res = request("/api/v1/me/results", session.Token)
	if err := json.Unmarshal(res.Body.Bytes(), &result); err != nil || result.ProfileRevision != 9 || result.Directions[0].ScorePercent != 100 {
		t.Fatal("results not updated")
	}
	state.state.Profile = nil
	if res := request("/api/v1/me/results", session.Token); res.Code != 409 || !strings.Contains(res.Body.String(), "PROFILE_REQUIRED") {
		t.Fatal(res.Body.String())
	}
	state.state.Profile = &p
	p.QuestionnaireVersion = "old"
	if res := request("/api/v1/me/results", session.Token); res.Code != 409 || !strings.Contains(res.Body.String(), "PROFILE_OUTDATED") {
		t.Fatal(res.Body.String())
	}
	p.QuestionnaireVersion = bank.QuestionnaireVersion
	delete(p.Skills, p.QuestionIDs[0])
	if res := request("/api/v1/me/results", session.Token); res.Code != 503 || !strings.Contains(res.Body.String(), "RESULTS_UNAVAILABLE") {
		t.Fatal(res.Body.String())
	}
	state.err = errors.New("offline")
	if res := request("/api/v1/me/results", session.Token); res.Code != 503 {
		t.Fatal(res.Code)
	}
}
