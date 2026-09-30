// Package geography validates city names against the same offline directory used by the frontend.
package geography

import (
	_ "embed"
	"encoding/json"
	"strings"
	"unicode"
)

//go:embed cities.json
var directoryJSON []byte

var canonicalNames = func() map[string]string {
	var data struct {
		Cities []struct {
			Value   string   `json:"value"`
			Aliases []string `json:"aliases"`
		} `json:"cities"`
	}
	if err := json.Unmarshal(directoryJSON, &data); err != nil {
		panic("invalid embedded city directory: " + err.Error())
	}
	values := make(map[string]string, len(data.Cities))
	for _, city := range data.Cities {
		values[searchKey(city.Value)] = city.Value
		for _, alias := range city.Aliases {
			values[searchKey(alias)] = city.Value
		}
	}
	return values
}()

func searchKey(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.TrimPrefix(value, "г.")
	return strings.Map(func(r rune) rune {
		if r == 'ё' {
			return 'е'
		}
		if unicode.IsSpace(r) || r == '-' || r == '–' || r == '—' || r == '‑' {
			return -1
		}
		return r
	}, value)
}

// Canonical only resolves a known spelling or explicit alias. It never guesses a typo.
// Repeated names require a regional qualifier, as emitted by the picker.
func Canonical(value string) (string, bool) {
	result, ok := canonicalNames[searchKey(value)]
	return result, ok
}
