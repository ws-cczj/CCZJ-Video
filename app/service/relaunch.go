package service

import (
	"cczjVideo/app/applog"
	"os"
	"os/exec"
	goruntime "runtime"
	"strconv"
	"strings"
	"time"
)

// 单实例锁（见 app.go 的 SingleInstance）让「重启应用」必须先等旧进程退出：
// 新进程如果抢在旧进程释放锁之前启动，会被判成第二实例并静默 os.Exit，
// 于是重启变成关软件。交接靠启动参数把旧进程 PID 传给新进程。
const relaunchArgPrefix = "--cczj-relaunch="

// relaunchHandoffTimeout 是交接等待上限：旧进程卡住时新进程照常往下启动，
// 让单实例锁去决定是唤醒旧窗口还是自己退出，不能在这里无限等。
const relaunchHandoffTimeout = 30 * time.Second

func relaunchArg(pid int) string {
	return relaunchArgPrefix + strconv.Itoa(pid)
}

func relaunchTargetPID(args []string) int {
	for _, arg := range args {
		if !strings.HasPrefix(arg, relaunchArgPrefix) {
			continue
		}
		pid, err := strconv.Atoi(strings.TrimPrefix(arg, relaunchArgPrefix))
		if err != nil || pid <= 0 {
			return 0
		}
		return pid
	}
	return 0
}

// AwaitRelaunchHandoff 必须在创建应用之前调用（main 的第一行）：那时既没打开数据库，
// 也还没抢单实例锁。没有交接参数时立即返回，正常双击启动不受影响。
func AwaitRelaunchHandoff(args []string) {
	if goruntime.GOOS != "windows" {
		return
	}
	pid := relaunchTargetPID(args)
	if pid <= 0 || pid == os.Getpid() {
		return
	}
	deadline := time.Now().Add(relaunchHandoffTimeout)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	applog.Warn("[Relaunch] 等待旧进程 %d 退出超时，直接启动", pid)
}

// processAlive 按 PID 问系统：新进程不是旧进程的子进程，拿不到 os.Process 句柄。
// tasklist 不可用时返回 false，退化成不等待（等同于加单实例锁之前的行为）。
func processAlive(pid int) bool {
	target := strconv.Itoa(pid)
	out, err := exec.Command("tasklist", "/FI", "PID eq "+target, "/NH").Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), target)
}
