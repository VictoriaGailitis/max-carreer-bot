package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const MaxInitDataBytes = 16 << 10

var ErrInvalidInitData = errors.New("invalid MAX initData")
var ErrExpiredInitData = errors.New("expired MAX initData")

type Identity struct {
	MaxID    int64
	AuthDate time.Time
}

type Validator struct{ secretKey [32]byte }

func NewValidator(botToken string) (*Validator, error) {
	if botToken == "" || len(botToken) > 1024 || strings.TrimSpace(botToken) != botToken || botToken == "YOUR_BOT_TOKEN" {
		return nil, errors.New("invalid MAX bot token")
	}
	mac := hmac.New(sha256.New, []byte("WebAppData"))
	_, _ = mac.Write([]byte(botToken))
	var secret [32]byte
	copy(secret[:], mac.Sum(nil))
	return &Validator{secretKey: secret}, nil
}

// Validate checks the raw window.WebApp.initData string. It never trusts the
// user JSON until the signature is verified and never logs raw initData.
func (v *Validator) Validate(raw string, now time.Time) (Identity, error) {
	if len(raw) == 0 || len(raw) > MaxInitDataBytes {
		return Identity{}, ErrInvalidInitData
	}
	values := make(map[string]string)
	for _, pair := range strings.Split(raw, "&") {
		key, encoded, ok := strings.Cut(pair, "=")
		if !ok || !validKey(key) {
			return Identity{}, ErrInvalidInitData
		}
		if _, exists := values[key]; exists {
			return Identity{}, ErrInvalidInitData
		}
		// MAX's reference uses decodeURIComponent: raw '+' remains '+'.
		value, err := url.PathUnescape(encoded)
		if err != nil {
			return Identity{}, ErrInvalidInitData
		}
		values[key] = value
	}
	hashText, ok := values["hash"]
	if !ok || len(hashText) != 64 {
		return Identity{}, ErrInvalidInitData
	}
	supplied, err := hex.DecodeString(hashText)
	if err != nil {
		return Identity{}, ErrInvalidInitData
	}
	delete(values, "hash")
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var check strings.Builder
	for i, key := range keys {
		if i > 0 {
			check.WriteByte('\n')
		}
		check.WriteString(key)
		check.WriteByte('=')
		check.WriteString(values[key])
	}
	mac := hmac.New(sha256.New, v.secretKey[:])
	_, _ = mac.Write([]byte(check.String()))
	if !hmac.Equal(mac.Sum(nil), supplied) {
		return Identity{}, ErrInvalidInitData
	}

	dateText, ok := values["auth_date"]
	if !ok {
		return Identity{}, ErrInvalidInitData
	}
	seconds, err := strconv.ParseInt(dateText, 10, 64)
	if err != nil || seconds <= 0 {
		return Identity{}, ErrInvalidInitData
	}
	authDate := time.Unix(seconds, 0).UTC()
	if authDate.After(now.Add(30*time.Second)) || now.Sub(authDate) > 5*time.Minute {
		return Identity{}, ErrExpiredInitData
	}
	userText, ok := values["user"]
	if !ok {
		return Identity{}, ErrInvalidInitData
	}
	maxID, err := parseUserID(userText)
	if err != nil {
		return Identity{}, ErrInvalidInitData
	}
	return Identity{MaxID: maxID, AuthDate: authDate}, nil
}

func validKey(key string) bool {
	if key == "" {
		return false
	}
	for i := 0; i < len(key); i++ {
		c := key[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' {
			continue
		}
		return false
	}
	return true
}

func parseUserID(value string) (int64, error) {
	decoder := json.NewDecoder(strings.NewReader(value))
	first, err := decoder.Token()
	if err != nil || first != json.Delim('{') {
		return 0, ErrInvalidInitData
	}
	seen := map[string]bool{}
	var id int64
	hasID := false
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return 0, ErrInvalidInitData
		}
		key, ok := token.(string)
		if !ok || seen[key] {
			return 0, ErrInvalidInitData
		}
		seen[key] = true
		var data json.RawMessage
		if err := decoder.Decode(&data); err != nil {
			return 0, ErrInvalidInitData
		}
		if key == "id" {
			if err := json.Unmarshal(data, &id); err != nil {
				return 0, ErrInvalidInitData
			}
			hasID = true
		}
	}
	last, err := decoder.Token()
	if err != nil || last != json.Delim('}') {
		return 0, ErrInvalidInitData
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return 0, ErrInvalidInitData
	}
	if !hasID || id <= 0 {
		return 0, ErrInvalidInitData
	}
	return id, nil
}
