package handoff

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// requireWindows 挡的是这几条对进程表的读写：非 Windows 上它们是空桩（见 procs_other.go，
// Await 第一行本来就按平台退化成不等待），在那里跑只会测到桩。
// 早先这里挡的是「本机有没有 tasklist」——那是旧实现的真依赖，改读进程快照之后依赖没了，
// 留着会让下一个读测试的人以为等待还要靠外部命令。
func requireWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "windows" {
		t.Skip("进程存活与同名枚举只在 Windows 上有实现")
	}
}

func TestTargetPID(t *testing.T) {
	if got := TargetPID([]string{"cczjVideo.exe"}); got != 0 {
		t.Errorf("无交接参数时应返回 0，得到 %d", got)
	}
	if got := TargetPID([]string{"cczjVideo.exe", Arg(4321)}); got != 4321 {
		t.Errorf("应解析出旧进程 PID，得到 %d", got)
	}
	for _, bad := range []string{
		"--cczj-relaunch:", "--cczj-relaunch:abc", "--cczj-relaunch:0", "--cczj-relaunch:-7",
		// 老写法（等号）在批处理那条路上会被 cmd 截断，见 argPrefix 的注释；这里钉住
		// 它不会被当成有效交接参数，免得两种格式同时活着。
		"--cczj-relaunch=", "--cczj-relaunch=1234",
	} {
		if got := TargetPID([]string{bad}); got != 0 {
			t.Errorf("%s 应视为无交接参数，得到 %d", bad, got)
		}
	}
}

// 交接参数要能原样穿过 cmd 的批处理参数解析（脚本里靠 %~5 接住）。cmd 把空格、Tab
// 和 = , ; 都当参数分隔符，所以这四种字符一个都不能出现在令牌里——出现就等于把
// PID 送到另一个参数位上去。
func TestArgSurvivesCmdBatchParameterSplitting(t *testing.T) {
	got := Arg(os.Getpid())
	if strings.ContainsAny(got, " \t=,;") {
		t.Errorf("交接参数 %q 含 cmd 的批处理分隔符，%%~5 会把它截断", got)
	}
}

// 批处理里的 start 只是原样拼接 Go 递来的参数，所以命令行形态也得在这儿钉住：
// 它必须是一个不带空白、不带引号的单独 token，cmd 的 start 才会把它当成新进程的参数，
// 而不是窗口标题或程序路径的一部分。
func TestArgIsSafeToSpliceIntoABatchLine(t *testing.T) {
	got := Arg(os.Getpid())
	if strings.ContainsAny(got, " \t\"&()^|") {
		t.Errorf("交接参数 %q 含 cmd 元字符或空白，拼进批处理 start 行会被拆坏", got)
	}
	if TargetPID([]string{got}) != os.Getpid() {
		t.Errorf("Arg 与 TargetPID 往返不一致: %q", got)
	}
}

func TestProcessAlive(t *testing.T) {
	requireWindows(t)
	if !processAlive(os.Getpid()) {
		t.Errorf("当前进程 %d 应判为活着", os.Getpid())
	}
	if processAlive(999999) {
		t.Error("不存在的 PID 应判为已退出")
	}
}

// 与上一条对称：这一条钉的是"真在等"。把 Await 写成空函数，下一条照样过、这一条必红——
// 而空函数正是这次要修的失效（新进程不等旧进程让位，就被单实例锁判成重复启动并静默退出）。
// 所以这里必须拿一个**真实存在过**的 PID 来等，不能只用编造的号。
func TestAwaitWaitsForALiveProcess(t *testing.T) {
	requireWindows(t)
	// ping -n 3 实测约 2.03 秒（发 3 个包、间隔两个），够跨过 200ms 的轮询粒度。
	cmd := exec.Command("ping", "-n", "3", "127.0.0.1")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pid := cmd.Process.Pid
	defer func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}()
	if !processAlive(pid) {
		t.Fatalf("子进程 %d 起不来，这条测不出等待", pid)
	}

	started := time.Now()
	Await([]string{"cczjVideo.exe", Arg(pid)})
	elapsed := time.Since(started)

	if elapsed < 1200*time.Millisecond {
		t.Errorf("只等了 %v，没有真的等旧进程退出（子进程约活 2 秒）", elapsed)
	}
	// 上限要远小于 30 秒：等满超时说明"Await 返回"和"进程退出"没关系，
	// 那种写法在真机上会把"更新后界面迟迟不来"变成常态。
	if elapsed > 10*time.Second {
		t.Errorf("等了 %v，子进程早已退出，应该远早于 30 秒的超时上限", elapsed)
	}
	if processAlive(pid) {
		t.Errorf("Await 返回时子进程 %d 还活着", pid)
	}
}

// 等一个已经消失的 PID 必须立刻返回，不能把 30 秒上限走完：正常安装那一档里 start
// 出去时旧进程早没了，这一条决定更新后界面是不是秒开。
func TestAwaitReturnsImmediatelyWhenTargetIsGone(t *testing.T) {
	requireWindows(t)
	started := time.Now()
	Await([]string{"cczjVideo.exe", Arg(999999)})
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Errorf("对不存在的 PID 等了 %v，应该立刻返回", elapsed)
	}
}

