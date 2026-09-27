package cryptostore

import (
	"bytes"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadHexKeyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "key")
	encoded := hex.EncodeToString(key(7)) + "\n"
	if err := os.WriteFile(path, []byte(encoded), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadHexKeyFile(path)
	if err != nil || !bytes.Equal(loaded, key(7)) {
		t.Fatalf("load key: %v", err)
	}
	if err := os.WriteFile(path, []byte("placeholder"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadHexKeyFile(path); err == nil {
		t.Fatal("accepted placeholder")
	}
	if err := os.WriteFile(path, []byte(hex.EncodeToString(make([]byte, 32))), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadHexKeyFile(path); err == nil {
		t.Fatal("accepted zero key")
	}
}

func TestIdentityLookupRotation(t *testing.T) {
	lookup, err := NewIdentityLookup(map[uint32][]byte{1: key(1), 2: key(2)}, 2)
	if err != nil {
		t.Fatal(err)
	}
	active, err := lookup.Active(1234567)
	if err != nil {
		t.Fatal(err)
	}
	candidates, err := lookup.Candidates(1234567)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 2 || candidates[0] != active || candidates[1].Version != 1 || candidates[0].Digest == candidates[1].Digest {
		t.Fatalf("wrong candidates: %+v", candidates)
	}
	other, err := lookup.Active(1234568)
	if err != nil || other.Digest == active.Digest {
		t.Fatal("MAX IDs collided")
	}
	if _, err := lookup.Candidates(0); err == nil {
		t.Fatal("accepted zero MAX ID")
	}
	if _, err := NewIdentityLookup(map[uint32][]byte{1: key(1)}, 2); err == nil {
		t.Fatal("accepted missing active lookup key")
	}
	if _, err := NewIdentityLookup(map[uint32][]byte{1: key(1), 2: key(1)}, 2); err == nil {
		t.Fatal("accepted duplicate lookup keys")
	}
}
