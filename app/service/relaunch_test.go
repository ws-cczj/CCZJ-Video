package service

import (
	"os"
	"os/exec"
	"testing"
)

func TestRelaunchTargetPID(t *testing.T) {
	if got := relaunchTargetPID([]string{"cczjVideo.exe"}); got != 0 {
		t.Errorf("无交接参数时应返回 0，得到 %d", got)
	}
	if got := relaunchTargetPID([]string{"cczjVideo.exe", relaunchArg(4321)}); got != 4321 {
		t.Errorf("应解析出旧进程 PID，得到 %d", got)
	}
	for _, bad := range []string{"--cczj-relaunch=", "--cczj-relaunch=abc", "--cczj-relaunch=0", "--cczj-relaunch=-7"} {
		if got := relaunchTargetPID([]string{bad}); got != 0 {
			t.Errorf("%s 应视为无交接参数，得到 %d", bad, got)
		}
	}
}

func TestProcessAlive(t *testing.T) {
	if _, err := exec.LookPath("tasklist"); err != nil {
		t.Skip("本机没有 tasklist，交接等待按设计退化成不等待")
	}
	if !processAlive(os.Getpid()) {
		t.Errorf("当前进程 %d 应判为活着", os.Getpid())
	}
	if processAlive(999999) {
		t.Error("不存在的 PID 应判为已退出")
	}
}
