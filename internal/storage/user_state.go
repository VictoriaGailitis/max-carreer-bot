package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"max-carreer-bot/internal/cryptostore"
	"max-carreer-bot/internal/profile"
	"max-carreer-bot/internal/questionnaire"
)

type UserState struct {
	pool   *pgxpool.Pool
	crypto *cryptostore.Store
	bank   *questionnaire.Bank
}

type State struct {
	Profile         *profile.Document `json:"profile"`
	ProfileRevision int64             `json:"profile_revision"`
	Draft           *profile.Document `json:"draft"`
	DraftRevision   int64             `json:"draft_revision"`
}

func NewUserState(pool *pgxpool.Pool, crypto *cryptostore.Store, bank *questionnaire.Bank) (*UserState, error) {
	if pool == nil || crypto == nil || bank == nil {
		return nil, errors.New("missing user state dependency")
	}
	return &UserState{pool, crypto, bank}, nil
}

type stateRow struct {
	profile          []byte
	profileAlgorithm sql.NullInt16
	profileKey       sql.NullInt32
	profileRevision  int64
	draft            []byte
	draftAlgorithm   sql.NullInt16
	draftKey         sql.NullInt32
	draftRevision    int64
}

func readState(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, userID string, lock bool) (stateRow, error) {
	query := `SELECT profile_ciphertext, profile_algorithm_version, profile_key_version, profile_revision,
		draft_ciphertext, draft_algorithm_version, draft_key_version, draft_revision
		FROM user_state WHERE user_id = $1::uuid`
	if lock {
		query += ` FOR UPDATE`
	}
	var row stateRow
	err := q.QueryRow(ctx, query, userID).Scan(&row.profile, &row.profileAlgorithm, &row.profileKey, &row.profileRevision,
		&row.draft, &row.draftAlgorithm, &row.draftKey, &row.draftRevision)
	return row, err
}

func (s *UserState) decode(userID string, kind cryptostore.EntityType, revision int64, ciphertext []byte, algorithm sql.NullInt16, key sql.NullInt32) (*profile.Document, error) {
	if ciphertext == nil {
		return nil, nil
	}
	if revision <= 0 || !algorithm.Valid || !key.Valid {
		return nil, cryptostore.ErrInvalidEnvelope
	}
	plain, err := s.crypto.Decrypt(cryptostore.Context{EntityType: kind, UserUUID: userID, SchemaVersion: profile.SchemaVersion, Revision: uint64(revision)},
		cryptostore.Envelope{Ciphertext: ciphertext, AlgorithmVersion: uint16(algorithm.Int16), KeyVersion: uint32(key.Int32)})
	if err != nil {
		return nil, err
	}
	var document profile.Document
	if err := json.Unmarshal(plain, &document); err != nil {
		return nil, err
	}
	return &document, nil
}

func (s *UserState) encode(userID string, kind cryptostore.EntityType, revision int64, doc profile.Document) (cryptostore.Envelope, error) {
	plain, err := json.Marshal(doc)
	if err != nil {
		return cryptostore.Envelope{}, err
	}
	return s.crypto.Encrypt(cryptostore.Context{EntityType: kind, UserUUID: userID, SchemaVersion: profile.SchemaVersion, Revision: uint64(revision)}, plain)
}

func (s *UserState) Get(ctx context.Context, userID string) (State, error) {
	row, err := readState(ctx, s.pool, userID, false)
	if err != nil {
		return State{}, err
	}
	return s.toState(userID, row)
}

func (s *UserState) toState(userID string, row stateRow) (State, error) {
	p, err := s.decode(userID, cryptostore.Profile, row.profileRevision, row.profile, row.profileAlgorithm, row.profileKey)
	if err != nil {
		return State{}, err
	}
	d, err := s.decode(userID, cryptostore.Draft, row.draftRevision, row.draft, row.draftAlgorithm, row.draftKey)
	if err != nil {
		return State{}, err
	}
	return State{Profile: p, ProfileRevision: row.profileRevision, Draft: d, DraftRevision: row.draftRevision}, nil
}

