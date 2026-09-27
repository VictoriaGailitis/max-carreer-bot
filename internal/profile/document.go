package profile

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"max-carreer-bot/internal/questionnaire"
)

const SchemaVersion = 1

type Document struct {
	SchemaVersion        int                        `json:"schema_version"`
	QuestionnaireVersion string                     `json:"questionnaire_version"`
	SelectionVersion     int                        `json:"selection_version"`
	Directions           []string                   `json:"directions"`
	Profiles             map[string]string          `json:"profiles,omitempty"`
	QuestionIDs          []string                   `json:"question_ids"`
	Goals                []string                   `json:"goals,omitempty"`
	EducationStage       string                     `json:"education_stage,omitempty"`
	StudyYear            *int                       `json:"study_year,omitempty"`
	PreferredFormat      string                     `json:"preferred_format,omitempty"`
	PreferredCities      []string                   `json:"preferred_cities,omitempty"`
	AnyCity              bool                       `json:"any_city,omitempty"`
	OtherCity            string                     `json:"other_city,omitempty"`
	BackendLanguages     []string                   `json:"backend_languages,omitempty"`
	Skills               map[string]json.RawMessage `json:"skills,omitempty"`
	CurrentStep          int                        `json:"current_step,omitempty"`
	BaseProfileRevision  int64                      `json:"base_profile_revision,omitempty"`
	CompletedAt          *time.Time                 `json:"completed_at,omitempty"`
}

type ValidationError struct{ Fields map[string]string }

func (e *ValidationError) Error() string  { return "invalid profile" }
func invalid(field, message string) error { return &ValidationError{map[string]string{field: message}} }

// Normalize verifies client-editable fields, derives the issued question set,
// and discards stale answers only when they belonged to the previous draft.
func Normalize(bank *questionnaire.Bank, draft Document, previous *Document) (Document, error) {
	if draft.CurrentStep < 1 || draft.CurrentStep > 64 {
		return Document{}, invalid("current_step", "Недопустимый шаг")
	}
	if len(draft.Directions) == 0 && draft.CurrentStep == 1 {
		if len(draft.Skills) > 0 || len(draft.Profiles) > 0 {
			return Document{}, invalid("directions", "Сначала выберите направление")
		}
		draft.SchemaVersion = SchemaVersion
		draft.QuestionnaireVersion = bank.QuestionnaireVersion
		draft.SelectionVersion = questionnaire.SelectionVersion
		draft.QuestionIDs = []string{}
		draft.CompletedAt = nil
		if err := validateCommon(draft, false); err != nil {
			return Document{}, err
		}
		return draft, nil
	}
	preview, err := bank.Preview(questionnaire.Selection{Directions: draft.Directions, Profiles: draft.Profiles})
	if err != nil {
		return Document{}, err
	}
	draft.SchemaVersion = SchemaVersion
	draft.QuestionnaireVersion = preview.QuestionnaireVersion
	draft.SelectionVersion = preview.SelectionVersion
	draft.Directions = preview.Directions
	draft.Profiles = preview.Profiles
	draft.QuestionIDs = make([]string, len(preview.Questions))
	allowed := make(map[string]bool, len(preview.Questions))
	for i, q := range preview.Questions {
		draft.QuestionIDs[i] = q.ID
		allowed[q.ID] = true
	}
	old := map[string]bool{}
	if previous != nil {
		for _, id := range previous.QuestionIDs {
			old[id] = true
		}
	}
	for id := range draft.Skills {
		if !allowed[id] && old[id] {
			delete(draft.Skills, id)
		}
	}
	if previous != nil && slices.Contains(previous.Directions, "backend") && !slices.Contains(draft.Directions, "backend") {
		draft.BackendLanguages = nil
	}
	if err := questionnaire.ValidatePartialAnswers(preview, draft.Skills); err != nil {
		return Document{}, err
	}
	if err := validateCommon(draft, false); err != nil {
		return Document{}, err
	}
	draft.CompletedAt = nil
	return draft, nil
}

