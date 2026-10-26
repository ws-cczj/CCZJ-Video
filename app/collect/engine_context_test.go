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

func TestFetchPageDoesNotRedownloadInvalidResponse(t *testing.T) {
	for _, item := range []struct{ body, kind string }{
		{`{"code":1,"list":"not an array"}`, "decode"},
		{`{"code":1002,"msg":"search refused","list":"refused"}`, "source"},
	} {
		t.Run(item.kind, func(t *testing.T) {
			stub := withStubBody(t, item.body)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			_, err := fetchPageWithRetry(ctx, "https://example.test/api", 1, FetchOptions{}, nil)
			if got := FetchFailureKind(err); got != item.kind || len(stub.requested) != 1 {
				t.Fatalf("failure=%s requests=%d error=%v", got, len(stub.requested), err)
			}
		})
	}
}
