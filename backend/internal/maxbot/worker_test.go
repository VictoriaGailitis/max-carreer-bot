package maxbot

import (
	"context"
	"errors"
	"testing"
	"time"
)

type workerInbox struct {
	claimed   bool
	finished  bool
	success   bool
	retryable bool
	after     time.Duration
}

func (f *workerInbox) Claim(context.Context, time.Time) (Delivery, bool, error) {
	f.claimed = true
	return Delivery{ID: "id", Attempts: 1, RecipientUserID: 42}, true, nil
}
func (f *workerInbox) Finish(_ context.Context, _ Delivery, success, retryable bool, after time.Duration, _ time.Time) error {
	f.finished = true
	f.success = success
	f.retryable = retryable
	f.after = after
	return nil
}
func (f *workerInbox) Cleanup(context.Context, time.Time) error { return nil }

type workerSender struct{}

func (workerSender) Send(_ context.Context, id int64) (SendResult, error) {
	if id != 42 {
		return SendResult{}, errors.New("wrong recipient")
	}
	return SendResult{Retryable: true, RetryAfter: 7 * time.Second}, errors.New("rate limited")
}

func TestWorkerRecordsRetry(t *testing.T) {
	inbox := &workerInbox{}
	w := Worker{Inbox: inbox, Sender: workerSender{}, Now: time.Now}
	processed, err := w.RunOnce(context.Background())
	if err != nil || !processed || !inbox.claimed || !inbox.finished || inbox.success || !inbox.retryable || inbox.after != 7*time.Second {
		t.Fatalf("worker: %+v %v %v", inbox, processed, err)
	}
}
