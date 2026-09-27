package maxbot

import (
	"context"
	"log"
	"time"
)

type DeliveryRepository interface {
	Claim(context.Context, time.Time) (Delivery, bool, error)
	Finish(context.Context, Delivery, bool, bool, time.Duration, time.Time) error
	Cleanup(context.Context, time.Time) error
}

type Worker struct {
	Inbox  DeliveryRepository
	Sender Sender
	Now    func() time.Time
}

func (w Worker) RunOnce(ctx context.Context) (bool, error) {
	d, ok, err := w.Inbox.Claim(ctx, w.Now())
	if err != nil || !ok {
		return false, err
	}
	sendCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	result, sendErr := w.Sender.Send(sendCtx, d.RecipientUserID)
	cancel()
	finishCtx, finishCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer finishCancel()
	if err := w.Inbox.Finish(finishCtx, d, sendErr == nil, result.Retryable, result.RetryAfter, w.Now()); err != nil {
		return true, err
	}
	if sendErr != nil {
		log.Printf("MAX greeting delivery failed; retryable=%t", result.Retryable)
	}
	return true, nil
}

func (w Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	lastCleanup := time.Time{}
	for {
		if ctx.Err() != nil {
			return
		}
		if w.Now().Sub(lastCleanup) >= time.Hour {
			cleanupCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			if err := w.Inbox.Cleanup(cleanupCtx, w.Now()); err != nil {
				log.Printf("MAX inbox cleanup failed")
			}
			cancel()
			lastCleanup = w.Now()
		}
		if _, err := w.RunOnce(ctx); err != nil {
			log.Printf("MAX delivery worker failed")
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
