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
	"cczjVideo/app/handoff"
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
	if err := ValidateInstallArtifact(filePath); err != nil {
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

// ValidateInstallArtifact 是两条安装档位共用的门口。之所以对外：「退出时安装」在点下
// 按钮那一刻先查一遍，界面就能当场说清这个包能不能装，而不是等几天之后退出时才发现。
//
// 名字对只说明"像是本更新器产出的"，不说明内容是那份被校验过的字节：下载中途被杀、
// 写盘失败都会留下半截文件，而"已下载"入口会把它原样递回这里。半截 exe 换掉本体之后
// 脚本还会清掉回滚副本，等于把应用装没了。装之前按下载时记下的摘要再核一遍。
func ValidateInstallArtifact(filePath string) error {
	// 这个函数最终会 exec 传进来的路径，而它对前端（含扩展包）是暴露的绑定，
	// 所以只允许安装本更新器落在应用目录里的产物。
	if !isUpdateArtifact(filePath) {
		applog.Warn("[Updater] 拒绝安装非更新产物: %s", filePath)
		return apperror.New(apperror.Validation, "只能安装本应用下载目录里的更新包")
	}
	if err := verifyRecordedArtifact(filePath); err != nil {
		applog.Warn("[Updater] 拒绝安装未通过摘要复核的产物 %s: %v", filePath, err)
		return err
	}
	return nil
}

// SwapOnExit 只把替换脚本交出去，不让本进程退场——它由应用的关停流程在最后一步调用。
//
// 这里重新走一遍安装校验：点下「退出时安装」和真正退出之间可能隔着几天，包可能被删、
// 被动、甚至根本没写全。校验没过就返回错误，让关停流程把这一条记进日志——退出时安装
// 失败不该拖住退出，更不该把旧版本换坏。
//
// 只支持 .exe：msi 有自己的安装界面，zip 要人手动解压，这两类都不该在没人看着的关停
// 时刻被自动跑起来。
func SwapOnExit(filePath string) error {
	if runtime.GOOS != "windows" {
		return apperror.New(apperror.Unavailable, "只有 Windows 版本支持在退出时替换程序本体")
	}
	if err := ValidateInstallArtifact(filePath); err != nil {
		return err
	}
	if strings.ToLower(filepath.Ext(filePath)) != ".exe" {
		return apperror.New(apperror.Unsupported, "退出时安装只支持可执行程序更新包")
	}
	return startSwapScript(filePath, false)
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
//
// 文件名定义在 handoff：新进程也读它，用它认出「我是刚被替换脚本拉起来的」（见 handoff.Await）。
func installResultPath() string {
	return handoff.SwapReceiptPath()
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

// runningExePath 返回当前进程 exe 的规范全路径，口径和替换脚本收到的 %~1 一致。
// .old 副本要找在哪儿、脚本要动哪个文件，都得走这一个函数：两边算出不同的目录，
// 用户点「退回上一版」就会对着一个永远不存在的副本失败。
func runningExePath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", apperror.Wrap(apperror.Storage, err, "获取当前可执行文件路径失败")
	}
	if dir, err := filepath.EvalSymlinks(filepath.Dir(exe)); err == nil {
		return filepath.Join(dir, filepath.Base(exe)), nil
	}
	return exe, nil
}

// installBySwapWindows 是「立即安装」这一档：交出替换脚本，然后让本进程退场。
// 替换规则本身见 buildSwapScript，脚本结论的回收见 TakeInstallResult。
func installBySwapWindows(newExePath string) error {
	if err := startSwapScript(newExePath, true); err != nil {
		return err
	}

	// 退出当前程序，让脚本接管
	go func() {
		time.Sleep(500 * time.Millisecond)
		exitProcess()
	}()
	return nil
}

// startSwapScript 校验新包、落脚本、以独立进程把它启动，然后立刻返回。
// startAfterSwap 决定脚本换完 exe 之后要不要把应用拉起来：「立即安装」要（用户不该
// 看到程序消失），关停流程里的「退出时安装」不要（关掉应用正是他要的那件事）。
//
// 这里刻意不 Wait：进程还活着时脚本换不动 exe，等它就是等死锁。
func startSwapScript(newExePath string, startAfterSwap bool) error {
	oldExeFull, err := runningExePath()
	if err != nil {
		return err
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

	appDir := filepath.Dir(oldExeFull)
	oldExeName := filepath.Base(oldExeFull)
	batPath := filepath.Join(appDir, swapScriptName)

	batContent := buildSwapScript(startAfterSwap)

	if err := os.WriteFile(batPath, []byte(batContent), 0755); err != nil {
		return apperror.Wrap(apperror.Storage, err, "创建更新脚本失败")
	}
	applog.Info("[Updater] 创建替换脚本: %s", batPath)
	applog.Info("[Updater] 旧 exe: %s", oldExeFull)
	applog.Info("[Updater] 新 exe: %s", newExeFull)
	applog.Info("[Updater] 换完之后是否启动新版本: %v", startAfterSwap)

	// 脚本正文里一个路径字面量都没有，四个路径全走参数。原因：Go 传参用的是宽字符
	// 命令行，cmd 拿到的是正确 Unicode；写死在脚本文本里的非 ASCII 路径会被 cmd 按
	// 控制台代码页（实测这台机器 936）重新解释，中文目录能把一整行 if 解析到别的
	// 命令上去。脚本名也用相对名 + cmd.Dir，应用目录连命令行都不出现在上面，
	// 路径里的 & ( ) 这些 cmd 元字符也就无从插手。
	//
	// 第五个参数是单实例锁的交接参数（见 buildSwapScript 第 6 条）。它对本进程 PID 取值，
	// 递给脚本时不需要再拼任何东西：格式与解析都由 app/handoff 一处定义。
	cmd := exec.Command("cmd", "/c", swapScriptName, oldExeFull, newExeFull, installResultPath(), oldExeName, handoff.Arg(os.Getpid()))
	cmd.Dir = appDir
	if err := cmd.Start(); err != nil {
		os.Remove(batPath)
		return apperror.Wrap(apperror.Unavailable, err, "启动更新脚本失败")
	}
	return nil
}

// swapScriptName 是替换脚本的文件名，放在应用目录里、以相对名启动。
const swapScriptName = "_update_swap.bat"

// buildSwapScript 生成热替换脚本的内容。脚本认五个参数：%1 旧 exe 全路径、
// %2 新 exe 全路径、%3 结论文件、%4 旧 exe 文件名、%5 单实例锁的交接参数（见 app/handoff），
// 所以正文可以保持纯 ASCII。两档安装（立即 / 退出时）共用这一份梯子，唯一分歧是形参
// startAfterSwap —— %5 是数据不是分支：两档都收，只有要 start 的那一档才会把它拼上去。
//
// 六条硬规矩：
//  1. 绝不先删旧 exe。以前 :FORCE_REPLACE 是 del 旧再 move 新，move 一失败就两头空，
//     用户丢的是整个程序；现在改成先改名再换入，换不回去就把名字改回来。
//  2. 等待必须有上限，也必须真的在等。以前按映像名轮询且没有退出条件；现在最多 30 次。
//     间隔用 ping 不用 timeout：脚本由 Go 以独立进程启动，stdin 是 NUL，timeout 撞上
//     非控制台输入会立刻报错返回（实测），30 次重试几百毫秒就烧完，本进程还没让位。
//  3. 「立即安装」这一档不管成功失败都得把应用交回去。失败还不 start 等于"点安装 →
//     程序消失 → 桌面多个没换成的目录"，用户只能自己找 exe 重开。
//  4. 「退出时安装」那一档反过来：一行 start 都不写。关掉应用本身就是用户要的事，
//     脚本不能把他刚关掉的窗口再开回来；这一档的失败靠结论文件在下次启动说话（见 ADR 0010、#195）。
//  5. 动手之前先给正在跑的 exe 留一份 .old。换入用的是 move，它把旧字节直接盖掉，
//     那份副本就再也找不回来了——而它是"一键退回上一版"（见 buildRollbackScript）
//     唯一的原料。copy 失败不拦安装：宁可有装得上的更新，也不为了退路把新版挡在门外。
//  6. 每一行 start 都要带上 %5 那个交接参数。:RENAME_REPLACE 是在 move 连败 30 次之后
//     才进去的，那一刻旧进程按定义还活着（文件锁就是它的信号），而给正在运行的 exe 改名
//     是 Windows 允许的——于是换入成功、回执写 ok、start 拉起新 exe，新进程抢在旧进程释放
//     单实例互斥体之前起来，被 Wails 判成第二实例并静默 os.Exit，旧进程随后退场。
//     用户看到的就是「点安装 → 程序关了 → 再也没回来」，而回执一切正常。带上交接参数，
//     新进程先等那个 PID 消失再抢锁（上限 30 秒，超时照常启动，不会在这儿无限等）。
func buildSwapScript(startAfterSwap bool) string {
	// 换入失败、改名失败、验证失败三种收场都要走这一段：先确认本体还在（不在就把
	// .old 改回原名），再把应用拉起来。宁可能够回滚，不可静默退出。
	//
	// startAfterSwap 是唯一分歧：「退出时安装」那一档不该把他刚关掉的窗口再开回来，
	// 所以那一版连一行 start 都没有。除了 start，两版脚本只差这一处开关，
	// 重试/改名/回执的梯子完全共用（见 TestBuildSwapScriptTiersDifferOnlyByStart）。
	restore := []string{
		`if not exist "%OLD%" ren "%OLD%.old" "%BASE%" >NUL 2>&1`,
	}
	if startAfterSwap {
		restore = append(restore, `if exist "%OLD%" start "" "%OLD%" %HANDOFF%`)
	}

	lines := []string{
		"@echo off",
		"setlocal",
		`set "OLD=%~1"`,
		`set "NEW=%~2"`,
		`set "RESULT=%~3"`,
		`set "BASE=%~4"`,
		`set "HANDOFF=%~5"`,
		// 趁本体还在原位给旧版留一份副本。读一个正在运行的 exe 是允许的（加载器按
		// share-read 打开它），所以这一步不必等进程退出。
		`copy /y "%OLD%" "%OLD%.old" >NUL 2>&1`,
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
		// 两档共用这一行文案：「退出时安装」那一版不会启动新版本，写「starting new version」
		// 就成了假话，而脚本的输出正是排查时看到的东西。
		"echo Replace OK.",
		`echo ok>"%RESULT%" 2>NUL`,
		// .old 不在这里删：它是这次替换留下的回滚副本，设置页的「退回上一版」用的就是它。
		// 下一次安装会 copy /y 覆盖掉，所以机器上最多只留一份、永远只落后一个版本。
	)
	if startAfterSwap {
		lines = append(lines, `start "" "%OLD%" %HANDOFF%`)
	}
	lines = append(lines, `del /f /q "%~f0"`)

	// 纯 ASCII + CRLF 行尾：正文里没有任何来自环境的字符串，代码页怎么设都改变不了
	// 脚本自身的解析。
	CRLF := "\r\n"
	return strings.Join(lines, CRLF) + CRLF
}

// swapBackupSuffix 是回滚副本的后缀。脚本里的 "%OLD%.old" 与 Go 侧找副本的路径
// 都指这一个文件，改一处就得改另一处。
const swapBackupSuffix = ".old"

// SwapBackupStat 定位上一次热替换留下的回滚副本，并给出它的文件信息。
// 第二个返回值为 false 表示眼下没有可用副本：路径解析不出来、文件不在、或是个目录。
// 空文件也算没有——那说明 copy 中途被打断，拿它换回本体只会把应用换成启动即崩。
func SwapBackupStat() (string, os.FileInfo, bool) {
	exe, err := runningExePath()
	if err != nil {
		return "", nil, false
	}
	path := exe + swapBackupSuffix
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || info.Size() == 0 {
		return "", nil, false
	}
	return path, info, true
}

// rollbackScriptName 是回滚脚本的文件名，和替换脚本一样放在应用目录、以相对名启动。
const rollbackScriptName = "_update_rollback.bat"

// RollbackUpdate 用 .old 副本换回上一个程序，然后让位给脚本。
//
// 这里不接收任何路径参数：副本位置由当前 exe 推出，前端只能按一个按钮。
// 这个入口是给"装上了但新版本更糟"准备的退路，所以校验只做在该做的那一层——
// 文件在不在、能不能换回去，剩下的都交给脚本按替换脚本那套规矩处理。
func RollbackUpdate() error {
	if runtime.GOOS != "windows" {
		return apperror.New(apperror.Unavailable, "只有 Windows 版本支持退回上一个程序副本")
	}
	exe, err := runningExePath()
	if err != nil {
		return err
	}
	backup, info, ok := SwapBackupStat()
	if !ok {
		return apperror.New(apperror.NotFound, "没有找到可退回的上一版程序副本")
	}

	appDir := filepath.Dir(exe)
	batPath := filepath.Join(appDir, rollbackScriptName)
	if err := os.WriteFile(batPath, []byte(buildRollbackScript()), 0755); err != nil {
		return apperror.Wrap(apperror.Storage, err, "创建回滚脚本失败")
	}
	applog.Info("[Updater] 创建回滚脚本: %s", batPath)
	applog.Info("[Updater] 退回副本: %s (%d 字节, %s)", backup, info.Size(), info.ModTime().Format(time.RFC3339))

	// 参数走命令行、脚本正文保持纯 ASCII，理由和 buildSwapScript 里那段注释一样。
	// 第四个参数是交接参数：回滚那一档也有"进程还没让位就把应用又拉一次"的时刻（见 :BACK_FAILED）。
	cmd := exec.Command("cmd", "/c", rollbackScriptName, exe, backup, installResultPath(), handoff.Arg(os.Getpid()))
	cmd.Dir = appDir
	if err := cmd.Start(); err != nil {
		os.Remove(batPath)
		return apperror.Wrap(apperror.Unavailable, err, "启动回滚脚本失败")
	}

	go func() {
		time.Sleep(500 * time.Millisecond)
		exitProcess()
	}()
	return nil
}

// buildRollbackScript 生成回滚脚本：%1 当前 exe 全路径、%2 副本全路径、%3 结论文件、
// %4 单实例锁的交接参数（见 app/handoff）。
//
// 它不复用替换脚本，因为替换脚本的 :RENAME_REPLACE 一进门就 del 掉 "%OLD%.old"：
// 把参数对调过去，第一件事是把回滚的原料删了，然后才发现换不回去。
//
// 规矩和替换脚本一致：有界重试等进程让位（move 撞分享冲突就是还没让位）、
// 不先删本体（move 是一步替换，目标被占用时源文件原地不动）、
// 成败都得把应用交回去、而且每一行 start 都带交接参数（换不回去那一支里本进程
// 按定义还活着，不带交接参数就是"点退回 → 程序关了 → 没回来"），
// 结论一律落文件（rollback_ok / rollback_failed）。
func buildRollbackScript() string {
	lines := []string{
		"@echo off",
		"setlocal",
		`set "OLD=%~1"`,
		`set "BACK=%~2"`,
		`set "RESULT=%~3"`,
		`set "HANDOFF=%~4"`,
		"echo Waiting for the running process to release the executable...",
		"set RETRY=0",
		":RETRY_BACK",
		`move /y "%BACK%" "%OLD%" >NUL 2>&1`,
		"if %ERRORLEVEL% EQU 0 goto :BACK_OK",
		"",
		"set /a RETRY+=1",
		"if %RETRY% GEQ 30 goto :BACK_FAILED",
		"echo Rollback failed (retry %RETRY%), waiting...",
		"ping -n 3 127.0.0.1 >NUL",
		"goto RETRY_BACK",
		"",
		// 换不回去：副本留在原地（用户还能再点一次或手工处理），本体保持不动。
		":BACK_FAILED",
		"echo Rollback failed. The current version is kept.",
		`echo rollback_failed>"%RESULT%" 2>NUL`,
		`if exist "%OLD%" start "" "%OLD%" %HANDOFF%`,
		`del /f /q "%~f0"`,
		"exit /b 1",
		"",
		":BACK_OK",
		"echo Rollback OK, starting the previous version...",
		`echo rollback_ok>"%RESULT%" 2>NUL`,
		// 副本已被 move 消费掉，这里不需要再清理它；旧版从此就是本体。
		`start "" "%OLD%" %HANDOFF%`,
		`del /f /q "%~f0"`,
	}

	// CRLF 行尾，理由同替换脚本。
	CRLF := "\r\n"
	return strings.Join(lines, CRLF) + CRLF
}
