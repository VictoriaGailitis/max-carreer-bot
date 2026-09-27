package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"
)

// This signature was calculated separately with Python's hmac library.
const signedFixture = "auth_date=1771409719&query_id=4c0ab423-342b-4e45-aea4-2747dbc500cd&user=%7B%22id%22%3A67890%2C%22first_name%22%3A%22Max%20%2B%20%D0%A2%D0%B5%D1%81%D1%82%22%7D&hash=d7f1f5337cbc7ad50544fdb236e6141e124f46fdf0f1be803ea49754ecc2c8d2"

func TestMAXSignedFixture(t *testing.T) {
	v, err := NewValidator("test-bot-token")
	if err != nil {
		t.Fatal(err)
	}
	got, err := v.Validate(signedFixture, time.Unix(1771409719+60, 0))
	if err != nil || got.MaxID != 67890 {
		t.Fatalf("fixture rejected: %+v, %v", got, err)
	}
	if !got.AuthDate.Equal(time.Unix(1771409719, 0)) {
		t.Fatalf("wrong auth date: %v", got.AuthDate)
	}
	if _, err := v.Validate(signedFixture, time.Unix(1771409719+301, 0)); !errors.Is(err, ErrExpiredInitData) {
		t.Fatalf("accepted stale initData: %v", err)
	}
	if _, err := v.Validate(signedFixture, time.Unix(1771409719-31, 0)); !errors.Is(err, ErrExpiredInitData) {
		t.Fatalf("accepted future initData: %v", err)
	}
}

func TestMAXRejectsTamperingAndAmbiguity(t *testing.T) {
	v, _ := NewValidator("test-bot-token")
	now := time.Unix(1771409719, 0)
	cases := []string{
		strings.Replace(signedFixture, "67890", "67891", 1),
		strings.Replace(signedFixture, "d7f1f533", "d7f1f534", 1),
		signedFixture + "&user=%7B%22id%22%3A67890%7D",
		signedFixture + "&hash=0000000000000000000000000000000000000000000000000000000000000000",
		strings.Replace(signedFixture, "auth_date", "%61uth_date", 1),
		strings.Replace(signedFixture, "%7B", "%ZZ", 1),
		strings.Repeat("a", MaxInitDataBytes+1),
	}
	for _, raw := range cases {
		if _, err := v.Validate(raw, now); !errors.Is(err, ErrInvalidInitData) {
			t.Fatalf("accepted ambiguous or tampered initData: %v", err)
		}
	}
	if _, err := NewValidator(""); err == nil {
		t.Fatal("accepted missing bot token")
	}
}

func TestMAXRejectsInvalidSignedUser(t *testing.T) {
	v, _ := NewValidator("test-bot-token")
	now := time.Unix(1771409719, 0)
	for _, user := range []string{
		`{"id":0}`, `{"id":-1}`, `{"id":"67890"}`,
		`{"id":1.5}`, `{"id":9223372036854775808}`,
		`{"id":1,"id":2}`, `[]`,
	} {
		raw := signForTest(map[string]string{"auth_date": "1771409719", "user": user})
		if _, err := v.Validate(raw, now); !errors.Is(err, ErrInvalidInitData) {
			t.Fatalf("accepted signed user %s: %v", user, err)
		}
	}
}

func TestMAXRawPlusIsNotSpace(t *testing.T) {
	v, _ := NewValidator("test-bot-token")
	raw := signForTest(map[string]string{
		"auth_date":   "1771409719",
		"start_param": "a+b",
		"user":        `{"id":67890}`,
	})
	raw = strings.Replace(raw, "start_param=a%2Bb", "start_param=a+b", 1)
	if _, err := v.Validate(raw, time.Unix(1771409719, 0)); err != nil {
		t.Fatalf("raw plus was changed: %v", err)
	}
}

func signForTest(values map[string]string) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	check := make([]string, 0, len(keys))
	parts := make([]string, 0, len(keys)+1)
	for _, key := range keys {
		check = append(check, key+"="+values[key])
		parts = append(parts, key+"="+strings.ReplaceAll(url.QueryEscape(values[key]), "+", "%20"))
	}
	secret := hmac.New(sha256.New, []byte("WebAppData"))
	_, _ = secret.Write([]byte("test-bot-token"))
	mac := hmac.New(sha256.New, secret.Sum(nil))
	_, _ = mac.Write([]byte(strings.Join(check, "\n")))
	return strings.Join(parts, "&") + "&hash=" + hex.EncodeToString(mac.Sum(nil))
}
