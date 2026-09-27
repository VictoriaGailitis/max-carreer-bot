package cryptostore

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
)

// LoadHexKeyFile reads exactly one 256-bit key encoded as 64 hexadecimal
// characters, optionally followed by one newline. No key bytes enter errors.
func LoadHexKeyFile(path string) ([]byte, error) {
	if path == "" {
		return nil, errors.New("key file path is empty")
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("inspect key file: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() > 66 {
		return nil, errors.New("invalid key file")
	}
	encoded, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read key file: %w", err)
	}
	value := strings.TrimSuffix(string(encoded), "\n")
	value = strings.TrimSuffix(value, "\r")
	if len(value) != 64 {
		return nil, errors.New("key file must contain 64 hex characters")
	}
	key, err := hex.DecodeString(value)
	if err != nil {
		return nil, errors.New("key file contains invalid hex")
	}
	if zeroKey(key) {
		return nil, errors.New("key file contains a placeholder key")
	}
	return key, nil
}

func zeroKey(key []byte) bool {
	var zero [32]byte
	return len(key) == len(zero) && subtle.ConstantTimeCompare(key, zero[:]) == 1
}

func duplicateKey(keys map[uint32][]byte) bool {
	for firstVersion, first := range keys {
		for secondVersion, second := range keys {
			if firstVersion < secondVersion && subtle.ConstantTimeCompare(first, second) == 1 {
				return true
			}
		}
	}
	return false
}

type LookupCandidate struct {
	Version uint32
	Digest  [32]byte
}

// IdentityLookup supports a temporary old key during lookup-key rotation.
// Callers should search every candidate before creating a new user.
type IdentityLookup struct {
	active uint32
	keys   map[uint32][]byte
}

func NewIdentityLookup(keys map[uint32][]byte, active uint32) (*IdentityLookup, error) {
	if active == 0 || len(keys) == 0 {
		return nil, errors.New("missing active lookup key")
	}
	copyKeys := make(map[uint32][]byte, len(keys))
	for version, key := range keys {
		if version == 0 || version > (1<<31)-1 || len(key) != 32 {
			return nil, fmt.Errorf("invalid lookup key version %d", version)
		}
		if zeroKey(key) {
			return nil, fmt.Errorf("placeholder lookup key version %d", version)
		}
		copyKeys[version] = append([]byte(nil), key...)
	}
	if duplicateKey(copyKeys) {
		return nil, errors.New("lookup key versions must use distinct keys")
	}
	if _, exists := copyKeys[active]; !exists {
		return nil, errors.New("active lookup key is not loaded")
	}
	return &IdentityLookup{active: active, keys: copyKeys}, nil
}

func (l *IdentityLookup) Active(maxID int64) (LookupCandidate, error) {
	if maxID <= 0 {
		return LookupCandidate{}, errors.New("MAX ID must be positive")
	}
	return LookupCandidate{Version: l.active, Digest: l.digest(l.active, maxID)}, nil
}

func (l *IdentityLookup) Candidates(maxID int64) ([]LookupCandidate, error) {
	if maxID <= 0 {
		return nil, errors.New("MAX ID must be positive")
	}
	versions := make([]uint32, 0, len(l.keys))
	for version := range l.keys {
		versions = append(versions, version)
	}
	sort.Slice(versions, func(i, j int) bool {
		if versions[i] == l.active {
			return true
		}
		if versions[j] == l.active {
			return false
		}
		return versions[i] > versions[j]
	})
	result := make([]LookupCandidate, 0, len(versions))
	for _, version := range versions {
		result = append(result, LookupCandidate{Version: version, Digest: l.digest(version, maxID)})
	}
	return result, nil
}

func (l *IdentityLookup) digest(version uint32, maxID int64) [32]byte {
	mac := hmac.New(sha256.New, l.keys[version])
	_, _ = mac.Write([]byte("max:"))
	_, _ = mac.Write([]byte(strconv.FormatInt(maxID, 10)))
	var result [32]byte
	copy(result[:], mac.Sum(nil))
	return result
}
