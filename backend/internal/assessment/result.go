// Package assessment describes self-reported experience, not tested competence.
package assessment

import (
	"encoding/json"
	"errors"
	"math"
	"slices"
	"sort"
	"time"

	"max-carreer-bot/internal/profile"
	"max-carreer-bot/internal/questionnaire"
)

const ScoringVersion = 1
const Disclaimer = "Результаты основаны на твоих ответах, а не на проверке знаний. Это ориентир для выбора следующего шага, а не оценка профессиональной квалификации."

var ErrOutdated = errors.New("assessment profile is outdated")
var ErrInvalidProfile = errors.New("assessment requires a complete valid profile")

type Skill struct {
	QuestionID  string `json:"question_id"`
	Text        string `json:"text"`
	Answer      *int   `json:"answer"`
	AnswerLabel string `json:"answer_label"`
}

type CourseFilter struct {
	Type      string `json:"type"`
	Direction string `json:"direction"`
}

type DirectionResult struct {
	DirectionID        string       `json:"direction_id"`
	Title              string       `json:"title"`
	ScorePercent       int          `json:"score_percent"`
	Level              string       `json:"level"`
	LevelLabel         string       `json:"level_label"`
	Description        string       `json:"description"`
	Basis              string       `json:"basis"`
	AssessedCount      int          `json:"assessed_count"`
	QuestionCount      int          `json:"question_count"`
	CoveragePercent    int          `json:"coverage_percent"`
	Skills             []Skill      `json:"skills"`
	FocusSkills        []Skill      `json:"focus_skills"`
	UnassessedSkillIDs []string     `json:"unassessed_skill_ids"`
	CourseFilter       CourseFilter `json:"course_filter"`
}

type Result struct {
	Kind                 string            `json:"kind"`
	ScoringVersion       int               `json:"scoring_version"`
	QuestionnaireVersion string            `json:"questionnaire_version"`
	ProfileRevision      int64             `json:"profile_revision"`
	CompletedAt          time.Time         `json:"completed_at"`
	EvaluatedAt          time.Time         `json:"evaluated_at"`
	Disclaimer           string            `json:"disclaimer"`
	Directions           []DirectionResult `json:"directions"`
}

// Build uses only the active completed profile. Drafts and the live catalog do
// not affect results. Null answers are excluded; all-null defaults to starter
// with explicit zero coverage, rather than claiming measured lack of skill.
func Build(bank *questionnaire.Bank, p profile.Document, revision int64, now time.Time) (Result, error) {
	if p.QuestionnaireVersion != bank.QuestionnaireVersion || p.SelectionVersion != questionnaire.SelectionVersion {
		return Result{}, ErrOutdated
	}
	if p.CompletedAt == nil {
		return Result{}, ErrInvalidProfile
	}
	preview, err := bank.Preview(questionnaire.Selection{Directions: p.Directions, Profiles: p.Profiles})
	if err != nil {
		return Result{}, ErrInvalidProfile
	}
	ids := make([]string, len(preview.Questions))
	for i, q := range preview.Questions {
		ids[i] = q.ID
	}
	if !slices.Equal(ids, p.QuestionIDs) {
		return Result{}, ErrOutdated
	}
	if err := questionnaire.ValidateAnswers(preview, p.Skills); err != nil {
		return Result{}, ErrInvalidProfile
	}

	result := Result{Kind: "self_assessment", ScoringVersion: ScoringVersion, QuestionnaireVersion: bank.QuestionnaireVersion, ProfileRevision: revision, CompletedAt: *p.CompletedAt, EvaluatedAt: now, Disclaimer: Disclaimer, Directions: make([]DirectionResult, 0, len(preview.Directions))}
	titles := map[string]string{}
	for _, d := range bank.Directions {
		titles[d.ID] = d.Title
	}
	for _, id := range preview.Directions {
		d := DirectionResult{DirectionID: id, Title: titles[id], Basis: "self_reported", Skills: []Skill{}, FocusSkills: []Skill{}, UnassessedSkillIDs: []string{}, CourseFilter: CourseFilter{Type: "course", Direction: id}}
		sum := 0
		for _, q := range preview.Questions {
			if q.DirectionID != id {
				continue
			}
			var value *int
			if err := json.Unmarshal(p.Skills[q.ID], &value); err != nil {
				return Result{}, ErrInvalidProfile
			}
			s := Skill{QuestionID: q.ID, Text: q.Text, Answer: value}
			for _, option := range bank.AnswerScale {
				if value == nil && option.Value == nil || value != nil && option.Value != nil && *value == *option.Value {
					s.AnswerLabel = option.Label
					break
				}
			}
			d.Skills = append(d.Skills, s)
			d.QuestionCount++
			if value == nil {
				d.UnassessedSkillIDs = append(d.UnassessedSkillIDs, q.ID)
				continue
			}
			d.AssessedCount++
			sum += *value
			if *value < 3 {
				d.FocusSkills = append(d.FocusSkills, s)
			}
		}
		if d.AssessedCount > 0 {
			d.ScorePercent = int(math.Round(float64(sum) * 100 / float64(3*d.AssessedCount)))
		} else {
			d.Basis = "no_self_assessment"
		}
		if d.QuestionCount > 0 {
			d.CoveragePercent = int(math.Round(float64(d.AssessedCount) * 100 / float64(d.QuestionCount)))
		}
		d.Level, d.LevelLabel, d.Description = level(d.ScorePercent)
		if d.AssessedCount == 0 {
			d.Description = "Ты пока не оценил навыки в этом направлении. Предлагаем начать с основ: этот уровень выбран из-за отсутствия самооценки, а не по результатам проверки знаний."
		}
		sort.SliceStable(d.FocusSkills, func(i, j int) bool { return *d.FocusSkills[i].Answer < *d.FocusSkills[j].Answer })
		if len(d.FocusSkills) > 3 {
			d.FocusSkills = d.FocusSkills[:3]
		}
		result.Directions = append(result.Directions, d)
	}
	return result, nil
}

func level(score int) (string, string, string) {
	switch {
	case score < 25:
		return "starter", "Можно начать с основ", "Начни с ключевых навыков и закрепляй их на небольших практических задачах."
	case score < 50:
		return "developing", "Основа уже складывается", "Ты уже знаком с частью ключевых навыков. Практика поможет чувствовать себя увереннее."
	case score < 75:
		return "solid", "Есть хорошая база", "На основные навыки уже можно опереться. Дальше — больше самостоятельной практики."
	default:
		return "confident", "Уверенная база", "По твоей самооценке база уже уверенная. Можно пробовать более сложные самостоятельные задачи."
	}
}
