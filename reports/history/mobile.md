# Android 与移动呈现历史

> 归档于 2026-10-22。以下保留当时的方案、判断和验证结果，部分内容已过时。当前规范见 [AGENTS.md](../../AGENTS.md)，维护入口见 [文档索引](../../docs/README.md)。旧“禁止”“待拍板”和外部技能引用不是现行开发要求。

## 原始记录
- [`docs/android-preview-2026-10-17.md`](#origin-android-preview-2026-10-17)
- [`docs/mobile-layout-2026-10-17.md`](#origin-mobile-layout-2026-10-17)
- [`docs/mobile-navigation-2026-10-18.md`](#origin-mobile-navigation-2026-10-18)
- [`docs/mobile-feedback-2026-10-18.md`](#origin-mobile-feedback-2026-10-18)
- [`docs/mobile-player-settings-2026-10-18.md`](#origin-mobile-player-settings-2026-10-18)

---

<a id="origin-android-preview-2026-10-17"></a>

## docs/android-preview-2026-10-17.md

<a id="origin-android-preview-2026-10-17--android-预览与-cideo-参考记录2026-10-17"></a>

### Android 预览与 Cideo 参考记录（2026-10-17）

10 月 18 日后续修正与验收见 [移动播放器、设置与豆瓣缓存修正](mobile.md#origin-mobile-player-settings-2026-10-18)。

移动端界面已按后续反馈建立独立外壳，管理改为完整页面。当前布局边界与回归方法以 [移动端布局记录](mobile.md#origin-mobile-layout-2026-10-17) 为准。

CCZJ Video 的 Go 依赖、前端运行时和本机 Wails CLI 已统一升级到 `v3.0.0-beta.30`。Windows 继续使用原有构建流程，Android 使用独立包名 `com.cczj.video.preview`，不会覆盖 Cideo。Android 数据库位于应用自己的私有目录，不读取或迁移 Windows 数据目录。

Wails 的 Android 支持仍是实验功能，见 [beta.30 发布说明](https://github.com/wailsapp/wails/releases/tag/v3.0.0-beta.30) 和 [Android 指南](https://v3.wails.io/guides/mobile/android/)。本次交付用于 MuMu 体验，不代表桌面功能已全部适配 Android。

<a id="origin-android-preview-2026-10-17--从-cideo-中采用什么"></a>

#### 从 Cideo 中采用什么

参考目录为 `C:\Users\18446\Desktop\Cideo`，只读取了代码，没有修改或删除这个项目。这里采用的是设计与生命周期处理方式，使用 CCZJ 现有 Vue、Go 和 Java 重新实现，没有引入 Flutter 运行时或复制 Dart 源文件。

| Cideo 中的实现 | CCZJ 中的处理 | 原因 |
| --- | --- | --- |
| `cideo_app_shell.dart` 的底部主导航与管理入口 | 手机显示首页、搜索、收藏、历史和管理；CCZJ 的管理采用完整工具页，设置有直接入口 | 手机不能直接沿用桌面侧栏占据大半屏幕的布局 |
| `player_system_ui.dart` 的全屏与退出恢复 | 手机页面竖屏；视频全屏临时切换传感器横屏、隐藏系统栏；退出恢复原先的方向请求与系统栏；平板保留系统方向 | 明确区分浏览和播放的方向策略，避免模拟器退出全屏后停在横屏 |
| `hls_proxy_server.dart`、`hls_proxy_response.dart` 的独立 HTTP 视频通道与 Range 传递 | Go 按需启动私有 loopback 媒体监听；Java 将响应流直接交给 WebView，保留查询参数、Range 和响应状态 | 官方通用资源桥会整段读取响应；分片经过完整 JNI 字节数组会延迟首字节并产生额外复制 |
| 会话所有权与停止后释放资源的思路 | 媒体通道属于 App 生命周期；关闭后停止监听与活动连接，禁止重新启动 | 避免单独引入不受管理的后台服务器 |
| 内存/磁盘缓存与预取策略 | 保留 CCZJ 已有的按需磁盘读取、缓存预算与播放生命周期管理 | 直接叠加 Cideo 的第二套缓存会增加占用、清理和一致性问题 |
| Android 运行指标桥 | 作为后续诊断参考，尚未移植 | Go 堆、Java/native 堆与 WebView 渲染进程需要分开计量，不能把 Java 堆当成应用全部内存 |
| MediaKit 原生播放器、MediaSession、前台服务、画中画和投屏 | 暂不接入，后续按具体功能验证 | 与 Flutter MethodChannel、MediaKit 状态绑定，不能直接接入 Wails/WebView 播放器 |
| 小说/漫画/Legado、Riverpod/GoRouter、Cideo 数据表 | 不引入 | 超出这次 CCZJ Android 预览范围，也会形成重复框架和存储模型 |

Cideo 的原始许可证为 MIT，版权标注 `Copyright (c) 2026 王硕`，保存在原项目 `LICENSE`；若后续直接移植源文件，需要同时保留其许可声明。Android 宿主基础文件来自 Wails beta.30 官方生成模板。

<a id="origin-android-preview-2026-10-17--实现边界"></a>

#### 实现边界

- Android 启动时使用公开的 `System.Environment()` 获取平台，再挂载界面。该宿主的同步环境缓存初始为空，直接调用同步判断曾导致安卓显示桌面标题栏和布局。
- Android 跳过桌面单实例、托盘与窗口拖放配置，隐藏 Windows 更新弹窗，数据库使用 `Mobile.StoragePath()`。首次启动已完成数据库初始化与迁移。
- 媒体监听仅绑定 `127.0.0.1` 的随机端口，使用每次启动随机生成的能力路径，仅暴露 `GET/HEAD /__cczj/hls`。不暴露 Wails RPC、静态资源或数据文件，不记录这个私有地址。
- Java 转发通过流式 `InputStream` 读取，关闭流时断开连接；原有 Go HLS 代理继续负责上游访问限制和播放列表改写，没有放开私网目标访问规则。网络安全配置只为 `127.0.0.1` 开放明文 HTTP。
- 安卓返回键先关闭视频全屏，再处理当前弹层，之后由 Vue Router 回退应用路由；无历史的非首页路由返回首页，根页面交给宿主退到后台。没有依赖 WebView 的原生历史判断，因为它在首次 Vue 路由跳转后可能仍报告无可退记录。
- 手机搜索栏拆成“来源/源搜索”和“输入/搜索”两行，避免桌面横排控件被窗口裁切；回归检查直接验证四个控件的可见边界。

<a id="origin-android-preview-2026-10-17--windows-构建与安装"></a>

#### Windows 构建与安装

需要 Go、Node、Wails beta.30 CLI，以及 Android SDK、完整的 Windows NDK 和 JDK 17+。本机验证组合是 JDK 21、NDK `28.2.13676358`、Gradle `9.1.0`、AGP `9.0.1`、Android API 35，最低系统 API 24。AGP 对应要求见 [官方说明](https://developer.android.com/build/releases/agp-9-0-0-release-notes)。

```powershell
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.30
npm --prefix frontend ci

# 查看连接设备
adb devices

# MuMu x86_64：构建、安装、启动
wails3 task android:run ANDROID_ARCH=amd64 DEVICE=emulator-5554

# 仅构建：默认 ARM64 手机包（尚未在真机验证）
wails3 task android:build

# 仅构建 MuMu x86_64 包
wails3 task android:build ANDROID_ARCH=amd64

# 需要显式指定本机工具链时
powershell -NoProfile -ExecutionPolicy Bypass -File build/android/build.ps1 `
  -Arch amd64 -Sdk "$env:LOCALAPPDATA/Android/Sdk" -JavaHome "C:/Environment/Java/jdk-21" `
  -Install -Device emulator-5554
```

脚本按需构建前端、生成 Wails Android overlay、编译 Go shared library，再清理并生成 debug APK；临时设置工具链环境变量，退出时恢复，不更改机器的默认 Java。`-SkipFrontend` 仅用于前端已构建且没有后续修改的情况。多设备连接时必须指定目标设备。

默认产物为 `bin/CCZJ-Video-android-arm64-preview.apk`，适用于支持 `arm64-v8a` 的 Android 7.0/API 24 及以上手机。显式指定 `ANDROID_ARCH=amd64` 时生成 `bin/CCZJ-Video-android-amd64-preview.apk`，用于 MuMu 等 x86_64 环境，不能安装到 ARM64 手机。`android:build` 和 `android:run` 使用 `ANDROID_ARCH` 选择目标，替代旧的 `ARCH` 参数，避免任务工具自动把电脑架构用于手机；直接调用 PowerShell 脚本仍使用 `-Arch`。不要只修改 APK 文件名，必须重新编译对应架构的原生库。桌面产物仍为 `bin/cczjVideo.exe`。APK 使用开发签名并启用 WebView 调试，仅供预览。

<a id="origin-android-preview-2026-10-17--mumu-原生应用的前端热更新"></a>

#### MuMu 原生应用的前端热更新

Android 的离线预览包默认从 Go 内嵌资源读取页面；普通桌面 `wails3 dev` 不会自动切换模拟器中的资源。当前项目新增原生宿主开发入口和 `android:dev` 任务：

```powershell
# 首次安装支持热更新的宿主，或改了 Go / Java / 绑定后
wails3 task android:dev INSTALL_HOST=1 DEVICE=emulator-5554

# 宿主已更新后，只启动开发服务，无需重新打包安装
wails3 task android:dev DEVICE=emulator-5554

# 可选：单独指定端口和架构
wails3 task android:dev PORT=19546 ARCH=amd64 DEVICE=emulator-5554
```

保持终端运行，保存 Vue、CSS、TypeScript 后 Vite HMR 更新模拟器。默认服务仅监听电脑 `127.0.0.1:19545`，通过本脚本创建的 ADB reverse 连接；不需要启动另一份 Windows 后端。Android Go 服务、应用来源、数据库和原生桥仍由原 APK 持有，页面地址仍为 `https://wails.localhost`。终端 Ctrl+C 或 Vite 退出时，脚本切回内嵌前端、移除自己创建的 reverse，并清理自己启动的 Node 进程；不清理其他设备转发或进程。

开发端口由 debug 宿主的 Intent 参数选择，只接受 loopback 端口；release 不启用此入口。Java 转发保留模块查询参数和真实响应 MIME，包括 Vue/TypeScript 编译后的 JavaScript；补充 `Accept: */*`，避免 Android URLConnection 缺省请求头导致 Vite 的首页回退返回 404。开发模式允许本地 HMR WebSocket 的混合内容，退出恢复禁止混合内容。媒体与 Wails API 仍走原有通道。

已通过 `python scripts/test-mobile-search-history.py --hmr` 实测：临时修改移动 CSS 后样式在 MuMu 生效，恢复文件后样式撤回，`performance.timeOrigin` 始终不变，确认全过程没有重载文档；测试结束恢复原 CSS 字节。日志和截图保存在 `dist/audit/mobile-search-history/`。

<a id="origin-android-preview-2026-10-17--验证"></a>

#### 验证

完整验证 `scripts/verify.ps1` 已通过：115 项前端测试、TypeScript 检查、生产前端构建、`go vet ./...`、全部 Go 包测试。Windows release 构建通过，文件版本为 `2.3.2.0`。新增媒体桥测试验证首字节在响应结束前到达、Range/206/查询参数传递、能力路径检查、方法限制以及关闭后禁止重启。

MuMu 使用 `emulator-5554`，Android 15/API 35，`x86_64`，WebView `110.0.5481.154.1`。APK 已安装并启动。可重跑真实宿主验证：

```powershell
python -m pip install -r scripts/native-profile-requirements.txt
python scripts/test-android-preview.py --device emulator-5554 --playback
```

脚本只连接预览包的 WebView，检查移动端平台、标题栏移除、页面无横向溢出及独立移动端外壳。播放时临时创建视频层，加载生产构建中的 hls.js，通过实际 Go HLS 代理播放 [Mux 公共 HLS 样例](https://test-streams.mux.dev/)，要求播放时间超过 3 秒且出现有效视频尺寸，再验证全屏横屏、原生返回退出与方向恢复。结束时销毁临时播放器、移除测试层和自己创建的 ADB 端口转发。管理、设置、表单和页面返回的检查由 `scripts/test-mobile-android-layout.py` 负责。不会导入采集源、迁移用户数据库或代替用户接受协议。

这覆盖实际 Android WebView 解码、媒体转发与原生全屏，尚不等同于采集源到详情、选集和生产播放器所有控制的完整端到端验证。公共样例有加载较慢和可恢复的网络失败，不能用这次样例的启动耗时推断所有源的表现。未完成 arm64 真机、长期播放耗电/内存、后台播放、系统通知控制和上架验证。

验证日志和截图保存在忽略目录 `dist/audit/`，包括 `wails-beta30-final-verify.log`、`wails-beta30-windows-final.log`、`android-final-interaction-smoke.log`、`android-preview-mobile.png` 和 `android-preview-playback.png`。

<a id="origin-android-preview-2026-10-17--播放器全屏与原生能力"></a>

#### 播放器全屏与原生能力

移动播放器小屏只保留播放、PiP、全屏三个按钮；横屏全屏展示完整控制，两侧纵向手势分别修改窗口亮度和媒体音量，离开播放器恢复原来的窗口亮度。PiP 走 Android Activity 原生接口，配置变化包含 screenSize / smallestScreenSize / screenLayout / orientation，避免浮窗时 Activity 重建。全屏 BACK 先交给锁屏及面板，再退出全屏。

DLNA 通过 Go 侧 SSDP / HTTP / SOAP 和 Android multicast lock 协作，不走网页跨域请求。电视或盒子须开启 DLNA，处于可互通的局域网；模拟器 NAT 无法保证 multicast 到达真实电视。`scripts/test-mobile-player-interactions.py` 覆盖生产播放器、原生亮度/音量、锁屏、面板返回及 PiP，`go test ./app/cast` 覆盖本地接收端协议流程。真实电视兼容性尚未验证。

---

<a id="origin-mobile-layout-2026-10-17"></a>

## docs/mobile-layout-2026-10-17.md

<a id="origin-mobile-layout-2026-10-17--移动端界面隔离与布局修正2026-10-17"></a>

### 移动端界面隔离与布局修正（2026-10-17）

10 月 18 日后续修正与验收见 [移动播放器、设置与豆瓣缓存修正](mobile.md#origin-mobile-player-settings-2026-10-18)。

上一版只隐藏桌面侧栏并添加底部按钮，仍然渲染桌面布局。实际数据下，历史标题会被搜索框挤成竖排，采集源工具栏会被裁切，首页轮播保留桌面的左右分栏，管理入口则把整个桌面侧栏塞进弹窗。此前的“页面宽度不溢出”检查没有发现被 `overflow-x: hidden` 裁掉的内部控件，也没有验证设置入口的可达性。

<a id="origin-mobile-layout-2026-10-17--界面边界"></a>

#### 界面边界

- `frontend/src/mobile/CczjMobileShell.vue` 拥有移动端顶部导航、滚动内容区、页面缓存和底部导航；移动端不再挂载桌面 `TitleBar` 或 `Sidebar`。
- `CczjMobileNavigation.vue` 使用路由链接进入首页、搜索、收藏、历史与管理。管理为 `/manage` 完整页面。
- `CczjMobileManage.vue` 提供明确的设置入口，以及采集源、下载、视频类型、回收站、最近更新和扩展包页面入口；不再重复列出底部主导航。
- 新界面和样式使用 `cczj-mobile-*` 命名。`styles/mobile.css` 只作为入口，加载 `mobile/frame.css`、`pages.css`、`settings.css`、`media.css` 和 `presentation.css`。业务页面的排版覆盖位于 `.cczj-mobile-shell` 下，设置覆盖位于 `.cczj-mobile-shell .cczj-mobile-settings` 下；播放器使用 `.cczj-mobile-player`、`.cczj-mobile-video` 专用边界。
- 数据、查询、采集、收藏、设置持久化继续使用原有 stores/API；没有新建第二套数据库或复制业务状态。

<a id="origin-mobile-layout-2026-10-17--页面与交互"></a>

#### 页面与交互

手机使用固定顶部入口和底部导航，内容区可以触摸滚动，但隐藏 WebView 原生与 CSS 桌面滚动条。移动外壳沿用主题背景图和控件语义；有壁纸时增加独立的中性阅读表面与背景遮罩，具体规则见下方视觉重排。之前的实色移动外壳遮挡了壁纸，这一做法已撤回；没有修改已保存的主题数据。

设置先显示分组列表，再进入独立分组页面。分组保存在 `/settings?group=...`，安卓返回键能够回到分组列表。日志、动画、诊断等动态扩展分组仍由原登记簿提供。安卓隐藏窗口宽高、拖动缩放、托盘、Windows 更新/回滚和旧桌面数据找回入口。关于页保留版本与许可信息，说明 APK 更新方式。

首页轮播改为封面与渐变文字叠层，收起导演等不适合窄屏的长字段，支持水平触摸翻页，纵向滑动继续交给页面。视频网格按手机两列、较宽设备三列布局。无来源的首页不显示空的筛选面板。

历史、收藏的标题与操作改为上下布局。采集源搜索和状态筛选占一行，四个主要操作占两行；后台调度状态与操作分行显示；源卡片中的图标操作加上可见文字。诊断在移动端跟随整页滚动，避免桌面固定高度工作区把内容困在内部滚动层。

返回键先退出视频全屏，再派发可取消的弹层事件，最后处理 Vue 路由或根页面退到后台。弹层处理使用独立阶段，避免懒加载的表单监听器注册较晚，导致同一次返回既关闭表单又离开页面。不可关闭的许可弹层继续遵守原有关闭限制。

<a id="origin-mobile-layout-2026-10-17--验证方法"></a>

#### 验证方法

```powershell
# 完整工程验证
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/verify.ps1

# 桌面设置回归，使用本机 Chrome
python scripts/test-settings-browser.py --channel chrome

# 构建并安装到 MuMu
powershell -NoProfile -ExecutionPolicy Bypass -File build/android/build.ps1 -Arch amd64 -Install -Device emulator-5554

# 真实 Android 页面与交互；只读业务数据，不提交表单、不启动采集
python scripts/test-mobile-android-layout.py --device emulator-5554

# 临时设置 WebView 的视口宽度，退出时恢复，不改变模拟器全局分辨率
python scripts/test-mobile-android-layout.py --width 320 --output mobile-layout-320
python scripts/test-mobile-android-layout.py --width 412 --output mobile-layout-412
python scripts/test-mobile-android-layout.py --width 768 --output mobile-layout-768

# 实际 HLS 传输、解码与全屏恢复
python scripts/test-android-preview.py --device emulator-5554 --playback
```

移动端回归包含 18 个页面/设置分组，检查内部操作控件的可见边界、标题宽高、轮播文字区、滚动条隐藏以及桌面选项移除；操作管理→设置→基本设置并用原生返回键逐级返回；打开采集源表单后检查边界，验证返回键只关闭表单。失败前后的截图和数值保存在忽略目录 `dist/audit/mobile-layout-*`。不会代替用户接受协议、保存偏好、添加来源或修改真实业务数据。

真实 MuMu 布局检查覆盖当前数据状态。它不能证明每个第三方扩展页、所有长文本和系统字体缩放组合都已适配，也不包含长期播放、后台服务和 arm64 真机测试。后续扩展页面需要继续遵守移动端布局边界。

本轮实测结果：MuMu 原始 360px 视口以及临时 320/412/768px 视口下，18 个页面/设置分组的检查和原生返回流程均通过。115 项前端测试、TypeScript/生产构建、前端约定、`go vet` 与全部 Go 包测试通过；本机 Chrome 的桌面设置/窗口回归及 Windows release 构建通过。新版 APK 已安装到 MuMu，采集源、主题和历史数据保留。

最终 360px 回归也验证了真实触摸事件可以切换轮播。HLS 公共样例实际解码、播放进度推进、全屏横屏及原生返回恢复通过，日志为 `dist/audit/mobile-final-playback.log`。

<a id="origin-mobile-layout-2026-10-17--搜索详情与真实播放器的补充修正"></a>

#### 搜索、详情与真实播放器的补充修正

上述旧检查没有覆盖真实详情/播放器，独立临时 video 的 HLS 检查也不能证明生产播放器的布局正确。新增 `scripts/test-mobile-media-layout.py` 从当前历史记录进入实际详情和播放器，改动前复现了：壁纸被覆盖、桌面搜索栏、海报下方的标题、播放器撑满竖屏、选集被隐藏，以及缺少移动触控栏。

- 搜索采用独立移动表单：输入/清除/提交在第一行，来源选择和源搜索开关在第二行。继续调用现有搜索与分页逻辑。
- 详情将海报和片名并排，播放/续播与收藏在独立操作行；导演和演员按需展开，简介、评分、线路、选集、下载及相关推荐仍使用现有数据与动作。
- 竖屏播放器上方为 16:9 画面，下方显示来源和剧集。移除桌面折叠/最小化入口；移动触控栏直接提供播放、进度和全屏，倍速/画质/连播/声音/视频信息等位于可滚动菜单。播放器状态、历史和增强管线仍由原组件持有，移动组件只负责呈现和发出操作事件。
- 点画面显示/隐藏控制栏，播放暂停由按钮操作；忽略触摸生成的兼容鼠标移动事件，防止刚显示的控制栏又被隐藏。续播提示缩短文案并分行排版。
- 菜单保持在视频全屏树内，全屏旋转与原生返回继续由 Android 宿主处理；普通模式下返回键先关闭菜单/视频信息，再导航离开。退出页面注销新增监听。
- 移动样式优先级高于懒加载的桌面 scoped 样式，避免进入播放页后再次被窄屏 `display: none` 覆盖。

```powershell
# 检查三页真实布局和内部控件边界（需要当前 Android 预览版中已有一条可播放历史）
python scripts/test-mobile-media-layout.py --output mobile-media-final
python scripts/test-mobile-media-layout.py --width 320 --output mobile-media-final-320
python scripts/test-mobile-media-layout.py --width 412 --output mobile-media-final-412
python scripts/test-mobile-media-layout.py --width 768 --output mobile-media-final-768

# 实际生产组件解码、触摸控制、菜单/视频信息返回、原生全屏，以及搜索提交
python scripts/test-mobile-media-layout.py --playback --search --output mobile-media-final
```

媒体回归会实际打开历史中的视频，正常推进并保存观看进度；`--search` 会搜索该片名并更新正常搜索历史。它不会接受协议、修改偏好、添加采集源或启动采集；退出恢复路由、临时视口和本脚本创建的 ADB 转发。截图和几何/解码结果存入 `dist/audit/mobile-media-*`。测试覆盖 MuMu x86_64 预览版，不能替代 arm64 真机和长时间播放测试。

补充修正后的实测：320/360/412/768px 下三页的布局及内部控件边界均通过；MuMu 的生产播放器实际解码 1920×1080 视频且时间推进，触摸唤出/暂停、菜单/视频信息的原生返回、640×360 全屏及竖屏恢复通过，实际提交搜索并返回已保存的视频结果。18 个页面/设置分组及原生返回、轮播触摸回归通过。115 项前端测试、TypeScript/生产构建、前端约定、Go vet/全部包测试，以及 Chrome 桌面设置和播放器回归通过。最终 APK 已覆盖安装，原有主题、采集源和应用数据保留。测试临时静音会在播放器卸载后恢复原偏好。

<a id="origin-mobile-layout-2026-10-17--视觉重排与设计-skill-整理"></a>

#### 视觉重排与设计 Skill 整理

用户要求更换设计流程后，项目安装并采用 `ui-ux-pro-max`（上游 `nextlevelbuilder/ui-ux-pro-max-skill`，固定版本 `56ea9cf174ed86786187daa9de0e6247cb0747e4`）。删除重复的 `frontend-design` 本地 Skill，备份位于忽略目录 `dist/audit/retired-frontend-design/`。开发、诊断、Vue、Wails 和回归测试类 Skill 各有独立用途，继续保留。Skill 安装及删除仅影响本项目的 `.agents/skills`，该目录由现有 Git 规则忽略。新规范记录在 `design-system/cczj-video/MASTER.md`。

- 首页移除重复大标题与英文眉题，改为单行品牌、搜索和设置入口；顶栏约 56px，图标触摸区 48px，去掉大块图标底色和底栏选中胶囊。
- 壁纸主题采用浅色白 91% / 深色 `#181b20` 92% 的阅读表面，背景分别叠白 30% / 黑 24% 遮罩。无壁纸使用原主题表面；输入、按钮和强调标记继续使用对应主题语义，修正输入背景错误复用卡片背景的问题。
- 400px 以下首页轮播降为 280px；推荐标题与说明分行，继续观看移除按钮移至卡片信息区，避免遮住画质标记。
- 搜索移除多重外框，焦点反馈集中到整个输入区域；来源开关保持清楚的文字与选中标记，历史采用轻量标签。
- 收藏夹和新建入口放在同一行，空状态使用 SVG 星标及局部阅读表面，移除重复说明和多余操作行。
- 清理前端中文中用户不认可的产品称谓，采集状态直接使用“已采集”“已保存”等描述。

本轮最终 APK 为 `bin/CCZJ-Video-android-amd64-preview.apk`，已覆盖安装至 MuMu `emulator-5554`。实际截图位于 `dist/audit/mobile-redesign-final-visual/`，包括当前壁纸、绿色、深蓝和黑色四种主题的首页、搜索、收藏共 12 组；320/412/768px 临时视口重复相同检查，共 48 组主题/页面组合。检查了标题、内部控件边界、顶部触摸区和采样文字对比度；三个临时宽度还检查了新建收藏夹表单打开及原生返回只关闭表单。测试等待关闭动画完成，不提交表单；主题仅临时修改响应式状态，退出恢复。

```powershell
python scripts/test-mobile-presentation.py --output mobile-redesign-final-visual
python scripts/test-mobile-presentation.py --width 320 --output mobile-redesign-final-320
python scripts/test-mobile-presentation.py --width 412 --output mobile-redesign-final-412
python scripts/test-mobile-presentation.py --width 768 --output mobile-redesign-final-768
python scripts/test-mobile-android-layout.py --output mobile-redesign-final-pages
python scripts/test-mobile-media-layout.py --playback --output mobile-redesign-player
```

本轮最终 18 页面、轮播触摸和原生返回回归通过；生产播放器实际解码 1920×1080、触控、菜单/视频信息返回、横屏全屏及恢复通过。115 项前端测试、TypeScript/生产构建、约定检查、Go vet/全部包测试，以及 Chrome 桌面设置回归通过。本轮没有重新生成 Windows 发布包。文本放大、减少动画、长期播放和 arm64 真机仍不在本轮实测覆盖范围内。

<a id="origin-mobile-layout-2026-10-17--轮播入口透明导航与历史长按"></a>

#### 轮播入口、透明导航与历史长按

后续按用户截图反馈，移动轮播不再渲染独立详情按钮，改为整张图片上的透明可访问按钮；沿用既有匹配与详情跳转逻辑，滑动、触摸取消和切换动画期间不会误跳转。海报取景从居中改为 `center 20%`，优先展示上部；保留 cover 以铺满区域，无法同时保证任意比例海报完全展示。桌面详情按钮保留。

顶底栏改为 80% 不透明度和局部 10px 模糊，移除上下分隔线，顶栏底部增加 12px 过渡；搜索、收藏等页面不再有硬边框式顶栏。内容上间距由 18px 缩到 12px。

历史页常态移除移动管理按钮，增加简短长按提示。按住记录 520ms 后进入管理并选中该条记录；移动超过 10px、取消触摸、离开页面或卸载时清理计时器，释放长按不进入详情。管理动作使用两列排列，选中卡片维持中性阅读背景；原生返回先退出管理再处理路由。桌面管理入口保留，删除仍调用原有业务动作。本轮没有执行真实删除或清空。

`python scripts/test-mobile-gestures.py` 在已安装 APK 上发送真实触摸事件，检查滚动取消长按、长按选中、松手不跳转、原生返回退出管理、普通点击进入详情，以及轮播滑动不跳转、点击图片进入已匹配详情。截图和结果位于 `dist/audit/mobile-gestures/`。主题检查遇到原生输入法时，先验证返回收起键盘，再验证关闭收藏夹表单，避免将 Android 正常的键盘返回误判为弹层失败。

本次实测通过：上述手势和原生返回；四种主题在 320/360/412/768px 下的 48 组首页、搜索、收藏呈现；18 页面和轮播触摸回归。115 项前端测试、工程验证及最终 TypeScript/生产构建、前端约定检查通过。最后一次样式修正后重新安装 APK 并重跑手势测试，增加了选中标题不沿用桌面低对比悬停色的断言。Android 调试连接使用 `am start -W` 等待 Activity 启动，避免覆盖安装后的冷启动进程尚未创建就查询 PID。最终包大小 39,817,861 字节，已安装到 MuMu。

<a id="origin-mobile-layout-2026-10-17--主页面去顶栏与搜索历史整理"></a>

#### 主页面去顶栏与搜索历史整理

用户进一步要求简化界面后，首页、搜索、收藏、历史与管理不再渲染移动顶栏；设置仍通过管理页进入，详情、设置及其他次级页面保留返回导航。移动轮播不再渲染圆点指示器，继续支持滑动和整图点击，底部文字区同步收紧。

移动搜索历史改为独立区域：标题与管理入口在上，普通词条在下方换行。常态无删除 X 和清空入口，点击只执行搜索；管理模式选择词条，删除/清空先打开确认弹窗，确认后才修改搜索历史。原生返回先取消确认，再退出管理；离开页面或执行搜索时退出管理状态。桌面历史词条保持既有操作。

本轮 115 项前端测试、TypeScript/生产构建、前端约定、Go vet/全部包测试和 Android 构建通过。MuMu 实测通过轮播滑动/整图点击、历史长按与返回、320px 下四种主题的 12 组页面呈现、18 页面回归，以及搜索历史选择/确认取消/原生返回；没有执行真实删除。最终 APK 为 39,863,397 字节。

新增原生前端开发流程见 `docs/android-preview-2026-10-17.md`。已验证 Vite CSS 修改和恢复在原文档中即时生效，开发服务退出时恢复内嵌页面并清理专属 ADB reverse；离线页面再次通过搜索历史管理与返回检查。后续 Vue/CSS/TypeScript 调整使用热更新，原生改动或离线包验收才重建安装。

<a id="origin-mobile-layout-2026-10-17--热更新下的页面细化"></a>

#### 热更新下的页面细化

本轮直接在已开启的原生 Vite HMR 中调整界面，没有重建或重新安装 APK：

- 首页继续观看改为横向海报、片名、集数行，完整条目可在默认首屏看到；搜索推荐中的观看记录采用相同结构。`VideoCard` 增加可选 `meta` 插槽，桌面继续使用原有默认信息。
- 首页移除观看记录增加确认，明确说明对观看历史的影响；取消和原生返回均保留记录。关闭页面清理弹窗状态，并修正全局视频 ID 下忙碌按钮的键值匹配。
- 历史页搜索前置，统计和长按提示同排，卡片海报缩为 64×96px，辅助徽标采用中性颜色；保留原有长按、批量操作和原生返回流程。
- 管理页将大块工具磁贴改为紧凑整行列表，保留名称、说明和设置入口；缩短中文设置说明，避免最后一个字单独换行。收藏空状态去掉重复文件夹名称并略微收紧高度。
- 统一移动海报徽标和按压反馈，取消桌面悬停在触摸后残留的放大、抬升和播放遮罩。

115 项前端测试、最终 TypeScript/生产构建和前端约定检查通过；真实手势、两处删除确认取消、原生返回、320/412/768px 的 36 组主题/页面呈现及 18 页面回归通过。CSS HMR 往返实测通过且原文件字节已恢复。默认 360px 五页最终截图和首屏可见性数据保存于 `dist/audit/mobile-refinement-final/`。这些改动已保存到源代码；离线 APK 仍为前一轮版本，下一次发布打包时才纳入本轮界面。

<a id="origin-mobile-layout-2026-10-17--移动播放器交互与-dlna2026-10-17-晚"></a>

#### 移动播放器交互与 DLNA（2026-10-17 晚）

本轮使用项目的 ui-ux-pro-max / Vue 指导移动交互，按 diagnosing-bugs 的复现循环修复轮播；Go 投屏模块按 golang-pro 的并发与资源生命周期要求实现。移动变化继续由 `cczj-mobile-*` 隔离，桌面控制共用原有业务处理，不改写主题或删除观看记录。

<a id="origin-mobile-layout-2026-10-17--已实现"></a>

##### 已实现

- 首页增加轻量搜索入口，不恢复“首页”大标题；轮播统一 Pointer Events，支持 MuMu 鼠标和手机触摸、拖动跟手、阈值切换与回弹。拖动不会点击进详情，纵向滚动仍交给页面。最初鼠标拖动复现失败，修复后同一路径通过。
- 小屏播放器底部只有播放/暂停、画中画、全屏三个按钮。续播改成紧凑浮条；“始终续播”移动开关放在全屏播放设置中，沿用原来的设置键。
- 横屏全屏复用桌面控制的播放、前后集、时间跳转、画质、倍速、音量、自动播放/连播、外挂字幕、广告报告、视频信息、评论、画中画和全屏动作；按两排组织。增强对比仍按原有条件显示。评论和收藏夹选择弹窗可进入播放器全屏树，避免 Teleport 到 body 后不可见。
- 横向滑动先显示进度预览、松手提交一次跳转，取消不提交；左侧纵滑调当前窗口亮度，右侧纵滑调 Android 媒体音量。离开播放器恢复进入前的窗口亮度，不修改系统亮度设置。
- 全屏左侧中部锁定触控；返回优先解锁/关闭面板，再退出全屏。修复 Chromium 自定义全屏视图先截获 BACK、绕过页面面板处理的问题。
- Android 原生 PiP 真正进入系统浮窗，保留同一 WebView 和播放进度。补齐 screenLayout / smallestScreenSize 配置变化处理，修复进入浮窗时 Activity 重建并触发原生退出的问题。桌面 PiP 接口不存在时不再假报成功。
- 全屏右上角 DLNA：同网段 SSDP 搜索 MediaRenderer / AVTransport，读取设备描述，发送原始视频 URL 和 DIDL 元数据，支持播放、暂停、继续、停止，以及跳到手机进度。成功发出播放指令后暂停手机本地播放。

<a id="origin-mobile-layout-2026-10-17--dlna-边界"></a>

##### DLNA 边界

`app/cast` 不依赖 Wails。设备控制 URL 只从已发现设备描述中取得，必须为 SSDP 发送者的私有 IPv4 地址；拒绝重定向、跨设备地址、回环和外部描述地址。XML/SOAP 响应最大 256 KiB，设备数与工作队列有上限，描述解析固定三个 worker。搜索约 3.5–6 秒，描述/控制请求分别设置超时，退出应用通过已有生命周期上下文取消；Android multicast lock 在搜索结束或离开播放器时释放。

投屏传输原始 HTTP(S) 视频地址，不把 wails.localhost / 本机代理地址发送给电视，不执行镜像、转码或 DRM 解密。部分电视不支持 HLS、源站请求头或特定编码，具体播放兼容性需要真实电视验证。MuMu NAT 也可能阻止局域网 multicast；空列表会明确提示网络和电视 DLNA 开关。

<a id="origin-mobile-layout-2026-10-17--验证"></a>

##### 验证

- `scripts/test-mobile-player-interactions.py`：生产采集源实际解码；横滑、取消、亮度、媒体音量、锁屏、全屏面板与原生返回；真实系统 pinned PiP、浮窗内时间推进、返回同一 WebView、亮度恢复通过。测试恢复临时播放器偏好与设备媒体音量。
- `scripts/test-mobile-media-layout.py --playback`：真实详情选集到生产播放器、触控、19 个全屏控制按钮、设置/信息面板与方向恢复通过。
- `scripts/test-mobile-gestures.py`：鼠标拖动中间位移、松手切换、真实触摸滑动/点击、观看历史长按/取消/返回通过，无真实删除。
- 320px / 768px 各四主题三页面，共 24 组呈现检查通过；18 页面与原生返回回归通过。实际默认 360px 截图见 `dist/audit/mobile-player-carousel/` 和 `dist/audit/mobile-player-interactions/`。
- 115 项前端测试、TypeScript / 生产构建、前端约定、go vet、全部 Go 包测试通过。Chrome 桌面播放器完整回归通过。
- Go DLNA 测试覆盖实际 UDP SSDP → HTTP 描述 → 去重、异常地址隔离、并发搜索拒绝，以及 SOAP 播放/暂停/跳转/停止、XML 转义、错误响应、超时取消和失效设备；语句覆盖率 87.8%。本机 Windows Go race 测试进程启动返回 `0xc0000139`，因此未得到 race 检测通过结果；普通测试与 vet 通过。未安装额外 lint 工具。
- 现场无可用电视/盒子，未宣称电视端成功出画。arm64 真机、多指/系统辅助功能手势、长期后台耗电和内存仍需另行验证。

原生 PiP 和返回行为参考 [Android PiP 文档](https://developer.android.com/develop/ui/views/picture-in-picture) 与 [OnBackInvokedDispatcher](https://developer.android.com/reference/android/window/OnBackInvokedDispatcher)，投屏动作参考 [UPnP AVTransport 规范](https://upnp.org/specs/av/UPnP-av-AVTransport-v1-Service.pdf)。

<a id="origin-mobile-layout-2026-10-17--收尾回归补充"></a>

##### 收尾回归补充

补测修复两处边界：Android 原生媒体音量采用单一入口，滑动或拖动音量条时清除媒体元素遗留的静音和二次衰减；静音状态恢复已加入真实 Android 测试。历史页海报缓存的访问时间读取/写入改为非响应式元数据，避免渲染中的 `get()` 自己读写同一依赖导致 `Maximum recursive updates`；内容名称/图片仍保持响应式，新增测试先失败再通过。

最终前端用例增至 116 项；最终校验与离线 APK 验收结果以 `dist/audit/mobile-player-verify.log`、`mobile-player-final-apk.log`、`mobile-player-interactions.log` 和 `mobile-player-carousel.log` 为准。本轮未生成 Windows 发布包。

最终 APK 已安装到 MuMu（`emulator-5554`）：`bin/CCZJ-Video-android-amd64-preview.apk`，40,029,369 字节，SHA-256 `da0bcb9af65e3a0bea1f4a7640c684725706416bd341109c8b446742f1486cad`。已切换到内嵌生产 assets，完整播放器交互与轮播/历史手势均通过；随后恢复原生 Vite HMR，便于继续界面迭代。全屏内评论 Teleport、字幕与广告报告面板及原生返回也已实测通过。最终 116 项前端用例、完整校验和桌面播放器回归通过。

---

<a id="origin-mobile-navigation-2026-10-18"></a>

## docs/mobile-navigation-2026-10-18.md

<a id="origin-mobile-navigation-2026-10-18--移动设置入口与页面过渡"></a>

### 移动设置入口与页面过渡

<a id="origin-mobile-navigation-2026-10-18--用户可见变化"></a>

#### 用户可见变化

视频模式底栏最后一项改为「设置」，直接打开设置目录。采集源、下载、视频类型、回收站和最近更新合入同一目录的常用工具区，不再经过独立管理页。旧 `/manage` 地址重定向到 `/settings`。

进入次级页面从右侧滑入，返回向右退出并露出上一页；同级底栏按入口顺序决定左右方向。顶栏、内容和底栏属于同一张页面，一起运动。动画复用现有时长与缓动，关闭页面动画、总动画或系统减少动态效果时退化为直接切换。

设置分组的标题返回及 Android 返回回到目录；正常从目录进入时使用 history back，不额外 push 一个目录页面，因此下一次返回不会重新进入刚离开的分组。独立打开分组时 replace 到目录。采集等工具从目录进入后也返回该目录。

<a id="origin-mobile-navigation-2026-10-18--复现与原因"></a>

#### 复现与原因

`python scripts/test-mobile-route-motion.py` 修改前输出：

```text
directSettings: false
settingsGroupHorizontalMotion: false
Bottom navigation still requires the extra Manage-to-Settings step
```

旧底栏指向 `/manage`；旧 `MotionTransition preset=page mode=out-in` 先淡出旧页，再进入新页，且使用竖向小位移。设置分组只改变 query，原来的页面 key 只有 path，因此目录与分组之间没有路由过渡。顶栏和底栏也在内容动画外立即换形，放大了跳动感。

<a id="origin-mobile-navigation-2026-10-18--实现与状态"></a>

#### 实现与状态

`mobile/MobileRouteStage.vue` 是视频、小说、漫画使用的页面过渡模块：固定视口内同时保留离开与进入两张完整页面，不先清空旧页；仅在过渡期间对 transform 建立合成层，旧页不接收操作。

每张页面获得自己的路由快照，防止离开的设置页提前响应新页 query，导致两张页面同时显示新内容。设置目录与各分组使用不同 key，分组内的基础设置标签保持原实例；视频播放器继续按视频身份保留实例，切集不重新创建播放器。原有缓存页名称继续用于 KeepAlive。

离开页面先解除它的滚动容器身份，进入页成为唯一 `.main-content`；缓存返回时恢复身份、滚动位置和回到顶部监听。设置目录与分组分别记录滚动位置。快速切换与取消过渡后移除旧页和 inert 状态，不依赖动画结束才更新路由语义。

共享阅读亮度由最新阅读会话持有，旧章节的退出清理不能覆盖新章节；原生跨章测试同时检查亮度与进度。小说与漫画的数据、阅读布局和偏好仍各自独立。

<a id="origin-mobile-navigation-2026-10-18--验证"></a>

#### 验证

- `scripts/test-mobile-route-motion.py`：实际 Android 页面、底栏直达设置、横向位置采样、不透明且无空白帧、滚动恢复、分组内标签、标题返回、Android 返回不绕圈、快速切换后的旧页清理。
- `scripts/test-mobile-settings-layout.py`：88 组设置布局、无控件裁切/重叠、主题编辑和首页渐变。
- `scripts/test-reading-comic.py --leave-comic`：小说/漫画实际阅读交互、跨章亮度与进度，以及 32 组漫画布局。
- `scripts/test-novel-desktop.py --mode novel` 与 `--mode comic`：各 12 组桌面布局与运行时错误检查。
- `scripts/test-mobile-player-interactions.py`：进度拖动/取消、亮度音量、锁屏、全屏设置/信息、DLNA 扫描、原生 PiP 和退出恢复。
- `scripts/verify.ps1`：136 项前端测试、类型/生产构建、约定、Go vet 和所有 Go 包测试。

逐帧位置采样可核对方向和透明度，不能用来宣称所有设备达到固定帧率。复现与验收产物保存在已忽略的 `dist/audit/mobile-route-motion/` 和 `dist/audit/route-motion-*`。

已在 MuMu `emulator-5554` 安装新预览 APK，并关闭热更新，使用包内生产资源再次通过入口、方向、返回、滚动、快速切换及减少动画检查。实际切换录屏：`dist/audit/mobile-route-motion/settings-navigation.mp4`。

APK 为 `bin/CCZJ-Video-android-amd64-preview.apk`，40,258,041 字节，SHA-256：`7abb4b8ed8cb6a30e7d6aab45080ccaad7b3fcc7d4fd0a4dd33560c42b147880`。验收后恢复 Vite 热更新，停留在设置目录。

---

<a id="origin-mobile-feedback-2026-10-18"></a>

## docs/mobile-feedback-2026-10-18.md

<a id="origin-mobile-feedback-2026-10-18--移动端列表与播放器交互调整2026-10-18"></a>

### 移动端列表与播放器交互调整（2026-10-18）

<a id="origin-mobile-feedback-2026-10-18--页面"></a>

#### 页面

- 视频、小说、漫画的主导航支持左右滑动，保留系统边缘返回、纵向滚动、首页轮播、输入与横向列表各自的手势。次级设置页不切换主导航。
- 模式按钮只保留一个模式图标，选中状态使用背景、边框与 aria-pressed，不额外增加对号。
- 移动列表长按进入编辑，显示删除 X；搜索历史立即删除，收藏、观看历史、继续观看、书架与漫画关注先确认。Android 返回先关闭确认，再退出编辑。桌面管理方式保留。
- 源搜索按钮使用桌面主题的主色与固定状态图标位置。
- 源搜索结果固定海报及信息区高度，缺少分类时仍与其他卡片等高。全选、保存所选及保存本页固定在底栏上方，选中不改变卡片排版。全部已保存时不虚假显示全选状态。
- 详情评分、热度与类型、年份、地区放在同一标签区。选集标题缩短，正序与下载位于区域右上，批量下载另起一行。
- 移动端返回顶部保留花朵，改为右下 48px 按钮，隐藏长藤蔓和漂浮装饰；避让导航与选择操作栏。

<a id="origin-mobile-feedback-2026-10-18--播放器"></a>

#### 播放器

- 竖屏保留播放、时间、画中画和全屏；全屏右下显示倍速、音量、画中画与退出全屏。
- 更多面板移除重复倍速、音量、画中画；画质使用移动触摸选项，保留现有处理逻辑。
- 返回与锁屏使用无背景、无边框图标。锁定后图标与颜色同时变化。
- 进度轨道与触摸范围铺满可用宽度，两侧各留 12px；移除独立灰色矩形渐变，使用整条底部遮罩。
- 顶部控制向下进入，底部控制向上进入。系统减少动画及应用动画开关仍生效。
- 打开面板或按住控制项时暂停自动隐藏；结束操作后重新计时。顶部、进度和底部使用同一移动可见状态；Android 返回先关闭操作面板。

<a id="origin-mobile-feedback-2026-10-18--验证与产物"></a>

#### 验证与产物

- `scripts/verify.ps1`：136 项前端测试、类型与生产构建、1694 对双语键、Go vet 和全部 Go 测试通过。最后的标签样式、全选状态调整后另行通过类型、构建和前端约定检查。
- `scripts/test-mobile-feedback-layout.py`：实际源站搜索 20 张卡片等高、操作栏固定与边界、选中不改变高度、花朵避让、详情右上操作与进度条几何。
- `scripts/test-mobile-player-more.py`：竖屏/全屏更多面板、触摸画质、倍速/音量打开超过 3 秒仍显示控制栏、锁屏颜色与 Android 返回。
- `scripts/test-mobile-player-interactions.py`：实际视频手势提交/取消、原生亮度音量、锁屏、设置/信息、DLNA 扫描、系统 PiP 以及亮度恢复。真实电视兼容性仍需电视验证。
- `scripts/test-mobile-player-chrome.py`：竖屏及全屏显隐、减少动画和两个动画开关。
- `scripts/test-player-browser.py --channel chrome`：桌面播放器完整浏览器回归通过。

截图与日志在忽略目录 `dist/audit/mobile-feedback-layout/`、`dist/audit/player-more/` 以及 `dist/audit/mobile-feedback-*`。预览包仍为 `bin/CCZJ-Video-android-amd64-preview.apk`。Android 打包脚本显式使用 UTF-8 读取中文版本文件，兼容 Windows PowerShell 的默认系统编码。

本轮补充验收：三种模式的底栏双向滑动、取消/短距离、输入/纵向/轮播排除，以及收藏、观看历史、继续观看、小说书架、漫画关注的长按确认与返回顺序均在 MuMu 通过。测试定位限定当前活动页面，避免将仍在退场的页面当作触摸目标。

新预览 APK 为 40,276,089 字节，SHA-256：`998a978992728c1b2a6ae98fa36c75e6ea9dedce7a43542c93983047600f92dc`，已覆盖安装到 MuMu。

包内生产资源再次验收：320px 页面布局、原生搜索历史长按/立即删除、桌面主题源搜索按钮，以及竖屏/全屏更多、触摸画质、倍速/音量面板保持显示、锁屏颜色和 Android 返回通过。该轮未依赖 Vite。
包内 320px 进度条真实拖动、返回详情、评论、黑色 poster、加载与滚动定位通过；竖屏/横屏显隐方向采样和减少动画开关通过。验收后已恢复端口 19545 的原生前端热更新。

---

<a id="origin-mobile-player-settings-2026-10-18"></a>

## docs/mobile-player-settings-2026-10-18.md

<a id="origin-mobile-player-settings-2026-10-18--移动播放器设置与豆瓣缓存修正"></a>

### 移动播放器、设置与豆瓣缓存修正

本页补充并更新 10 月 17 日的移动适配记录。仅移动端布局使用 `cczj-mobile-*` 外壳；桌面端继续使用既有布局。

<a id="origin-mobile-player-settings-2026-10-18--界面"></a>

#### 界面

- 首页在搜索入口与首张海报背后增加主题中性色渐变，高度随视口在 160–240px 之间变化，逐渐露出用户壁纸。没有恢复“首页”大标题，也不改写主题配置。
- 设置入口按显示与体验、数据与工具、应用信息分组，提供简短说明。基础设置一次显示外观、首页、数据与网络、维护中的一个分类，分类通过路由记录；顶部返回回到设置目录。子设置页释放底部导航空间，移除重复的“全部设置”返回行。
- 移动基础设置不再展示 Windows 字体选项和未被移动网格使用的列数、密度控件。开关触摸区、数字加减和下拉触摸高度至少 44px。首页选项采用与左右滑动一致的说明。
- 高级设置的豆瓣队列表格在手机上呈现为带字段名称的记录卡。主题编辑弹窗减少双重内边距，颜色选择改为两列、补上输入名称、扩大操作区域、隐藏滚动条；原生返回取消预览。
- 竖屏播放器保留返回、可触摸拖动的进度条、播放时间，以及播放、画中画、全屏三个底部动作。简介与评论通过下方标签切换，第一次打开评论才加载；阅读时视频保持在顶部。
- 移动视频使用明确的纯黑 poster，加载采用小圆环和文字，暂停不再挂载桌面装饰图片。修正 `play` 早于首帧可用时过早隐藏加载状态的问题。
- 标题、进度和操作栏使用轻微横移与淡入淡出。速度、幅度沿用 motion token；系统减少动画、总动画关闭或弹层动画关闭时停止过渡。过渡不增加循环计时器。
- Wails 3 包装过的服务错误统一还原编码；评论不可用显示明确错误和重试入口，不将请求失败当作“暂无评论”。切换影片或排序后，迟到响应不能覆盖当前请求。

<a id="origin-mobile-player-settings-2026-10-18--豆瓣数据"></a>

#### 豆瓣数据

热榜原有成功缓存为 **1 小时**，评论原有成功缓存为 **24 小时**，均保留，超过用户要求的半小时。此次补齐评论跨进程保存；刷新时钟仍取真实抓取时间，读取不能续期。

评论缓存按豆瓣 ID、页码、排序隔离。内存最多 200 页，数据库快照最多保存最近 80 页且不超过 8MiB；只在成功抓取后写入。逐页编码后再组装快照，避免为缩减超大快照反复序列化整个集合。明确清理缓存时也移除快照。

24 小时后、7 天内的评论先显示已有内容，并尝试一次后台刷新。失败不修改原始时间，失败刷新退避 30 分钟；首次失败同样退避，重复进入不会连续撞限流。同一页的并发首次访问和后台刷新合并为一次抓取。

热榜和评论共用交互请求流程：全局 IP 间隔闸门、静默期、已有 Cookie/请求头、最多一次现有验证流程与一次重试，以及有界响应读取。评论页的登录或异常 HTML 不会作为有效空评论缓存。没有加入登录凭据，也不承诺外部服务始终可访问。

<a id="origin-mobile-player-settings-2026-10-18--验证"></a>

#### 验证

- `scripts/verify.ps1`：120 项前端测试、TypeScript、生产构建、前端约定、Go vet 与全部 Go 包测试通过；最终修改后另行重跑前端测试、构建、Go vet 和全部 Go 包测试。
- `python scripts/test-mobile-settings-layout.py`：真实 MuMu WebView，320 / 360 / 412 / 768px，当前壁纸主题与深色主题，共 88 个设置页面组合；检查可见控件边界、文本与控件重叠、单分类显示、返回和首页渐变。另覆盖主题编辑的 44px 操作区、颜色输入名称和原生返回取消。
- `python scripts/test-mobile-player-feedback.py --width 320`：真实进度条触摸拖动、返回详情、评论延迟挂载、纯黑 poster、无桌面装饰图、加载反馈、阅读时视频固定。修复后实际取到当前影片评论。
- `python scripts/test-mobile-player-chrome.py`：真实播放时，竖屏和全屏分别验证入场与退场过程、最终可见状态、系统减少动画和两个动画开关。
- `python scripts/test-mobile-player-interactions.py`：实际横滑提交/取消、原生亮度与媒体音量、锁屏/返回、全屏设置/信息、DLNA 搜索、PiP 和退出恢复亮度通过。真实电视兼容性仍待设备验证。
- `python scripts/test-mobile-comments-restart.py`：需原生前端热更新运行。先加载实际评论后重启 Android 进程，评论从数据库缓存恢复，实测约 17ms；诊断计数为缓存命中 1、网络抓取成功/失败均 0。
- `python scripts/test-settings-browser.py --channel chrome --port 19647`：桌面设置布局、控件、窗口信号与开屏生命周期回归通过。

截图、结果与日志保存在忽略目录 `dist/audit/`。测试临时主题只修改内存，结束后恢复；没有删除收藏、来源或历史，播放测试会正常记录观看进度。

<a id="origin-mobile-player-settings-2026-10-18--预览与开发"></a>

#### 预览与开发

Android 包：`bin/CCZJ-Video-android-amd64-preview.apk`。此次包含 Go 豆瓣修复，因此需更新一次原生宿主；页面继续使用 `wails3 task android:dev DEVICE=emulator-5554` 热更新。仅改 Vue / CSS / TypeScript 不需要反复安装，改 Go / Java 后需要重建宿主。该包仍为开发签名的 Android 预览版。


最终 x86_64 包为 40,054,233 字节，SHA-256：`7efc016e14a80ba1f6ab9e50c88f3f16fb77a7f48c73484379861570d19962b6`，已覆盖安装到 MuMu。关闭前端开发转发后，内嵌生产页面的 360px 播放器反馈测试通过，实际评论可读；随后恢复前端热更新供继续开发。
