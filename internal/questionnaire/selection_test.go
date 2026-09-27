package questionnaire

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func testBank(t *testing.T) *Bank {
	t.Helper()
	bank, err := Load(filepath.Join("..", "..", "data", "question-bank.v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	return bank
}

func ids(preview Preview) []string {
	result := make([]string, len(preview.Questions))
	for i, q := range preview.Questions {
		result[i] = q.ID
	}
	return result
}

func TestBankAndSelection(t *testing.T) {
	bank := testBank(t)
	if len(bank.Directions) != 9 || len(bank.Questions) != 126 {
		t.Fatalf("unexpected bank size: %d directions, %d questions", len(bank.Directions), len(bank.Questions))
	}
	tests := []struct {
		name      string
		selection Selection
		want      []string
		count     int
	}{
		{"one", Selection{Directions: []string{"backend"}}, []string{"BACK-01", "BACK-02", "BACK-03", "BACK-04", "BACK-05", "BACK-06", "BACK-07", "BACK-08"}, 8},
		{"two normalized", Selection{Directions: []string{"analytics", "backend"}}, []string{"BACK-01", "DATA-01", "BACK-02", "DATA-02", "BACK-03", "DATA-03"}, 12},
		{"three", Selection{Directions: []string{"design", "backend", "analytics"}}, []string{"BACK-01", "DATA-01", "DESIGN-01", "BACK-02", "DATA-02", "DESIGN-02"}, 16},
		{"mobile ios", Selection{Directions: []string{"mobile"}, Profiles: map[string]string{"mobile": "ios"}}, []string{"IOS-01", "MOBILE-01", "IOS-02", "MOBILE-02", "IOS-03", "MOBILE-03", "IOS-04", "MOBILE-04"}, 8},
		{"management all", Selection{Directions: []string{"management"}, Profiles: map[string]string{"management": "management-all"}}, []string{"PROJECT-01", "BA-02", "PRODUCT-03", "PROJECT-04", "BA-05", "PRODUCT-06", "PROJECT-07", "BA-08"}, 8},
		{"product alias", Selection{Directions: []string{"product"}}, []string{"PRODUCT-01", "PRODUCT-02"}, 8},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := bank.Preview(tt.selection)
			if err != nil {
				t.Fatal(err)
			}
			if got.QuestionCount != tt.count || len(got.Questions) != tt.count {
				t.Fatalf("count: %+v", got)
			}
			if !reflect.DeepEqual(ids(got)[:len(tt.want)], tt.want) {
				t.Fatalf("got %v, want prefix %v", ids(got), tt.want)
			}
		})
	}
}

func TestInvalidSelections(t *testing.T) {
	bank := testBank(t)
	selections := []Selection{
		{}, {Directions: []string{"unknown"}}, {Directions: []string{"backend", "backend"}},
		{Directions: []string{"mobile"}, Profiles: map[string]string{"mobile": "ba"}},
		{Directions: []string{"backend"}, Profiles: map[string]string{"mobile": "ios"}},
		{Directions: []string{"product", "management"}},
		{Directions: []string{"backend", "frontend", "ml", "devops"}},
	}
	for _, selection := range selections {
		if _, err := bank.Preview(selection); err == nil {
			t.Fatalf("accepted %+v", selection)
		}
	}
}

func TestAnswersDistinguishAbsentNullAndZero(t *testing.T) {
	preview, err := testBank(t).Preview(Selection{Directions: []string{"backend"}})
	if err != nil {
		t.Fatal(err)
	}
	answers := map[string]json.RawMessage{}
	for _, q := range preview.Questions {
		answers[q.ID] = json.RawMessage("0")
	}
	answers[preview.Questions[0].ID] = json.RawMessage("null")
	if err := ValidateAnswers(preview, answers); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{"4", "1.5", `"2"`, "true"} {
		answers[preview.Questions[0].ID] = json.RawMessage(invalid)
		if err := ValidateAnswers(preview, answers); err == nil {
			t.Fatalf("accepted %s", invalid)
		}
	}
	delete(answers, preview.Questions[0].ID)
	if err := ValidateAnswers(preview, answers); err == nil || !strings.Contains(err.(*ValidationError).Fields["answers."+preview.Questions[0].ID], "отсутствует") {
		t.Fatalf("missing answer: %v", err)
	}
	answers["OTHER-01"] = json.RawMessage("0")
	if err := ValidateAnswers(preview, answers); err == nil {
		t.Fatal("accepted extra answer")
	}
}
