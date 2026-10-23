package service

import (
	"cczjVideo/app/apperror"
	"cczjVideo/app/collection"
	"cczjVideo/app/lifecycle"
	"context"
	"testing"
)

func TestCollectStatusBatchBoundsAndDeduplicates(t *testing.T) {
	a := &App{background: lifecycle.NewGroup(), collection: collection.NewService(nil, nil, context.Background())}
	rows, err := a.GetCollectStatuses(context.Background(), []string{"batch_a", "batch_a", "batch_b"})
	if err != nil || len(rows) != 2 || rows[0].SourceKey != "batch_a" || rows[1].SourceKey != "batch_b" {
		t.Fatal(rows, err)
	}
	if _, err = a.GetCollectStatuses(context.Background(), make([]string, 257)); !apperror.Is(err, apperror.Validation) {
		t.Fatal(err)
	}
	if _, err = a.GetCollectStatuses(context.Background(), []string{"../invalid"}); !apperror.Is(err, apperror.Validation) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = a.GetCollectStatuses(ctx, []string{"batch_c"}); !apperror.Is(err, apperror.Cancelled) {
		t.Fatal(err)
	}
}
