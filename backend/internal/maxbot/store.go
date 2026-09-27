package maxbot

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"max-carreer-bot/internal/cryptostore"
)

const taskTTL = 48 * time.Hour

type Inbox struct {
	pool   *pgxpool.Pool
	crypto *cryptostore.Store
}

type Delivery struct {
	ID              string
	Attempts        int
	RecipientUserID int64
}

func NewInbox(pool *pgxpool.Pool, crypto *cryptostore.Store) (*Inbox, error) {
	if pool == nil || crypto == nil {
		return nil, errors.New("missing bot inbox dependency")
	}
	return &Inbox{pool: pool, crypto: crypto}, nil
}

func (i *Inbox) Ready(ctx context.Context) error {
	var name string
	return i.pool.QueryRow(ctx, `SELECT 'bot_inbox'::regclass::text`).Scan(&name)
}

// Enqueue commits the encrypted task before the webhook can be acknowledged.
func (i *Inbox) Enqueue(ctx context.Context, task Task, now time.Time) error {
	if task.RecipientUserID <= 0 || task.EventKey == ([32]byte{}) || task.RecipientLookup == ([32]byte{}) {
		return errors.New("invalid bot task")
	}
	id, err := newUUID()
	if err != nil {
		return err
	}
	plain, err := json.Marshal(task)
	if err != nil {
		return err
	}
	envelope, err := i.crypto.Encrypt(taskContext(id), plain)
	if err != nil {
		return err
	}
	_, err = i.pool.Exec(ctx, `INSERT INTO bot_inbox
		(id,event_key,recipient_lookup,payload_ciphertext,payload_algorithm_version,payload_key_version,status,next_attempt_at,expires_at,created_at)
		VALUES ($1::uuid,$2,$3,$4,$5,$6,'pending',$7,$8,$7) ON CONFLICT (event_key) DO NOTHING`,
		id, task.EventKey[:], task.RecipientLookup[:], envelope.Ciphertext, int16(envelope.AlgorithmVersion), int32(envelope.KeyVersion), now, now.Add(taskTTL))
	return err
}

func (i *Inbox) Claim(ctx context.Context, now time.Time) (Delivery, bool, error) {
	// A crashed fifth attempt is terminal after its lease expires.
	_, err := i.pool.Exec(ctx, `UPDATE bot_inbox SET status='failed',payload_ciphertext=NULL,payload_algorithm_version=NULL,
		payload_key_version=NULL,locked_until=NULL,finished_at=$1
		WHERE status='processing' AND attempts>=5 AND locked_until<=$1`, now)
	if err != nil {
		return Delivery{}, false, err
	}
	var d Delivery
	var ciphertext []byte
	var algorithm int16
	var key int32
	err = i.pool.QueryRow(ctx, `WITH due AS (
		SELECT id FROM bot_inbox WHERE expires_at>$1 AND attempts<5 AND
		((status='pending' AND next_attempt_at<=$1) OR (status='processing' AND locked_until<=$1))
		ORDER BY next_attempt_at,created_at FOR UPDATE SKIP LOCKED LIMIT 1
	) UPDATE bot_inbox b SET status='processing',attempts=b.attempts+1,locked_until=$2
	FROM due WHERE b.id=due.id RETURNING b.id::text,b.attempts,b.payload_ciphertext,b.payload_algorithm_version,b.payload_key_version`,
		now, now.Add(30*time.Second)).Scan(&d.ID, &d.Attempts, &ciphertext, &algorithm, &key)
	if errors.Is(err, pgx.ErrNoRows) {
		return Delivery{}, false, nil
	}
	if err != nil {
		return Delivery{}, false, err
	}
	plain, err := i.crypto.Decrypt(taskContext(d.ID), cryptostore.Envelope{Ciphertext: ciphertext, AlgorithmVersion: uint16(algorithm), KeyVersion: uint32(key)})
	if err != nil {
		return Delivery{}, false, err
	}
	var task Task
	if err = json.Unmarshal(plain, &task); err != nil || task.RecipientUserID <= 0 {
		return Delivery{}, false, errors.New("invalid stored bot task")
	}
	d.RecipientUserID = task.RecipientUserID
	return d, true, nil
}

func (i *Inbox) Finish(ctx context.Context, d Delivery, success, retryable bool, retryAfter time.Duration, now time.Time) error {
	if d.ID == "" || d.Attempts < 1 || d.Attempts > 5 {
		return errors.New("invalid delivery")
	}
	if success || !retryable || d.Attempts >= 5 {
		status := "failed"
		if success {
			status = "sent"
		}
		_, err := i.pool.Exec(ctx, `UPDATE bot_inbox SET status=$3,payload_ciphertext=NULL,payload_algorithm_version=NULL,
			payload_key_version=NULL,locked_until=NULL,finished_at=$4 WHERE id=$1::uuid AND attempts=$2 AND status='processing'`,
			d.ID, d.Attempts, status, now)
		return err
	}
	delay := time.Second * time.Duration(1<<uint(d.Attempts))
	if retryAfter > delay {
		delay = retryAfter
	}
	if delay > 5*time.Minute {
		delay = 5 * time.Minute
	}
	_, err := i.pool.Exec(ctx, `UPDATE bot_inbox SET status='pending',locked_until=NULL,next_attempt_at=$3
		WHERE id=$1::uuid AND attempts=$2 AND status='processing'`, d.ID, d.Attempts, now.Add(delay))
	return err
}

func (i *Inbox) Cleanup(ctx context.Context, now time.Time) error {
	_, err := i.pool.Exec(ctx, `DELETE FROM bot_inbox WHERE expires_at<=$1`, now)
	return err
}

func taskContext(id string) cryptostore.Context {
	return cryptostore.Context{EntityType: cryptostore.BotTask, UserUUID: id, SchemaVersion: 1, Revision: 1}
}

func newUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:]), nil
}