func (s *UserState) PutDraft(ctx context.Context, userID string, expected int64, input profile.Document) (State, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return State{}, err
	}
	defer tx.Rollback(ctx)
	row, err := readState(ctx, tx, userID, true)
	if err != nil {
		return State{}, err
	}
	if row.draftRevision != expected {
		return State{}, profile.ErrRevisionConflict
	}
	state, err := s.toState(userID, row)
	if err != nil {
		return State{}, err
	}
	input, err = profile.Normalize(s.bank, input, state.Draft)
	if err != nil {
		return State{}, err
	}
	if state.Draft == nil {
		input.BaseProfileRevision = row.profileRevision
	} else {
		input.BaseProfileRevision = state.Draft.BaseProfileRevision
	}
	revision := row.draftRevision + 1
	envelope, err := s.encode(userID, cryptostore.Draft, revision, input)
	if err != nil {
		return State{}, err
	}
	_, err = tx.Exec(ctx, `UPDATE user_state SET draft_ciphertext=$2, draft_algorithm_version=$3, draft_key_version=$4,
		draft_revision=$5, updated_at=now() WHERE user_id=$1::uuid`, userID, envelope.Ciphertext, int16(envelope.AlgorithmVersion), int32(envelope.KeyVersion), revision)
	if err != nil {
		return State{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return State{}, err
	}
	state.Draft = &input
	state.DraftRevision = revision
	return state, nil
}

func (s *UserState) DeleteDraft(ctx context.Context, userID string) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	row, err := readState(ctx, tx, userID, true)
	if err != nil {
		return err
	}
	if row.draft == nil {
		return tx.Commit(ctx)
	}
	_, err = tx.Exec(ctx, `UPDATE user_state SET draft_ciphertext=NULL, draft_algorithm_version=NULL, draft_key_version=NULL,
		draft_revision=draft_revision+1, updated_at=now() WHERE user_id=$1::uuid`, userID)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *UserState) Complete(ctx context.Context, userID string, draftRevision int64, key string, now time.Time) (State, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return State{}, err
	}
	defer tx.Rollback(ctx)
	row, err := readState(ctx, tx, userID, true)
	if err != nil {
		return State{}, err
	}
	requestHash := sha256.Sum256([]byte(fmt.Sprintf("%d", draftRevision)))
	var savedHash, resultCipher []byte
	var resultRevision int64
	var resultAlgorithm int16
	var resultKey int32
	err = tx.QueryRow(ctx, `SELECT request_hash, profile_revision, result_ciphertext, result_algorithm_version, result_key_version
		FROM idempotency_records WHERE user_id=$1::uuid AND key=$2::uuid AND expires_at>$3`, userID, key, now).
		Scan(&savedHash, &resultRevision, &resultCipher, &resultAlgorithm, &resultKey)
	if err == nil {
		if string(savedHash) != string(requestHash[:]) {
			return State{}, profile.ErrRevisionConflict
		}
		result, err := s.decode(userID, cryptostore.Profile, resultRevision, resultCipher,
			sql.NullInt16{Int16: resultAlgorithm, Valid: true}, sql.NullInt32{Int32: resultKey, Valid: true})
		if err != nil {
			return State{}, err
		}
		return State{Profile: result, ProfileRevision: resultRevision, DraftRevision: row.draftRevision}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return State{}, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM idempotency_records WHERE user_id=$1::uuid AND key=$2::uuid AND expires_at<=$3`, userID, key, now); err != nil {
		return State{}, err
	}
	if row.draft == nil || row.draftRevision != draftRevision {
		return State{}, profile.ErrRevisionConflict
	}
	state, err := s.toState(userID, row)
	if err != nil {
		return State{}, err
	}
	if state.Draft.BaseProfileRevision != row.profileRevision {
		return State{}, profile.ErrRevisionConflict
	}
	completed, err := profile.Complete(s.bank, *state.Draft, now)
	if err != nil {
		return State{}, err
	}
	revision := row.profileRevision + 1
	envelope, err := s.encode(userID, cryptostore.Profile, revision, completed)
	if err != nil {
		return State{}, err
	}
	_, err = tx.Exec(ctx, `UPDATE user_state SET profile_ciphertext=$2, profile_algorithm_version=$3, profile_key_version=$4,
		profile_revision=$5, draft_ciphertext=NULL, draft_algorithm_version=NULL, draft_key_version=NULL,
		draft_revision=draft_revision+1, updated_at=now() WHERE user_id=$1::uuid`, userID,
		envelope.Ciphertext, int16(envelope.AlgorithmVersion), int32(envelope.KeyVersion), revision)
	if err != nil {
		return State{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO idempotency_records (user_id, key, request_hash, profile_revision,
		result_ciphertext, result_algorithm_version, result_key_version, expires_at)
		VALUES ($1::uuid,$2::uuid,$3,$4,$5,$6,$7,$8)`, userID, key, requestHash[:], revision,
		envelope.Ciphertext, int16(envelope.AlgorithmVersion), int32(envelope.KeyVersion), now.Add(24*time.Hour))
	if err != nil {
		return State{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return State{}, err
	}
	return State{Profile: &completed, ProfileRevision: revision, DraftRevision: row.draftRevision + 1}, nil
}

func (s *UserState) DeleteData(ctx context.Context, userID string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM users WHERE id=$1::uuid`, userID)
	return err
}
