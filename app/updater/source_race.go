package updater

import (
	"context"
	"fmt"
	"sync"
	"time"

	"cczjVideo/app/parallel"
)

// Metadata mirrors compete in a finite, owned batch. A blocked GitHub route
// must not consume the whole manual-check deadline before mirrors are tried.
func firstUpdateSource[T any](ctx context.Context, urls []string, fetch func(context.Context, string) (*T, error)) (*T, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var mu sync.Mutex
	var result *T
	var last error
	err := parallel.Each(ctx, len(urls), 3, func(ctx context.Context, i int) error {
		attempt, stop := context.WithTimeout(ctx, 4*time.Second)
		defer stop()
		value, err := fetch(attempt, urls[i])
		mu.Lock()
		defer mu.Unlock()
		if err == nil && value != nil {
			if result == nil {
				result = value
				cancel()
			}
		} else if err != nil {
			last = err
		}
		return nil
	})
	if result != nil {
		return result, nil
	}
	if err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("all update metadata sources failed: %w", last)
}
