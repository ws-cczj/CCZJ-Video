// Package handoff 是「再拉起一份本应用」这件事的跨进程约定。
//
// 应用强制单实例（见根目录 app.go 的 SingleInstance）：第二个进程一启动就被判成重复启动，
// 把已在跑的窗口叫到前台，然后自己 os.Exit。所以任何"退出当前进程 + 再开一个进程"的动作
// 都必须先让旧进程把互斥体交出来，否则重启/更新在用户眼里就是"点了一下，软件没了"。
//
// 交接靠启动参数带旧进程 PID：新进程在抢锁之前先等这个 PID 消失。约定只在这里定义一次，
// 三个使用方都引用它——main 在启动第一行等（Await）、设置页的「重启」造参数（Arg）、
// 换 exe 的批处理脚本把同一个参数拼进每一行 start。
//
// 参数只能保证「会传参数的旧进程」拉起的那一跳。装着 2.3.1 及更早版本的机器，脚本里没有
// 这个参数（那几版的 buildSwapScript 就是 `start "" "%OLD%"`），而它们照样会在 start 之前
// 往回执文件写 ok——所以 Await 还认这条线索，见下面的 swappedInRecently。
package handoff

import (
	"cczjVideo/app/applog"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"strings"
	"time"
)

// argPrefix 用冒号而不是等号：cmd 把 = 当作批处理参数的分隔符，带 = 的令牌递进脚本
// 会被截成两段（实测 --cczj-relaunch=1234 进去之后 %~5 只剩 --cczj-relaunch，PID 变成 %6），
// 于是交接参数在最关键的那条路（批处理脚本）上静默失效。冒号不是分隔符。
const argPrefix = "--cczj-relaunch:"

// waitTimeout 是交接等待上限：旧进程卡住时新进程照常往下启动，
// 让单实例锁去决定是唤醒旧窗口还是自己退出，不能在这里无限等。
const waitTimeout = 30 * time.Second

// pollInterval 是等待的轮询步长。等的是「另一个进程退场」这件事，200ms 已经够细：
// 换程序时用户本来就等了几十秒重试，再粗会出现「窗口迟迟不来」。
// 每一轮读一次系统进程表（见 procs_windows.go），不起子进程——早先这里是每轮起一个
// tasklist，而本应用按 -H windowsgui 构建，起控制台程序会闪黑窗，200ms 一次就是刷屏。
const pollInterval = 200 * time.Millisecond

// swapReceiptName 是替换脚本回写结论的文件名。路径由这里定义、updater 引过去：
// 写它的是脚本、读它的是新进程，两边一个字不一样就等于这条兜底线索永远捡不到。
const swapReceiptName = "cczj_video_update_result"

// receiptVerdictOK 只在替换成功那一支出现，而那一支紧接着就 start 新进程。
// swap_failed / verify_failed 两支不 start，读到它们不该等。
const receiptVerdictOK = "ok"

// receiptFreshness 是把回执当作「刚刚发生过替换」的时间窗：脚本写 ok 与 start 之间只差
// 几毫秒，15 秒已经很宽；再长就会把用户自己双击那一次也算成更新。
const receiptFreshness = 15 * time.Second

// SwapReceiptPath 返回替换脚本写结论的位置（%TEMP% 下，与 exe 装在哪里无关）。
func SwapReceiptPath() string {
	return filepath.Join(os.TempDir(), swapReceiptName)
}

// Arg 造出交接参数。把 PID 交给对方进程是它的唯一职责：谁再拉起一份应用，谁就带上它。
func Arg(pid int) string {
	return argPrefix + strconv.Itoa(pid)
}

// TargetPID 从启动参数里取要等的 PID；没有交接参数、或参数不成样子时返回 0（表示不等）。
func TargetPID(args []string) int {
	for _, arg := range args {
		if !strings.HasPrefix(arg, argPrefix) {
			continue
		}
		pid, err := strconv.Atoi(strings.TrimPrefix(arg, argPrefix))
		if err != nil || pid <= 0 {
			return 0
		}
		return pid
	}
	return 0
}

// Await 必须在创建应用之前调用（main 的第一行）：那时既没打开数据库，也还没抢单实例锁。
// 三条退出路径：带交接参数就等那个 PID；没参数但刚被替换脚本拉起来就等同名实例；
// 两者都不是（正常双击）就立即返回。
func Await(args []string) {
	if goruntime.GOOS != "windows" {
		return
	}
	if pid := TargetPID(args); pid > 0 && pid != os.Getpid() {
		waitUntil("旧进程 "+strconv.Itoa(pid), func() bool { return processAlive(pid) })
		return
	}
	if !swappedInRecently(SwapReceiptPath(), time.Now()) {
		return
	}
	image, err := currentImageName()
	if err != nil {
		return
	}
	waitUntil("同名实例 "+image, func() bool { return len(sameImagePIDs(image)) > 0 })
}

// swappedInRecently 认的是「这份回执是刚刚完成的热替换写的」。
// 拉起我们的脚本由**上一版**程序生成，那一版可能还不认识交接参数，但 2.3.0 起它就会在
// start 之前把 ok 写进这个文件——这是那条路上唯一还留着的信号。
func swappedInRecently(path string, now time.Time) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	if now.Sub(info.ModTime()) > receiptFreshness || now.Sub(info.ModTime()) < 0 {
		return false
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(content)) == receiptVerdictOK
}

// waitUntil 每 200ms 问一次系统，条件一消失就返回；等满上限也只出一行日志，
// 之后照常启动——让单实例锁去决定是唤醒旧窗口还是自己退出，不能在这里无限等。
func waitUntil(desc string, busy func() bool) {
	if !busy() {
		return
	}
	applog.Info("[Relaunch] 等待%s退出后再抢单实例锁", desc)
	deadline := time.Now().Add(waitTimeout)
	for time.Now().Before(deadline) {
		if !busy() {
			return
		}
		time.Sleep(pollInterval)
	}
	applog.Warn("[Relaunch] 等待%s超时，直接启动", desc)
}

func currentImageName() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Base(exe), nil
}
