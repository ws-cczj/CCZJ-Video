package douban

import (
	"testing"
	"time"

	"cczjVideo/app/db"
)

func TestChartDoesNotRefreshWithinSevenDaysAcrossRestart(t *testing.T) {
	for _, restart := range []bool{false, true} {
		t.Run(map[bool]string{false: "memory", true: "restart"}[restart], func(t *testing.T) {
			isolateChartCache(t)
			if err := db.InitDB(t.TempDir()); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(db.Close)
			at := time.Now().Add(-6 * 24 * time.Hour).Truncate(time.Second)
			items := []DoubanChartItem{chartItem("111111", "本周榜单")}
			setChartCache(items, at)
			if restart {
				saveChartSnapshot(items, at)
				setChartCache(nil, time.Time{})
			}
			if _, gotAt, ok := chartDisplayable(); !ok || !gotAt.Equal(at) {
				t.Fatalf("snapshot lost original fetch time: %s %v", gotAt, ok)
			}
			calls := 0
			fetchChartPage = func(string) (string, error) {
				calls++
				return "", nil
			}
			// 走真实刷新入口，确保重启恢复的六天缓存也不会触发网络。
			if _, err := refreshChart(true); err != nil || calls != 0 {
				t.Fatalf("chart refreshed before seven days: calls=%d err=%v", calls, err)
			}
			if _, err := FetchDoubanChart(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
