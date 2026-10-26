package updater

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
)

func TestMetadataMirrorWinsAndJoinsBlockedDirectRequest(t *testing.T) {
	var completed atomic.Int32
	started := make(chan struct{})
	value, err := firstUpdateSource(context.Background(), []string{"blocked", "mirror"}, func(ctx context.Context, url string) (*int, error) {
		defer completed.Add(1)
		if url == "blocked" {
			close(started)
			<-ctx.Done()
			return nil, ctx.Err()
		}
		<-started
		n := 243
		return &n, nil
	})
	if err != nil || value == nil || *value != 243 || completed.Load() != 2 {
		t.Fatalf("mirror did not settle owned requests: value=%v err=%v completed=%d", value, err, completed.Load())
	}
}
func TestMetadataCancellationIsPreserved(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := firstUpdateSource(ctx, []string{"one"}, func(context.Context, string) (*int, error) { t.Fatal("cancelled request started"); return nil, nil })
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
