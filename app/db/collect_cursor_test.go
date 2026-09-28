package db

import (
	"testing"
	"time"
)

func TestIncrementalWindowDerivesHoursFromCursor(t *testing.T) {
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name      string
		cursor    CollectCursor
		wantHours int
		wantFull  bool
	}{
		{
			name:     "从未成功采集过只能全量",
			cursor:   CollectCursor{},
			wantFull: true,
		},
		{
			name:      "刚刚覆盖到现在也要留边界余量",
			cursor:    CollectCursor{CoveredUntilUnix: now.Unix()},
			wantHours: cursorWindowOverlap + 1,
		},
		{
			name:      "停机一小时向上取整并重叠",
			cursor:    CollectCursor{CoveredUntilUnix: now.Add(-time.Hour).Unix()},
			wantHours: 2 + cursorWindowOverlap,
		},
		{
			name:      "不足一小时也算一小时",
			cursor:    CollectCursor{CoveredUntilUnix: now.Add(-10 * time.Minute).Unix()},
			wantHours: 1 + cursorWindowOverlap,
		},
		{
			name:      "时钟回拨按最小窗口而不是负数",
			cursor:    CollectCursor{CoveredUntilUnix: now.Add(2 * time.Hour).Unix()},
			wantHours: 1 + cursorWindowOverlap,
		},
		{
			name:     "缺口大到上限就退回全量",
			cursor:   CollectCursor{CoveredUntilUnix: now.Add(-40 * 24 * time.Hour).Unix()},
			wantFull: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hours, full := IncrementalWindow(tt.cursor, now)
			if full != tt.wantFull {
				t.Fatalf("full = %v, want %v", full, tt.wantFull)
			}
			if !full && hours != tt.wantHours {
				t.Fatalf("hours = %d, want %d", hours, tt.wantHours)
			}
			if full && hours != 0 {
				t.Fatalf("full window must report no hours, got %d", hours)
			}
		})
	}
}

func TestCollectCursorPersistsAcrossReadsAndOnlyAdvances(t *testing.T) {
	if err := InitDB(boundaryTestDBDir); err != nil {
		t.Fatal(err)
	}
	const sourceKey = "cursor_test"
	defer ResetCollectCursor(DB(), sourceKey)

	if err := ResetCollectCursor(DB(), sourceKey); err != nil {
		t.Fatal(err)
	}
	got, err := GetCollectCursor(sourceKey)
	if err != nil {
		t.Fatal(err)
	}
	if got.SourceKey != sourceKey || got.CoveredUntilUnix != 0 || got.LastAttemptUnix != 0 {
		t.Fatalf("missing cursor should read as zero value, got %+v", got)
	}

	base := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	if err := MarkCollectAttempt(sourceKey, base); err != nil {
		t.Fatal(err)
	}
	got, _ = GetCollectCursor(sourceKey)
	if got.LastAttemptUnix != base.Unix() {
		t.Fatalf("last_attempt_unix = %d, want %d", got.LastAttemptUnix, base.Unix())
	}
	if got.CoveredUntilUnix != 0 {
		t.Fatal("attempt alone must not move the watermark forward")
	}

	if err := AdvanceCollectCursor(sourceKey, base.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	got, _ = GetCollectCursor(sourceKey)
	if got.CoveredUntilUnix != base.Add(time.Hour).Unix() {
		t.Fatalf("covered_until_unix = %d", got.CoveredUntilUnix)
	}

	// 重放或并发下迟到的旧窗口不能把水位线拉回去，否则已覆盖的时间会被永久跳过。
	if err := AdvanceCollectCursor(sourceKey, base); err != nil {
		t.Fatal(err)
	}
	got, _ = GetCollectCursor(sourceKey)
	if got.CoveredUntilUnix != base.Add(time.Hour).Unix() {
		t.Fatalf("stale window moved the watermark back to %d", got.CoveredUntilUnix)
	}

	if err := ResetCollectCursor(DB(), sourceKey); err != nil {
		t.Fatal(err)
	}
	got, _ = GetCollectCursor(sourceKey)
	if got.CoveredUntilUnix != 0 || got.LastAttemptUnix != 0 {
		t.Fatalf("reset left %+v", got)
	}
}
