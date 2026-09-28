package collect

import (
	"errors"
	"strings"
	"testing"
)

func TestErrorKindClassifiesRunOutcome(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"no error", nil, ""},
		{"fetch only", &CollectError{Stats: &RunStats{FetchFailures: []int{3}}}, "fetch"},
		{"save only", &CollectError{Stats: &RunStats{SaveFailures: []int{2, 5}}}, "save"},
		{"both", &CollectError{Stats: &RunStats{FetchFailures: []int{3}, SaveFailures: []int{5}}}, "partial"},
		{"fatal", errors.New("第 1 页采集失败"), "fatal"},
	}
	for _, tc := range cases {
		if got := ErrorKind(tc.err); got != tc.want {
			t.Errorf("%s: ErrorKind = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestCollectErrorListsEveryMissingPage(t *testing.T) {
	msg := (&CollectError{Stats: &RunStats{
		Saved:         40,
		FetchFailures: []int{3, 7},
		SaveFailures:  []int{9},
	}}).Error()

	for _, want := range []string{"2 页取页失败: 3,7", "1 页入库失败: 9", "已入库 40 条"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error text missing %q: %s", want, msg)
		}
	}
}

func TestRunStatsUnfinishedIgnoresEmptyPages(t *testing.T) {
	// 空页只是提示，不代表数据丢失；只有整页取不到或写不进才算未完成。
	if (&RunStats{EmptyPages: []int{4}}).unfinished() {
		t.Error("empty pages alone must not mark the run unfinished")
	}
	if !(&RunStats{SaveFailures: []int{4}}).unfinished() {
		t.Error("save failures must mark the run unfinished")
	}
}
