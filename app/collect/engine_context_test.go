package collect

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestFetchPageWithRetryHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	started := time.Now()
	_, err := fetchPageWithRetry(ctx, "https://example.test/api", 1, FetchOptions{}, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
		t.Fatalf("canceled retry took %v", elapsed)
	}
}
