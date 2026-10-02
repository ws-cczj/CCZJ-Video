package collection

import (
	"os"
	"testing"
	"time"

	"cczjVideo/app/applog"
)

// TestMain 把 applog 单例绑到临时目录，避免测试灌进用户真实的日志文件。
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "cczj-collection-test-")
	if err != nil {
		panic(err)
	}
	if err := applog.Init(dir); err != nil {
		panic(err)
	}
	code := m.Run()
	applog.Default().Close()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// 豆瓣的间隔下限是限速闸门的一部分，不能因为谁传了个 0 或负数就变成忙等。
// 门面还得把夹取后的真实值回传，否则设置项显示 1 秒、实际跑 1 分钟。
func TestSetDoubanIntervalNeverGoesBelowTheFloor(t *testing.T) {
	svc := NewSchedulerService(5 * time.Minute)

	for _, in := range []time.Duration{0, -time.Hour, time.Second, 30 * time.Second} {
		if got := svc.SetDoubanInterval(in); got < time.Minute {
			t.Fatalf("传入 %v 得到 %v，低于 1 分钟下限", in, got)
		}
	}

	if got := svc.SetDoubanInterval(10 * time.Minute); got != 10*time.Minute {
		t.Fatalf("放宽到 10 分钟应原样生效，得到 %v", got)
	}
	interval, running, _, _ := svc.DoubanSchedule()
	if interval != 10*time.Minute {
		t.Fatalf("DoubanSchedule 与 SetDoubanInterval 不一致: %v", interval)
	}
	if running {
		t.Fatal("没调用过 Start，不应报告在运行")
	}
}

// 关停顺序不保证 Start 跑过：构造完直接 Stop 也得活着回来。
// 采集调度器在这时还是 nil，解引用就是一次启动崩溃。
func TestStopBeforeStartDoesNotPanic(t *testing.T) {
	NewSchedulerService(time.Minute).Stop()
}
