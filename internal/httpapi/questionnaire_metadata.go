package httpapi

import (
	"net/http"

	"max-carreer-bot/internal/questionnaire"
)

func (a *authHandler) questionnaire(w http.ResponseWriter, r *http.Request) {
	type generalQuestion struct {
		ID   string `json:"id"`
		Text string `json:"text"`
	}
	writeJSON(w, http.StatusOK, struct {
		QuestionnaireVersion string                       `json:"questionnaire_version"`
		SelectionVersion     int                          `json:"selection_version"`
		Directions           []questionnaire.Direction    `json:"directions"`
		Profiles             []questionnaire.Profile      `json:"profiles"`
		AnswerScale          []questionnaire.AnswerOption `json:"answer_scale"`
		Goals                []string                     `json:"goals"`
		EducationStages      []string                     `json:"education_stages"`
		PreferredFormats     []string                     `json:"preferred_formats"`
		BackendLanguages     []string                     `json:"backend_languages"`
		GeneralQuestions     []generalQuestion            `json:"general_questions"`
	}{
		a.bank.QuestionnaireVersion, questionnaire.SelectionVersion, a.bank.Directions,
		a.bank.Profiles, a.bank.AnswerScale,
		[]string{"course", "internship", "event", "explore"},
		[]string{"vocational", "undergraduate", "graduate", "alumni", "other", "prefer_not_to_say"},
		[]string{"online", "offline", "both"},
		[]string{"go", "python", "java", "javascript", "typescript", "csharp", "cpp", "php", "ruby", "rust", "kotlin", "other", "none"},
		[]generalQuestion{
			{"Q1", "Какие направления тебе интересны?"},
			{"Q2", "Что ты ищешь сейчас?"},
			{"Q3", "На каком этапе обучения ты сейчас?"},
			{"Q4", "Какой формат тебе подходит?"},
			{"Q5", "Где ты можешь участвовать очно?"},
		},
	})
}
