package assessment

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"max-carreer-bot/internal/profile"
	"max-carreer-bot/internal/questionnaire"
)

func fixture(t *testing.T, selection questionnaire.Selection, answers []string) (*questionnaire.Bank, profile.Document) {
	t.Helper()
	b, err := questionnaire.Load("../../data/question-bank.v1.json")
	if err != nil {
		t.Fatal(err)
	}
	preview, err := b.Preview(selection)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1800000000, 0).UTC()
	p := profile.Document{QuestionnaireVersion: b.QuestionnaireVersion, SelectionVersion: questionnaire.SelectionVersion, Directions: preview.Directions, Profiles: preview.Profiles, CompletedAt: &now, Skills: map[string]json.RawMessage{}}
	for i, q := range preview.Questions {
		p.QuestionIDs = append(p.QuestionIDs, q.ID)
		p.Skills[q.ID] = json.RawMessage(answers[i%len(answers)])
	}
	return b, p
}

func TestScoringAndCoverage(t *testing.T) {
	for _, tt := range []struct {
		name                      string
		answers                   []string
		score, assessed, coverage int
		level, basis              string
	}{
		{"zero", []string{"0"}, 0, 8, 100, "starter", "self_reported"},
		{"one", []string{"1"}, 33, 8, 100, "developing", "self_reported"},
		{"two", []string{"2"}, 67, 8, 100, "solid", "self_reported"},
		{"three", []string{"3"}, 100, 8, 100, "confident", "self_reported"},
		{"partial null", []string{"null", "3"}, 100, 4, 50, "confident", "self_reported"},
		{"all null", []string{"null"}, 0, 0, 0, "starter", "no_self_assessment"},
		{"mixed", []string{"0", "1", "2", "3"}, 50, 8, 100, "solid", "self_reported"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			b, p := fixture(t, questionnaire.Selection{Directions: []string{"backend"}}, tt.answers)
			r, err := Build(b, p, 7, *p.CompletedAt)
			if err != nil {
				t.Fatal(err)
			}
			d := r.Directions[0]
			if d.ScorePercent != tt.score || d.AssessedCount != tt.assessed || d.CoveragePercent != tt.coverage || d.Level != tt.level || d.Basis != tt.basis {
				t.Fatalf("unexpected: %+v", d)
			}
			if len(d.UnassessedSkillIDs) != 8-tt.assessed || d.QuestionCount != 8 || r.ProfileRevision != 7 || r.Kind != "self_assessment" || r.Disclaimer == "" {
				t.Fatalf("metadata: %+v", r)
			}
			if d.CourseFilter.Type != "course" || d.CourseFilter.Direction != "backend" {
				t.Fatal(d.CourseFilter)
			}
			for _, s := range d.Skills {
				if s.AnswerLabel == "" {
					t.Fatal("missing scale label")
				}
			}
			for _, s := range d.FocusSkills {
				if s.Answer == nil || *s.Answer == 3 {
					t.Fatal("unassessed/max answer in focus")
				}
			}
			if len(d.FocusSkills) > 3 {
				t.Fatal("too many focus skills")
			}
		})
	}
}

func TestLevelBoundaries(t *testing.T) {
	for _, tt := range []struct {
		score int
		want  string
	}{{0, "starter"}, {24, "starter"}, {25, "developing"}, {49, "developing"}, {50, "solid"}, {74, "solid"}, {75, "confident"}, {100, "confident"}} {
		got, _, _ := level(tt.score)
		if got != tt.want {
			t.Errorf("%d: %s", tt.score, got)
		}
	}
}

func TestAllDirectionsAndProfiles(t *testing.T) {
	for _, selection := range []questionnaire.Selection{
		{Directions: []string{"backend", "frontend", "devops"}},
		{Directions: []string{"ml", "analytics", "security"}},
		{Directions: []string{"mobile", "management"}, Profiles: map[string]string{"mobile": "ios", "management": "ba"}},
		{Directions: []string{"mobile", "management"}, Profiles: map[string]string{"mobile": "android", "management": "management-all"}},
		{Directions: []string{"mobile"}, Profiles: map[string]string{"mobile": "flutter"}},
	} {
		b, p := fixture(t, selection, []string{"3"})
		r, err := Build(b, p, 1, *p.CompletedAt)
		if err != nil {
			t.Fatal(err)
		}
		count := 0
		for _, d := range r.Directions {
			if d.ScorePercent != 100 || d.CoveragePercent != 100 {
				t.Fatal(d)
			}
			count += d.QuestionCount
		}
		if count != len(p.QuestionIDs) || len(r.Directions) != len(selection.Directions) {
			t.Fatal("wrong issued set")
		}
	}
}

func TestFocusIsStableAndPerDirection(t *testing.T) {
	b, p := fixture(t, questionnaire.Selection{Directions: []string{"backend", "frontend"}}, []string{"0", "3"})
	r, err := Build(b, p, 1, *p.CompletedAt)
	if err != nil {
		t.Fatal(err)
	}
	if r.Directions[0].ScorePercent != 0 || r.Directions[1].ScorePercent != 100 || len(r.Directions[1].FocusSkills) != 0 {
		t.Fatal(r)
	}
	for i, s := range r.Directions[0].FocusSkills {
		if s.QuestionID != p.QuestionIDs[i*2] {
			t.Fatal("unstable focus order")
		}
	}
}

func TestRejectsOutdatedAndCorruptProfiles(t *testing.T) {
	for _, tt := range []struct {
		name   string
		mutate func(*profile.Document)
		want   error
	}{
		{"version", func(p *profile.Document) { p.QuestionnaireVersion = "old" }, ErrOutdated},
		{"selection version", func(p *profile.Document) { p.SelectionVersion = 0 }, ErrOutdated},
		{"issued set", func(p *profile.Document) { p.QuestionIDs = p.QuestionIDs[1:] }, ErrOutdated},
		{"missing", func(p *profile.Document) { delete(p.Skills, p.QuestionIDs[0]) }, ErrInvalidProfile},
		{"extra", func(p *profile.Document) { p.Skills["INVENTED"] = json.RawMessage("3") }, ErrInvalidProfile},
		{"invalid", func(p *profile.Document) { p.Skills[p.QuestionIDs[0]] = json.RawMessage("4") }, ErrInvalidProfile},
		{"draft", func(p *profile.Document) { p.CompletedAt = nil }, ErrInvalidProfile},
	} {
		t.Run(tt.name, func(t *testing.T) {
			b, p := fixture(t, questionnaire.Selection{Directions: []string{"backend"}}, []string{"2"})
			tt.mutate(&p)
			_, err := Build(b, p, 1, time.Now())
			if !errors.Is(err, tt.want) {
				t.Fatal(err)
			}
		})
	}
}
