package questionnaire

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
)

const SelectionVersion = 1

type AnswerOption struct {
	Value *int   `json:"value"`
	Label string `json:"label"`
}

type Direction struct {
	ID              string   `json:"id"`
	Title           string   `json:"title"`
	CoreQuestionIDs []string `json:"core_question_ids"`
}

type Profile struct {
	ID            string   `json:"id"`
	Title         string   `json:"title"`
	DirectionID   string   `json:"direction_id"`
	QuestionIDs   []string `json:"question_ids,omitempty"`
	SelectionRule string   `json:"selection_rule,omitempty"`
}

type Question struct {
	ID          string `json:"id"`
	DirectionID string `json:"direction_id"`
	Text        string `json:"text"`
	ProfileID   string `json:"profile_id,omitempty"`
	Kind        string `json:"kind,omitempty"`
}

type Bank struct {
	SchemaVersion        int            `json:"schema_version"`
	QuestionnaireVersion string         `json:"questionnaire_version"`
	AnswerScale          []AnswerOption `json:"answer_scale"`
	Directions           []Direction    `json:"directions"`
	Profiles             []Profile      `json:"profiles"`
	Questions            []Question     `json:"questions"`

	directionByID map[string]Direction
	profileByID   map[string]Profile
	questionByID  map[string]Question
}

func Load(path string) (*Bank, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() > 2<<20 {
		return nil, errors.New("bank exceeds 2 MiB")
	}
	var bank Bank
	decoder := json.NewDecoder(io.LimitReader(f, 2<<20))
	if err := decoder.Decode(&bank); err != nil {
		return nil, fmt.Errorf("decode bank: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, errors.New("bank has trailing JSON")
	}
	if err := bank.validate(); err != nil {
		return nil, err
	}
	return &bank, nil
}

func (b *Bank) validate() error {
	if b.SchemaVersion != 1 || b.QuestionnaireVersion == "" {
		return errors.New("unsupported bank version")
	}
	b.directionByID = make(map[string]Direction, len(b.Directions))
	b.profileByID = make(map[string]Profile, len(b.Profiles))
	b.questionByID = make(map[string]Question, len(b.Questions))
	for _, d := range b.Directions {
		if d.ID == "" || d.Title == "" || len(d.CoreQuestionIDs) == 0 {
			return errors.New("invalid direction")
		}
		if _, exists := b.directionByID[d.ID]; exists {
			return fmt.Errorf("duplicate direction %s", d.ID)
		}
		b.directionByID[d.ID] = d
	}
	for _, p := range b.Profiles {
		if _, exists := b.directionByID[p.DirectionID]; !exists {
			return fmt.Errorf("profile %s has unknown direction", p.ID)
		}
		if p.ID == "" {
			return errors.New("empty profile ID")
		}
		if _, exists := b.profileByID[p.ID]; exists {
			return fmt.Errorf("duplicate profile %s", p.ID)
		}
		b.profileByID[p.ID] = p
	}
	for _, q := range b.Questions {
		if q.ID == "" || q.Text == "" || q.Kind != "self_assessment" {
			return fmt.Errorf("invalid question %s", q.ID)
		}
		if _, exists := b.directionByID[q.DirectionID]; !exists {
			return fmt.Errorf("question %s has unknown direction", q.ID)
		}
		if q.ProfileID != "" {
			p, exists := b.profileByID[q.ProfileID]
			if !exists || p.DirectionID != q.DirectionID {
				return fmt.Errorf("question %s has invalid profile", q.ID)
			}
		}
		if _, exists := b.questionByID[q.ID]; exists {
			return fmt.Errorf("duplicate question %s", q.ID)
		}
		b.questionByID[q.ID] = q
	}
	for _, d := range b.Directions {
		if err := b.checkIDs(d.ID, d.CoreQuestionIDs); err != nil {
			return err
		}
	}
	for _, p := range b.Profiles {
		if p.SelectionRule == "legacy_mixed_management" {
			if p.ID != "management-all" || p.DirectionID != "management" {
				return fmt.Errorf("invalid selection rule for %s", p.ID)
			}
			continue
		}
		if p.SelectionRule != "" || len(p.QuestionIDs) == 0 {
			return fmt.Errorf("invalid profile %s", p.ID)
		}
		if err := b.checkIDs(p.DirectionID, p.QuestionIDs); err != nil {
			return err
		}
		if p.DirectionID == "mobile" && p.ID != "mobile-all" && len(p.QuestionIDs) > len(b.directionByID["mobile"].CoreQuestionIDs) {
			return fmt.Errorf("mobile profile %s has too many questions", p.ID)
		}
	}
	if len(b.AnswerScale) != 5 {
		return errors.New("invalid answer scale")
	}
	seen := map[int]bool{}
	hasNull := false
	for _, option := range b.AnswerScale {
		if option.Label == "" {
			return errors.New("empty answer label")
		}
		if option.Value == nil {
			if hasNull {
				return errors.New("duplicate null answer")
			}
			hasNull = true
			continue
		}
		if *option.Value < 0 || *option.Value > 3 || seen[*option.Value] {
			return errors.New("invalid answer scale value")
		}
		seen[*option.Value] = true
	}
	if !hasNull || len(seen) != 4 {
		return errors.New("incomplete answer scale")
	}
	return nil
}

func (b *Bank) checkIDs(direction string, ids []string) error {
	seen := map[string]bool{}
	for _, id := range ids {
		q, exists := b.questionByID[id]
		if !exists || q.DirectionID != direction || seen[id] {
			return fmt.Errorf("invalid question reference %s for %s", id, direction)
		}
		seen[id] = true
	}
	return nil
}
