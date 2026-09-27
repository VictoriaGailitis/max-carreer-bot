package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"max-carreer-bot/internal/cryptostore"
)

func TestGenerateKeyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data-key")
	if err := generateKeyFile(path, bytes.NewReader(bytes.Repeat([]byte{9}, 32))); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("key mode %o", info.Mode().Perm())
	}
	key, err := cryptostore.LoadHexKeyFile(path)
	if err != nil || !bytes.Equal(key, bytes.Repeat([]byte{9}, 32)) {
		t.Fatalf("invalid generated key: %v", err)
	}
	if err := generateKeyFile(path, bytes.NewReader(bytes.Repeat([]byte{8}, 32))); err == nil {
		t.Fatal("overwrote existing key")
	}
}