// 兜底信号只认「刚刚写下的成功回执」。这一条钉的是它不能把别的东西当成刚替换完：
// 失败回执那两支脚本根本不会 start 新进程，等满 30 秒只会让下一次双击迟迟不开界面；
// 陈旧的 ok 是上一次成功更新留下的，早已与本次启动无关。
func TestSwappedInRecentlyAcceptsOnlyAFreshOKReceipt(t *testing.T) {
	now := time.Now()
	dir := t.TempDir()
	path := filepath.Join(dir, swapReceiptName)

	cases := []struct {
		name  string
		mtime time.Time
		body  string
		want  bool
	}{
		{"刚写完的 ok", now.Add(-time.Second), "ok\r\n", true},
		{"没有换行的 ok", now.Add(-time.Second), "ok", true},
		{"换文件失败", now.Add(-time.Second), "swap_failed\r\n", false},
		{"验证失败", now.Add(-time.Second), "verify_failed\r\n", false},
		{"内容不是结论", now.Add(-time.Second), "hello\r\n", false},
		{"空文件", now.Add(-time.Second), "", false},
		{"超出时间窗的 ok", now.Add(-receiptFreshness - time.Minute), "ok\r\n", false},
		{"来自未来的 ok", now.Add(time.Minute), "ok\r\n", false},
	}
	for _, tc := range cases {
		if err := os.WriteFile(path, []byte(tc.body), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, tc.mtime, tc.mtime); err != nil {
			t.Fatal(err)
		}
		if got := swappedInRecently(path, now); got != tc.want {
			t.Errorf("%s: swappedInRecently=%v，期望 %v", tc.name, got, tc.want)
		}
	}
	if swappedInRecently(filepath.Join(dir, "missing"), now) {
		t.Error("回执文件不存在时应判为没有被替换")
	}
}

// 正常双击（既没参数也没新鲜回执）不能被兜底分支拖慢：这一条是那条路上唯一的价格，
// 写错了就是每次启动都可能白等 30 秒。
func TestAwaitReturnsImmediatelyWithoutArgOrReceipt(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TEMP", dir)
	t.Setenv("TMP", dir)
	started := time.Now()
	Await([]string{"cczjVideo.exe"})
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Errorf("没有交接参数也没有回执，等了 %v，应该立刻返回", elapsed)
	}
}

func TestSameImagePIDsSeesAnotherInstanceButNotItself(t *testing.T) {
	requireWindows(t)
	cmd := exec.Command("ping", "-n", "3", "127.0.0.1")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pid := cmd.Process.Pid
	defer func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}()

	seen := sameImagePIDs("ping.exe")
	if !containsPID(seen, pid) {
		t.Errorf("刚起的 ping 子进程 %d 没被枚举到，得到 %v", pid, seen)
	}
	if containsPID(seen, os.Getpid()) {
		t.Error("枚举结果把自己的 PID 也算进去了，等待就会变成等自己")
	}
	if containsPID(sameImagePIDs("no_such_image_here.exe"), pid) {
		t.Error("按别的镜像名不该看到这个进程")
	}
}

// 整条兜底路径：拉起我们的那份程序**不认识**交接参数（2.3.1 及更早就是这样），
// 但它会在 start 前把 ok 写进回执。新进程必须在抢单实例锁之前等掉那个还活着的旧进程，
// 否则就是这次要修的原样现场——脚本换成功、新进程被判重复启动、界面再也不来。
// 子进程就是本测试自己，镜像名与父进程相同，这正是等待条件要匹配的东西。
func TestAwaitWaitsForSameImageWhenTheSpawnerIsAnOlderBuild(t *testing.T) {
	if os.Getenv("CCZJ_TEST_SLEEP_PROCESS") == "1" {
		time.Sleep(3 * time.Second)
		return
	}
	requireWindows(t)

	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	env := append(os.Environ(), "CCZJ_TEST_SLEEP_PROCESS=1")
	child := exec.Command(exe, "-test.run", "TestAwaitWaitsForSameImageWhenTheSpawnerIsAnOlderBuild")
	child.Env = env
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = child.Process.Kill()
		_, _ = child.Process.Wait()
	}()
	if !processAlive(child.Process.Pid) {
		t.Fatalf("子进程 %d 起不来，这条测不出等待", child.Process.Pid)
	}

	dir := t.TempDir()
	t.Setenv("TEMP", dir)
	t.Setenv("TMP", dir)
	receipt := filepath.Join(dir, swapReceiptName)
	if err := os.WriteFile(receipt, []byte("ok\r\n"), 0644); err != nil {
		t.Fatal(err)
	}

	started := time.Now()
	Await([]string{"cczjVideo.exe"}) // 老脚本的 start 行就是这个样子：没有交接参数。
	elapsed := time.Since(started)

	if elapsed < 1200*time.Millisecond {
		t.Errorf("只等了 %v，没有真的等那个还活着的同名实例", elapsed)
	}
	if elapsed > 20*time.Second {
		t.Errorf("等了 %v，子进程只有 3 秒寿命，应远早于 30 秒上限", elapsed)
	}
	if processAlive(child.Process.Pid) {
		t.Errorf("Await 返回时同名实例 %d 还活着", child.Process.Pid)
	}
}

func containsPID(pids []int, target int) bool {
	for _, pid := range pids {
		if pid == target {
			return true
		}
	}
	return false
}
