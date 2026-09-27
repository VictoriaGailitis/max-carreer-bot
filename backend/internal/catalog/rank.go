package catalog

import (
	"bytes"
	"encoding/json"
	"slices"
	"sort"
	"time"

	"max-carreer-bot/internal/profile"
)

type Score struct {
	DirectionMatch bool
	GoalMatch      bool
	ConditionsRank int
	ExperienceRank int
	Reasons        []Reason
}

func Rank(p profile.Document, o Opportunity) Score {
	score := Score{ConditionsRank: conditionsRank(p, o), ExperienceRank: experienceRank(p, o), Reasons: make([]Reason, 0, 2)}
	for _, direction := range o.Directions {
		if slices.Contains(p.Directions, direction) {
			score.DirectionMatch = true
			score.Reasons = append(score.Reasons, Reason{Code: "direction_match", RefID: direction})
			break
		}
	}
	if slices.Contains(p.Goals, o.Type) {
		score.GoalMatch = true
		if len(score.Reasons) < 2 {
			score.Reasons = append(score.Reasons, Reason{Code: "goal_match", RefID: o.Type})
		}
	}
	if len(score.Reasons) < 2 {
		if o.NoPrerequisites != nil && *o.NoPrerequisites {
			score.Reasons = append(score.Reasons, Reason{Code: "no_prerequisites"})
		} else if score.ConditionsRank >= 1 && score.ExperienceRank >= 1 && (score.ConditionsRank == 1 || score.ExperienceRank == 1) {
			score.Reasons = append(score.Reasons, Reason{Code: "requirements_unknown"})
		}
	}
	return score
}

func conditionsRank(p profile.Document, o Opportunity) int {
	unknown := false
	needsCity := false
	switch o.Format {
	case "online":
		if p.PreferredFormat == "offline" {
			return 0
		}
	case "offline":
		if p.PreferredFormat == "online" {
			return 0
		}
		needsCity = true
	case "hybrid":
		if p.PreferredFormat == "online" && !o.FullyRemoteAllowed {
			return 0
		}
		needsCity = !o.FullyRemoteAllowed || p.PreferredFormat == "offline"
	case "unknown":
		unknown = true
	}
	if needsCity {
		if len(o.Cities) == 0 {
			unknown = true
		} else if !p.AnyCity {
			match := false
			for _, city := range o.Cities {
				if slices.Contains(p.PreferredCities, city) || p.OtherCity == city {
					match = true
					break
				}
			}
			if !match {
				return 0
			}
		}
	}
	if e := o.EducationRequirements; e != nil {
		if len(e.Stages) > 0 {
			if p.EducationStage == "prefer_not_to_say" || p.EducationStage == "" {
				unknown = true
			} else if !slices.Contains(e.Stages, p.EducationStage) {
				return 0
			}
		}
		if e.StudyYearMin != nil || e.StudyYearMax != nil {
			if p.StudyYear == nil {
				unknown = true
			} else {
				if e.StudyYearMin != nil && *p.StudyYear < *e.StudyYearMin {
					return 0
				}
				if e.StudyYearMax != nil && *p.StudyYear > *e.StudyYearMax {
					return 0
				}
			}
		}
	}
	if unknown {
		return 1
	}
	return 2
}

func experienceRank(p profile.Document, o Opportunity) int {
	if o.NoPrerequisites != nil && *o.NoPrerequisites {
		return 2
	}
	if len(o.SkillRequirements) == 0 {
		return 1
	}
	unknown := false
	for _, requirement := range o.SkillRequirements {
		raw, present := p.Skills[requirement.SkillID]
		if !present || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			unknown = true
			continue
		}
		var answer int
		if err := json.Unmarshal(raw, &answer); err != nil {
			unknown = true
			continue
		}
		if answer < requirement.MinExperience {
			return 0
		}
	}
	if unknown {
		return 1
	}
	return 2
}

func EffectiveStatus(o Opportunity, now time.Time) (status, hint string) {
	if o.PublicationStatus != "published" {
		return "unavailable", ""
	}
	if o.Availability == "closed" || (o.DeadlineAt != nil && now.After(*o.DeadlineAt)) ||
		(o.Type == "event" && o.DeadlineAt == nil && o.EndsAt != nil && now.After(*o.EndsAt)) {
		return "closed", ""
	}
	if o.Availability == "unknown" || (o.DeadlineAt == nil && (o.Type != "event" || o.EndsAt == nil)) {
		return "active", "check_source"
	}
	return "active", ""
}

type Filters struct{ Type, Direction, Format string }

func BuildResult(stored Stored, p profile.Document, profileRevision int64, now time.Time, filters Filters) Result {
	type candidate struct {
		item  ResultItem
		score Score
	}
	candidates := make([]candidate, 0, len(stored.Items))
	result := Result{CatalogVersion: stored.Version, ProfileRevision: profileRevision, RankingVersion: RankingVersion, EvaluatedAt: now, Items: make([]ResultItem, 0)}
	for _, o := range stored.Items {
		if o.IsDemo || o.PublicationStatus != "published" {
			continue
		}
		status, hint := EffectiveStatus(o, now)
		if status != "active" {
			continue
		}
		if filters.Type != "" && filters.Type != o.Type {
			continue
		}
		if filters.Direction != "" && !slices.Contains(o.Directions, filters.Direction) {
			continue
		}
		if filters.Format != "" && filters.Format != o.Format {
			continue
		}
		score := Rank(p, o)
		if score.DirectionMatch {
			result.HasDirectionMatches = true
		}
		candidates = append(candidates, candidate{ResultItem{Opportunity: o, EffectiveStatus: status, AvailabilityHint: hint, Reasons: score.Reasons}, score})
	}
	sort.Slice(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		if a.score.DirectionMatch != b.score.DirectionMatch {
			return a.score.DirectionMatch
		}
		if a.score.GoalMatch != b.score.GoalMatch {
			return a.score.GoalMatch
		}
		if a.score.ConditionsRank != b.score.ConditionsRank {
			return a.score.ConditionsRank > b.score.ConditionsRank
		}
		if a.score.ExperienceRank != b.score.ExperienceRank {
			return a.score.ExperienceRank > b.score.ExperienceRank
		}
		if a.item.DeadlineAt == nil && b.item.DeadlineAt != nil {
			return false
		}
		if a.item.DeadlineAt != nil && b.item.DeadlineAt == nil {
			return true
		}
		if a.item.DeadlineAt != nil && b.item.DeadlineAt != nil && !a.item.DeadlineAt.Equal(*b.item.DeadlineAt) {
			return a.item.DeadlineAt.Before(*b.item.DeadlineAt)
		}
		return a.item.ID < b.item.ID
	})
	for _, c := range candidates {
		result.Items = append(result.Items, c.item)
	}
	return result
}
