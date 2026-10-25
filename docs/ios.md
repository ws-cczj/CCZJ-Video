# iOS 本地应用构建与验收

iOS 使用现有 Go 服务、Vue 移动宿主和 SQLite；不部署应用服务器。当前为适配阶段，Wails beta.30 的 iOS 支持仍属实验性。Windows 检查、Android 回归、iOS 编译、模拟器运行与 iPhone 真机验收分别报告，不能互相替代。

## 只有 Windows 时

仓库提供手动触发的 [iOS workflow](../.github/workflows/ios.yml)。将代码推送到自己的 GitHub 仓库后，在 Actions 中选择 **iOS build and simulator smoke test → Run workflow**。流程使用 macOS runner 编译，并且不读取 Apple 账号、签名证书或手机数据，不上传 Release。

- `device` 作业生成 `ios-device-requires-signing` artifact，内含 `*-resign.ipa`。它仅带本地完整性签名，必须用个人 Apple 账号重新签名后才能在 iPhone 安装。
- `simulator` 作业创建一个独立的临时 iOS 模拟器，验证三种模式启动、SQLite 可用以及重启后数据保留；输出 `ios-simulator-evidence` 的日志、结果和截图。截图仅供人工检查，脚本不把截图存在当作界面交互通过。
- 任一作业失败时先查看日志。设备包编译通过不能证明模拟器或真机功能通过。私有仓库的 macOS 作业可能消耗 Actions 额度，运行前按自己的账户额度安排。

Windows 可先生成和检查工程配置，无需连接手机：

```powershell
python build/ios/build.py --prepare-only
```

生成物位于被忽略的 `build/ios/generated/`，不提交机器绝对路径。脚本要求 CLI 与 `go.mod` 的 Wails 版本一致；iOS 版本从 `version.json` 读取，不维护第二份版本号。最低 iOS 16。

## macOS 本地构建

需要完整 Xcode（包含 iOS SDK）、已安装的 iOS 模拟器 runtime、项目 Go／Node 与匹配的 Wails CLI。安装前端依赖后运行：

```bash
npm --prefix frontend ci
python3 build/ios/build.py --platform device
python3 build/ios/build.py --platform simulator
python3 scripts/test-ios-simulator.py
```

构建输出在 `bin/ios/device/` 和 `bin/ios/simulator/`。模拟器测试只创建、修改和删除自身的临时设备，不卸载已有模拟器中的应用。其证据在 `dist/audit/ios-simulator/`。`--skip-frontend` 仅用于前端刚构建且此后未改动的情况。`--bundle-id` 可选择自签所需的唯一反向域名身份；日后覆盖安装和续签要保持该身份与账号一致。

## 个人安装与续签

Windows 可按 [AltStore Classic 官方步骤](https://faq.altstore.io/altstore-classic/how-to-install-altstore-windows) 安装 AltServer、连接并信任 iPhone，然后安装 AltStore。iOS 16 及以上需要按提示启用开发者模式。在手机 AltStore 中导入待签名 IPA，使用自己的免费 Apple 账号安装。不要将账号密码或证书放进仓库或 workflow。

免费 Personal Team 的安装配置有效期为七天，建议到期前刷新。AltStore 刷新需要能连接电脑上的 AltServer（同一 Wi-Fi 或 USB）；电脑不必在日常使用时保持在线。到期不能打开时通过同一账号覆盖安装，不要先卸载；备份仍由用户定期导出。详见 [Apple 免费账号限制](https://developer.apple.com/help/account/basics/about-your-developer-account)与 [AltStore 刷新说明](https://faq.altstore.io/altstore-classic/your-altstore)。

## 平台差异

- 通过原生环境选择移动宿主，不按屏幕宽度或 UA 猜测。iOS 开启内联播放，使用 UIKit 安全区域和现有手机布局。
- iOS 优先走原生 HLS。`/__cczj/mobile-media` 提供当前进程的临时本机媒体地址，`/__cczj/ios-hls` 的子清单、密钥和片段保留随机访问路径。监听只绑定 `127.0.0.1`，应用退出时关闭；它不是需部署的远程服务器。Android 原端点仍保留兼容。
- 普通图片与文件选择沿用 Wails／WebKit；备份、日志和诊断的分享通过已有文件入口检查应用目录和真实文件后交给 UIKit。分享面板打开不代表用户已经另存成功，取消仍保留本地导出。
- 沉浸使用现有页面布局覆盖应用视口，不承诺隐藏 iOS 系统栏。画中画按 WebKit 视频能力探测，不能用 Android 桥接能力推断。
- 音量由 iPhone 音量键或控制中心调整，不显示无效的手势音量百分比。阅读／播放亮度在缺少原生控制时使用现有画面遮罩。
- iOS 不挂载 Android APK 更新控制器；关于页说明重新签名覆盖安装。后端拒绝直接执行 iOS 更新包。
- 当前不开放 iOS DLNA 投屏入口：组播 entitlement、局域网权限与拒绝恢复尚未实现和验证。免费自签不保证可以使用相关 entitlement。
- 不声明通用后台下载能力。iOS 可挂起或终止后台应用；下载恢复沿用已有持久化任务，后台／锁屏与中断恢复仍需实测。

## iPhone 集中验收

手机无需长期连接开发电脑。首个可安装包生成后，选择方便的时间安装并检查：

1. 冷启动、拒绝／接受条款、视频／小说／漫画切换与搜索。
2. 导入真实来源，阅读小说和漫画，重开应用后核对书架、进度和偏好。
3. HLS 播放、加密／多层清单、切线、拖动、字幕、横竖屏、沉浸退出及画中画进入／退出。
4. 文件选择取消后重试，备份导出到“文件”、分享取消、选择文件导入和诊断报告分享。
5. 锁屏、切后台、恢复、断网与重试；下载中断后恢复；系统音量与画面亮度。
6. 同账号续签、同身份覆盖安装后核对数据。测试前先导出备份，不用卸载重装代替覆盖升级。

失败时记录 iOS 版本、操作步骤与现象，并通过设置中的诊断／日志分享导出；报告由用户主动提供，不自动上传。当前 Windows 环境无法给出 iOS 编译和真机通过结论。
