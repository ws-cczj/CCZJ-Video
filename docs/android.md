# Android 开发与验收

Android 当前为预览版。手机通常使用 ARM64，MuMu 使用 x86_64；二者需要分别编译原生库，不能通过改 APK 文件名互换。最低 Android API 24。构建版本由根目录 `version.json` 注入。

## 环境与构建

Windows 需要项目 Go、Node、Wails v3 CLI，以及 Android SDK、NDK、JDK 17+。`build/android/build.ps1` 临时选择工具链并在结束后恢复环境，不修改机器默认 Java。

```powershell
adb devices -l

# ARM64 手机 Debug 预览包
wails3 task android:build

# MuMu 构建并安装到指定设备
wails3 task android:run ANDROID_ARCH=amd64 DEVICE=emulator-5554

# 独立验收包，避免改动用户预览应用数据
powershell -NoProfile -ExecutionPolicy Bypass -File build/android/build.ps1 `
  -Arch amd64 -ApplicationId com.cczj.video.acceptance -Install -Device emulator-5554
```

预览包名是 `com.cczj.video.preview`，验收包名是 `com.cczj.video.acceptance`。`-SkipFrontend` 只用于前端已构建且之后没有改动的情况。Release 签名选择、不可调试 APK 检查与 GitHub 文件准备见 [发布流程](releases.md)。

## MuMu 原生应用的前端热更新

```powershell
# 首次安装宿主，或 Go／Java／绑定变动后
wails3 task android:dev INSTALL_HOST=1 DEVICE=emulator-5554

