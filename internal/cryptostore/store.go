// Package cryptostore encrypts user documents before they reach persistent storage.
package cryptostore

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

const (
	AlgorithmVersion = 1
	MaxDocumentBytes = 1 << 20
	appName          = "student-navigator"
)

var (
	ErrInvalidContext  = errors.New("invalid document context")
	ErrInvalidEnvelope = errors.New("invalid encrypted document")
	ErrAuthentication  = errors.New("document authentication failed")
	ErrUnknownKey      = errors.New("unknown document key version")
)

type EntityType string

const (
	Profile   EntityType = "profile"
	Draft     EntityType = "draft"
	Favorites EntityType = "favorites"
	BotTask   EntityType = "bot_task"
)

// Context must be built from trusted database identifiers and revisions, not
// from a client request body. Every value becomes part of the AEAD AAD.
type Context struct {
	EntityType    EntityType
	UserUUID      string
	SchemaVersion uint32
	Revision      uint64
}

// Envelope maps to algorithm_version, key_version and ciphertext columns.
// Ciphertext is the 12-byte nonce followed by encrypted data and the GCM tag.
type Envelope struct {
	AlgorithmVersion uint16 `json:"algorithm_version"`
	KeyVersion       uint32 `json:"key_version"`
	Ciphertext       []byte `json:"ciphertext"`
}

type Store struct {
	active uint32
	keys   map[uint32]cipher.AEAD
}

// NewStore takes independent 32-byte AES keys by version. The active version
// is used for new writes; old versions remain available for reads and rewrap.
func NewStore(keys map[uint32][]byte, active uint32) (*Store, error) {
	if active == 0 || len(keys) == 0 {
		return nil, errors.New("missing active data key")
	}
	s := &Store{active: active, keys: make(map[uint32]cipher.AEAD, len(keys))}
	for version, key := range keys {
		if version == 0 || version > (1<<31)-1 || len(key) != 32 {
			return nil, fmt.Errorf("invalid data key version %d", version)
		}
		if zeroKey(key) {
			return nil, fmt.Errorf("placeholder data key version %d", version)
		}
		block, err := aes.NewCipher(key)
		if err != nil {
			return nil, err
		}
		aead, err := cipher.NewGCMWithRandomNonce(block)
		if err != nil {
			return nil, err
		}
		s.keys[version] = aead
	}
	if duplicateKey(keys) {
		return nil, errors.New("data key versions must use distinct keys")
	}
	if _, ok := s.keys[active]; !ok {
		return nil, errors.New("active data key is not loaded")
	}
	return s, nil
}

func (s *Store) Encrypt(ctx Context, plaintext []byte) (Envelope, error) {
	aad, err := associatedData(ctx)
	if err != nil {
		return Envelope{}, err
	}
	if len(plaintext) > MaxDocumentBytes {
		return Envelope{}, errors.New("document exceeds size limit")
	}
	aead := s.keys[s.active]
	ciphertext := aead.Seal(nil, nil, plaintext, aad)
	return Envelope{AlgorithmVersion: AlgorithmVersion, KeyVersion: s.active, Ciphertext: ciphertext}, nil
}

func (s *Store) Decrypt(ctx Context, envelope Envelope) ([]byte, error) {
	aad, err := associatedData(ctx)
	if err != nil {
		return nil, err
	}
	if envelope.AlgorithmVersion != AlgorithmVersion {
		return nil, ErrInvalidEnvelope
	}
	aead, ok := s.keys[envelope.KeyVersion]
	if !ok {
		return nil, ErrUnknownKey
	}
	if len(envelope.Ciphertext) < aead.Overhead() || len(envelope.Ciphertext) > MaxDocumentBytes+aead.Overhead() {
		return nil, ErrInvalidEnvelope
	}
	plaintext, err := aead.Open(nil, nil, envelope.Ciphertext, aad)
	if err != nil {
		return nil, ErrAuthentication
	}
	return plaintext, nil
}

// Rewrap changes only the encryption key. The caller must atomically update
// key_version and ciphertext if the database revision still matches ctx.
func (s *Store) Rewrap(ctx Context, envelope Envelope) (Envelope, error) {
	if envelope.KeyVersion == s.active {
		// Still authenticate before reporting that no write is necessary.
		if _, err := s.Decrypt(ctx, envelope); err != nil {
			return Envelope{}, err
		}
		return envelope, nil
	}
	plaintext, err := s.Decrypt(ctx, envelope)
	if err != nil {
		return Envelope{}, err
	}
	return s.Encrypt(ctx, plaintext)
}

// AAD uses length-delimited fields and fixed-width integers; changes to its
// encoding require a new AlgorithmVersion, not an in-place reinterpretation.
func associatedData(ctx Context) ([]byte, error) {
	var entity byte
	switch ctx.EntityType {
	case Profile:
		entity = 1
	case Draft:
		entity = 2
	case Favorites:
		entity = 3
	case BotTask:
		entity = 4
	default:
		return nil, ErrInvalidContext
	}
	if ctx.SchemaVersion == 0 || ctx.Revision == 0 {
		return nil, ErrInvalidContext
	}
	uuid, err := parseUUID(ctx.UserUUID)
	if err != nil {
		return nil, ErrInvalidContext
	}
	aad := make([]byte, 0, 1+len(appName)+1+16+4+8)
	aad = append(aad, byte(len(appName)))
	aad = append(aad, appName...)
	aad = append(aad, entity)
	aad = append(aad, uuid[:]...)
	aad = binary.BigEndian.AppendUint32(aad, ctx.SchemaVersion)
	aad = binary.BigEndian.AppendUint64(aad, ctx.Revision)
	return aad, nil
}

func parseUUID(value string) ([16]byte, error) {
	var out [16]byte
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return out, ErrInvalidContext
	}
	compact := strings.ReplaceAll(value, "-", "")
	_, err := hex.Decode(out[:], []byte(compact))
	if err == nil && out == ([16]byte{}) {
		return out, ErrInvalidContext
	}
	return out, err
}
