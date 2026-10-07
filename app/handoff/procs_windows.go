//go:build windows

package handoff

import (
	"os"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// eachProcess 走一遍系统进程表，把 (PID, 映像名) 逐个交给 visit；visit 返回 false 就停。
//
// 这里不起子进程。原先这两问是 exec.Command("tasklist", ...)，而本应用按 -H windowsgui
// 构建（没有控制台可依附），每次启动一个控制台程序都会为它新开一个控制台窗口——等待是
// 每 200ms 轮一次，用户在桌面上看到的就是连续几十下黑窗闪烁。快照顺带改掉了对 tasklist
// 的列格式与本地化输出的依赖。
// 快照拿不到时什么都不报，等同于"没人在跑"：等待退化成不等待，交给单实例锁决胜负。
func eachProcess(visit func(pid int, image string) bool) {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return
	}
	defer windows.CloseHandle(snapshot)

	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	for err := windows.Process32First(snapshot, &entry); err == nil; err = windows.Process32Next(snapshot, &entry) {
		if !visit(int(entry.ProcessID), windows.UTF16ToString(entry.ExeFile[:])) {
			return
		}
	}
}

// processAlive 按 PID 问系统：新进程不是旧进程的子进程，拿不到 os.Process 句柄。
func processAlive(pid int) bool {
	alive := false
	eachProcess(func(candidate int, _ string) bool {
		if candidate == pid {
			alive = true
			return false
		}
		return true
	})
	return alive
}

// sameImagePIDs 列出与本人同名、但不是自己的进程。按名字而不是按完整路径是有意的：
// 单实例互斥体按 UniqueID 开，任何一份本应用都会占着它，与装在哪个目录无关。
// 映像名是进程加载时定下的，换 exe 的脚本把那个文件改名让位也改不动它——正因如此，
// 被改名让路的那个旧进程照样在这里被认出来。
func sameImagePIDs(image string) []int {
	self := os.Getpid()
	var pids []int
	eachProcess(func(candidate int, name string) bool {
		if candidate != self && strings.EqualFold(name, image) {
			pids = append(pids, candidate)
		}
		return true
	})
	return pids
}