func Complete(bank *questionnaire.Bank, draft Document, now time.Time) (Document, error) {
	if draft.SchemaVersion != SchemaVersion || draft.QuestionnaireVersion != bank.QuestionnaireVersion || draft.SelectionVersion != questionnaire.SelectionVersion {
		return Document{}, invalid("questionnaire_version", "Версия опросника изменилась")
	}
	preview, err := bank.Preview(questionnaire.Selection{Directions: draft.Directions, Profiles: draft.Profiles})
	if err != nil {
		return Document{}, err
	}
	if len(preview.Questions) != len(draft.QuestionIDs) {
		return Document{}, invalid("question_ids", "Список вопросов изменился")
	}
	for i, q := range preview.Questions {
		if draft.QuestionIDs[i] != q.ID {
			return Document{}, invalid("question_ids", "Список вопросов изменился")
		}
	}
	if err := validateCommon(draft, true); err != nil {
		return Document{}, err
	}
	if err := questionnaire.ValidateAnswers(preview, draft.Skills); err != nil {
		return Document{}, err
	}
	draft.CurrentStep = 0
	draft.BaseProfileRevision = 0
	draft.CompletedAt = &now
	return draft, nil
}

func validateCommon(d Document, complete bool) error {
	if err := oneOfMany("goals", d.Goals, []string{"course", "internship", "event", "explore"}, 4); err != nil {
		return err
	}
	if slices.Contains(d.Goals, "explore") && len(d.Goals) != 1 {
		return invalid("goals", "Изучение возможностей выбирается отдельно")
	}
	if complete && len(d.Goals) == 0 {
		return invalid("goals", "Выберите цель")
	}
	if d.EducationStage != "" && !slices.Contains([]string{"vocational", "undergraduate", "graduate", "alumni", "other", "prefer_not_to_say"}, d.EducationStage) {
		return invalid("education_stage", "Неизвестный этап обучения")
	}
	if complete && d.EducationStage == "" {
		return invalid("education_stage", "Укажите этап обучения")
	}
	if d.StudyYear != nil && (*d.StudyYear < 1 || *d.StudyYear > 6 || !slices.Contains([]string{"vocational", "undergraduate", "graduate"}, d.EducationStage)) {
		return invalid("study_year", "Курс допустим только для учащихся: 1–6")
	}
	if d.PreferredFormat != "" && !slices.Contains([]string{"online", "offline", "both"}, d.PreferredFormat) {
		return invalid("preferred_format", "Неизвестный формат")
	}
	if complete && d.PreferredFormat == "" {
		return invalid("preferred_format", "Выберите формат")
	}
	if d.PreferredFormat == "online" && (d.AnyCity || len(d.PreferredCities) > 0 || d.OtherCity != "") {
		return invalid("preferred_cities", "Города не нужны для онлайн-формата")
	}
	if d.AnyCity && (len(d.PreferredCities) > 0 || d.OtherCity != "") {
		return invalid("any_city", "Любой город выбирается отдельно")
	}
	if err := uniqueStrings("preferred_cities", d.PreferredCities, 20, 80); err != nil {
		return err
	}
	if strings.TrimSpace(d.OtherCity) != d.OtherCity || len(d.OtherCity) > 80 {
		return invalid("other_city", "Некорректный город")
	}
	if complete && d.PreferredFormat != "online" && !d.AnyCity && len(d.PreferredCities) == 0 && d.OtherCity == "" {
		return invalid("preferred_cities", "Укажите город или любой город")
	}
	if err := oneOfMany("backend_languages", d.BackendLanguages, []string{"go", "python", "java", "javascript", "typescript", "csharp", "cpp", "php", "ruby", "rust", "kotlin", "other", "none"}, 13); err != nil {
		return err
	}
	if slices.Contains(d.BackendLanguages, "none") && len(d.BackendLanguages) != 1 {
		return invalid("backend_languages", "Нет опыта выбирается отдельно")
	}
	if !slices.Contains(d.Directions, "backend") && len(d.BackendLanguages) > 0 {
		return invalid("backend_languages", "Языки доступны только для бэкенда")
	}
	return nil
}

func oneOfMany(field string, values, permitted []string, max int) error {
	if len(values) > max {
		return invalid(field, "Слишком много значений")
	}
	for i, v := range values {
		if !slices.Contains(permitted, v) || slices.Contains(values[:i], v) {
			return invalid(field, "Неизвестное или повторяющееся значение")
		}
	}
	return nil
}
func uniqueStrings(field string, values []string, max, maxLen int) error {
	if len(values) > max {
		return invalid(field, "Слишком много значений")
	}
	for i, v := range values {
		if v == "" || len(v) > maxLen || strings.TrimSpace(v) != v || slices.Contains(values[:i], v) {
			return invalid(field, "Некорректное или повторяющееся значение")
		}
	}
	return nil
}

var ErrRevisionConflict = errors.New("revision conflict")
