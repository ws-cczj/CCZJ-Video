//go:build windows

package updater

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 字符串断言证明不了 cmd 真能把脚本跑起来。这三件事都只有实跑才能发现：
// 路径全部走参数（非 ASCII 目录不再被代码页读坏）、等待是真的在等、
// 换不进去的时候旧 exe 还挂在原来的名字上。
//
// 脚本里的 start 在这里被换成 echo：用例用的是假 exe，真去执行它只会弹错误框。
func TestSwapScriptRunsForReal(t *testing.T) {
	t.Run("idle target swaps cleanly", func(t *testing.T) {
		dir, oldExe, newExe, result, base := swapProbeDir(t)
		runSwapScript(t, dir, oldExe, newExe, result, base)

		if verdict := readVerdict(t, result); verdict != "ok" {
			t.Fatalf("回执 = %q, 期望 ok", verdict)
		}
		if got := readFileOrNil(t, oldExe); got != "NEW" {
			t.Fatalf("换入后旧名上应该是新字节，实际 %q", got)
		}
		if _, err := os.Stat(newExe); !os.IsNotExist(err) {
			t.Fatalf("产物应该被移走，stat = %v", err)
		}
		if _, err := os.Stat(filepath.Join(dir, swapScriptName)); !os.IsNotExist(err) {
			t.Fatalf("脚本应该自我删除，stat = %v", err)
		}
	})

	// 占用旧 exe：move 与 ren 都会撞分享冲突，脚本必须承认失败、把本体留在原位，
	// 并且真的等过重试间隔——busy-wait 的 timeout 写法在这一条里会当场露馅。
	t.Run("locked target fails without losing the app", func(t *testing.T) {
		dir, oldExe, newExe, result, base := swapProbeDir(t)
		lock, err := os.OpenFile(oldExe, os.O_RDWR, 0644)
		if err != nil {
			t.Fatal(err)
		}
		defer lock.Close()

		started := time.Now()
		runSwapScript(t, dir, oldExe, newExe, result, base)
		// 探针把重试上限从 30 缩到 2，也就是只等一轮（约 2 秒）。等待一旦退回
		// timeout 那种写法，这里会掉到 0.1 秒以下——那时整条重试等于没等。
		if elapsed := time.Since(started); elapsed < 1500*time.Millisecond {
			t.Fatalf("重试没有真的等待，只用了 %v", elapsed)
		}
		if verdict := readVerdict(t, result); verdict != "swap_failed" {
			t.Fatalf("回执 = %q, 期望 swap_failed", verdict)
		}
		if got := readFileOrNil(t, oldExe); got != "OLD" {
			t.Fatalf("失败后应用本体应还是旧字节，实际 %q", got)
		}
		if got := readFileOrNil(t, newExe); got != "NEW" {
			t.Fatalf("失败后产物应原地留着等重试，实际 %q", got)
		}
	})
}

// swapProbeDir 建一个带中文和空格的"应用目录"：这是脚本正文里写死路径时最先炸的
// 场景（cmd 按控制台代码页读批处理），也是本应用真实存在的安装位置形态。
func swapProbeDir(t *testing.T) (dir, oldExe, newExe, result, base string) {
	t.Helper()
	dir = filepath.Join(t.TempDir(), "中文 目录")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	base = "cczjVideo.exe"
	oldExe = filepath.Join(dir, base)
	newExe = filepath.Join(dir, stagingPrefix+".exe")
	result = filepath.Join(dir, "install-result")
	if err := os.WriteFile(oldExe, []byte("OLD"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newExe, []byte("NEW"), 0644); err != nil {
		t.Fatal(err)
	}
	script := strings.ReplaceAll(buildSwapScript(), `start "" "%OLD%"`, `echo would-start`)
	// 锁占用那一轮最多等 60 秒，探针里缩到 2 次：既要真的等过，也不能把测试拖死。
	script = strings.ReplaceAll(script, "if %RETRY% GEQ 30", "if %RETRY% GEQ 2")
	if err := os.WriteFile(filepath.Join(dir, swapScriptName), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	return dir, oldExe, newExe, result, base
}

func runSwapScript(t *testing.T, dir, oldExe, newExe, result, base string) {
	t.Helper()
	// 与 installBySwapWindows 同样的启动方式：相对脚本名 + cmd.Dir + 路径全走参数，
	// 而且 stdin 是 NUL（exec.Cmd 的默认），这样 timeout 那一类问题才复现得出来。
	cmd := exec.Command("cmd", "/c", swapScriptName, oldExe, newExe, result, base)
	cmd.Dir = dir
	combined, err := cmd.CombinedOutput()
	if err != nil {
		t.Logf("脚本退出：%v\n%s", err, combined)
	}
}

func readVerdict(t *testing.T, result string) string {
	t.Helper()
	data, err := os.ReadFile(result)
	if err != nil {
		t.Fatalf("读不到回执文件: %v", err)
	}
	return strings.TrimSpace(strings.TrimSuffix(string(data), "\r\n"))
}

func readFileOrNil(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		return "<missing>"
	}
	return string(data)
}
