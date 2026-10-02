package update

import (
	"os"
	"testing"

	"cczjVideo/app/applog"
	"cczjVideo/app/updater"
)

// TestMain 抢在任何 applog 调用之前把单例绑到临时目录。
// applog.Default() 会在单例为空时用 %APPDATA% 的生产目录把它建出来，而这里的
// 校正分支正是要写警告日志的，不绑就会灌进用户真实的日志文件。
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "cczj-update-test-")
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

// setCompiledVersion 把编译版本临时改成用例想要的值。Version 是 ldflags 注入点，
// 测试二进制里恒为 "dev"，不改就没法构造「标记比本体高」这个真实故障形状。
func setCompiledVersion(t *testing.T, v string) {
	t.Helper()
	old := updater.Version
	updater.Version = v
	t.Cleanup(func() { updater.Version = old })
}

// 装失败时不能替失败保密：标记谎称装上了 2.2.1，而本体仍是 2.1.0，
// 版本号必须回到本体，下一次检查才会重新把这个更新提示出来。
func TestReconcileInstalledMarkerDiscardsUninstalledVersion(t *testing.T) {
	setCompiledVersion(t, "2.1.0")
	updater.RecordInstalledVersion("2.2.1")
	t.Cleanup(updater.ClearInstalledVersion)

	if got := updater.EffectiveVersion(); got != "2.2.1" {
		t.Fatalf("前提不成立：装完那一刻的 EffectiveVersion = %q, 期望 %q", got, "2.2.1")
	}

	if got := reconcileInstalledMarker(); got != "2.1.0" {
		t.Fatalf("返回的运行版本 = %q, 期望本体的 %q", got, "2.1.0")
	}
	if got := updater.EffectiveVersion(); got != "2.1.0" {
		t.Fatalf("标记没被清掉，EffectiveVersion 仍是 %q", got)
	}
}

// 装成功时同样要清标记：此刻本体已经是新版本，留着只会让下一次启动误判。
func TestReconcileInstalledMarkerClearsMatchedVersion(t *testing.T) {
	setCompiledVersion(t, "2.2.1")
	updater.RecordInstalledVersion("2.2.1")
	t.Cleanup(updater.ClearInstalledVersion)

	if got := reconcileInstalledMarker(); got != "2.2.1" {
		t.Fatalf("返回的运行版本 = %q, 期望 %q", got, "2.2.1")
	}
	if _, ok := installMarkerPresent(); ok {
		t.Fatal("版本已匹配，标记应当被清除")
	}
}

// 回滚到更低的编译版本时，旧标记不该把版本号顶上去。
func TestReconcileInstalledMarkerIgnoresLowerMarker(t *testing.T) {
	setCompiledVersion(t, "2.3.0")
	updater.RecordInstalledVersion("2.2.1")
	t.Cleanup(updater.ClearInstalledVersion)

	if got := reconcileInstalledMarker(); got != "2.3.0" {
		t.Fatalf("返回的运行版本 = %q, 期望本体的 %q", got, "2.3.0")
	}
}

func installMarkerPresent() (string, bool) {
	// EffectiveVersion 高于编译版本，就只有「标记还在且更高」一种可能。
	eff, compiled := updater.EffectiveVersion(), updater.Version
	if eff == compiled {
		return "", false
	}
	return eff, true
}
