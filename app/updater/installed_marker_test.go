package updater

import (
	"testing"
)

// 安装标记写在应用目录，启动时读一次就被清掉；诊断台要在清理之后还能报出"上次安装
// 声称换成了哪个版本"。以前诊断台去数据目录找同名文件，那个位置从来没人写过，字段
// 一直空着——现场证据等于没有。
func TestInstalledMarkerStaysReadableAfterTheStartupClear(t *testing.T) {
	original := Version
	Version = "2.2.0"
	t.Cleanup(func() { Version = original })
	t.Cleanup(ClearInstalledVersion)
	if got := InstalledMarkerSeenThisRun(); got != "" {
		// 只为本用例准备：包级状态被更早的用例写过就不好判断断言在验证谁。
		t.Skipf("本用例要求进程内还没读过标记，已有值 %q", got)
	}

	RecordInstalledVersion("2.2.1")
	if got := EffectiveVersion(); got != "2.2.1" {
		t.Fatalf("EffectiveVersion = %q, 期望标记把版本顶上去", got)
	}
	ClearInstalledVersion()

	if got := InstalledMarkerSeenThisRun(); got != "2.2.1" {
		t.Fatalf("InstalledMarkerSeenThisRun = %q, 期望清理后仍读到 %q", got, "2.2.1")
	}
	// 标记没了之后再读一次，不能被空值抹掉：那是诊断台唯一还留着的那次安装证据。
	if got := getInstalledVersion(); got != "" {
		t.Fatalf("getInstalledVersion after clear = %q, 期望空", got)
	}
	if got := InstalledMarkerSeenThisRun(); got != "2.2.1" {
		t.Fatalf("证据被后续的空读取抹掉了: %q", got)
	}
}
