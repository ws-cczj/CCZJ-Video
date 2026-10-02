package updater

import (
	"os"
	"strings"
	"testing"
)

// useTempDir 把 os.TempDir() 指向用例专属目录，installResultPath 跟着挪过去。
func useTempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("TMP", dir)
	t.Setenv("TEMP", dir)
	return dir
}

func TestTakeInstallResultConsumesOnce(t *testing.T) {
	useTempDir(t)
	if err := os.WriteFile(installResultPath(), []byte("swap_failed\r\n"), 0644); err != nil {
		t.Fatal(err)
	}

	verdict, ok := TakeInstallResult()
	if !ok {
		t.Fatal("第一次读取应当拿到结论")
	}
	if verdict != "swap_failed" {
		t.Fatalf("结论 = %q, 期望 %q", verdict, "swap_failed")
	}
	if _, err := os.Stat(installResultPath()); !os.IsNotExist(err) {
		t.Fatalf("读取后结论文件应被删掉，stat = %v", err)
	}
	if _, ok := TakeInstallResult(); ok {
		t.Fatal("结论是一次性的，第二次不该再读到")
	}
}

func TestTakeInstallResultWithoutFile(t *testing.T) {
	useTempDir(t)
	if verdict, ok := TakeInstallResult(); ok {
		t.Fatalf("没有结论文件时不该返回内容，得到 %q", verdict)
	}
}

// 脚本写结论、Go 侧读结论，两边必须落在同一个路径上；各写各的就等于没有通道。
// 正文里一个路径字面量都没有（全部走参数），所以断言针对的是 %变量名。
func TestBuildSwapScriptReportsEveryOutcome(t *testing.T) {
	script := buildSwapScript()

	for _, wire := range []string{`set "OLD=%~1"`, `set "NEW=%~2"`, `set "RESULT=%~3"`, `set "BASE=%~4"`} {
		if !strings.Contains(script, wire) {
			t.Fatalf("脚本缺少参数接线 %q:\n%s", wire, script)
		}
	}
	// 三种收场都要留回执，否则失败仍然是静默的。
	for _, line := range []string{
		`echo ok>"%RESULT%"`,
		`echo swap_failed>"%RESULT%"`,
		`echo verify_failed>"%RESULT%"`,
	} {
		if !strings.Contains(script, line) {
			t.Fatalf("脚本缺少回执 %q:\n%s", line, script)
		}
	}
	// 回归：绝不先删旧 exe，换不回去要把名字还回来。
	if strings.Contains(script, `del /f /q "%OLD%"`) {
		t.Fatalf("脚本在替换前删除了旧 exe:\n%s", script)
	}
	if !strings.Contains(script, `ren "%OLD%.old" "%BASE%"`) {
		t.Fatalf("换入失败时没有把旧 exe 改回原名:\n%s", script)
	}
	// 回归：中断留下的 .old 必须在改名让位之前清掉。ren 不覆盖同名目标，
	// 留着它这条分支就永远走不通，用户此后每次更新都装不上。
	clearAt := strings.Index(script, `del /f /q "%OLD%.old"`)
	renameAt := strings.Index(script, `ren "%OLD%" "%BASE%.old"`)
	if renameAt < 0 {
		t.Fatalf("脚本没有把旧 exe 改名让位:\n%s", script)
	}
	if clearAt < 0 || clearAt > renameAt {
		t.Fatalf("清残留 .old 缺失或排在改名之后:\n%s", script)
	}
	// 回归：等待必须有上限，否则机器上另一份同名 exe 会把替换永远卡住。
	if !strings.Contains(script, "if %RETRY% GEQ 30 goto :RENAME_REPLACE") {
		t.Fatalf("重试没有上限:\n%s", script)
	}
	// 脚本必须全是 ASCII：cmd.exe 按控制台代码页读批处理，非 ASCII 字节会被读成
	// 别的字，中文安装目录能把一整行 if 解析到别的命令上去。
	for _, r := range script {
		if r > 127 {
			t.Fatalf("脚本含非 ASCII 字符 %q", r)
		}
	}
}

// timeout 撞上非控制台 stdin 会立刻报错返回（脚本由 Go 以独立进程启动，stdin 是 NUL），
// 于是"每次等 2 秒、最多 30 次"变成几百毫秒烧完，本进程还没让位就开始改名。
func TestBuildSwapScriptActuallyWaits(t *testing.T) {
	script := buildSwapScript()
	if strings.Contains(script, "timeout") {
		t.Fatalf("脚本用 timeout 等待，独立进程下它不会真等:\n%s", script)
	}
	if !strings.Contains(script, "ping -n 3 127.0.0.1 >NUL") {
		t.Fatalf("脚本没有可用的等待命令:\n%s", script)
	}
}

// ren 的第二个参数只要带路径，cmd 就报"文件名、目录名或卷标语法不正确"（实测 rc=1），
// 整条"先改名再换入"的补救分支等于不存在——而那是唯一还能装上新版的分支。
func TestBuildSwapScriptRenameTargetsAreBareNames(t *testing.T) {
	found := 0
	for _, line := range strings.Split(buildSwapScript(), "\r\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "ren ") {
			continue
		}
		found++
		parts := strings.Split(trimmed, `"`)
		if len(parts) < 5 {
			t.Fatalf("ren 行解析不出两个带引号的参数: %q", line)
		}
		for _, arg := range []string{parts[1], parts[3]} {
			if strings.ContainsAny(arg, `/\`) {
				t.Fatalf("ren 的参数 %q 带了路径（第二个参数必须是纯文件名）: %q", arg, line)
			}
		}
	}
	if found == 0 {
		t.Fatal("脚本里一个 ren 都没有，改名让位的分支被删了")
	}
}

// 点"安装"之后本进程退场，脚本无论成败都得把应用交回用户手里；
// 成功分支也不能顺手删掉 .old——那是新版本起不来时唯一的退路。
func TestBuildSwapScriptHandsTheAppBack(t *testing.T) {
	script := buildSwapScript()
	if got := strings.Count(script, `start "" "%OLD%"`); got < 3 {
		t.Fatalf("start 旧 exe 出现 %d 次，期望至少 3 次（成功 + 两种失败）:\n%s", got, script)
	}
	for _, label := range []string{":FAILED", ":REPLACE_OK"} {
		block := scriptBlock(script, label)
		if !strings.Contains(block, `start "" "%OLD%"`) {
			t.Fatalf("%s 分支没有把应用交回用户:\n%s", label, block)
		}
	}
	if strings.Contains(scriptBlock(script, ":START_NEW"), `del /f /q "%OLD%.old"`) {
		t.Fatalf("成功分支删掉了 .old 回滚副本:\n%s", script)
	}
}

// scriptBlock 取某个标签到下一个标签之间的脚本片段。标签必须整行匹配：
// ":FAILED" 这类字符串在 `goto :FAILED` 里先出现过，按子串找会拿到跳转那一段。
func scriptBlock(script, label string) string {
	at := strings.Index(script, "\r\n"+label+"\r\n")
	if at < 0 {
		return ""
	}
	start := at + 2
	if next := strings.Index(script[start+1:], "\r\n:"); next >= 0 {
		return script[start : start+1+next]
	}
	return script[start:]
}
