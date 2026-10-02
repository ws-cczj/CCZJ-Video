package updater

import (
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

	// 名字对只说明"像是本更新器产出的"，不说明内容是那份被校验过的字节：下载中途被杀、
	// 写盘失败都会留下半截文件，而"已下载"入口会把它原样递回这里。半截 exe 换掉本体之后
	// 脚本还会清掉回滚副本，等于把应用装没了。装之前按下载时记下的摘要再核一遍。
	if err := verifyRecordedArtifact(filePath); err != nil {
		applog.Warn("[Updater] 拒绝安装未通过摘要复核的产物 %s: %v", filePath, err)
		return err
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

// installResultPath 是替换脚本回写结论的位置。
//
// 脚本必须先于本进程存活，所以 cmd.Start() 之后不能 Wait——等了就是死锁：进程不退出
// 脚本就换不动 exe，脚本不写完进程就取不到结论。结论只能跨进程传：脚本落一个文件，
// 下一次启动读掉。读掉即删，一次性通道不会留下垃圾。
func installResultPath() string {
	return filepath.Join(os.TempDir(), "cczj_video_update_result")
}

// TakeInstallResult 取回上一次热替换脚本写下的结论，并消费掉它。
// 返回的第二个值为 false 表示没有结论文件（首次启动，或非 Windows 路径）。
func TakeInstallResult() (string, bool) {
	path := installResultPath()
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	_ = os.Remove(path)
	return strings.TrimSpace(string(data)), true
}

// installBySwapWindows 通过批处理脚本实现 exe 热替换：
// 校验更新包 → 落脚本 → 独立进程启动脚本 → 本进程让位退出。
// 替换规则本身见 buildSwapScript，脚本结论的回收见 TakeInstallResult。
func installBySwapWindows(newExePath string) error {
	oldExe, err := os.Executable()
	if err != nil {
		return apperror.Wrap(apperror.Storage, err, "获取当前可执行文件路径失败")
	}
	oldExe, err = filepath.EvalSymlinks(oldExe)
	if err != nil {
		return apperror.Wrap(apperror.Storage, err, "解析可执行文件路径失败")
	}

	newExeFull, err := filepath.Abs(newExePath)
	if err != nil {
		return apperror.Wrap(apperror.Validation, err, "解析更新包路径失败")
	}
	// 装一个不存在或空的文件，换来的只是一个启动即消失的 exe。
	info, err := os.Stat(newExeFull)
	if err != nil {
		return apperror.Wrap(apperror.Validation, err, "更新包不可用")
	}
	if info.Size() == 0 {
		return apperror.New(apperror.Validation, "更新包是空文件，已放弃安装")
	}

	appDir := filepath.Dir(oldExe)
	oldExeFull := oldExe
	oldExeName := filepath.Base(oldExe)
	batPath := filepath.Join(appDir, swapScriptName)

	batContent := buildSwapScript()

	if err := os.WriteFile(batPath, []byte(batContent), 0755); err != nil {
		return apperror.Wrap(apperror.Storage, err, "创建更新脚本失败")
	}
	applog.Info("[Updater] 创建替换脚本: %s", batPath)
	applog.Info("[Updater] 旧 exe: %s", oldExeFull)
	applog.Info("[Updater] 新 exe: %s", newExeFull)

	// 脚本正文里一个路径字面量都没有，四个路径全走参数。原因：Go 传参用的是宽字符
	// 命令行，cmd 拿到的是正确 Unicode；写死在脚本文本里的非 ASCII 路径会被 cmd 按
	// 控制台代码页（实测这台机器 936）重新解释，中文目录能把一整行 if 解析到别的
	// 命令上去。脚本名也用相对名 + cmd.Dir，应用目录连命令行都不出现在上面，
	// 路径里的 & ( ) 这些 cmd 元字符也就无从插手。
	cmd := exec.Command("cmd", "/c", swapScriptName, oldExeFull, newExeFull, installResultPath(), oldExeName)
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

// swapScriptName 是替换脚本的文件名，放在应用目录里、以相对名启动。
const swapScriptName = "_update_swap.bat"

// buildSwapScript 生成热替换脚本的内容。脚本只认四个参数：%1 旧 exe 全路径、
// %2 新 exe 全路径、%3 结论文件、%4 旧 exe 文件名，所以正文可以保持纯 ASCII。
//
// 三条硬规矩：
//  1. 绝不先删旧 exe。以前 :FORCE_REPLACE 是 del 旧再 move 新，move 一失败就两头空，
//     用户丢的是整个程序；现在改成先改名再换入，换不回去就把名字改回来。
//  2. 等待必须有上限，也必须真的在等。以前按映像名轮询且没有退出条件；现在最多 30 次。
//     间隔用 ping 不用 timeout：脚本由 Go 以独立进程启动，stdin 是 NUL，timeout 撞上
//     非控制台输入会立刻报错返回（实测），30 次重试几百毫秒就烧完，本进程还没让位。
//  3. 不管成功失败都得把应用交回去。失败还不 start 等于"点安装 → 程序消失 →
//     桌面多个没换成的目录"，用户只能自己找 exe 重开。
func buildSwapScript() string {
	// 换入失败、改名失败、验证失败三种收场都要走这两行：先确认本体还在（不在就把
	// .old 改回原名），再把它拉起来。宁可能够回滚，不可静默退出。
	restore := []string{
		`if not exist "%OLD%" ren "%OLD%.old" "%BASE%" >NUL 2>&1`,
		`if exist "%OLD%" start "" "%OLD%"`,
	}

	lines := []string{
		"@echo off",
		"setlocal",
		`set "OLD=%~1"`,
		`set "NEW=%~2"`,
		`set "RESULT=%~3"`,
		`set "BASE=%~4"`,
		// 不查 tasklist：进程还活着时 exe 本身就是锁住的，move 一定失败。
		// 按映像名匹配会被机器上另一份同名 exe 卡住，按 PID 匹配又依赖 tasklist 的
		// 列格式和本地化输出，都不如直接拿文件锁当信号。有界重试就是等待退出。
		"echo Waiting for the old process to release the executable...",
		"set RETRY=0",
		":RETRY_REPLACE",
		`move /y "%NEW%" "%OLD%" >NUL 2>&1`,
		"if %ERRORLEVEL% EQU 0 goto :REPLACE_OK",
		"",
		"set /a RETRY+=1",
		"if %RETRY% GEQ 30 goto :RENAME_REPLACE",
		"echo Move failed (retry %RETRY%), waiting...",
		// 约 2 秒一次：发 3 个包、间隔两个，实测 2.03 秒，输出丢给 NUL 不影响等待。
		"ping -n 3 127.0.0.1 >NUL",
		"goto RETRY_REPLACE",
		"",
		// 直接换不动（文件仍被占用）：先把旧 exe 改名让位，改名是可逆的，删除不是。
		":RENAME_REPLACE",
		"echo Move failed, renaming the old binary aside...",
		// ren 不会覆盖同名目标。上一次中断（断电、杀进程、回滚也失败）留下的 .old 会让
		// 这里永远失败，而这是唯一能换入新版的分支——一次卡死就再也没能装上过。
		// 删掉的只是上一版自己的副本，本体此刻还挂在旧名字上。
		`del /f /q "%OLD%.old" >NUL 2>&1`,
		// ren 的第二个参数只能是文件名：给它带路径，cmd 直接报"文件名、目录名或卷标
		// 语法不正确"（实测 rc=1），整条补救分支等于不存在。
		`ren "%OLD%" "%BASE%.old" >NUL 2>&1`,
		"if %ERRORLEVEL% NEQ 0 goto :FAILED",
		`move /y "%NEW%" "%OLD%" >NUL 2>&1`,
		"if %ERRORLEVEL% EQU 0 goto :REPLACE_OK",
		// 换入失败就把原名还回去，绝不留下一个没有本体的目录。
		`ren "%OLD%.old" "%BASE%" >NUL 2>&1`,
		"",
		":FAILED",
		"echo Replace failed. The current version is kept.",
		`echo swap_failed>"%RESULT%" 2>NUL`,
	}
	lines = append(lines, restore...)
	lines = append(lines,
		`del /f /q "%~f0"`,
		"exit /b 1",
		"",
		":REPLACE_OK",
		// 验证替换成功（检查新文件是否存在于目标位置）
		`if exist "%OLD%" goto :START_NEW`,
		"echo Verification failed: target not found!",
		`echo verify_failed>"%RESULT%" 2>NUL`,
	)
	lines = append(lines, restore...)
	lines = append(lines,
		`del /f /q "%~f0"`,
		"exit /b 1",
		"",
		":START_NEW",
		"echo Replace OK, starting new version...",
		`echo ok>"%RESULT%" 2>NUL`,
		// .old 不在这里删：它是这次替换唯一的回滚副本，留着等新版本真的跑起来，
		// 由下一次启动的 Go 侧确认版本对得上再清理（见 ClearSwapBackup）。
		`start "" "%OLD%"`,
		`del /f /q "%~f0"`,
	)

	// 纯 ASCII + CRLF 行尾：正文里没有任何来自环境的字符串，代码页怎么设都改变不了
	// 脚本自身的解析。
	CRLF := "\r\n"
	return strings.Join(lines, CRLF) + CRLF
}

// ClearSwapBackup 删除热替换留下的 .old 回滚副本。
// 只有在确认当前跑的就是编译版本（即 .installed_version 已对齐）之后才该调用：
// 新版本起不来时，那个文件是用户唯一还能退回旧版的东西。
func ClearSwapBackup() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	if dir, err := filepath.EvalSymlinks(filepath.Dir(exe)); err == nil {
		exe = filepath.Join(dir, filepath.Base(exe))
	}
	path := exe + ".old"
	if err := os.Remove(path); err != nil {
		if !os.IsNotExist(err) {
			applog.Warn("[Updater] 清理替换备份失败: %v", err)
		}
		return
	}
	applog.Info("[Updater] 已清理替换备份: %s", path)
}
