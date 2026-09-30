package catalog

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"max-carreer-bot/internal/questionnaire"
)

const MaxFileBytes = 4 << 20

var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{1,63}$`)

func ValidID(id string) bool { return idPattern.MatchString(id) }

type Validator struct {
	bank       *questionnaire.Bank
	domains    []string
	directions map[string]bool
	skills     map[string]bool
}

// NewValidator requires an explicit editorial allowlist. Subdomains of an
// allowed domain are accepted, but unrelated suffixes and IPs are not.
func NewValidator(bank *questionnaire.Bank, allowedDomains []string) (*Validator, error) {
	if bank == nil || len(allowedDomains) == 0 {
		return nil, errors.New("catalog validator needs bank and domains")
	}
	v := &Validator{bank: bank, directions: map[string]bool{}, skills: map[string]bool{}}
	for _, domain := range allowedDomains {
		domain = strings.ToLower(strings.TrimSpace(domain))
		if domain == "" || domain == "localhost" || net.ParseIP(domain) != nil || strings.ContainsAny(domain, "/:@ ") || !strings.Contains(domain, ".") {
			return nil, fmt.Errorf("invalid allowed domain %q", domain)
		}
		v.domains = append(v.domains, domain)
	}
	for _, direction := range bank.Directions {
		v.directions[direction.ID] = true
	}
	for _, question := range bank.Questions {
		v.skills[question.ID] = true
	}
	return v, nil
}

func (v *Validator) Validate(raw []byte) (Validated, error) {
	if len(raw) == 0 || len(raw) > MaxFileBytes {
		return Validated{}, errors.New("catalog file size is invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var dataset Dataset
	if err := decoder.Decode(&dataset); err != nil {
		return Validated{}, fmt.Errorf("catalog JSON: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return Validated{}, errors.New("catalog has trailing JSON")
	}
	if dataset.SchemaVersion != SchemaVersion {
		return Validated{}, errors.New("unsupported catalog schema version")
	}
	if len(dataset.Items) > MaxItems {
		return Validated{}, errors.New("catalog exceeds 500 cards")
	}
	seen := map[string]bool{}
	for i, item := range dataset.Items {
		if seen[item.ID] {
			return Validated{}, fmt.Errorf("items[%d].id: duplicate ID", i)
		}
		seen[item.ID] = true
		if err := v.validateItem(item, dataset.IsDemo); err != nil {
			return Validated{}, fmt.Errorf("items[%d].%w", i, err)
		}
	}
	return Validated{Dataset: dataset, Raw: append([]byte(nil), raw...), Hash: sha256.Sum256(raw)}, nil
}

func (v *Validator) validateItem(o Opportunity, datasetDemo bool) error {
	if !idPattern.MatchString(o.ID) {
		return errors.New("id: invalid stable ID")
	}
	if !slices.Contains([]string{"course", "internship", "event"}, o.Type) {
		return errors.New("type: invalid type")
	}
	if o.OrganizerID != "vk" {
		return errors.New("organizer_id: expected vk")
	}
	for _, field := range []struct {
		name, value string
		max         int
	}{{"title", o.Title, 160}, {"summary", o.Summary, 500}, {"description", o.Description, 10000}} {
		if err := checkText(field.value, field.max); err != nil {
			return fmt.Errorf("%s: %w", field.name, err)
		}
	}
	if len(o.Directions) == 0 || len(o.Directions) > 8 {
		return errors.New("directions: expected 1–8 directions")
	}
	seen := map[string]bool{}
	for _, id := range o.Directions {
		if !v.directions[id] || seen[id] {
			return errors.New("directions: unknown or repeated ID")
		}
		seen[id] = true
	}
	if len(o.Specializations) > 10 {
		return errors.New("specializations: too many values")
	}
	for _, name := range o.Specializations {
		if err := checkText(name, 80); err != nil {
			return fmt.Errorf("specializations: %w", err)
		}
	}
	if !slices.Contains([]string{"online", "offline", "hybrid", "unknown"}, o.Format) {
		return errors.New("format: invalid format")
	}
	if o.FullyRemoteAllowed && o.Format != "hybrid" {
		return errors.New("fully_remote_allowed: only for hybrid")
	}
	if len(o.Cities) > 20 {
		return errors.New("cities: too many cities")
	}
	citySeen := map[string]bool{}
	for _, city := range o.Cities {
		if err := checkText(city, 80); err != nil {
			return fmt.Errorf("cities: %w", err)
		}
		if citySeen[city] {
			return errors.New("cities: duplicate city")
		}
		citySeen[city] = true
	}
	if o.EducationRequirements != nil {
		e := o.EducationRequirements
		if err := checkText(e.Basis, 500); err != nil {
			return fmt.Errorf("education_requirements.basis: %w", err)
		}
		if len(e.Stages) == 0 && e.StudyYearMin == nil && e.StudyYearMax == nil {
			return errors.New("education_requirements: empty requirement")
		}
		stages := map[string]bool{}
		for _, stage := range e.Stages {
			if !slices.Contains([]string{"vocational", "undergraduate", "graduate", "alumni", "other"}, stage) || stages[stage] {
				return errors.New("education_requirements.stages: invalid stage")
			}
			stages[stage] = true
		}
		if e.StudyYearMin != nil && (*e.StudyYearMin < 1 || *e.StudyYearMin > 6) {
			return errors.New("education_requirements.study_year_min: expected 1–6")
		}
		if e.StudyYearMax != nil && (*e.StudyYearMax < 1 || *e.StudyYearMax > 6) {
			return errors.New("education_requirements.study_year_max: expected 1–6")
		}
		if e.StudyYearMin != nil && e.StudyYearMax != nil && *e.StudyYearMin > *e.StudyYearMax {
			return errors.New("education_requirements: year range reversed")
		}
	}
	if o.RequirementsText != nil {
		if err := checkText(*o.RequirementsText, 2000); err != nil {
			return fmt.Errorf("requirements_text: %w", err)
		}
	}
	if len(o.SkillRequirements) > 50 {
		return errors.New("skill_requirements: too many requirements")
	}
	skillSeen := map[string]bool{}
	for _, requirement := range o.SkillRequirements {
		if !v.skills[requirement.SkillID] || skillSeen[requirement.SkillID] {
			return errors.New("skill_requirements.skill_id: unknown or repeated ID")
		}
		skillSeen[requirement.SkillID] = true
		if requirement.MinExperience < 0 || requirement.MinExperience > 3 {
			return errors.New("skill_requirements.min_experience: expected 0–3")
		}
		if err := checkText(requirement.Basis, 500); err != nil {
			return fmt.Errorf("skill_requirements.basis: %w", err)
		}
	}
	if o.NoPrerequisites != nil && *o.NoPrerequisites && (len(o.SkillRequirements) > 0 || o.EducationRequirements != nil) {
		return errors.New("no_prerequisites: conflicts with mandatory requirements")
	}
	if err := validateDetails(o.Type, o.Details); err != nil {
		return err
	}
	if o.StartsAt != nil && o.EndsAt != nil && o.EndsAt.Before(*o.StartsAt) {
		return errors.New("ends_at: before starts_at")
	}
	if err := v.checkURL(o.SourceURL); err != nil {
		return fmt.Errorf("source_url: %w", err)
	}
	if err := v.checkURL(o.ActionURL); err != nil {
		return fmt.Errorf("action_url: %w", err)
	}
	if o.CheckedAt.IsZero() {
		return errors.New("checked_at: required")
	}
	if !slices.Contains([]string{"draft", "published", "archived"}, o.PublicationStatus) {
		return errors.New("publication_status: invalid status")
	}
	if !slices.Contains([]string{"open", "closed", "unknown"}, o.Availability) {
		return errors.New("availability: invalid status")
	}
	if o.IsDemo != datasetDemo {
		return errors.New("is_demo: differs from dataset")
	}
	return nil
}

func validateDetails(kind string, details Details) error {
	count := 0
	if details.Course != nil {
		count++
	}
	if details.Internship != nil {
		count++
	}
	if details.Event != nil {
		count++
	}
	if count != 1 {
		return errors.New("details: expected one type-specific object")
	}
	switch kind {
	case "course":
		if details.Course == nil {
			return errors.New("details: expected course")
		}
		if details.Course.PriceText == "" && details.Course.DurationText == "" {
			return errors.New("details.course: empty")
		}
		for _, value := range []string{details.Course.PriceText, details.Course.DurationText} {
			if value != "" {
				if err := checkText(value, 300); err != nil {
					return err
				}
			}
		}
	case "internship":
		if details.Internship == nil {
			return errors.New("details: expected internship")
		}
		if details.Internship.CompensationText == "" && details.Internship.DurationText == "" {
			return errors.New("details.internship: empty")
		}
		for _, value := range []string{details.Internship.CompensationText, details.Internship.DurationText} {
			if value != "" {
				if err := checkText(value, 300); err != nil {
					return err
				}
			}
		}
	case "event":
		if details.Event == nil {
			return errors.New("details: expected event")
		}
		if details.Event.Venue == "" && details.Event.RegistrationText == "" {
			return errors.New("details.event: empty")
		}
		for _, value := range []string{details.Event.Venue, details.Event.RegistrationText} {
			if value != "" {
				if err := checkText(value, 300); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func checkText(value string, maxRunes int) error {
	if value == "" || strings.TrimSpace(value) != value || utf8.RuneCountInString(value) > maxRunes || !utf8.ValidString(value) {
		return errors.New("invalid or oversized text")
	}
	for _, r := range value {
		if unicode.IsControl(r) && r != '\n' {
			return errors.New("control character")
		}
	}
	return nil
}

func (v *Validator) checkURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Opaque != "" || u.User != nil || u.Host == "" || u.Port() != "" || u.Fragment != "" {
		return errors.New("expected HTTPS URL without userinfo, port or fragment")
	}
	host := strings.ToLower(u.Hostname())
	if host == "localhost" || net.ParseIP(host) != nil || strings.HasSuffix(host, ".local") {
		return errors.New("local or IP host is forbidden")
	}
	for _, domain := range v.domains {
		if host == domain || strings.HasSuffix(host, "."+domain) {
			return nil
		}
	}
	return errors.New("host is not in editorial allowlist")
}
