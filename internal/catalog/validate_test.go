package catalog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"max-carreer-bot/internal/questionnaire"
)

func testValidator(t *testing.T) (*Validator, []byte) {
	t.Helper()
	bank, err := questionnaire.Load(filepath.Join("..", "..", "data", "question-bank.v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	validator, err := NewValidator(bank, []string{"example.test"})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "catalog-demo.v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	return validator, raw
}

func TestValidateDemoCatalog(t *testing.T) {
	v, raw := testValidator(t)
	result, err := v.Validate(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Dataset.IsDemo || len(result.Dataset.Items) != 3 {
		t.Fatalf("unexpected result: %+v", result.Dataset)
	}
	for _, item := range result.Dataset.Items {
		if item.OrganizerID != "vk" {
			t.Fatalf("invalid organizer: %+v", item)
		}
	}
}

func TestCatalogValidationRejectsInvalidCard(t *testing.T) {
	v, raw := testValidator(t)
	var original Dataset
	if err := json.Unmarshal(raw, &original); err != nil {
		t.Fatal(err)
	}
	mutations := []struct {
		name string
		edit func(*Dataset)
	}{
		{"duplicate ID", func(d *Dataset) { d.Items[1].ID = d.Items[0].ID }},
		{"unknown skill", func(d *Dataset) { d.Items[1].SkillRequirements[0].SkillID = "be_http_api" }},
		{"foreign host", func(d *Dataset) { d.Items[0].ActionURL = "https://example.test.evil.invalid/" }},
		{"local host", func(d *Dataset) { d.Items[0].ActionURL = "https://127.0.0.1/" }},
		{"userinfo", func(d *Dataset) { d.Items[0].ActionURL = "https://x@example.test/" }},
		{"port", func(d *Dataset) { d.Items[0].ActionURL = "https://example.test:8443/" }},
		{"unpublished requirement", func(d *Dataset) {
			d.Items[0].SkillRequirements = []SkillRequirement{{SkillID: "BACK-01", MinExperience: 2, Basis: "test"}}
		}},
		{"mismatched demo flag", func(d *Dataset) { d.Items[0].IsDemo = false }},
		{"reversed event", func(d *Dataset) { d.Items[2].EndsAt = d.Items[2].StartsAt; d.Items[2].StartsAt = d.Items[0].DeadlineAt }},
	}
	for _, test := range mutations {
		t.Run(test.name, func(t *testing.T) {
			copyRaw, _ := json.Marshal(original)
			var data Dataset
			if err := json.Unmarshal(copyRaw, &data); err != nil {
				t.Fatal(err)
			}
			test.edit(&data)
			bad, _ := json.Marshal(data)
			if _, err := v.Validate(bad); err == nil {
				t.Fatal("accepted invalid catalog")
			}
		})
	}
}
