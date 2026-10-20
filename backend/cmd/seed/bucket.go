package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// On a fresh install SeaweedFS's master may still be electing a leader when
// the seed starts; its gateway refuses connections until then.
const (
	bucketWait  = 2 * time.Minute
	bucketRetry = 2 * time.Second
)

// bucketMaker is the one call of attachment.S3Store the wait needs.
type bucketMaker interface {
	EnsureBucket(ctx context.Context) error
}

// waitForBucket gives the object store a moment, as waitReady does Gitea.
func waitForBucket(ctx context.Context, store bucketMaker, wait, retry time.Duration, log *slog.Logger) error {
	deadline := time.Now().Add(wait)
	for {
		err := store.EnsureBucket(ctx)
		if err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("attachment bucket, still failing after %s: %w", wait, err)
		}
		log.Info("the object store is not answering yet; trying again", "error", err)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(retry):
		}
	}
}
