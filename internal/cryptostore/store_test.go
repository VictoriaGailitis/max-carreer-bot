package cryptostore

import (
	"bytes"
	"errors"
	"testing"
)

const (
	userA = "6d305789-d179-4c87-9d96-695dd88891f1"
	userB = "aac1b36b-6809-4bb5-a2a0-7de05ded930c"
)

func key(value byte) []byte { return bytes.Repeat([]byte{value}, 32) }

func testContext() Context {
	return Context{EntityType: Profile, UserUUID: userA, SchemaVersion: 1, Revision: 1}
}

func TestEncryptRoundTripAndRandomNonce(t *testing.T) {
	store, err := NewStore(map[uint32][]byte{1: key(1)}, 1)
	if err != nil {
		t.Fatal(err)
	}
	ctx := testContext()
	plaintext := []byte(`{"skills":{"BACK-01":2}}`)
	first, err := store.Encrypt(ctx, plaintext)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Encrypt(ctx, plaintext)
	if err != nil {
		t.Fatal(err)
	}
	if first.AlgorithmVersion != 1 || first.KeyVersion != 1 {
		t.Fatalf("wrong envelope versions: %+v", first)
	}
	if len(first.Ciphertext) != len(plaintext)+28 {
		t.Fatalf("unexpected envelope length %d", len(first.Ciphertext))
	}
	if bytes.Equal(first.Ciphertext, second.Ciphertext) {
		t.Fatal("random nonce produced identical ciphertext")
	}
	for _, encrypted := range []Envelope{first, second} {
		got, err := store.Decrypt(ctx, encrypted)
		if err != nil || !bytes.Equal(got, plaintext) {
			t.Fatalf("round trip failed: %v", err)
		}
	}
}

func TestAuthenticationBindsOwnerTypeSchemaAndRevision(t *testing.T) {
	store, err := NewStore(map[uint32][]byte{1: key(1)}, 1)
	if err != nil {
		t.Fatal(err)
	}
	ctx := testContext()
	envelope, err := store.Encrypt(ctx, []byte("private"))
	if err != nil {
		t.Fatal(err)
	}
	mutations := []Context{
		{EntityType: Profile, UserUUID: userB, SchemaVersion: 1, Revision: 1},
		{EntityType: Draft, UserUUID: userA, SchemaVersion: 1, Revision: 1},
		{EntityType: Profile, UserUUID: userA, SchemaVersion: 2, Revision: 1},
		{EntityType: Profile, UserUUID: userA, SchemaVersion: 1, Revision: 2},
	}
	for _, altered := range mutations {
		if _, err := store.Decrypt(altered, envelope); !errors.Is(err, ErrAuthentication) {
			t.Fatalf("accepted altered context %+v: %v", altered, err)
		}
	}
	tampered := envelope
	tampered.Ciphertext = bytes.Clone(envelope.Ciphertext)
	tampered.Ciphertext[len(tampered.Ciphertext)-1] ^= 1
	if _, err := store.Decrypt(ctx, tampered); !errors.Is(err, ErrAuthentication) {
		t.Fatalf("accepted tampered tag: %v", err)
	}
	tampered.Ciphertext = []byte{1, 2, 3}
	if _, err := store.Decrypt(ctx, tampered); !errors.Is(err, ErrInvalidEnvelope) {
		t.Fatalf("accepted truncated ciphertext: %v", err)
	}
	tampered = envelope
	tampered.AlgorithmVersion = 2
	if _, err := store.Decrypt(ctx, tampered); !errors.Is(err, ErrInvalidEnvelope) {
		t.Fatalf("accepted unknown algorithm: %v", err)
	}
}

func TestRewrapRetainsContextAndChangesKey(t *testing.T) {
	oldStore, err := NewStore(map[uint32][]byte{1: key(1)}, 1)
	if err != nil {
		t.Fatal(err)
	}
	ctx := testContext()
	old, err := oldStore.Encrypt(ctx, []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	rotated, err := NewStore(map[uint32][]byte{1: key(1), 2: key(2)}, 2)
	if err != nil {
		t.Fatal(err)
	}
	newEnvelope, err := rotated.Rewrap(ctx, old)
	if err != nil {
		t.Fatal(err)
	}
	if newEnvelope.KeyVersion != 2 || bytes.Equal(old.Ciphertext, newEnvelope.Ciphertext) {
		t.Fatal("rewrap did not change encryption key")
	}
	got, err := rotated.Decrypt(ctx, newEnvelope)
	if err != nil || string(got) != "secret" {
		t.Fatalf("rotated decrypt: %v", err)
	}
	if _, err := oldStore.Decrypt(ctx, newEnvelope); !errors.Is(err, ErrUnknownKey) {
		t.Fatalf("old key decrypted new envelope: %v", err)
	}
	if _, err := rotated.Rewrap(Context{EntityType: Profile, UserUUID: userB, SchemaVersion: 1, Revision: 1}, old); !errors.Is(err, ErrAuthentication) {
		t.Fatalf("rewrap accepted wrong owner: %v", err)
	}
}

func TestInvalidKeysAndContext(t *testing.T) {
	if _, err := NewStore(map[uint32][]byte{1: key(1)}, 2); err == nil {
		t.Fatal("accepted missing active key")
	}
	if _, err := NewStore(map[uint32][]byte{1: []byte("short")}, 1); err == nil {
		t.Fatal("accepted short key")
	}
	if _, err := NewStore(map[uint32][]byte{1: make([]byte, 32)}, 1); err == nil {
		t.Fatal("accepted zero key")
	}
	if _, err := NewStore(map[uint32][]byte{1: key(1), 2: key(1)}, 2); err == nil {
		t.Fatal("accepted duplicate rotation key")
	}
	store, err := NewStore(map[uint32][]byte{1: key(1)}, 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, ctx := range []Context{
		{EntityType: Profile, UserUUID: "invalid", SchemaVersion: 1, Revision: 1},
		{EntityType: "other", UserUUID: userA, SchemaVersion: 1, Revision: 1},
		{EntityType: Profile, UserUUID: userA, SchemaVersion: 0, Revision: 1},
		{EntityType: Profile, UserUUID: userA, SchemaVersion: 1, Revision: 0},
	} {
		if _, err := store.Encrypt(ctx, nil); !errors.Is(err, ErrInvalidContext) {
			t.Fatalf("accepted context %+v: %v", ctx, err)
		}
	}
	if _, err := store.Encrypt(testContext(), make([]byte, MaxDocumentBytes+1)); err == nil {
		t.Fatal("accepted oversized document")
	}
}
