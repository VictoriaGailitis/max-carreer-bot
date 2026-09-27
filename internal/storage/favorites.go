package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"sort"

	"github.com/jackc/pgx/v5"

	"max-carreer-bot/internal/catalog"
	"max-carreer-bot/internal/cryptostore"
)

var ErrFavoritesLimit = errors.New("favorites limit reached")

type FavoritesState struct {
	IDs      []string `json:"ids"`
	Revision int64    `json:"revision"`
}

type favoritesDocument struct {
	SchemaVersion int      `json:"schema_version"`
	IDs           []string `json:"ids"`
}

func (s *UserState) decodeFavorites(userID string, ciphertext []byte, algorithm sql.NullInt16, key sql.NullInt32, revision int64) (FavoritesState, error) {
	state := FavoritesState{IDs: []string{}, Revision: revision}
	if ciphertext == nil {
		return state, nil
	}
	if !algorithm.Valid || !key.Valid || revision <= 0 {
		return FavoritesState{}, cryptostore.ErrInvalidEnvelope
	}
	plain, err := s.crypto.Decrypt(cryptostore.Context{EntityType: cryptostore.Favorites, UserUUID: userID, SchemaVersion: 1, Revision: uint64(revision)},
		cryptostore.Envelope{Ciphertext: ciphertext, AlgorithmVersion: uint16(algorithm.Int16), KeyVersion: uint32(key.Int32)})
	if err != nil {
		return FavoritesState{}, err
	}
	var document favoritesDocument
	if err := json.Unmarshal(plain, &document); err != nil {
		return FavoritesState{}, err
	}
	if document.SchemaVersion != 1 || len(document.IDs) > 500 {
		return FavoritesState{}, cryptostore.ErrInvalidEnvelope
	}
	for i, id := range document.IDs {
		if !catalog.ValidID(id) || slices.Contains(document.IDs[:i], id) {
			return FavoritesState{}, cryptostore.ErrInvalidEnvelope
		}
	}
	state.IDs = document.IDs
	return state, nil
}

func (s *UserState) GetFavorites(ctx context.Context, userID string) (FavoritesState, error) {
	var ciphertext []byte
	var algorithm sql.NullInt16
	var key sql.NullInt32
	var revision int64
	err := s.pool.QueryRow(ctx, `SELECT favorites_ciphertext,favorites_algorithm_version,favorites_key_version,favorites_revision FROM user_state WHERE user_id=$1::uuid`, userID).
		Scan(&ciphertext, &algorithm, &key, &revision)
	if err != nil {
		return FavoritesState{}, err
	}
	return s.decodeFavorites(userID, ciphertext, algorithm, key, revision)
}

func (s *UserState) MutateFavorite(ctx context.Context, userID, id string, add bool) (FavoritesState, error) {
	if !catalog.ValidID(id) {
		return FavoritesState{}, errors.New("invalid favorite ID")
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return FavoritesState{}, err
	}
	defer tx.Rollback(ctx)
	var ciphertext []byte
	var algorithm sql.NullInt16
	var key sql.NullInt32
	var revision int64
	err = tx.QueryRow(ctx, `SELECT favorites_ciphertext,favorites_algorithm_version,favorites_key_version,favorites_revision FROM user_state WHERE user_id=$1::uuid FOR UPDATE`, userID).
		Scan(&ciphertext, &algorithm, &key, &revision)
	if err != nil {
		return FavoritesState{}, err
	}
	state, err := s.decodeFavorites(userID, ciphertext, algorithm, key, revision)
	if err != nil {
		return FavoritesState{}, err
	}
	found := slices.Contains(state.IDs, id)
	if (add && found) || (!add && !found) {
		return state, tx.Commit(ctx)
	}
	if add {
		if len(state.IDs) >= 500 {
			return FavoritesState{}, ErrFavoritesLimit
		}
		state.IDs = append(state.IDs, id)
		sort.Strings(state.IDs)
	} else {
		state.IDs = slices.Delete(state.IDs, slices.Index(state.IDs, id), slices.Index(state.IDs, id)+1)
	}
	state.Revision++
	plain, err := json.Marshal(favoritesDocument{SchemaVersion: 1, IDs: state.IDs})
	if err != nil {
		return FavoritesState{}, err
	}
	envelope, err := s.crypto.Encrypt(cryptostore.Context{EntityType: cryptostore.Favorites, UserUUID: userID, SchemaVersion: 1, Revision: uint64(state.Revision)}, plain)
	if err != nil {
		return FavoritesState{}, err
	}
	_, err = tx.Exec(ctx, `UPDATE user_state SET favorites_ciphertext=$2,favorites_algorithm_version=$3,favorites_key_version=$4,
		favorites_revision=$5,updated_at=now() WHERE user_id=$1::uuid`, userID, envelope.Ciphertext, int16(envelope.AlgorithmVersion), int32(envelope.KeyVersion), state.Revision)
	if err != nil {
		return FavoritesState{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return FavoritesState{}, err
	}
	return state, nil
}
