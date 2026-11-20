package lifecycle

import (
	"context"
	"errors"
	"fmt"
)

// Closers releases resources in reverse registration order. The owner
// serializes registration and shutdown, and can retry failed closers.
type Closers struct {
	entries []closer
}

type closer struct {
	name  string
	close func(context.Context) error
}

func (s *Closers) Add(name string, close func(context.Context) error) {
	s.entries = append(s.entries, closer{name: name, close: close})
}

func (s *Closers) Close(ctx context.Context) error {
	var failures error
	for i := len(s.entries) - 1; i >= 0; i-- {
		entry := &s.entries[i]
		if entry.close == nil {
			continue
		}
		if err := entry.close(ctx); err != nil {
			failures = errors.Join(failures, fmt.Errorf("close %s: %w", entry.name, err))
		} else {
			entry.close = nil
		}
	}
	if failures == nil {
		s.entries = nil
	}
	return failures
}
