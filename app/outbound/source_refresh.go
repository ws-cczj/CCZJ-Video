package outbound

import (
	"context"
	"errors"
	"fmt"
	"time"

	"cczjVideo/app/parallel"
)

type sourceHTTPError struct{ status int }

func (e sourceHTTPError) Error() string { return fmt.Sprintf("proxy source HTTP %d", e.status) }

func sourceErrorCode(err error) string {
	var status sourceHTTPError
	switch {
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, errSourceChallenge):
		return "verification"
	case errors.Is(err, errSourceFormat):
		return "format"
	case errors.As(err, &status):
		return fmt.Sprintf("http_%d", status.status)
	default:
		return "unavailable"
	}
}

func sourceBackoff(interval time.Duration, failures int) time.Duration {
	if interval >= 6*time.Hour {
		return interval
	}
	for i := 1; i < failures && interval < 6*time.Hour; i++ {
		interval *= 2
	}
	if interval > 6*time.Hour {
		interval = 6 * time.Hour
	}
	return interval
}

func (p *Pool) updateSources(ctx context.Context, lists []candidateBatch) ([]candidateBatch, []error) {
	// Source downloads have a shared deadline as well as an individual limit,
	// leaving time in the refresh budget for independent node validation.
	downloadCtx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	results := make([]error, len(p.sources))
	_ = parallel.Each(downloadCtx, len(p.sources), 3, func(ctx context.Context, i int) error {
		s := p.sources[i]
		if !s.config.Enabled || s.status.NextAttempt > time.Now().Unix() {
			return nil
		}
		sourceCtx, stop := context.WithTimeout(ctx, 12*time.Second)
		defer stop()
		raw, err := p.downloadSource(sourceCtx, s.config.URL)
		var rows []candidate
		if err == nil {
			rows, err = decodeSource(s.config, raw)
		}
		p.mu.Lock()
		defer p.mu.Unlock()
		interval := time.Duration(s.config.RefreshMinutes) * time.Minute
		if err != nil {
			results[i] = err
			if s.status.Failures < 16 {
				s.status.Failures++
			}
			s.status.State = "error"
			s.status.LastError = sourceErrorCode(err)
			s.status.NextAttempt = time.Now().Add(sourceBackoff(interval, s.status.Failures)).Unix()
			return nil
		}
		s.rows = rows
		s.updated = time.Now()
		s.status.Candidates = len(rows)
		s.status.LastSuccess = s.updated.Unix()
		s.status.NextAttempt = s.updated.Add(interval).Unix()
		s.status.Failures = 0
		s.status.LastError = ""
		s.status.State = "ready"
		if len(rows) == 0 {
			s.status.State = "empty"
		}
		return nil
	})
	failures := []error{}
	for i, s := range p.sources {
		if results[i] != nil {
			failures = append(failures, results[i])
		}
		if !s.config.Enabled || len(s.rows) == 0 || time.Since(s.updated) > 2*time.Hour {
			continue
		}
		// Rotate even when the downloaded list is identical. Dead leading rows
		// must not monopolize every subsequent validation batch.
		start := s.cursor % len(s.rows)
		list := append([]candidate(nil), s.rows[start:]...)
		list = append(list, s.rows[:start]...)
		lists = append(lists, candidateBatch{rows: list, source: s})
	}
	return lists, failures
}

type candidateBatch struct {
	rows   []candidate
	source *sourceState
}

func (p *Pool) validationBatch(lists []candidateBatch) []candidate {
	if len(lists) == 0 {
		return nil
	}
	start := p.batchCursor % len(lists)
	p.batchCursor = (start + 1) % len(lists)
	ordered := append([]candidateBatch(nil), lists[start:]...)
	ordered = append(ordered, lists[:start]...)
	return selectCandidates(ordered, p.config.MaxCandidates)
}

func selectCandidates(lists []candidateBatch, limit int) []candidate {
	rows := []candidate{}
	seen := map[string]bool{}
	consumed := make([]int, len(lists))
	defer func() {
		for i, batch := range lists {
			if batch.source != nil && len(batch.rows) > 0 {
				batch.source.cursor = (batch.source.cursor + consumed[i]) % len(batch.rows)
			}
		}
	}()
	for index := 0; len(rows) < limit; index++ {
		more := false
		for j, batch := range lists {
			list := batch.rows
			if index >= len(list) {
				continue
			}
			more = true
			consumed[j] = index + 1
			c := list[index]
			if !seen[c.URL] {
				seen[c.URL] = true
				rows = append(rows, c)
			}
			if len(rows) >= limit {
				break
			}
		}
		if !more {
			break
		}
	}
	return rows
}
