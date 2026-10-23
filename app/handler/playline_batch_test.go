package handler

import (
	"cczjVideo/app/model"
	"context"
	"errors"
	"sync/atomic"
	"testing"
)

func TestProbeLinesBoundsNetworkAndRetainsRowsOnCancellation(t *testing.T) {
	lines := make([]*model.PlayLine, 100)
	for i := range lines {
		lines[i] = line(i, "line", "https://example.invalid/video.mp4")
	}
	ctx, cancel := context.WithCancel(context.Background())
	entered := make(chan struct{}, 4)
	var calls atomic.Int32
	done := make(chan struct{})
	var items []*PlayLineSpeedItem
	var err error
	go func() {
		items, err = probePlayLines(ctx, lines, 1, func(ctx context.Context, line *model.PlayLine, episode *model.Episode) *PlayLineSpeedItem {
			calls.Add(1)
			entered <- struct{}{}
			<-ctx.Done()
			return &PlayLineSpeedItem{Index: line.Index, Name: line.Name, EpNum: episode.EpNum}
		})
		close(done)
	}()
	for range 4 {
		<-entered
	}
	cancel()
	<-done
	if !errors.Is(err, context.Canceled) || calls.Load() != 4 || len(items) != 100 {
		t.Fatal(calls.Load(), len(items), err)
	}
	for i, item := range items {
		if item == nil || item.Index != i {
			t.Fatal(i, item)
		}
	}
}
