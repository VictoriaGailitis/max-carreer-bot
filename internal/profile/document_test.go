package profile

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"max-carreer-bot/internal/questionnaire"
)

func bankForTest(t *testing.T) *questionnaire.Bank {
	t.Helper()
	b, err := questionnaire.Load(filepath.Join("..", "..", "data", "question-bank.v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestDraftAndCompletion(t *testing.T) {
	b := bankForTest(t)
	draft, err := Normalize(b, Document{CurrentStep: 1}, nil)
	if err != nil || len(draft.QuestionIDs) != 0 {
		t.Fatalf("empty first step: %+v %v", draft, err)
	}
	draft, err = Normalize(b, Document{CurrentStep: 8, Directions: []string{"backend"}, Goals: []string{"explore"}, EducationStage: "undergraduate", PreferredFormat: "online", BackendLanguages: []string{"go"}}, &draft)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Complete(b, draft, time.Now()); err == nil {
		t.Fatal("accepted missing answers")
	}
	draft.Skills = map[string]json.RawMessage{}
	for _, id := range draft.QuestionIDs {
		draft.Skills[id] = json.RawMessage("0")
	}
	draft.Skills[draft.QuestionIDs[0]] = json.RawMessage("null")
	completed, err := Complete(b, draft, time.Now())
	if err != nil || completed.CompletedAt == nil {
		t.Fatalf("complete: %v", err)
	}
	draft.Goals = []string{"explore", "course"}
	if _, err := Complete(b, draft, time.Now()); err == nil {
		t.Fatal("accepted exclusive goal with another goal")
	}
}

func TestDirectionChangePrunesOldAnswers(t *testing.T) {
	b := bankForTest(t)
	old, err := Normalize(b, Document{CurrentStep: 2, Directions: []string{"backend"}, BackendLanguages: []string{"go"}, Skills: map[string]json.RawMessage{"BACK-01": json.RawMessage("3")}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := Normalize(b, Document{CurrentStep: 2, Directions: []string{"frontend"}, BackendLanguages: []string{"go"}, Skills: map[string]json.RawMessage{"BACK-01": json.RawMessage("3"), "FRONT-01": json.RawMessage("1")}}, &old)
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Skills) != 1 || len(updated.BackendLanguages) != 0 {
		t.Fatalf("stale fields retained: %+v", updated)
	}
	if _, err := Normalize(b, Document{CurrentStep: 2, Directions: []string{"frontend"}, Skills: map[string]json.RawMessage{"OTHER-01": json.RawMessage("0")}}, &old); err == nil {
		t.Fatal("accepted invented question")
	}
}