# 只改 Vue／CSS／TypeScript，继续使用已安装宿主
wails3 task android:dev DEVICE=emulator-5554
```

普通桌面 `wails3 dev` 不会自动替换模拟器中已内嵌的页面。热更新用于界面迭代，发布和原生验收仍需重新构建 APK。

## 平台能力与入口

- 原生环境先经 `api/runtime.ts` 初始化，布局由 `platform/presentation.ts` 选择；不能把窄窗口当成 Android。
- 桌面窗口、托盘、资源管理器与退出时安装属于桌面能力。Android 使用宿主的文件选择、只读文件分享和系统 APK 安装器。
- 视频全屏要恢复方向、系统栏、屏幕边距与亮度；返回键先处理全屏、弹层和管理状态，再处理应用导航。
- 文件选择取消后可以再次打开，分享取消不应丢失本地导出，权限拒绝后应有可执行的恢复入口。
- 设备差异需要实测。MuMu 通过不等于鸿蒙 4 真机通过，APK 构建成功不等于安装、播放和升级成功。

## 投屏发现与接收协议

应用内设备列表使用 DLNA／UPnP AVTransport，电视或盒子需要开启相应接收服务。同一 Wi-Fi 不是协议兼容的保证；海康部分交互屏提供 [WonderCast、Miracast 或 Chromecast](https://pro-av.hikvision.com/en/products/interactive-flat-panel-display/interactive-flat-panels/ultra-series/)，不能据此认定未知型号支持 DLNA。Android 投屏面板提供“打开系统无线投屏”入口，通过 `ACTION_CAST_SETTINGS` 进入系统连接；WonderCast 专用接收仍需配套助手。

`app/cast/discovery.go` 在每个有效私网 IPv4 组播接口上发送 MediaRenderer、AVTransport、根设备和全部设备查询，间隔 1.1 秒重发一次，接收窗口 4.5 秒，总任务最多 8 秒。描述获取使用三路有界队列，按地址去重；只接受与回复发送端同 IP 的私网 HTTP 描述和控制地址，不跟随跳转。请求取消或宿主退出会关闭套接字、停止描述请求并等待工作结束。Android Wi-Fi 组播锁仅在搜索期间持有；准备阶段失败和关闭面板也必须释放搜索状态。

回归检查：

```powershell
$env:CCZJ_TEST_LAN='1'
go test ./app/cast -run 'TestSSDP|TestDiscoveryLAN' -count=1 -v
python scripts/test-cast-browser.py --port 19320
python scripts/test-cast-android.py --device emulator-5554 --host-ip <接收端可访问的电脑局域网IPv4>
```

浏览器脚本需在 19320 端口启动前端 Vite，验证生产投屏面板的取消、迟到结果、错误重试、系统设置失败恢复及横竖屏。原生脚本使用独立 acceptance Debug 包，并要求另行安装、启用一个真正的 DLNA 接收端；测试会发送合成视频、控制接收端、打开系统投屏设置并按返回键验证再次搜索。Kodi 可从[官方 Android 下载页](https://kodi.tv/download/android/)获取，按[官方 UPnP 设置说明](https://kodi.wiki/view/Settings/Services/UPnP_DLNA)开启接收能力。同模拟器测试先打开 Kodi，等待接收服务启动，再运行脚本，避免接收应用在后台被系统冻结。

本次使用官方稳定版 Kodi 21.3，在 MuMu 中验证真实 SSDP、描述请求、SOAP 播放控制及接收端拉取和播放视频，证据在 `dist/audit/cast-discovery/`。Go 常规测试、局域网回归和 vet 已通过；本机 race 测试二进制启动失败（Windows `0xc0000139`），未获得 race 结论。海康实体电视、鸿蒙 4 手机、Windows 原生投屏及路由器 Wi-Fi 组播尚未连接验收，不把同模拟器接收端测试算作这些环境通过。

## 可重复验收

Python 环境要求见 `scripts/native-profile-requirements.txt`，WebView 调试使用 Debug 宿主；测试连接复用 `scripts/android_webview.py`。以下脚本固定使用独立验收包，不会确认安装或发送分享文件：

```powershell
python scripts/test-mobile-settings-capabilities.py --device emulator-5554 --output dist/audit/mobile-settings
python scripts/test-android-file-input.py --device emulator-5554 --output dist/audit/android-file-input
python scripts/test-update-installer-android.py --device emulator-5554 --output dist/audit/update-installer
```

安装交接脚本使用本地 APK 样本，需先准备对应验收与发布产物，具体前置条件见脚本。其他原生脚本可能默认连接预览包，运行前检查参数和数据影响。没有获得恢复方案时，不通过清空用户应用数据绕过问题。

长按临时倍速回归用 `scripts/test-player-hold.py`：默认连接本地 Vite 测试页，`--android --device emulator-5554` 连接预览包的原生 WebView，复用 Android 调试入口装入当前前端。它用本地循环视频检查真实触摸、持续倍速提示、松手／取消／全屏／换集恢复及选定倍速不被临时值覆盖，结束恢复原页面和播放器偏好。开发宿主通过不代替正式 APK 或华为真机验收。

最新结果见 [移动设置验收](../reports/mobile-settings-2026-10-22/README.md)、[阅读性能验收](../reports/reading-performance-2026-10-22/README.md)；原始移动方案保存在[历史归档](../reports/history/mobile.md)。

弱网与源保存验收使用 `python scripts/test-playback-weak-network-android.py --device <adb设备号>`。先安装内嵌当前 `frontend/dist` 的 acceptance Debug 包；脚本复用 WebView 调试与 adb reverse，在原生手机表单保存临时源，检查立即显示、搜索拒绝后的同源网页建议回退和日志，再在原生播放器实际解码 HLS，经历 3 Mbps → 650 Kbps → 3 Mbps 的总带宽变化。`--scenario offline` 增加五秒断流。结束删除临时源并恢复设置、路由，不清空应用数据；证据位于 `dist/audit/weak-network/android-*.json`。MuMu 上的安装、源操作与弱网播放结论仅适用于该 Android 宿主，仍不等于实体手机的蜂窝切换、功耗和解码兼容性通过。

线路清晰度的原生验收入口为 `python scripts/test-playback-routes-android.py --device <adb设备号>`，同样要求当前 acceptance Debug 包。现有脚本覆盖 Go 元数据探测、远端预览不入库、清晰度排序后的实际触摸切换；其左下角提示断言和底栏全屏线路选择器属于旧布局。当前改为加载区阶段文案、右上角全屏线路入口、可拖动的底部抽屉及全屏上下集按钮，按用户要求仅完成代码，未运行新验收或打包。再次运行脚本前应同步这些交互断言，旧截图不作为新布局通过证据。说明与桌面验收入口见[播放线路清晰度](request-pipelines.md#播放线路清晰度)，证据放 `dist/audit/playback-routes/`。

## 移动路由共享背景

`MobileRouteStage` 在路由场景之外保留一个背景层，页面本身透明。视频模式沿用用户主题图片和遮罩，路由通过有限、稳定的取景坐标移动同一背景；取景区域预留 15% 边缘，只动画 transform。手势预览插值坐标，取消恢复原取景；设置子页、返回和连续导航不重建图片层。背景与内容同向但位移更小，页面内容改为完整滑出，避免透明页面相互叠字。桌面维持原绘制方式，小说／漫画保留各自画布色，阅读器及播放器继续拥有不透明的内容底色。系统减少动态效果、页面动画关闭都禁用背景过渡。

重复验收：安装与当前 `frontend/dist` 匹配的独立 acceptance Debug 包后，运行 `python scripts/test-mobile-backdrop.py --device emulator-5554`。脚本在宿主中临时展示已有内置水墨背景，不持久修改用户主题；检查单背景节点、跨页、滑动取消、动画逐帧边缘覆盖、减少动态效果和模式隔离，结束恢复模式和页面。证据放 `dist/audit/shared-backdrop/`；帧回调不是硬件 FPS，MuMu 验证不能代替鸿蒙真机。
