package update

import (
	"os"
	"path/filepath"
	"testing"

	"cczjVideo/app/settings"
	"cczjVideo/app/updater"
)

// 这一档的全部状态就是库里的「pending_install」那一条记录：点下按钮时写下，
// 进程最后一次收尾时兑现。测试因此只盯两件事——写进去读得回来，以及兑现之后真的没了。

func newDBBackedService(t *testing.T) *Service {
	t.Helper()
	// settings 就是那张表，这里打真库（TestMain 已经把库开在临时目录里）。
	return NewService(settings.NewService(), nil)
}

// 点下「退出时安装」不该装任何东西，更不该把来路不明的路径留在库里：
// 那条记录几天后会被关停流程拿去 move 本体的 exe。
func TestScheduleInstallOnExitRefusesNonArtifacts(t *testing.T) {
	dir := t.TempDir()
	foreign := filepath.Join(dir, "cczjVideo.exe")
	if err := os.WriteFile(foreign, []byte("不是本更新器产出的字节"), 0644); err != nil {
		t.Fatal(err)
	}
	s := newDBBackedService(t)
	t.Cleanup(func() { _ = s.clearPendingInstall() })

	for _, path := range []string{"", foreign} {
		if err := s.ScheduleInstallOnExit(path, "9.9.9"); err == nil {
			t.Fatalf("路径 %q 被当成可安装的包收下了", path)
		}
		if got := s.PendingInstallInfo(); got.Available {
			t.Fatalf("被拒绝的安排还是留下了记录: %+v", got)
		}
	}
}

// 安排要跨得过"点完按钮"和"真正退出"之间那几天，所以写进去必须读得回来；
// 撤掉也必须真的没了——界面那个按钮的文案全押在这个值上。
func TestPendingInstallRecordRoundTrips(t *testing.T) {
	s := newDBBackedService(t)
	t.Cleanup(func() { _ = s.clearPendingInstall() })

	record := pendingInstallRecord{Path: `C:\Apps\CCZJ Video\cczj_update_9.exe`, Version: "2.3.2"}
	if err := s.writePendingInstall(record); err != nil {
		t.Fatal(err)
	}
	got := s.PendingInstallInfo()
	if !got.Available || got.Path != record.Path || got.Version != record.Version {
		t.Fatalf("读回来的安排 = %+v, 期望 %+v", got, record)
	}

	if err := s.CancelInstallOnExit(); err != nil {
		t.Fatal(err)
	}
	if s.PendingInstallInfo().Available {
		t.Fatal("取消之后安排还在")
	}
	raw, err := s.settings.Get(settingPendingInstall)
	if err != nil {
		t.Fatal(err)
	}
	if raw != "" {
		t.Fatalf("取消之后原始记录仍是 %q, 期望空", raw)
	}
}

// 这条记录可能是别的版本写的、也可能被人改坏。解不开就当没有安排：
// 关停流程拿半截 JSON 去 move 本体，换来的是一次装坏了的应用。
func TestUnreadablePendingInstallReadsAsNone(t *testing.T) {
	s := newDBBackedService(t)
	t.Cleanup(func() { _ = s.clearPendingInstall() })

	for _, raw := range []string{"{不是 JSON", `{"path":"","version":"2.3.2"}`} {
		if err := s.settings.Set(settingPendingInstall, raw); err != nil {
			t.Fatal(err)
		}
		if s.PendingInstallInfo().Available {
			t.Fatalf("记录 %q 被当成了安排", raw)
		}
		if err := s.InstallOnShutdown(false); err != nil {
			t.Fatalf("没有安排时关停不该报错: %v", err)
		}
	}
}

// 兑现时刻的关键一条：安排一定被消费掉，脚本接不接得住是另一件事。
// 留着它，下次退出会把同一个包再换一遍；而这次到底换没换成，由脚本回执说话。
func TestInstallOnShutdownConsumesTheArrangement(t *testing.T) {
	s := newDBBackedService(t)
	t.Cleanup(func() { _ = s.clearPendingInstall() })

	missing := filepath.Join(t.TempDir(), "cczj_update_gone.exe")
	if err := s.writePendingInstall(pendingInstallRecord{Path: missing, Version: "9.9.9"}); err != nil {
		t.Fatal(err)
	}
	if err := s.InstallOnShutdown(false); err == nil {
		t.Fatal("装一个不在磁盘上的包却返回了成功")
	}
	if s.PendingInstallInfo().Available {
		t.Fatal("兑现失败之后安排还留着，下次退出会再换一遍")
	}
}

// 重启不算「用户关掉了应用」：RestartApp 已经把同一个 exe 又拉起一份，新进程立刻
// 重新锁住那个文件，这时候换不动，还会留下一条用户从没取消过的失败回执。
func TestInstallOnShutdownKeepsArrangementForRelaunch(t *testing.T) {
	s := newDBBackedService(t)
	t.Cleanup(func() { _ = s.clearPendingInstall() })

	if err := s.writePendingInstall(pendingInstallRecord{Path: `C:\Apps\cczj_update_9.exe`, Version: "9.9.9"}); err != nil {
		t.Fatal(err)
	}
	if err := s.InstallOnShutdown(true); err != nil {
		t.Fatalf("这次退出是重启，兑现流程不该报任何错: %v", err)
	}
	if !s.PendingInstallInfo().Available {
		t.Fatal("这次是重启，安排却被消费掉了")
	}
}

// 跑着的已经是当初安排要装的那个版本，说明它已经生效（或者用户自己覆盖过 exe）：
// 这条安排就再没有下次退出可兑现了，留着只会把同一个包再换一遍。
func TestReconcilePendingInstallDropsFulfilledArrangement(t *testing.T) {
	s := newDBBackedService(t)
	t.Cleanup(func() { _ = s.clearPendingInstall() })

	if err := s.writePendingInstall(pendingInstallRecord{Path: "cczj_update.exe", Version: updater.Version}); err != nil {
		t.Fatal(err)
	}
	s.reconcilePendingInstall()
	if s.PendingInstallInfo().Available {
		t.Fatal("已经生效的安排没被清掉")
	}

	// 版本还没换到那里：这条安排得留着，等一次真正的退出。
	if err := s.writePendingInstall(pendingInstallRecord{Path: "cczj_update.exe", Version: "9.9.9"}); err != nil {
		t.Fatal(err)
	}
	s.reconcilePendingInstall()
	if !s.PendingInstallInfo().Available {
		t.Fatal("还没生效就被清掉了")
	}
}

// 「现在就装」取代「退出时装」：安排必须一起清掉，否则装完之后那次退出
// 会把同一个包再 move 一遍——而那时 exe 旁边站着的是刚换好的新版本。
func TestInstallSupersedesPendingOnExitArrangement(t *testing.T) {
	s := newDBBackedService(t)
	t.Cleanup(func() { _ = s.clearPendingInstall() })

	if err := s.writePendingInstall(pendingInstallRecord{Path: `C:\Apps\cczj_update_9.exe`, Version: "9.9.9"}); err != nil {
		t.Fatal(err)
	}
	// 安装本身会因为这个路径不是更新产物而失败，这里只看安排有没有被取代。
	if err := s.Install(`C:\Windows\notepad.exe`); err == nil {
		t.Fatal("前提不成立：非更新产物的路径居然被接受了")
	}
	if s.PendingInstallInfo().Available {
		t.Fatal("立即安装之后安排还留着")
	}
}
