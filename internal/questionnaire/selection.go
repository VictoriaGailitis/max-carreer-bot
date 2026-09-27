package questionnaire

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
)

type Selection struct {
	Directions []string          `json:"directions"`
	Profiles   map[string]string `json:"profiles,omitempty"`
}

type Preview struct {
	QuestionnaireVersion string            `json:"questionnaire_version"`
	SelectionVersion     int               `json:"selection_version"`
	Directions           []string          `json:"directions"`
	Profiles             map[string]string `json:"profiles,omitempty"`
	QuestionCount        int               `json:"question_count"`
	Questions            []PreviewQuestion `json:"questions"`
}

type PreviewQuestion struct {
	ID          string `json:"id"`
	DirectionID string `json:"direction_id"`
	Text        string `json:"text"`
}

type ValidationError struct {
	Fields map[string]string
}

func (e *ValidationError) Error() string { return "invalid questionnaire input" }

func invalid(field, message string) error {
	return &ValidationError{Fields: map[string]string{field: message}}
}

// Preview normalizes the selection and returns the exact deterministic question set.
func (b *Bank) Preview(selection Selection) (Preview, error) {
	if len(selection.Directions) < 1 || len(selection.Directions) > 3 {
		return Preview{}, invalid("directions", "Выберите от 1 до 3 направлений")
	}
	directions := append([]string(nil), selection.Directions...)
	profiles := make(map[string]string)
	for i, id := range directions {
		if id == "product" {
			if selection.Profiles["management"] != "" && selection.Profiles["management"] != "product" {
				return Preview{}, invalid("profiles.management", "Профиль не соответствует направлению")
			}
			directions[i] = "management"
			profiles["management"] = "product"
		} else if _, ok := b.directionByID[id]; !ok {
			return Preview{}, invalid("directions", "Неизвестное направление: "+id)
		}
	}
	seen := map[string]bool{}
	for _, id := range directions {
		if seen[id] {
			return Preview{}, invalid("directions", "Направления не должны повторяться")
		}
		seen[id] = true
	}
	for id, profileID := range selection.Profiles {
		if !seen[id] {
			return Preview{}, invalid("profiles."+id, "Профиль указан без направления")
		}
		profile, ok := b.profileByID[profileID]
		if !ok || profile.DirectionID != id {
			return Preview{}, invalid("profiles."+id, "Неизвестный профиль направления")
		}
		if current := profiles[id]; current != "" && current != profileID {
			return Preview{}, invalid("profiles."+id, "Профиль не соответствует направлению")
		}
		profiles[id] = profileID
	}
	for _, id := range directions {
		switch id {
		case "mobile":
			if profiles[id] == "" {
				profiles[id] = "mobile-all"
			}
		case "management":
			if profiles[id] == "" {
				profiles[id] = "product"
			}
		default:
			if profiles[id] != "" {
				return Preview{}, invalid("profiles."+id, "Для направления нет профилей")
			}
		}
	}
	order := map[string]int{}
	for i, d := range b.Directions {
		order[d.ID] = i
	}
	sort.Slice(directions, func(i, j int) bool { return order[directions[i]] < order[directions[j]] })

	target := []int{0, 8, 12, 16}[len(directions)]
	base, extra := target/len(directions), target%len(directions)
	buckets := make([][]string, len(directions))
	for i, id := range directions {
		pool, err := b.pool(id, profiles[id])
		if err != nil {
			return Preview{}, err
		}
		count := base
		if i < extra {
			count++
		}
		if len(pool) < count {
			return Preview{}, fmt.Errorf("bank has too few questions for %s", id)
		}
		buckets[i] = pool[:count]
	}
	questions := make([]PreviewQuestion, 0, target)
	for len(questions) < target {
		for i := range buckets {
			if len(buckets[i]) == 0 {
				continue
			}
			q := b.questionByID[buckets[i][0]]
			questions = append(questions, PreviewQuestion{ID: q.ID, DirectionID: q.DirectionID, Text: q.Text})
			buckets[i] = buckets[i][1:]
		}
	}
	return Preview{
		QuestionnaireVersion: b.QuestionnaireVersion,
		SelectionVersion:     SelectionVersion,
		Directions:           directions,
		Profiles:             profiles,
		QuestionCount:        target,
		Questions:            questions,
	}, nil
}

func (b *Bank) pool(directionID, profileID string) ([]string, error) {
	core := b.directionByID[directionID].CoreQuestionIDs
	if directionID == "mobile" && profileID != "mobile-all" {
		platform := b.profileByID[profileID].QuestionIDs
		pool := make([]string, 0, len(core)+len(platform))
		for i, id := range platform {
			pool = append(pool, id, core[i])
		}
		return append(pool, core[len(platform):]...), nil
	}
	if directionID == "management" {
		if profileID == "management-all" {
			project := b.profileByID["project"].QuestionIDs
			ba := b.profileByID["ba"].QuestionIDs
			product := b.profileByID["product"].QuestionIDs
			if len(project) < 8 || len(ba) < 8 || len(product) < 8 {
				return nil, fmt.Errorf("incomplete management profiles")
			}
			pool := make([]string, 8)
			for i := range pool {
				switch i % 3 {
				case 0:
					pool[i] = project[i]
				case 1:
					pool[i] = ba[i]
				default:
					pool[i] = product[i]
				}
			}
			return pool, nil
		}
		return b.profileByID[profileID].QuestionIDs, nil
	}
	return core, nil
}

// ValidateAnswers requires every issued ID, including explicit JSON null, and
// rejects IDs that were not issued. The caller should use the server's Preview.
func ValidateAnswers(preview Preview, answers map[string]json.RawMessage) error {
	if err := ValidatePartialAnswers(preview, answers); err != nil {
		return err
	}
	if answers == nil {
		return invalid("answers", "Ответы обязательны")
	}
	issued := make(map[string]bool, len(preview.Questions))
	for _, question := range preview.Questions {
		issued[question.ID] = true
	}
	for id := range answers {
		if !issued[id] {
			return invalid("answers."+id, "Вопрос не был выдан")
		}
	}
	for _, question := range preview.Questions {
		raw, present := answers[question.ID]
		if !present {
			return invalid("answers."+question.ID, "Ответ отсутствует")
		}
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			continue
		}
		var value int
		if err := json.Unmarshal(raw, &value); err != nil || value < 0 || value > 3 {
			return invalid("answers."+question.ID, "Допустимы 0, 1, 2, 3 или null")
		}
	}
	return nil
}

// ValidatePartialAnswers accepts an unfinished draft but rejects non-issued IDs
// and malformed answer values before they can be persisted.
func ValidatePartialAnswers(preview Preview, answers map[string]json.RawMessage) error {
	issued := make(map[string]bool, len(preview.Questions))
	for _, q := range preview.Questions {
		issued[q.ID] = true
	}
	for id, raw := range answers {
		if !issued[id] {
			return invalid("answers."+id, "Вопрос не был выдан")
		}
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			continue
		}
		var value int
		if err := json.Unmarshal(raw, &value); err != nil || value < 0 || value > 3 {
			return invalid("answers."+id, "Допустимы 0, 1, 2, 3 или null")
		}
	}
	return nil
}
