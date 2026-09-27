package auth

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestIssueSessionAndBearer(t *testing.T) {
	now := time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)
	one, err := IssueSession(now)
	if err != nil {
		t.Fatal(err)
	}
	two, err := IssueSession(now)
	if err != nil {
		t.Fatal(err)
	}
	if one.Token == two.Token || one.TokenHash == two.TokenHash {
		t.Fatal("session tokens repeated")
	}
	if !one.ExpiresAt.Equal(now.Add(SessionTTL)) {
		t.Fatalf("wrong expiry: %v", one.ExpiresAt)
	}
	hash, err := HashBearer("Bearer " + one.Token)
	if err != nil || hash != one.TokenHash {
		t.Fatalf("bearer failed: %v", err)
	}
	for _, header := range []string{"", "bearer " + one.Token, "Bearer  " + one.Token, "Bearer " + one.Token + "x", "Bearer " + strings.Repeat("!", 43)} {
		if _, err := HashBearer(header); !errors.Is(err, ErrInvalidBearer) {
			t.Fatalf("accepted %q: %v", header, err)
		}
	}
}
