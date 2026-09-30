package profile

import (
	"errors"
	"testing"
	"time"
)

func TestCityValidationOnSave(t *testing.T) {
	bank := bankForTest(t)
	base := Document{CurrentStep: 9, Directions: []string{"frontend"}, PreferredFormat: "offline"}
	for _, city := range []string{"Мосвка", "Лондон", "Несуществующийгород", "Берёзовский"} {
		draft := base
		draft.PreferredCities = []string{city}
		_, err := Normalize(bank, draft, nil)
		var validation *ValidationError
		if !errors.As(err, &validation) || validation.Fields["preferred_cities[0]"] == "" {
			t.Errorf("expected field error for %q: %v", city, err)
		}
	}
	base.PreferredCities = []string{"москва", "СПБ"}
	saved, err := Normalize(bank, base, nil)
	if err != nil || saved.PreferredCities[0] != "Москва" || saved.PreferredCities[1] != "Санкт-Петербург" {
		t.Fatalf("canonical save: %+v %v", saved.PreferredCities, err)
	}
	if base.PreferredCities[0] != "москва" {
		t.Fatal("mutated input slice")
	}
	base.PreferredCities = []string{"Москва", "МОСКВА"}
	if _, err := Normalize(bank, base, nil); err == nil {
		t.Fatal("accepted normalized duplicate")
	}
	base.PreferredCities = nil
	base.OtherCity = "Мосвка"
	if _, err := Normalize(bank, base, nil); err == nil {
		t.Fatal("other_city bypassed validation")
	}
	base.OtherCity = ""
	base.AnyCity = true
	if _, err := Normalize(bank, base, nil); err != nil {
		t.Fatalf("any city rejected: %v", err)
	}
}

func TestCompletionRevalidatesLegacyCity(t *testing.T) {
	// Persisted pre-validation drafts must not bypass the new check.
	_, err := Complete(bankForTest(t), Document{PreferredCities: []string{"Мосвка"}}, time.Now())
	var validation *ValidationError
	if !errors.As(err, &validation) || validation.Fields["preferred_cities[0]"] == "" {
		t.Fatalf("legacy typo accepted: %v", err)
	}
}

func TestCityWithLongRegionalQualifier(t *testing.T) {
	// This valid name is under 80 characters, but over 80 bytes in UTF-8.
	city := "Радужный (Ханты-Мансийский Автономный округ - Югра АО)"
	_, err := Normalize(bankForTest(t), Document{CurrentStep: 9, Directions: []string{"frontend"}, PreferredFormat: "offline", PreferredCities: []string{city}}, nil)
	if err != nil {
		t.Fatalf("valid regional qualifier rejected: %v", err)
	}
}
