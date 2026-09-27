package catalog

import "time"

const SchemaVersion = 1
const RankingVersion = 1
const MaxItems = 500

type Dataset struct {
	SchemaVersion int           `json:"schema_version"`
	IsDemo        bool          `json:"is_demo"`
	Items         []Opportunity `json:"items"`
}

type EducationRequirements struct {
	Stages       []string `json:"stages,omitempty"`
	StudyYearMin *int     `json:"study_year_min,omitempty"`
	StudyYearMax *int     `json:"study_year_max,omitempty"`
	Basis        string   `json:"basis"`
}

type SkillRequirement struct {
	SkillID       string `json:"skill_id"`
	MinExperience int    `json:"min_experience"`
	Basis         string `json:"basis"`
}

type Details struct {
	Course     *CourseDetails     `json:"course,omitempty"`
	Internship *InternshipDetails `json:"internship,omitempty"`
	Event      *EventDetails      `json:"event,omitempty"`
}

type CourseDetails struct {
	PriceText    string `json:"price_text,omitempty"`
	DurationText string `json:"duration_text,omitempty"`
}

type InternshipDetails struct {
	CompensationText string `json:"compensation_text,omitempty"`
	DurationText     string `json:"duration_text,omitempty"`
}

type EventDetails struct {
	Venue            string `json:"venue,omitempty"`
	RegistrationText string `json:"registration_text,omitempty"`
}

type Opportunity struct {
	ID                    string                 `json:"id"`
	Type                  string                 `json:"type"`
	OrganizerID           string                 `json:"organizer_id"`
	Title                 string                 `json:"title"`
	Summary               string                 `json:"summary"`
	Description           string                 `json:"description"`
	Directions            []string               `json:"directions"`
	Specializations       []string               `json:"specializations,omitempty"`
	Format                string                 `json:"format"`
	FullyRemoteAllowed    bool                   `json:"fully_remote_allowed,omitempty"`
	Cities                []string               `json:"cities"`
	EducationRequirements *EducationRequirements `json:"education_requirements"`
	RequirementsText      *string                `json:"requirements_text"`
	SkillRequirements     []SkillRequirement     `json:"skill_requirements"`
	NoPrerequisites       *bool                  `json:"no_prerequisites"`
	Details               Details                `json:"details"`
	StartsAt              *time.Time             `json:"starts_at"`
	EndsAt                *time.Time             `json:"ends_at"`
	DeadlineAt            *time.Time             `json:"deadline_at"`
	SourceURL             string                 `json:"source_url"`
	ActionURL             string                 `json:"action_url"`
	CheckedAt             time.Time              `json:"checked_at"`
	PublicationStatus     string                 `json:"publication_status"`
	Availability          string                 `json:"availability"`
	IsDemo                bool                   `json:"is_demo"`
}

type Validated struct {
	Dataset Dataset
	Raw     []byte
	Hash    [32]byte
}

type Stored struct {
	Version string
	Items   []Opportunity
}

type ResultItem struct {
	Opportunity
	EffectiveStatus  string   `json:"effective_status"`
	AvailabilityHint string   `json:"availability_hint,omitempty"`
	Reasons          []Reason `json:"reasons"`
}

type Result struct {
	CatalogVersion      string       `json:"catalog_version"`
	ProfileRevision     int64        `json:"profile_revision"`
	RankingVersion      int          `json:"ranking_version"`
	EvaluatedAt         time.Time    `json:"evaluated_at"`
	HasDirectionMatches bool         `json:"has_direction_matches"`
	Items               []ResultItem `json:"items"`
}

type Reason struct {
	Code  string `json:"code"`
	RefID string `json:"ref_id,omitempty"`
}
