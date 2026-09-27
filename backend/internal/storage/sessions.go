package storage

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"max-carreer-bot/internal/cryptostore"
)

type Sessions struct {
	pool   *pgxpool.Pool
	lookup *cryptostore.IdentityLookup
}

func NewSessions(pool *pgxpool.Pool, lookup *cryptostore.IdentityLookup) (*Sessions, error) {
	if pool == nil || lookup == nil {
		return nil, errors.New("missing session storage dependency")
	}
	return &Sessions{pool: pool, lookup: lookup}, nil
}

// Exchange creates or finds a user by HMAC lookup and saves only the bearer
// token's SHA-256 hash. The user and session are committed atomically.
func (s *Sessions) Exchange(ctx context.Context, maxID int64, tokenHash [32]byte, expiresAt time.Time) (string, error) {
	candidates, err := s.lookup.Candidates(maxID)
	if err != nil {
		return "", err
	}
	if expiresAt.IsZero() {
		return "", errors.New("missing session expiry")
	}
	active := candidates[0]
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	var userID string
	var foundVersion uint32
	for _, candidate := range candidates {
		err = tx.QueryRow(ctx, `SELECT id::text FROM users WHERE max_id_lookup = $1 FOR UPDATE`, candidate.Digest[:]).Scan(&userID)
		if err == nil {
			foundVersion = candidate.Version
			break
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return "", err
		}
	}
	if userID == "" {
		newID, err := randomUUID()
		if err != nil {
			return "", err
		}
		err = tx.QueryRow(ctx, `
			INSERT INTO users (id, max_id_lookup, lookup_key_version)
			VALUES ($1::uuid, $2, $3)
			ON CONFLICT (max_id_lookup) DO UPDATE SET last_seen_at = now()
			RETURNING id::text`, newID, active.Digest[:], int32(active.Version)).Scan(&userID)
		if err != nil {
			return "", err
		}
	} else {
		if foundVersion != active.Version {
			_, err = tx.Exec(ctx, `UPDATE users SET max_id_lookup = $1, lookup_key_version = $2, last_seen_at = now() WHERE id = $3::uuid`, active.Digest[:], int32(active.Version), userID)
		} else {
			_, err = tx.Exec(ctx, `UPDATE users SET last_seen_at = now() WHERE id = $1::uuid`, userID)
		}
		if err != nil {
			return "", err
		}
	}
	if _, err := tx.Exec(ctx, `INSERT INTO user_state (user_id) VALUES ($1::uuid) ON CONFLICT (user_id) DO NOTHING`, userID); err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO sessions (token_hash, user_id, expires_at) VALUES ($1, $2::uuid, $3)`, tokenHash[:], userID, expiresAt); err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return userID, nil
}

func (s *Sessions) Lookup(ctx context.Context, tokenHash [32]byte, now time.Time) (string, bool, error) {
	var userID string
	err := s.pool.QueryRow(ctx, `SELECT user_id::text FROM sessions WHERE token_hash = $1 AND expires_at > $2 AND revoked_at IS NULL`, tokenHash[:], now).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return userID, true, nil
}

func (s *Sessions) Revoke(ctx context.Context, tokenHash [32]byte, now time.Time) error {
	_, err := s.pool.Exec(ctx, `UPDATE sessions SET revoked_at = $2 WHERE token_hash = $1 AND revoked_at IS NULL`, tokenHash[:], now)
	return err
}

func (s *Sessions) Ready(ctx context.Context) error {
	var users, sessions, state, idempotency string
	if err := s.pool.QueryRow(ctx, `SELECT COALESCE(to_regclass('public.users')::text, ''), COALESCE(to_regclass('public.sessions')::text, ''), COALESCE(to_regclass('public.user_state')::text, ''), COALESCE(to_regclass('public.idempotency_records')::text, '')`).Scan(&users, &sessions, &state, &idempotency); err != nil {
		return err
	}
	if users == "" || sessions == "" || state == "" || idempotency == "" {
		return errors.New("user migration is missing")
	}
	return nil
}

func randomUUID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("generate UUID: %w", err)
	}
	value[6] = value[6]&0x0f | 0x40
	value[8] = value[8]&0x3f | 0x80
	encoded := hex.EncodeToString(value[:])
	return encoded[:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:], nil
}
