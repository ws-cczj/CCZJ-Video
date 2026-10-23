package lifecycle

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRequestStopsOnEitherOwner(t *testing.T) {
	for _, owner := range []string{"request", "shutdown"} {
		t.Run(owner, func(t *testing.T) {
			request, cancelRequest := context.WithCancel(context.Background())
			defer cancelRequest()
			shutdown, cancelShutdown := context.WithCancel(context.Background())
			defer cancelShutdown()
			ctx, release := WithShutdown(request, shutdown)
			defer release()
			if owner == "request" {
				cancelRequest()
			} else {
				cancelShutdown()
			}
			select {
			case <-ctx.Done():
			case <-time.After(time.Second):
				t.Fatal("request did not stop")
			}
		})
	}
}

func TestRequestRetainsDeadlineAndDoesNotCancelOwners(t *testing.T) {
	deadline := time.Now().Add(time.Hour)
	request, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	shutdown := context.Background()
	ctx, release := WithShutdown(request, shutdown)
	if got, ok := ctx.Deadline(); !ok || got != deadline {
		t.Fatal("deadline lost")
	}
	release()
	if !errors.Is(ctx.Err(), context.Canceled) || request.Err() != nil {
		t.Fatal("release cancelled the request owner")
	}
}
