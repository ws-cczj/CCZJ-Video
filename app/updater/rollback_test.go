package updater

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// 没有副本时必须在落脚本之前就失败：这个入口一被调用，进程半秒后就要让位，
// 前置校验漏过去，用户看到的就是"点了按钮，程序没了，什么都没换"。
func TestRollbackUpdateRefusesWithoutABackup(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(exe + swapBackupSuffix); statErr == nil {
		t.Skipf("测试二进制旁边恰好有 %s 副本，这一条测不了", swapBackupSuffix)
	}

	err = RollbackUpdate()
	if err == nil {
		t.Fatal("没有回滚副本时 RollbackUpdate 不该返回成功")
	}
	switch runtime.GOOS {
	case "windows":
		if !strings.Contains(err.Error(), "NOT_FOUND") {
			t.Fatalf("错误 = %q, 期望带 NOT_FOUND 码", err)
		}
	default:
		if !strings.Contains(err.Error(), "UNAVAILABLE") {
			t.Fatalf("错误 = %q, 期望带 UNAVAILABLE 码", err)
		}
	}
	// 拒绝要拒在落笔之前：脚本文件不该出现在这里。
	if _, statErr := os.Stat(filepath.Join(filepath.Dir(exe), rollbackScriptName)); statErr == nil {
		t.Fatal("校验失败却已经写了回滚脚本")
	}
}

// 副本判得越严，界面就越不会把一个换不回去的文件端上来。
func TestSwapBackupStatReportsOnlyUsableCopies(t *testing.T) {
	path, info, ok := SwapBackupStat()
	if ok && (path == "" || info == nil || info.Size() == 0) {
		t.Fatalf("报告可用却给不出可用现场: path=%q info=%v", path, info)
	}
}

// 回滚脚本是替换脚本的镜像，但它跑在一个更脆的时刻：用户已经确认新版本有问题。
// 这里钉住的是"失败不能更糟"这一条——一次退回失败不该让他连程序都打不开。
func TestBuildRollbackScriptShape(t *testing.T) {
	script := buildRollbackScript()

	for _, wire := range []string{`set "OLD=%~1"`, `set "BACK=%~2"`, `set "RESULT=%~3"`, `set "HANDOFF=%~4"`} {
		if !strings.Contains(script, wire) {
			t.Fatalf("脚本缺少参数接线 %q:\n%s", wire, script)
		}
	}
	for _, line := range []string{
		`echo rollback_ok>"%RESULT%"`,
		`echo rollback_failed>"%RESULT%"`,
	} {
		if !strings.Contains(script, line) {
			t.Fatalf("脚本缺少回执 %q:\n%s", line, script)
		}
	}
	// 副本是 move 走的，不是一进门就 del：换不回去时用户还得靠它再试一次。
	if strings.Contains(script, `del /f /q "%BACK%"`) {
		t.Fatalf("脚本直接删除了回滚副本:\n%s", script)
	}
	// 本体同样是 move 的目标而不是被删的对象。
	if strings.Contains(script, `del /f /q "%OLD%"`) {
		t.Fatalf("脚本删除了当前 exe:\n%s", script)
	}
	if !strings.Contains(script, "if %RETRY% GEQ 30 goto :BACK_FAILED") {
		t.Fatalf("重试没有上限:\n%s", script)
	}
	if strings.Contains(script, "timeout") {
		t.Fatalf("脚本用 timeout 等待，独立进程下它不会真等:\n%s", script)
	}
	if !strings.Contains(script, "ping -n 3 127.0.0.1 >NUL") {
		t.Fatalf("脚本没有可用的等待命令:\n%s", script)
	}
	// 成败都要把应用交回去，脚本自己收尾。
	if got := strings.Count(script, `start "" "%OLD%"`); got < 2 {
		t.Fatalf("start 出现 %d 次，期望至少 2 次（成功 + 失败）:\n%s", got, script)
	}
	// 交回去那一行还得带上单实例锁的交接参数，否则失败分支里"本进程还活着"这一条
	// 会让新实例被静默判成第二实例——点退回 → 程序关了 → 没回来。
	assertEveryStartHandsOff(t, script)
	if got := strings.Count(script, `del /f /q "%~f0"`); got != 2 {
		t.Fatalf("脚本自我清理出现 %d 次，期望 2 次:\n%s", got, script)
	}
	// 与替换脚本同一条规矩：正文必须纯 ASCII，路径全走参数。
	for _, r := range script {
		if r > 127 {
			t.Fatalf("脚本含非 ASCII 字符 %q", r)
		}
	}
}

// 回滚不能复用替换脚本：它的 :RENAME_REPLACE 一进门就 del 掉 "%OLD%.old"，
// 参数一对调，第一件事就是吃掉回滚的原料。两条路径同名不同命，这里钉住边界。
func TestRollbackScriptIsNotSwapScript(t *testing.T) {
	rollback := buildRollbackScript()
	for _, forbidden := range []string{`%OLD%.old`, `%NEW%`, `%BASE%`, ":RENAME_REPLACE"} {
		if strings.Contains(rollback, forbidden) {
			t.Fatalf("回滚脚本里出现了替换脚本的 %q:\n%s", forbidden, rollback)
		}
	}
}
