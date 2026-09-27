package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
	"time"
)

const SessionTTL = 12 * time.Hour

var ErrInvalidBearer = errors.New("invalid bearer token")

type IssuedSession struct {
	Token     string
	TokenHash [32]byte
	ExpiresAt time.Time
}

func IssueSession(now time.Time) (IssuedSession, error) {
	var random [32]byte
	if _, err := rand.Read(random[:]); err != nil {
		return IssuedSession{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(random[:])
	return IssuedSession{Token: token, TokenHash: sha256.Sum256([]byte(token)), ExpiresAt: now.Add(SessionTTL).UTC()}, nil
}

func HashBearer(header string) ([32]byte, error) {
	var zero [32]byte
	if !strings.HasPrefix(header, "Bearer ") {
		return zero, ErrInvalidBearer
	}
	token := strings.TrimPrefix(header, "Bearer ")
	if len(token) != 43 {
		return zero, ErrInvalidBearer
	}
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(decoded) != 32 || base64.RawURLEncoding.EncodeToString(decoded) != token {
		return zero, ErrInvalidBearer
	}
	return sha256.Sum256([]byte(token)), nil
}
