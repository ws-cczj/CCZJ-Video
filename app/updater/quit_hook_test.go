package updater

import "testing"

// 安装更新的退出通道只由 quitHook 承载：硬退出会跳过应用收尾（取消并等待采集与下载、
// 关日志、关库），丢掉的是正在提交的那一批数据。这里盯住"注入了钩子就一定走钩子"。
func TestExitProcessPrefersQuitHook(t *testing.T) {
	called := 0
	quitHook = func() { called++ }
	t.Cleanup(func() { quitHook = nil })

	exitProcess()

	if called != 1 {
		t.Fatalf("退出钩子调用次数 = %d，期望 1", called)
	}
}
