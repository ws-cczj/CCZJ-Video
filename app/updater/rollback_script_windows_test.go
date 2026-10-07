//go:build windows

package updater

import (
	"cczjVideo/app/handoff"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 回滚脚本跑在用户已经确认"新版本更糟"的时刻，所以它的两条收场都必须站得住：
// 成功时副本被消费掉、本体变回旧字节；失败时本体不动、副本还留着可以再审一次。
// 这两件事字符串断言都证明不了，只能实跑。
func TestRollbackScriptRunsForReal(t *testing.T) {
	t.Run("idle target rolls back cleanly", func(t *testing.T) {
		dir, exe, backup, result := rollbackProbeDir(t)
		out := runRollbackScript(t, dir, exe, backup, result)

		if !strings.Contains(out, handoff.Arg(os.Getpid())) {
			t.Fatalf("脚本没有把交接参数带到 start 那一行，输出:\n%s", out)
		}
		if verdict := readVerdict(t, result); verdict != "rollback_ok" {
			t.Fatalf("回执 = %q, 期望 rollback_ok", verdict)
		}
		if got := readFileOrNil(t, exe); got != "OLD" {
			t.Fatalf("退回后本体应该是旧字节，实际 %q", got)
		}
		if _, err := os.Stat(backup); !os.IsNotExist(err) {
			t.Fatalf("副本应被 move 消费掉，stat = %v", err)
		}
		if _, err := os.Stat(filepath.Join(dir, rollbackScriptName)); !os.IsNotExist(err) {
			t.Fatalf("脚本应该自我删除，stat = %v", err)
		}
	})

	// 本体被占用（用户没让开、或杀毒软件抓着）：move 一直失败，脚本必须承认没换成，
	// 并且把两边都留在原位——副本一旦被吃掉，用户连再试一次的机会都没了。
	t.Run("locked target keeps the app and the backup", func(t *testing.T) {
		dir, exe, backup, result := rollbackProbeDir(t)
		lock, err := os.OpenFile(exe, os.O_RDWR, 0644)
		if err != nil {
			t.Fatal(err)
		}
		defer lock.Close()

		started := time.Now()
		runRollbackScript(t, dir, exe, backup, result)
		if elapsed := time.Since(started); elapsed < 1500*time.Millisecond {
			t.Fatalf("重试没有真的等待，只用了 %v", elapsed)
		}
		if verdict := readVerdict(t, result); verdict != "rollback_failed" {
			t.Fatalf("回执 = %q, 期望 rollback_failed", verdict)
		}
		if got := readFileOrNil(t, exe); got != "NEW" {
			t.Fatalf("失败后本体应还是当前字节，实际 %q", got)
		}
		if got := readFileOrNil(t, backup); got != "OLD" {
			t.Fatalf("失败后副本应原地留着，实际 %q", got)
		}
	})
}

// rollbackProbeDir 造一个"当前版本(NEW) + 回滚副本(OLD) + 中文空格目录"的现场。
func rollbackProbeDir(t *testing.T) (dir, exe, backup, result string) {
	t.Helper()
	dir = filepath.Join(t.TempDir(), "中文 目录")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	exe = filepath.Join(dir, "cczjVideo.exe")
	backup = exe + swapBackupSuffix
	result = filepath.Join(dir, "install-result")
	if err := os.WriteFile(exe, []byte("NEW"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(backup, []byte("OLD"), 0644); err != nil {
		t.Fatal(err)
	}
	script := strings.ReplaceAll(buildRollbackScript(), `start "" "%OLD%" %HANDOFF%`, `echo would-start %HANDOFF%`)
	script = strings.ReplaceAll(script, "if %RETRY% GEQ 30", "if %RETRY% GEQ 2")
	if err := os.WriteFile(filepath.Join(dir, rollbackScriptName), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	return dir, exe, backup, result
}

func runRollbackScript(t *testing.T, dir, exe, backup, result string) string {
	t.Helper()
	// 与 RollbackUpdate 同样的启动方式：相对脚本名 + cmd.Dir + 路径全走参数，
	// 第四个参数是交接参数（用例里那行 start 已被换成 echo）。
	cmd := exec.Command("cmd", "/c", rollbackScriptName, exe, backup, result, handoff.Arg(os.Getpid()))
	cmd.Dir = dir
	combined, err := cmd.CombinedOutput()
	if err != nil {
		t.Logf("脚本退出：%v\n%s", err, combined)
	}
	return string(combined)
}
