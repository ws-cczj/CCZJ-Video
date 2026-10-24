package library

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCloseCancelsAllReadingKindsBeforeWaitingForAStuckRead(t *testing.T) {
	s := NewService(func() string { return t.TempDir() })
	text, pages, trends := make(chan context.Context, 1), make(chan context.Context, 1), make(chan context.Context, 1)
	release := make(chan struct{})
	go func() {
		_, _ = s.textReads.Do(context.Background(), "text", func(ctx context.Context) (*ChapterText, error) { text <- ctx; <-release; return nil, ctx.Err() })
	}()
	go func() {
		_, _ = s.pageReads.Do(context.Background(), "pages", func(ctx context.Context) (*ChapterPages, error) { pages <- ctx; <-ctx.Done(); return nil, ctx.Err() })
	}()
	go func() {
		_, _ = s.trendReads.Do(context.Background(), "trends", func(ctx context.Context) (*ComicTrendPage, error) { trends <- ctx; <-ctx.Done(); return nil, ctx.Err() })
	}()
	textCtx, pageCtx, trendCtx := <-text, <-pages, <-trends
	closed := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	defer func() {
		close(release)
		cleanup, stop := context.WithTimeout(context.Background(), time.Second)
		defer stop()
		_ = s.Close(cleanup)
	}()
	go func() { closed <- s.Close(ctx) }()
	select {
	case <-textCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("close did not cancel the first reader")
	}
	for _, other := range []context.Context{pageCtx, trendCtx} {
		select {
		case <-other.Done():
		case <-time.After(200 * time.Millisecond):
			t.Fatal("stuck text reader delays cancellation of comic pages or trends")
		}
	}
	cancel()
	if err := <-closed; err == nil {
		t.Fatal("close reported a drained reader while it is still blocked")
	}
}

func TestCancelledReadingRequestStopsBeforeCatalogLookup(t *testing.T) {
	s := setupLibraryTest(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, call := range []func() error{
		func() error { _, err := s.Detail(ctx, 123, false); return err },
		func() error { _, err := s.ChapterText(ctx, 123); return err },
		func() error { _, err := s.ChapterPages(ctx, 123); return err },
	} {
		if err := call(); !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled request reached catalog lookup: %v", err)
		}
	}
}
