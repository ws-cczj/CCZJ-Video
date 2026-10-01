package updater

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"cczjVideo/app/apperror"
	"cczjVideo/app/applog"
)

// quitHook 是服务层递进来的退出通道（见 SetQuitHook）。
var quitHook func()

// SetQuitHook 注入退出通道。装更新必然要让当前进程退场，而退出得走应用的关停流程
// —— 记录退出时刻、取消并等待采集与下载收尾、关日志、关库。updater 自己 os.Exit
// 会把这些全跳过，留下的就是「更新一次，库脏一点」；但 updater 不能反向 import
// 服务层，所以通道由外部传进来。没注入时退回硬退出，至少替换脚本能拿到控制权。
func SetQuitHook(hook func()) { quitHook = hook }

// exitProcess 请求当前进程退场，优先交给注入的通道去优雅收尾。
func exitProcess() {
	if quitHook != nil {
		quitHook()
		return
	}
	os.Exit(0)
}

// InstallUpdate 执行安装：替换当前可执行文件为新版本并重启
// Windows: 创建批处理脚本，等待旧进程退出后替换 exe 并重启
// 其他平台: 直接启动新版本（或让用户手动处理）
func InstallUpdate(filePath string) error {
	// 这个函数最终会 exec 传进来的路径，而它对前端（含扩展包）是暴露的绑定，
	// 所以只允许安装本更新器落在应用目录里的产物。
	if !isUpdateArtifact(filePath) {
		applog.Warn("[Updater] 拒绝安装非更新产物: %s", filePath)
		return apperror.New(apperror.Validation, "只能安装本应用下载目录里的更新包")
	}

	applog.Info("[Updater] 准备安装更新: %s", filePath)

	ext := strings.ToLower(filepath.Ext(filePath))

	switch runtime.GOOS {
	case "windows":
		switch ext {
		case ".exe":
			return installBySwapWindows(filePath)
		case ".msi":
			// MSI 安装包有自己的安装逻辑，直接启动
			return launchAndExit(filePath)
		default:
			// zip/7z 等：打开所在目录让用户手动处理
			dir := filepath.Dir(filePath)
			_ = exec.Command("explorer", "/select,", filePath).Start()
			_ = exec.Command("explorer", dir).Start()
			go func() {
				time.Sleep(500 * time.Millisecond)
				exitProcess()
			}()
			return nil
		}
	case "darwin":
		return launchAndExit(filePath)
	default:
		return launchAndExit(filePath)
	}
}

// launchAndExit 启动文件并退出当前程序
func launchAndExit(filePath string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command(filePath)
	case "darwin":
		cmd = exec.Command("open", filePath)
	default:
		cmd = exec.Command("xdg-open", filePath)
	}
	if cmd != nil {
		_ = cmd.Start()
	}
	go func() {
		time.Sleep(500 * time.Millisecond)
		exitProcess()
	}()
	return nil
}

// installBySwapWindows 通过批处理脚本实现 exe 热替换
// 流程：当前进程退出 → 脚本等待退出 → 替换 exe → 启动新版本 → 清理脚本
func installBySwapWindows(newExePath string) error {
	oldExe, err := os.Executable()
	if err != nil {
		return apperror.Wrap(apperror.Storage, err, "获取当前可执行文件路径失败")
	}
	oldExe, err = filepath.EvalSymlinks(oldExe)
	if err != nil {
		return apperror.Wrap(apperror.Storage, err, "解析可执行文件路径失败")
	}

	appDir := filepath.Dir(oldExe)
	oldExeFull := oldExe
	newExeFull, _ := filepath.Abs(newExePath)
	batPath := filepath.Join(appDir, "_update_swap.bat")
	oldExeName := filepath.Base(oldExe)

	// 使用纯 ASCII + CRLF 行尾，避免 cmd.exe 默认代码页编码问题
	// 使用绝对路径，避免 %~dp0 在含空格路径下的解析问题
	CRLF := "\r\n"
	lines := []string{
		"@echo off",
		fmt.Sprintf("echo Waiting for %s to exit...", oldExeName),
		":WAIT_LOOP",
		fmt.Sprintf(`tasklist /FI "IMAGENAME eq %s" 2>NUL | find /I "%s">NUL`, oldExeName, oldExeName),
		"if %ERRORLEVEL% EQU 0 (",
		"    timeout /t 1 /nobreak >NUL",
		"    goto WAIT_LOOP",
		")",
		"",
		"echo Process exited, replacing executable...",
		"timeout /t 1 /nobreak >NUL",
		"",
		// 带重试的替换循环（Windows 进程退出后文件可能仍被锁定）
		"set RETRY=0",
		":RETRY_REPLACE",
		fmt.Sprintf(`move /y "%s" "%s" >NUL 2>&1`, newExeFull, oldExeFull),
		"if %ERRORLEVEL% EQU 0 goto :REPLACE_OK",
		"",
		"set /a RETRY+=1",
		"if %RETRY% GEQ 15 goto :FORCE_REPLACE",
		"echo Move failed (retry %RETRY%), waiting...",
		"timeout /t 2 /nobreak >NUL",
		"goto RETRY_REPLACE",
		"",
		// 强制替换：先删除旧文件再移动
		":FORCE_REPLACE",
		"echo Force replacing...",
		fmt.Sprintf(`del /f /q "%s" >NUL 2>&1`, oldExeFull),
		"timeout /t 1 /nobreak >NUL",
		fmt.Sprintf(`move /y "%s" "%s" >NUL 2>&1`, newExeFull, oldExeFull),
		"if %ERRORLEVEL% EQU 0 goto :REPLACE_OK",
		"",
		"echo Replace failed after 15 retries!",
		`del /f /q "%~f0"`,
		"exit /b 1",
		"",
		":REPLACE_OK",
		// 验证替换成功（检查新文件是否存在于目标位置）
		fmt.Sprintf(`if not exist "%s" (`, oldExeFull),
		"echo Verification failed: target not found!",
		`del /f /q "%~f0"`,
		"exit /b 1",
		")",
		"echo Replace OK, starting new version...",
		fmt.Sprintf(`start "" "%s"`, oldExeFull),
		`del /f /q "%~f0"`,
	}
	batContent := strings.Join(lines, CRLF) + CRLF

	if err := os.WriteFile(batPath, []byte(batContent), 0755); err != nil {
		return apperror.Wrap(apperror.Storage, err, "创建更新脚本失败")
	}
	applog.Info("[Updater] 创建替换脚本: %s", batPath)
	applog.Info("[Updater] 旧 exe: %s", oldExeFull)
	applog.Info("[Updater] 新 exe: %s", newExeFull)

	// 启动批处理脚本（独立进程）
	// 不使用 start 命令包裹（嵌套引号在含空格路径下解析不可靠）
	cmd := exec.Command("cmd", "/c", batPath)
	cmd.Dir = appDir
	if err := cmd.Start(); err != nil {
		os.Remove(batPath)
		return apperror.Wrap(apperror.Unavailable, err, "启动更新脚本失败")
	}

	// 退出当前程序，让脚本接管
	go func() {
		time.Sleep(500 * time.Millisecond)
		exitProcess()
	}()
	return nil
}
