package geography

import "testing"

func TestCanonical(t *testing.T) {
	for input, want := range map[string]string{"москва": "Москва", " СПБ ": "Санкт-Петербург", "Санкт Петербург": "Санкт-Петербург", "г. Москва": "Москва", "Дмитров": "Дмитров", "Алупка": "Алупка"} {
		got, ok := Canonical(input)
		if !ok || got != want {
			t.Errorf("Canonical(%q) = %q, %v; want %q", input, got, ok, want)
		}
	}
	for _, input := range []string{"Мосвка", "Город которого нет", "Берёзовский", "Лондон", "Московская"} {
		if got, ok := Canonical(input); ok {
			t.Errorf("accepted unknown or ambiguous %q: %q", input, got)
		}
	}
	if first, ok := Canonical("Орёл"); !ok {
		t.Fatal("missing Орёл")
	} else if second, _ := Canonical("Орел"); first != second {
		t.Fatal("е/ё mismatch")
	}
}
