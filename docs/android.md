# Android 开发与验收

当前项目按用户要求由用户操作 MuMu 或真机验收，代理默认只负责修改、编译、静态检查及安装交付。用户再次明确要求代理操作设备时，只通过 ADB／WebView 调试端口，不使用 computer-use，也不新增或扩写自动化测试代码、夹具及验收脚本。下文已有脚本作为历史入口保留，不作为默认验收流程。

移动系统栏跟随当前可见表面：启动动画显示期间使用启动背景；首页、短剧、搜索、收藏和设置五个主路由共用顶部渐变，系统栏对背景叠加渐变后取色；普通播放页和已实际显示视频的短剧使用黑底白字。短剧空状态和剧场目录沿用页面背景，不能仅根据短剧路由切黑。视频加载失败在播放器内部居中显示，提供重试、切换线路和返回的真实入口。样式参考 [Video.js ErrorDisplay](https://docs.videojs.com/error-display.js.html) 与 [Android SplashScreen](https://developer.android.com/develop/ui/views/launch/splash-screen)，复用项目控件与原生桥接。

Android 当前为预览版。手机通常使用 ARM64，MuMu 的原生架构通常为 x86_64；先用 `adb shell getprop ro.product.cpu.abilist` 核对可运行的 ABI，部分 MuMu 实例同时支持 ARM64 发布包。仅支持 x86_64 的设备需要对应原生库，不能通过改 APK 文件名互换。最低 Android API 24。构建版本由根目录 `version.json` 注入。

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

## 覆盖升级与数据排查

相同包名、相同签名的覆盖安装应保留应用数据；构建脚本使用 `adb install -r`（见 [Android 官方安装参数](https://developer.android.com/tools/adb#move)）。2.4.3 与当前版本都使用宿主 `getFilesDir()/CCZJ Video/cczj_video.db`；SQLite 在迁移前保存 `schema-backups`，不通过删库升级。预览与验收包使用不同私有目录，不能用验收包的空库判断预览包数据丢失。

覆盖后列表为空时，先保留现场并在设置中导出诊断和启动日志，核对版本、包名、`data_dir`、schema 版本、来源身份／启用状态、目录数量与分类可见规则。无数据提示只表示当前列表为空，不能据此认定物理数据库被删除。`yyzy_tv` 不是运行时内置的视频来源；来源由用户添加或导入、备份恢复及原有数据库保留，具体现场以源保存／导入日志为准。诊断前不要卸载、清除应用数据或重建库；需要恢复时先导出当前内容备份，再核实已有迁移备份。

真实 Release 的数据比对可使用同证书 instrumentation，Android 会校验测试包和目标包的签名。`scripts/android-upgrade-audit.py` 使用本机既有预览密钥构建测试包，停止预览进程后复制 SQLite、WAL／SHM 和日志，记录版本、UID、安装时间、目录、代理／VPN 和逐表摘要；不替换目标 APK，不注入业务数据。每次使用新的 label，避免混用快照。安装新版本前后分别执行：

```powershell
python scripts/android-upgrade-audit.py --device emulator-5554 --label before-install
# 在应用内下载并经系统安装器覆盖安装，然后保存另一份快照
python scripts/android-upgrade-audit.py --device emulator-5554 --label after-install
# 明确需要重新测试下载时：先保存快照，再仅删除私有 updates 目录的暂存 APK 及校验文件
python scripts/android-upgrade-audit.py --device emulator-5554 --label before-redownload --delete-downloads
```

2026-10-27 在支持 ARM64 的 MuMu 中安装原始 2.4.4 Release，实际添加来源并采集 62 条目录、收藏一部影片、播放至 343.555189 秒，再从应用内下载并交给系统覆盖为原始 2.4.5 Release。八张业务表的行数和内容摘要全部一致，UID、首次安装时间和数据目录不变；升级后界面仍可见收藏。证据位于 `dist/audit/upgrade-244-245/`。这条链路未复现丢失，不能代替用户手机上 2.4.3 升级现场的诊断。

重下载前已删除上次暂存 APK。Android 默认代理和全局代理均为空、VPN 未启用；Windows 的 Clash 仅启用手动代理，无 TUN 路由。实际 Android 下载日志显示 GitHub 直连超时，选择 `v6.gh-proxy.org` 完成 55,703,193 字节下载并通过校验。本次结果说明该环境无需本机 Clash 代理即可完成更新，不保证所有网络和公共前缀持续可用。

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

投屏面板以附近设备和刷新为主要入口，网络排查说明收进“连接帮助”，不提供没有配套接收端的扫码连接。接收端确认 PLAYING 且没有媒体错误后才暂停手机播放；如果设备报告了位置，还须看到时间大于零。发送指令成功、电视尚未播放或状态请求失败时不提前中断本地播放。确认投屏后阻止 Android 返回前台时自动恢复手机视频，停止投屏、换片或用户明确点击本地播放后解除这项保护。

`app/cast/discovery.go` 在每个有效私网 IPv4 组播接口上发送 MediaRenderer、AVTransport、根设备和全部设备查询，间隔 1.1 秒重发一次，接收窗口 4.5 秒，总任务最多 8 秒。描述获取使用三路有界队列，按地址去重；只接受与回复发送端同 IP 的私网 HTTP 描述和控制地址，不跟随跳转。请求取消或宿主退出会关闭套接字、停止描述请求并等待工作结束。Android Wi-Fi 组播锁仅在搜索期间持有；准备阶段失败和关闭面板也必须释放搜索状态。

回归检查：

```powershell
$env:CCZJ_TEST_LAN='1'
go test ./app/cast -run 'TestSSDP|TestDiscoveryLAN' -count=1 -v
python scripts/test-cast-browser.py --port 19320
python scripts/test-cast-android.py --device emulator-5554 --host-ip <接收端可访问的电脑局域网IPv4>
# 连同实际播放页触摸、连接状态与停止操作一起检查：
python scripts/test-cast-android.py --device emulator-5554 --host-ip <接收端可访问的电脑局域网IPv4> --ui
```

浏览器脚本需在 19320 端口启动前端 Vite，验证生产投屏面板的取消、迟到结果、错误重试、系统设置失败恢复及横竖屏。原生脚本使用独立 acceptance Debug 包，并要求另行安装、启用一个真正的 DLNA 接收端；测试会发送合成视频、控制接收端、打开系统投屏设置并按返回键验证再次搜索。Kodi 可从[官方 Android 下载页](https://kodi.tv/download/android/)获取，按[官方 UPnP 设置说明](https://kodi.wiki/view/Settings/Services/UPnP_DLNA)开启接收能力。同模拟器测试先打开 Kodi，等待接收服务启动，再运行脚本，避免接收应用在后台被系统冻结。

MuMu 的接收测试还需要完成 Kodi 首次启动权限流程，开启 UPnP support 和 Allow remote control via UPnP；仅安装 APK 不会启用接收服务。2026-10-27 复测时这两个开关关闭、应用停在权限提示，发送端得到空列表。完成接收端初始化并重启后，实际发现、视频拉取、播放位置前进、暂停／继续／跳转／停止及系统设置返回后再搜索通过，证据在 `dist/audit/cast-pairing/native/`。同模拟器后台接收端长时间闲置可能被挂起，脚本先把 Kodi 带到前台再搜索；这项准备不替代真实路由器和实体电视验收。

加 `--ui` 会创建一个临时视频源，在生产播放页点开“更多设置 → 工具 → 投屏到电视”，选择真实接收端，确认接收端拉取媒体、界面显示已播放后手机暂停，并通过面板停止投屏。结束时删除测试源，恢复原有源启用状态、私网设置和页面；证据在 `dist/audit/cast-pairing/native-ui/`。

2.4.4 使用 Kodi 21.3，在 MuMu 中验证真实 SSDP、描述请求、SOAP 操作、接收端拉取视频，以及 `GetTransportInfo` 返回 PLAYING、`GetPositionInfo` 的播放时间实际前进。暂停／继续／跳转／停止及系统设置返回后再次搜索通过，证据在 `dist/audit/cast-244/`。同模拟器接收端需要保持前台视频 Surface；脚本在发起播放前切到 Kodi。Go 投屏测试和本机 race 检查通过。海康实体电视、鸿蒙 4 手机、Windows 原生投屏及路由器 Wi-Fi 组播尚未连接验收，不把同模拟器接收端测试算作这些环境通过。

投屏诊断按 `cast_device_id` 和 `cast_session_id` 串联各次控制请求，RPC 的 request／operation 身份保留。`cast.discovery.*` 记录接口、查询次数、收到／拒绝的回复与耗时；`cast.description.*` 区分发现后设备描述失败；`cast.soap.*` 记录操作、HTTP 状态、UPnP 错误码、响应大小／摘要和耗时；`cast.status` 记录接收端播放状态及位置。空设备列表不等于视频错误，SOAP 成功不等于解码成功；持续 STOPPED／ERROR_OCCURRED 与无法取得状态分别显示。日志不保存原始视频响应和完整带签名查询地址。

## 可重复验收

Python 环境要求见 `scripts/native-profile-requirements.txt`，WebView 调试使用 Debug 宿主；测试连接复用 `scripts/android_webview.py`。以下脚本固定使用独立验收包，不会确认安装或发送分享文件：

```powershell
python scripts/test-mobile-settings-capabilities.py --device emulator-5554 --output dist/audit/mobile-settings
python scripts/test-android-file-input.py --device emulator-5554 --output dist/audit/android-file-input
python scripts/test-update-installer-android.py --device emulator-5554 --output dist/audit/update-installer
python scripts/test-update-installer-android.py --device emulator-5554 --output dist/audit/update-notifications/native-markdown --presentation-only
python scripts/test-collection-tips-android.py --device emulator-5554 --source-url <真实视频采集API>
python scripts/test-typography-android.py --device emulator-5554
```

安装交接脚本使用本地 APK 样本，需先准备对应验收与发布产物，具体前置条件见脚本。其他原生脚本可能默认连接预览包，运行前检查参数和数据影响。没有获得恢复方案时，不通过清空用户应用数据绕过问题。

采集 Tips 脚本只向 acceptance 包创建临时来源，执行两次真实单页采集，检查顶部提示、触摸关闭、自动消失和跳转展开，再删除自己的来源。2026-10-27 当前本地构建在 MuMu 通过，证据位于 `dist/audit/update-notifications/native/`；浏览器的更新完成、后台下载和 320／390／1280px 提示回归复用 `scripts/test-update-browser.py`。

字号脚本通过基础设置的实际下拉框切换 12px 与 20px，比较标签、按钮、标题与下拉框的计算字号，重启后再检查持久化；结束恢复原设置。证据放 `dist/audit/page-polish/`。逐页浏览器检查见[样式复用](cczj-styles.md#字体与大字号布局)，不把 Android 后端支持的浏览器页面验收表述成 Windows 原生窗口验收。

长按临时倍速回归用 `scripts/test-player-hold.py`：默认连接本地 Vite 测试页，`--android --device emulator-5554` 连接预览包的原生 WebView，复用 Android 调试入口装入当前前端。它用本地循环视频检查真实触摸、持续倍速提示、松手／取消／全屏／换集恢复及选定倍速不被临时值覆盖，结束恢复原页面和播放器偏好。开发宿主通过不代替正式 APK 或华为真机验收。

最新结果见 [移动设置验收](https://github.com/ws-cczj/CCZJ-Video/blob/v2.4.5/reports/mobile-settings-2026-10-22/README.md)、[阅读性能验收](https://github.com/ws-cczj/CCZJ-Video/blob/v2.4.5/reports/reading-performance-2026-10-22/README.md)；原始移动方案保存在[历史归档](https://github.com/ws-cczj/CCZJ-Video/blob/v2.4.5/reports/history/mobile.md)。

弱网与源保存验收使用 `python scripts/test-playback-weak-network-android.py --device <adb设备号>`。先安装内嵌当前 `frontend/dist` 的 acceptance Debug 包；脚本复用 WebView 调试与 adb reverse，在原生手机表单保存临时源，检查立即显示、搜索拒绝后的同源网页建议回退和日志，再在原生播放器实际解码 HLS，经历 3 Mbps → 650 Kbps → 3 Mbps 的总带宽变化。`--scenario offline` 增加五秒断流。结束删除临时源并恢复设置、路由，不清空应用数据；证据位于 `dist/audit/weak-network/android-*.json`。MuMu 上的安装、源操作与弱网播放结论仅适用于该 Android 宿主，仍不等于实体手机的蜂窝切换、功耗和解码兼容性通过。

线路清晰度的原生验收入口为 `python scripts/test-playback-routes-android.py --device <adb设备号>`，要求当前 acceptance Debug 包。脚本覆盖 Go 元数据探测、远端预览不入库、清晰度排序后的实际触摸切换、加载区阶段文案与续播提示避让，以及右上角全屏线路入口和底部抽屉。说明与桌面验收入口见[播放线路清晰度](request-pipelines.md#播放线路清晰度)，证据放 `dist/audit/playback-routes/`；以每次运行的实际结果为准。

## 移动路由共享背景

手机播放器按钮位置回归使用 `python scripts/test-mobile-player-layout.py --device emulator-5554`，只操作 acceptance 包。复用原生绑定与媒体服务，验证普通播放／全屏的右上角线路入口、底部弹幕开关、线路抽屉和 320／360px 控件边界；结束恢复来源、设置与页面。界面截图使用无时间码的本地验收片，弱网测试片自带的时间码不属于播放器界面。截图与结果位于 `dist/audit/mobile-player-layout`。

系统栏安全区统一由 `MainActivity` 的根视图消费，WebView 不重复预留顶部高度；栏色和图标明暗跟随当前应用模式的主题画布，短剧不再单独覆盖系统栏或手机底栏，退出全屏后重新应用。手机次级页面标题栏保持透明，沿用共享主题背景，不再铺设固定白色／黑色横条；返回键与标题保留。主题页通过右下角悬浮按钮编辑当前主题，不在卡片列表上方占用操作区。日志导出生成在应用私有 `exports/logs` 下，随后直接打开系统分享界面；只有在分享目标中保存后才会出现在用户选定的文件夹中，取消分享不代表已写入下载目录。

对应隔离回归运行 `python scripts/test-mobile-feedback-android.py`，先安装当前前端对应的 acceptance Debug 包。它验证 XML 源搜索、浅色／深色主题与编辑入口、短剧推荐及剧场不覆盖外壳与原生栏色，以及导出后打开系统选择器并在取消后保留分享入口；测试来源、网络许可和主题恢复到进入前状态。截图位于 `dist/audit/mobile-feedback`，模拟器结论不代替华为真机验收。

`MobileRouteStage` 在路由场景之外保留一个背景层，页面本身透明。视频模式沿用用户主题图片和遮罩，路由通过有限、稳定的取景坐标移动同一背景；背景使用五列、两行的固定画布：五个视频主路由等宽分区，子页使用稳定的第二行取景；小说／漫画按各自三个主路由划分。只动画 transform，预览页不加边缘阴影，避免同一张图上出现竖向色带。手势预览插值坐标，取消恢复原取景；设置子页、返回和连续导航不重建图片层。背景与内容同向但位移更小，页面内容改为完整滑出，避免透明页面相互叠字。桌面维持原绘制方式，小说／漫画保留各自画布色，阅读器及播放器继续拥有不透明的内容底色。系统减少动态效果、页面动画关闭都禁用背景过渡。

重复验收：安装与当前 `frontend/dist` 匹配的独立 acceptance Debug 包后，运行 `python scripts/test-mobile-backdrop.py --device emulator-5554`。脚本在宿主中临时展示已有内置水墨背景，不持久修改用户主题；检查单背景节点、跨页、滑动取消、动画逐帧边缘覆盖、减少动态效果和模式隔离，结束恢复模式和页面。证据放 `dist/audit/shared-backdrop/`；逐帧检查同时确认主路由间位移等于一个视口宽度、滑动预览没有边缘阴影；帧回调不是硬件 FPS，MuMu 验证不能代替鸿蒙真机。

移动端日志复用共享 LogPanel：概览和级别／来源筛选默认收起，搜索与记录列表靠前；输出级别和导出格式使用统一 Segment，日志详情使用统一 Modal 并支持系统返回关闭。列表保留固定 76px 双行窗口化渲染，完整长记录在详情中换行，历史文件和原始文本共用原有读取、取消与导出接口。回归入口 `scripts/test-mobile-logs-layout.py` 覆盖窄屏、中英文、浅深主题、筛选、长记录和历史查看；导出系统交互沿用 `scripts/test-mobile-feedback-android.py`。

设置首页省去总说明，分组及工具页不重复提供右上角搜索／设置链接；弹幕源页复用宿主标题。移动小／中／大按钮使用公共密度变量按屏宽变化，最小高度分别在 36～40px、40～44px、44～48px 间调整，内容或字体变大时允许自然撑高；页面留白使用共享 section-padding，不能把每个控件强制改成同一高度。

视频壁纸的主路由取景向下偏移 6% 视口高度。短剧的禁止横向切页标记仅随实际播放场景挂载，空态、错误和加载态可以切换主路由。`backdropSystemBars` 复用有界图片像素读取，按当前取景顶部、背景透明度和页面遮罩计算原生栏色；Java 桥接只更新临时外观及系统图标明暗，不覆盖 SQLite 主题或开屏缓存。原生根容器同时更新，兼容系统将状态栏设为透明的情况。

真实目录验收使用 `scripts/test-real-catalog-android.py --snapshot <桌面库只读快照>`，只向 acceptance 包装载视频来源、目录、收藏和历史，并在首次操作前保留该包原库。真实桌面库不写入，目录数据不打入 APK。检查首页、搜索、收藏、短剧空态双向滑动，以及原生栏色桥接和开屏缓存隔离；结果及截图归入 `dist/audit/real-mobile/`。

五项底部导航的中间搜索入口使用 48px 主题色圆形按钮，保留搜索图标与“搜索”文字。所有入口的文字底部对齐，圆形和点击区域完整留在底栏内，不用负边距覆盖短剧进度条；选中时沿用主题强调色，不使用缩放或扩散动画。

播放状态验收运行 `scripts/test-playback-state-android.py`：使用独立来源验证前四集 99%、第五集 15% 的继续观看、收藏夹选择、重启后收藏，以及弹幕字号滑杆与保存。退出时删除本次来源并恢复设置和 WebView 缓存。真实目录导入前归档旧进度缓存，导入后清掉测试目录遗留的编号缓存，从复制的 SQLite 历史重建进度，避免相同 global_id 命中旧测试片段。此脚本只用于 acceptance 包，不用于真机 preview 数据。

手机详情页的选集区直接展示集数，不挂载 `PlayLinePicker` 的来源探测和重试卡片；播放时通过右上角线路抽屉选择。桌面详情页保留线路选择。
