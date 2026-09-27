package detail

import (
	"cczjVideo/app/db"
	"cczjVideo/app/model"
	"context"
	"errors"
	"testing"
	"time"
)

func TestWaitingDetailRequestHonorsContextCancellation(t *testing.T) {
	s := New(time.Minute)
	started, release := make(chan struct{}), make(chan struct{})
	s.fetchFn = func(c *db.CatalogItem) (*Result, error) {
		close(started)
		<-release
		return &Result{Video: &model.Video{GlobalId: c.GlobalID}, Catalog: c}, nil
	}
	catalog := &db.CatalogItem{SourceKey: "s", GlobalID: 1}
	go func() { _, _ = s.getCatalog(catalog) }()
	<-started
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := s.getCatalogContext(ctx, catalog)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v, want context cancellation", err)
	}
	close(release)
}
