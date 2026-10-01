package service

import (
	"testing"
	"time"

	"cczjVideo/app/db"
	"cczjVideo/app/model"
)

// 探测节流的判定必须是纯函数：它决定的是"要不要对一个外部源站发请求"，
// 一旦要连数据库才能测，最容易错的边界（刚好到点、刚好差一秒）反而测不到。
func TestProbeRetryAfter(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	interval := sourceProbeMinInterval

	cases := []struct {
		name        string
		lastProbe   time.Time
		wantAllowed bool
		wantSeconds int
	}{
		{name: "从未探测", lastProbe: time.Time{}, wantAllowed: true},
		{name: "刚探过", lastProbe: now, wantSeconds: 300},
		{name: "差一秒到点", lastProbe: now.Add(-interval + time.Second), wantSeconds: 1},
		{name: "差 1.2 秒到点向上取整", lastProbe: now.Add(-interval + 1200*time.Millisecond), wantSeconds: 2},
		{name: "正好到点", lastProbe: now.Add(-interval), wantAllowed: true},
		{name: "超过间隔", lastProbe: now.Add(-interval - time.Hour), wantAllowed: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			seconds, allowed := probeRetryAfter(now, tc.lastProbe, interval)
			if allowed != tc.wantAllowed {
				t.Fatalf("allowed = %v, want %v", allowed, tc.wantAllowed)
			}
			if !tc.wantAllowed && seconds != tc.wantSeconds {
				t.Fatalf("retryAfter = %d 秒, want %d 秒", seconds, tc.wantSeconds)
			}
			if tc.wantAllowed && seconds != 0 {
				t.Fatalf("放行时不该还带等待秒数，得到 %d", seconds)
			}
		})
	}
}

// 冷却中的结果不能是空的：界面拿到 ok=false 又没有任何说明，
// 读起来就像"这个源挂了"，而实际只是"还没到可以再戳的时候"。
func TestCoolingProbeCarriesLastSample(t *testing.T) {
	source := &model.Source{
		SourceKey: "yyzy_tv",
		Name:      "永艺资源",
		ApiUrl:    "https://example.invalid/api.php/provide/vod/",
		Enabled:   1,
	}
	last := db.SourceHealth{
		SourceKey:      source.SourceKey,
		LastOK:         true,
		LastError:      "上一次超时",
		LastSampleUnix: time.Unix(1759046400, 0).Unix(),
	}

	probe := coolingProbe(source, last, 137)

	if !probe.Skipped {
		t.Fatal("冷却中的结果必须标记 skipped")
	}
	if probe.RetryAfterSec != 137 {
		t.Fatalf("retry_after_sec = %d, want 137", probe.RetryAfterSec)
	}
	if probe.ProbeTimeUnix != last.LastSampleUnix {
		t.Fatalf("probe_time_unix = %d, want 上次样本时间 %d", probe.ProbeTimeUnix, last.LastSampleUnix)
	}
	if !probe.OK || probe.Error != "上一次超时" {
		t.Fatalf("状态应沿用上次样本，得到 ok=%v err=%q", probe.OK, probe.Error)
	}
	// 冷却时没发请求，就不该有状态码和延迟——带上会被读成本次实测值。
	if probe.StatusCode != 0 || probe.LatencyMS != 0 {
		t.Fatalf("冷却结果不应带本次状态码/延迟，得到 %d / %dms", probe.StatusCode, probe.LatencyMS)
	}
	if probe.URL == "" {
		t.Fatal("冷却结果仍要带上探测地址，界面得知道戳的是哪个接口")
	}
	if source.Enabled == 1 && !probe.Enabled {
		t.Fatal("enabled 标记要跟着源本身走，不能因为冷却就报成未启用")
	}
}

// 点阵的分桶边界是这套可视化唯一会读错的地方：同一格里挑哪条、窗口外的样本算不算，
// 直接决定用户看到的是"这半小时一直不通"还是"刚通了一次"。
func TestBucketProbeSamples(t *testing.T) {
	const (
		slotSecs    = int64(300)
		count       = 10
		firstBucket = int64(100) // 槽号，不是时间戳：窗口起点是 firstBucket*slotSecs
	)

	sample := func(bucket int, ok bool, latency int64, errText string) db.SourceHealthSample {
		return db.SourceHealthSample{
			TsUnix:    (firstBucket + int64(bucket)) * slotSecs,
			OK:        ok,
			LatencyMS: latency,
			Err:       errText,
		}
	}

	t.Run("空样本全是未探测", func(t *testing.T) {
		slots := bucketProbeSamples(nil, firstBucket, slotSecs, count)
		if len(slots) != count {
			t.Fatalf("点数 = %d, want %d", len(slots), count)
		}
		for i, slot := range slots {
			if slot.Probed {
				t.Fatalf("第 %d 个点不该有探测结果", i)
			}
			if want := (firstBucket + int64(i)) * slotSecs; slot.BucketUnix != want {
				t.Fatalf("第 %d 个点的窗口起点 = %d, want %d", i, slot.BucketUnix, want)
			}
		}
	})

	t.Run("同格保留最新且窗口外忽略", func(t *testing.T) {
		// 样本从库里出来是"新→旧"，所以同格里第二条是更旧的那条。
		slots := bucketProbeSamples([]db.SourceHealthSample{
			sample(9, true, 120, ""),
			sample(9, false, 999, "旧的失败"),
			sample(4, false, 0, "不应答"),
			sample(-1, true, 1, "窗口外"),
			sample(count, true, 1, "窗口外"),
		}, firstBucket, slotSecs, count)

		if !slots[9].Probed || !slots[9].OK || slots[9].LatencyMS != 120 {
			t.Fatalf("同格应保留最新那条，得到 %+v", slots[9])
		}
		if slots[9].Error != "" {
			t.Fatalf("同格更旧那条的错误不该漏进来：%q", slots[9].Error)
		}
		if !slots[4].Probed || slots[4].OK || slots[4].Error != "不应答" {
			t.Fatalf("失败点要带错误文本，得到 %+v", slots[4])
		}
		probed := 0
		for _, slot := range slots {
			if slot.Probed {
				probed++
			}
		}
		if probed != 2 {
			t.Fatalf("窗口外的样本不该画出一个点，probed = %d, want 2", probed)
		}
	})
}
