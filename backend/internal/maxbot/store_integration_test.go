package maxbot

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"max-carreer-bot/internal/cryptostore"
)

// The smoke script opts in against its disposable PostgreSQL instance.
func TestInboxIntegration(t *testing.T) {
	urlFile := os.Getenv("BOT_INBOX_TEST_DB_URL_FILE")
	keyFile := os.Getenv("BOT_INBOX_TEST_DATA_KEY_FILE")
	if urlFile == "" || keyFile == "" {
		t.Skip("integration database not configured")
	}
	data, err := os.ReadFile(urlFile)
	if err != nil {
		t.Fatal(err)
	}
	key, err := cryptostore.LoadHexKeyFile(keyFile)
	if err != nil {
		t.Fatal(err)
	}
	crypto, err := cryptostore.NewStore(map[uint32][]byte{1: key}, 1)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	inbox, err := NewInbox(pool, crypto)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	first, ok, err := inbox.Claim(ctx, now)
	if err != nil || !ok || first.RecipientUserID != 99887766 || first.Attempts != 1 {
		t.Fatalf("claim: %+v %v %v", first, ok, err)
	}
	if err := inbox.Finish(ctx, first, false, true, 0, now); err != nil {
		t.Fatal(err)
	}
	again, ok, err := inbox.Claim(ctx, now.Add(3*time.Second))
	if err != nil || !ok || again.ID != first.ID || again.Attempts != 2 || again.RecipientUserID != first.RecipientUserID {
		t.Fatalf("retry: %+v %v %v", again, ok, err)
	}
	if err := inbox.Finish(ctx, again, true, false, 0, now.Add(3*time.Second)); err != nil {
		t.Fatal(err)
	}
	var status string
	var payload []byte
	if err := pool.QueryRow(ctx, `SELECT status,payload_ciphertext FROM bot_inbox WHERE id=$1::uuid`, first.ID).Scan(&status, &payload); err != nil {
		t.Fatal(err)
	}
	if status != "sent" || payload != nil {
		t.Fatalf("finished task retained payload: %s", status)
	}
}
