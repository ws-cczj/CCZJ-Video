package operation

import (
	"context"
	"testing"
	"time"
)

func TestMetricsKeepModesSeparateAndClassifyCancellation(t *testing.T) {
	var metrics Metrics
	metrics.Record(Result{Meta: Meta{Name: "video.detail", Mode: "video"}, Duration: 3 * time.Millisecond})
	metrics.Record(Result{Meta: Meta{Name: "reading.chapter", Mode: "novel"}, Duration: 2 * time.Millisecond, Err: context.Canceled})
	rows := metrics.Snapshot("novel")
	if len(rows) != 1 || rows[0].Cancelled != 1 || rows[0].TotalMS != 2 {
		t.Fatal(rows)
	}
	if len(metrics.Snapshot("comic")) != 0 {
		t.Fatal("inactive mode observations leaked")
	}
}
