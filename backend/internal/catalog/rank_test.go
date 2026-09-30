package catalog

import (
	"encoding/json"
	"testing"
	"time"

	"max-carreer-bot/internal/profile"
)

func TestRankAndFiltering(t *testing.T) {
	v, raw := testValidator(t)
	validated, err := v.Validate(raw)
	if err != nil {
		t.Fatal(err)
	}
	items := validated.Dataset.Items
	for i := range items {
		items[i].IsDemo = false
	}
	p := profile.Document{Directions: []string{"backend"}, Goals: []string{"course"}, EducationStage: "undergraduate", PreferredFormat: "online", Skills: map[string]json.RawMessage{"BACK-01": json.RawMessage("0")}}
	if got := Rank(p, items[0]); !got.DirectionMatch || !got.GoalMatch || got.ConditionsRank != 2 || got.ExperienceRank != 2 {
		t.Fatalf("course score: %+v", got)
	}
	if got := Rank(p, items[1]); got.DirectionMatch || got.ConditionsRank != 0 || got.ExperienceRank != 1 {
		t.Fatalf("hybrid score: %+v", got)
	}
	when := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	result := BuildResult(Stored{Version: "v", Items: items}, p, 7, when, Filters{})
	if len(result.Items) != 3 || result.Items[0].ID != "demo-course-backend" || !result.HasDirectionMatches || result.ProfileRevision != 7 {
		t.Fatalf("unexpected ranking: %+v", result)
	}
	filtered := BuildResult(Stored{Version: "v", Items: items}, p, 7, when, Filters{Type: "event"})
	if len(filtered.Items) != 1 || filtered.Items[0].Type != "event" {
		t.Fatalf("filter: %+v", filtered)
	}
	if got := BuildResult(Stored{Version: "v", Items: items}, p, 7, when, Filters{Direction: "devops"}); len(got.Items) != 0 {
		t.Fatalf("empty filter: %+v", got)
	}
}

func TestDeadlineAndUnknownDoNotMaskMismatch(t *testing.T) {
	v, raw := testValidator(t)
	dataset, err := v.Validate(raw)
	if err != nil {
		t.Fatal(err)
	}
	o := dataset.Dataset.Items[1]
	p := profile.Document{Directions: []string{"analytics"}, PreferredFormat: "both", PreferredCities: []string{"Казань"}, EducationStage: "prefer_not_to_say", Skills: map[string]json.RawMessage{"DATA-01": json.RawMessage("null")}}
	if got := Rank(p, o); got.ConditionsRank != 0 || got.ExperienceRank != 1 {
		t.Fatalf("known city mismatch masked: %+v", got)
	}
	p.PreferredCities = []string{"Москва"}
	if got := Rank(p, o); got.ConditionsRank != 1 {
		t.Fatalf("unknown education: %+v", got)
	}
	p.Skills["DATA-01"] = json.RawMessage("0")
	if got := Rank(p, o); got.ExperienceRank != 0 {
		t.Fatalf("known skill mismatch: %+v", got)
	}
	deadline := *o.DeadlineAt
	if status, _ := EffectiveStatus(o, deadline); status != "active" {
		t.Fatalf("deadline boundary: %s", status)
	}
	if status, _ := EffectiveStatus(o, deadline.Add(time.Nanosecond)); status != "closed" {
		t.Fatalf("expired deadline: %s", status)
	}
	o = dataset.Dataset.Items[2]
	if status, _ := EffectiveStatus(o, o.EndsAt.Add(time.Nanosecond)); status != "closed" {
		t.Fatalf("event ended: %s", status)
	}
}
