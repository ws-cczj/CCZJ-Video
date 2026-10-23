package operation

import (
	"cczjVideo/app/apperror"
	"context"
	"errors"
	"strings"
	"testing"
)

func TestCorrelationPreservesCauseAndObservesOnce(t *testing.T) {
	cause := errors.New("private SQL and path")
	count := 0
	_, err := Run(context.Background(), Meta{Name: "test.save", Mode: "video"}, func(r Result) {
		count++
		if r.RequestID == "" || r.Duration < 0 {
			t.Fatal(r)
		}
	}, func(context.Context) (bool, error) {
		return false, apperror.Wrap(apperror.Storage, cause, "保存失败")
	})
	if count != 1 || !errors.Is(err, cause) {
		t.Fatal(count, err)
	}
	public := apperror.Public(err)
	if public.Code != apperror.Storage || public.Operation != "test.save" || public.RequestID == "" || strings.Contains(public.Message, "private") {
		t.Fatal(public)
	}
	if public.Logged {
		t.Fatal("metadata falsely claimed a log")
	}
}
func TestCancellationSkipsHandler(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	_, err := Run(ctx, Meta{}, nil, func(context.Context) (bool, error) { called = true; return true, nil })
	if called || !errors.Is(err, context.Canceled) {
		t.Fatal(called, err)
	}
}
func TestRecoveryKeepsStackPrivate(t *testing.T) {
	_, err := Run(context.Background(), Meta{Name: "panic"}, nil, func(context.Context) (bool, error) { panic("private value") })
	public := apperror.Public(err)
	if public.Code != apperror.Internal || strings.Contains(err.Error(), "private") || !strings.Contains(apperror.Diagnostic(err).Error(), "goroutine") {
		t.Fatal(public, err)
	}
}

func TestRequestIdentityTravelsThroughWorkerContext(t *testing.T) {
	var observed Result
	value, err := Run(context.Background(), Meta{Name: "video.detail", Pipeline: "rpc.GetVideoDetail"}, func(result Result) { observed = result }, func(ctx context.Context) (Identity, error) {
		result := make(chan Identity, 1)
		go func() {
			identity, ok := IdentityFromContext(ctx)
			if !ok {
				result <- Identity{}
				return
			}
			result <- identity
		}()
		return <-result, nil
	})
	if err != nil || value.RequestID == "" || value.RequestID != observed.RequestID || value.Pipeline != "rpc.GetVideoDetail" {
		t.Fatal(value, observed, err)
	}
}
