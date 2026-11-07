# Android 开发与验收

画中画由主 Activity 的 `supportsPictureInPicture` 声明及系统能力决定，不是普通运行时权限，不请求悬浮窗权限代替它。`CczjPlayerDevice` 不再把 AppOps 的 MODE_DEFAULT 当作拒绝，前端实际调用原生入口，由结果事件反馈成功／拒绝；竖屏比例在系统允许范围内留有取整余量。真实拒绝日志包含系统 API 与 PiP feature，用于区分入口失败和设备不支持。部分 OEM 的画中画入口位于特殊应用访问或小窗设置，普通权限列表中可能没有该选项。依据见 [Android 画中画文档](https://developer.android.com/develop/ui/views/picture-in-picture)。本轮已修正已确认的判断与比例问题，尚未证明所有厂商系统均可正常进入。

当前项目按用户要求由用户操作 MuMu 或真机验收，代理默认只负责修改、编译、静态检查及安装交付。用户再次明确要求代理操作设备时，只通过 ADB／WebView 调试端口，不使用 computer-use，也不新增或扩写自动化测试代码、夹具及验收脚本。下文已有脚本作为历史入口保留，不作为默认验收流程。

移动系统栏跟随当前可见表面：启动动画显示期间使用启动背景；首页、短剧、搜索、收藏和设置五个主路由共用顶部渐变，系统栏对背景叠加渐变后取色；详情等次级页面也在整页表面叠加同一顶部渐变，使导航、内容与系统栏衔接；普通播放页和已实际显示视频的短剧使用黑底白字。短剧空状态和剧场目录沿用页面背景，不能仅根据短剧路由切黑。视频加载失败在播放器内部居中显示，提供重试、切换线路和返回的真实入口。样式参考 [Video.js ErrorDisplay](https://docs.videojs.com/error-display.js.html) 与 [Android SplashScreen](https://developer.android.com/develop/ui/views/launch/splash-screen)，复用项目控件与原生桥接。

短剧详情尚未准备好时，`drama-playback-surface` 不单独设置深色底，页头和错误文字继续消费应用主题；准备中的翻页和已挂载播放器由 `cczj-short-drama-surface` 提供观看配色。观看底栏的表面遮罩不透明，避免壁纸透过后与原生导航栏出现色差。列表取数与远端详情是不同请求。同一来源在 MuMu 可用不能证明真机的详情请求可用；需核对版本／前端构建标识、来源配置和请求日志。2026-11-01 真机日志中，不同详情 ID 返回相同列表及正文指纹；直接请求同一接口确认 XML `ac=detail` 忽略 `ids`，`ac=videolist` 才返回目标及播放地址。兼容逻辑收敛在 `collect.FetchVideoDetailWithStrategy`，仅为符合条件的标准 CMS XML 详情补一次请求，继续核对身份；不改来源配置、关闭身份校验或清除数据。源站响应已核对，修复后的真机／MuMu 播放仍待验收。

手机主题选择的扩散起始半径覆盖当前主题卡片并向外留出 12px，使固定色样外的页面在首帧即可变化；桌面和动效设置的局部预览沿用原起点。关闭／减少动态效果、快速重复选择及中断仍由 `createThemeReveal` 和 motion store 管理。手机主题编辑入口固定在右侧且高于回顶部64px，共用取消／完成入口保留左侧；这些布局与动画修改仍需真机核对大字号、长文案、滚动后点击及取消恢复。

跨页面颜色交接由 `MobileRouteStage` 与 `useBackdropSystemBars` 共同管理。手势阶段直接使用已有拖动比例；自动切换和落位阶段只读取一次动画时长与曲线，分别交给底栏的 Web Animations 和 Android UI 线程的 ValueAnimator，不逐帧读取页面 transform 或桥接颜色。交接起点在旧媒体离场清理前保存，过渡中不重新采样主题，结束后再刷新。次级页面返回的栏色进度按实际行程归一化；新移动、取消、结束和销毁由当前过渡所有者中止旧动画。旧宿主缺少可选原生动画方法时使用有限的前端进度回退。短剧预览根据已准备的播放身份选择配色，但不启动隐藏播放器；初始空态和剧场保持用户主题。路由直接移动真实页面，不隐藏页面显示通用骨架，也不在落位后叠加内容入场。MuMu 的历史有限录屏位于 `dist/audit/mumu-current/`，后续性能采样位于 `dist/audit/mumu-performance-current/`；仅代表对应构建与操作，不证明真机无闪烁或所有切换均流畅。

全屏方向由媒体尺寸和宿主能力共同决定：视频宽高已知时，横向视频请求 `SENSOR_LANDSCAPE`，允许正常／反向横屏随重力翻转；竖向视频请求 `PORTRAIT`，保持竖屏沉浸。元数据未就绪时使用当前播放器的呈现方向，媒体尺寸更新后同步策略。Android 的传感器横屏不受系统自动旋转开关限制，也不会随手机竖起自动退出全屏，语义见 [Activity 方向策略](https://developer.android.com/guide/topics/manifest/activity-element#screen)。无需在 JavaScript 中重复采集重力传感器。播放器锁定控件在 Android 全屏下同时请求 `LOCKED` 固定当前方向，解锁恢复内容策略；退出恢复进入前的方向，未改全系统旋转设置。阅读沉浸未指定媒体策略时保留原方向。Android 16 起面向 API36 的大屏可能忽略方向限制，应适应实际视口，见 [大屏方向规则](https://developer.android.com/develop/adaptive-apps/guides/app-orientation-aspect-ratio-resizability)。

既有 `usePlayerPresentationMode` 管理 inline／entering／fullscreen／exiting，切换期间阻止重复请求；Android 通过 `api/mobilePlayer.ts → WailsJSBridge → MainActivity` 传递本次请求身份及方向，进入确认同时核对原生 custom view 的方向与宽高，不仅依赖 Configuration。退出先由原生恢复方向、系统栏和布局，再确认浏览器状态；原生退出布局等待最多3秒，前端收尾最多4秒，按钮与系统返回共用退出阶段。底层 WebView 在不透明全屏视图下保持 VISIBLE，保留绘制表面；会话期间使用 JUMPCUT 旋转策略，退出后恢复原窗口策略。进入布局等待最多5秒，前端确认最多7秒；失败／超时按请求身份清理自有工作并恢复宿主方向、系统栏、边距、cutout 和背景，前端显示可重试提示。切换、旋转与隐藏时取消拖动／临时倍速；播放器与主路由均为系统边缘返回保留两侧20 CSS像素起始区域。所有者释放监听、计时器及原生会话，不重建媒体或 HLS。旧宿主缺少新增桥接时沿用浏览器全屏，新增方向与布局确认能力需安装本轮宿主。

上述全屏调整的交付验收范围为：横向／竖向及短剧播放，正常／反向横屏，系统自动旋转关闭，锁定／解锁，旋转中拖动／长按的取消，快速重复点击，系统返回，进入失败后的重试，以及小说／漫画沉浸方向。启动、无媒体／加载失败、已有进度与恢复也需检查。代码编译和打包不代表 MuMu／实体手机行为通过；本会话已获授权在 MuMu 通过 ADB／WebView 直接复查普通视频进出全屏，有限几何记录见 dist/audit/mumu-current/fullscreen-entry-after.json 与 fullscreen-exit-after.json；这不覆盖全部上述场景，实体设备重力感应与折叠屏布局仍待用户确认。

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

预览包名是 `com.cczj.video.preview`，验收包名是 `com.cczj.video.acceptance`。桌面显示名为 `CCZJ Video`，由 [strings.xml](../build/android/app/src/main/res/values/strings.xml) 的 `app_name` 提供，Manifest 引用该资源；显示名称调整保持既有包名与签名，以便覆盖安装保留数据。`-SkipFrontend` 只用于前端已构建且之后没有改动的情况。Release 签名选择、不可调试 APK 检查与 GitHub 文件准备见 [发布流程](releases.md)。

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

播放打断由 `usePlaybackInterruptions` 协调媒体身份、用户意图及宿主可见性。Android `CczjPlayerDevice` 在Activity生命周期内接收 `ACTION_AUDIO_BECOMING_NOISY`，通过已有WebView事件通知暂停；耳机断开后不会因回前台或重新获得焦点自动外放。宿主onStop（实际PiP除外）与前端visibilitychange取消手势／临时倍速并保存进度，只有本控制器暂停的同一媒体、用户仍要求播放时才在回前台恢复。用户暂停、系统媒体按钮暂停、换媒体、主动跳转及离场会使旧恢复记录失效。系统媒体按钮沿用浏览器Media Session接口，监听由当前播放器持有和清理；切换普通／全屏不更换媒体会话，PiP进入中及实际PiP保留原会话。

音频焦点继续由Chromium媒体会话申请和响应，不再创建第二个Android焦点请求者。Android15面向API35的应用只有前台或前台服务才能请求焦点，见 [Android音频焦点](https://developer.android.com/media/optimize/audio-focus)。Chromium区分系统临时暂停与用户暂停，系统恢复只恢复系统暂停的会话，界面媒体按钮走已登记的action handler，见 [Chromium媒体会话实现](https://chromium.googlesource.com/chromium/src/+/5031db4958a0c5f10704595742fc89b18061e5de/content/browser/media/session/media_session_impl.cc)。这说明复用现有焦点所有者的依据，不证明所有WebView／OEM来电行为均已通过验收。

本轮待用户验收：弱网恢复及持续失败后的重试，播放／暂停状态下切线与保留倍速、静音，候选源失败或剧集不对应时保留原视频；来电期间、临时通知抢焦点、耳机断开、锁屏、手动暂停后切后台、旋转、PiP及返回；短剧连续推荐去重、屏蔽后预热取消、撤销／重启后屏蔽保留、无候选与恢复推荐、追剧和返回推荐。旧版本录像不代表本轮行为通过。本轮不新增或运行自动化验收脚本，编译安装结论与实体手机行为分开报告。

- 原生环境先经 `api/runtime.ts` 初始化，布局由 `platform/presentation.ts` 选择；不能把窄窗口当成 Android。
- 桌面窗口、托盘、资源管理器与退出时安装属于桌面能力。Android 使用宿主的文件选择、只读文件分享和系统 APK 安装器。
- 视频全屏要恢复方向、系统栏、屏幕边距与亮度；返回键先处理全屏、弹层和管理状态，再处理应用导航。
- 文件选择取消后可以再次打开，分享取消不应丢失本地导出，权限拒绝后应有可执行的恢复入口。
- 设备差异需要实测。MuMu 通过不等于鸿蒙 4 真机通过，APK 构建成功不等于安装、播放和升级成功。

播放器左侧上下手势通过 `api/mobilePlayer.ts` 的原生接口调整当前 Activity 的 `WindowManager.LayoutParams.screenBrightness`，手势起点读取设备实际亮度，范围为 2%～100%；不以视频／画布 CSS 滤镜代替屏幕亮度。退出播放时恢复进入前的窗口值，包括跟随系统的默认值，不修改系统全局亮度设置。播放器与阅读器通过调用方身份协调恢复，离场页面不能覆盖新页面已接管的亮度；没有原生亮度能力的平台不显示虚假的亮度进度。接口依据 [Android screenBrightness 文档](https://developer.android.com/reference/android/view/WindowManager.LayoutParams#screenBrightness)。

普通播放器在“更多 → 播放”设置长按倍速和画面适配；短剧播放页通过页头设置按钮使用同一份控件与偏好。长按默认 2 倍，可选 2／2.5／3／4 倍，不降低已经选择的更高速度；松手、取消、换集或离场恢复原速度。`player_hold_speed` 与 `player_picture_fit` 通过设置队列写入 SQLite，异步读取不覆盖读取期间的用户选择，保存失败显示重试提示。

移动端暂停后的继续播放按钮位于右下角：全屏图标为 64px，非全屏内嵌图标为 40px，触摸目标不小于 44px。定位规则高于桌面播放器的作用域样式；复用播放器 ResizeObserver 读取底部控制栈高度，控制栏显示时在其上方留 12px，隐藏时靠近视频底边，并计入系统安全区；全屏控制栏换行也使用实际高度。图标位移与控制栏共用进入／退出时长和曲线，关闭覆盖动效或减少动态效果时取消位移动画。播放且控制栏隐藏时，独立的 2px 迷你进度贴住视频底边；该进度不接收触摸，拖动仍由展开的进度控件处理。短剧保留独立观看控件，不消费普通播放按钮定位。

详情初始保持加载态，避免共享 video store 的上一次结果先展示、挂载后又切回加载造成重复进入闪烁。手机次级路由最多保留一个缓存页面，包含最近详情；返回复用其 DOM 和滚动位置，避免大集表重建阻塞栏色过渡。详情隐藏后不刷新播放进度，重新激活时校验共享 store 的来源／作品身份，换源后按原详情路由恢复，再通过共用 sceneTasks 等待页面过渡结束后刷新进度和收藏，播放离场提交的进度事件也走同一调度入口。主标签缓存和桌面缓存规则保持各自所有者。详情选集按内容自然收缩，最小一行、最大三行实测行高（含两处行间距），超出独立滚动；手机播放页的选集视口仍为 clamp(180px,40dvh,320px)，排序留在区域外。手机选集默认三列，包含日期副行的按钮不再强制两列；上下／左右内边距为 2px／4px，触摸目标至少 44px并允许长标题自然增高。日期副行、播放进度与底部进度条保留，百分比没有背景气泡。无有效豆瓣 ID 不装配手机评论选项，ID 消失时回到简介。

画面适配参考 [VLC Android 的屏幕比例选项](https://docs.videolan.me/vlc-android-user/video/video_player.html) 和 [Media3 的 FIT／FIXED_WIDTH／FIXED_HEIGHT／FILL／ZOOM](https://github.com/androidx/media/blob/release/libraries/ui/src/main/java/androidx/media3/ui/AspectRatioFrameLayout.java)：自动沿用普通视频完整显示、移动竖屏短剧等比铺满；完整显示可能留边，等比铺满可能裁切，拉伸铺满可能变形，适应宽度／高度固定对应轴并居中裁切或留边。根据视频固有尺寸与容器尺寸直接计算媒体元素宽高，不只修改 `object-fit`；元数据、视频分辨率、容器尺寸和设置变化都重新应用几何。全屏使用同一偏好，画质增强共用原视频的成像几何，暂停时切换也同步画布。移动端支持容器单位时用 cqw／cqh 表达实际视频元素尺寸，随原生旋转在同一次布局中变化，避免 ResizeObserver 回调前短暂保持旧像素尺寸；旧 WebView 和桌面沿用像素几何。容器单位语义见 [CSS 容器相对长度](https://www.w3.org/TR/css-contain-3/#container-lengths)，画布仍由同一 ResizeObserver 跟随。系统画中画始终完整显示。视频与容器比例相同时多种模式可呈现相同外观，选项下显示实际视频与播放区域比例，仅在比例接近时提示效果可能相同。点击选项直接对当前 video 引用重新应用尺寸；只有实际画中画状态才强制完整显示；片源本身编码的黑边属于画面内容，完整显示不会自动识别并删除。

## 投屏发现与接收协议

应用内设备列表使用 DLNA／UPnP AVTransport，电视或盒子需要开启相应接收服务。同一 Wi-Fi 不是协议兼容的保证；海康部分交互屏提供 [WonderCast、Miracast 或 Chromecast](https://pro-av.hikvision.com/en/products/interactive-flat-panel-display/interactive-flat-panels/ultra-series/)，不能据此认定未知型号支持 DLNA。Android 投屏面板提供“打开系统无线投屏”入口，通过 `ACTION_CAST_SETTINGS` 进入系统连接；WonderCast 专用接收仍需配套助手。

2026-11-05 用户新版日志在 14:53、14:55、14:56 多次记录发现操作以 0ms 失败，原因为 `netlinkrib: permission denied`：Android 在读取网络接口时拒绝 Go 的 netlink 枚举，搜索尚未进入 SSDP／SOAP 阶段。这是本次发现链路的明确失败点，不能归为电视没有响应或解码不兼容。此前提供的旧 2.4.0 日志没有投屏阶段记录，不能用其缺少错误记录否定新版故障。

Android 发现入口现为 `CczjMobileCast → platform.cast.interfaces() → mobilePlayer.readCastInterfaces → WailsJSBridge.playerCastInterfaces → CczjPlayerDevice.castInterfaces`。原生通过 ConnectivityManager 获取非 VPN 的 Wi-Fi／以太网，使用 [getLinkProperties](https://developer.android.com/reference/android/net/ConnectivityManager#getLinkProperties(android.net.Network)) 读取接口名和私网 IPv4；API26+ 用 [Os.if_nametoindex](https://developer.android.com/reference/android/system/Os#if_nametoindex(java.lang.String)) 读取索引，API24／25 仅对该已知接口名使用 `NetworkInterface.getByName().getIndex()`，不枚举接口列表。不申请位置权限，不读取 SSID，沿用已有 ACCESS_NETWORK_STATE 权限。

桥接返回最多 16 条 `{index,address}`，前端运行时校验后调用 `DiscoverCastDevicesOnInterfaces(ctx, []cast.NetworkInterface)`。Go 再次检查私网 IPv4、正索引和数量限制，通过 `net.ListenUDP` 绑定验证地址属于本机，再由 x/net/ipv4 设置多播接口；Android／Linux 路径使用 IP_MULTICAST_IF 的索引，不再调用 `net.Interfaces`、`InterfaceByIndex` 或 `Addrs`。原生读取失败明确返回 UNAVAILABLE，不作为“未找到电视”；旧宿主缺少桥接及桌面仍保留 `DiscoverCastDevices` 兼容入口。这个修复消除接口枚举权限失败，不代表任意手机和电视必有共同协议或可播放任意视频。

投屏面板以附近设备和刷新为主要入口，网络排查说明收进“连接帮助”，不提供没有配套接收端的扫码连接。Kodi 作为接收端需在 Settings → Services → UPnP/DLNA 开启 Enable UPnP support 与 Allow remote control via UPnP；TV/PVR 页面不负责接收投屏，HTTP 远程控制也不能代替 DLNA 接收，设置依据见 [Kodi 官方 UPnP/DLNA 说明](https://kodi.wiki/view/Settings/Services/UPnP_DLNA)。模拟器能访问网页不证明 SSDP 组播可达；应分别核对接收功能、切换应用后 Kodi 是否仍运行、同一局域网及组播可达性。跨宿主或跨实例时再检查 NAT／桥接、VPN 与网络隔离，不把未找到设备直接归因为电视编码或 HLS。`app/cast/status.go` 返回 `media_state`：`matched` 表示 `GetPositionInfo.TrackURI` 与本次发送地址或本会话已登记的子媒体资源匹配，或 TrackURI 缺失时 `GetMediaInfo.CurrentURI` 匹配；未登记的两个非空媒体地址冲突时保留未知状态，`other` 表示接收端报告其他媒体，`unknown` 表示未能确认媒体归属。`matched` 仅确认接收端返回了本次地址，不代表已经取流或解码。自动交接要求本片匹配、无媒体错误且连续有效 PLAYING 样本中的时钟实际推进；单次正时间、固定时间、双零时间或仅回显地址均不足。采样间隔须在 1–12 秒内，时间推进至少 0.5 秒且不超过采样间隔四倍加 2 秒，防止缓存、跳转和后台旧样本误确认。重发／换设备／结束会话清除确认，跳转、查询失败和前后台切换清除相邻时钟样本。不能把上一个视频的 PLAYING 和进度当成本次成功。没有可核对进度（包括地址已匹配）的设备允许用户查看电视画面后主动转为遥控，并明确显示手动确认；`other` 不允许确认或采纳进度。零位置不被直接判为播放失败，但也不自动宣告播放成功。未确认的时钟不写入本片历史，自动续播 Seek 等待电视提供有效位置，避免向尚未起播的设备提前跳转。电视未提供有效进度时显示明确状态并禁用拖动和快进退，不用手机暂停的时间或 00:00/00:00 冒充电视进度；拖动只预览，提交时才发送 Seek；请求后显示目标位置和“正在跳转”，仅电视返回接近目标的进度才确认，8 秒未确认则回到实际进度并反馈，历史仍只消费接收端时间。

CCZJ 的 DLNA 媒体传输由 [CastRelay](../app/proxy/cast_relay.go) 复用 [HLSService](../app/proxy/hls.go)，电视读取手机提供的局域网 HTTP 会话地址；源站 HTTPS、重定向、User-Agent／Referer 和 Cookie 沿手机播放的同一客户端处理。播放列表、子列表、分片、密钥和初始化段均改写到会话资源地址；无扩展名且 MIME 错标的子列表按内容识别，TrackURI 只认可本次实际登记的地址，MP4 保留 Range 流式传输，HEAD 与 GET 的播放列表表示一致。确认交接后暂停手机本地 HLS 拉流、恢复重试和分片预取；媒体转发由 Go 所有者继续执行，不依赖手机画面和前端轮询。手机可以息屏，但必须保持应用运行及网络连接。发送指令成功、电视尚未播放或状态请求失败时不提前中断本地播放。确认投屏后，原播放区由紧凑遥控接管，保留设备、更换设备、停止、播放／暂停、前后 10 秒、电视进度和展开入口，简介／评论继续可用；展开后复用现有播放器抽屉的全页布局，收起及系统返回只关闭遥控页。小屏、独立遥控页和原生全屏分别使用 CczjMobileCastRemote 的 compact／page／fullscreen 呈现，设备、状态、命令及生命周期仍由 CczjMobileCast 持有。内嵌视图 Teleport 到播放器宿主以避开已隐藏的本地控制栈；全屏确认起播后收起设备面板，遥控直接覆盖整个宿主。全页和原生全屏均保持设备名／更换设备在上、主播放与前后 10 秒居中、进度在下的纵向层次；短横屏压缩间距以保持全部控件可用；退出全屏继续显示小屏遥控，不结束会话。标题栏的返回区域只占一个触摸目标，不能用透明整行遮住遥控；本地锁屏、续播和画面增强分割线在投屏期间不显示。本地加载、错误和字幕提示让出显示，停止投屏后沿原流程恢复；普通播放按钮打开控制面板，不能解除投屏保护。停止投屏、电视报告 STOPPED／NO_MEDIA_PRESENT 或换片后解除保护，本地播放由用户主动恢复。面板提供重新发送、更换设备和可用时的系统投屏入口，停止请求失败时保留真实错误并提供明确的“断开连接”选择，提醒电视可能仍在播放；用户主动断开后才解除手机播放保护，失败不应把用户困在遥控状态。

2026-11-06 的接收端现场记录中，82 次状态查询均无有效进度，位置／时长持续为零，仍报告 PLAYING／PAUSED_PLAYBACK 且媒体地址匹配；该组合不能触发自动成功或自动续播 Seek。现场版本采用电视直连原始 HTTPS HLS，而手机经过本地媒体代理，取流上下文存在明确差异。本轮将实际发送链改接局域网转发，消除两条路径在 HTTPS、请求头和 Cookie 上的差异；这不等于已经证明差异是现场不出画面的唯一原因。另一网络／时点对附件地址以普通请求和项目手机 HLS 同款 User-Agent／Referer 分别请求均收到 403，不能当作电视端实收状态。新实现先在手机端检查源站响应和内容，再发送局域网地址；源站拒绝或返回 HTML 时直接反馈错误。没有新增转码或私有 SDK，接收端协议／编码能力仍需实际设备验证，不设置品牌白名单。

附件日志的关键时间线是：12:28:00.689 SetAVTransportURI 成功，00.774 Play 成功，00.814 状态仍为位置／时长双零且 has_position=false，00.886 已发送 Seek。这定位了起播检查到续播跳转之间的缺失保护；HTTP 200 只证明动作响应成功，不能证明该时刻可以跳转。修复不依赖电视品牌。

[readiness.go](../app/cast/readiness.go) 按接收设备持有可取消的命令所有权，并按本次 session 保存 Play 接受状态与有效时钟样本。SetURI／Play／Pause／Seek／Stop 串行，状态读取可并行；发现刷新保留同一设备的命令所有者与会话证据。新发送在 Play 接受后才允许采样；尚未接受时发起的旧查询、其他 session 和错误媒体不能写入起播证据。Seek 出口重新读取本次媒体状态，必须已观察到时钟推进且当前仍有有效位置；失败记录 cast.seek.rejected／CAST_NOT_READY，完全不发送 SOAP Seek。暂停 Seek 兼容回退在进入和恢复 Play 后都复核同一条件；成功 Seek 清采样基线，成功 Stop／新媒体发送清全部证据；位置回退超过两秒撤销时钟确认，后续可信推进可重新建立。前端自动续播与快进退同样要求时钟确认，用户手动确认遥控不能绕过该保护。日志增加 cast.play.accepted、cast.control.dispatch 的 action／Seek 目标秒数及 cast.status.seek_ready，保留会话关联，不记录凭据或响应正文。实际电视是否因提前 Seek 被打断仍需同片验证，不将该已确认缺陷宣称为全部不出画面的唯一原因。

局域网监听绑定发现电视时实际使用的私网 IPv4，兼容入口才用到电视的路由推导地址，避免 Android Go 的接口枚举权限问题。会话能力路径使用随机密钥，只接受所选接收端 IP 的 GET／HEAD，仅开放原视频和播放列表实际引用的资源，不接受任意 URL 查询。最多四个会话，每个最多十二条连接、八个在途请求、32768 个资源及 8 MiB 地址元数据；停止、重发、已起播后接收端结束／换片、应用退出均撤销连接，空闲三十分钟或会话十二小时到期收回。应用关闭等待监听停止，超时不会重新启动。普通手机播放和 iOS 媒体桥仍消费同一 HLS 实现，局域网授权入口独立。

Android 的 [cast_host_android.go](../app/service/cast_host_android.go) 只在存在转发会话时启动现有 WailsForegroundService，传入 connectedDevice 类型；Manifest 同步声明权限，沿 [Android 连接设备前台服务规则](https://developer.android.com/develop/background-work/services/fgs/service-types#connected-device) 使用网络交互类型。服务持有有界 CPU 唤醒锁及 Wi-Fi 锁，最后一个会话释放后停止并释放锁；进程重建不重启失效能力地址。桌面宿主不启动 Android 服务。

手机投屏面板提供“手机转发／电视直连”，已选择设备时切换方式会重新发送，并重新建立确认；默认从头播放，按钮可选择下次发送时续播当前位置。`PlayCastVideo` 保留原默认转发契约，新绑定 `PlayCastVideoWithOptions` 只接受 relay／direct，会话保存实际方式，发现刷新保持；直连不创建局域网监听或前台服务，状态标为 unobservable，不把无取流日志解释为无请求。失败重发后重新查询实际会话，身份变化时交还本地保护，保留最后可信进度。

[cast_probe.go](../app/proxy/cast_probe.go) 在转发会话45秒仍未收到媒体且未暂停时执行一次手机源检查，避免正常起播时额外访问：沿首个变体最多三级列表，再抽取首个密钥、初始化段和媒体数据，最多六次请求、总计八秒、每列表256KiB、其他资源513字节，最多约770KiB响应体；超限或失败只记录抽样结果，不阻断电视播放。与实际取流共用客户端、Cookie、源站Referer与网络预算；停止／重发撤销请求，退出等待检查完成。它不遍历全部变体／分片，不验证解码，也不保证手机与电视访问条件相同；额外请求可能影响带一次性鉴权的源，应结合直连对照分析。

日志中 `cast.source.*` 的 observation_scope=phone_sample 与 `cast.stream.*` 的 receiver_response 区分手机抽样和电视实际请求；HLS只记录版本、主列表／加密／初始化段／音轨分离标志及CODECS声明的固定布尔提示，不保留列表正文或地址，也不把CODECS声明当作实际分片检测。每类实际资源的首次请求、首字节和完成有独立记录，DNS／TLS证书／超时／鉴权／限流／HTTP／I/O失败使用固定分类。来自源站的HTML／JSON在局域网返回前拒绝，合法text/plain媒体不再仅凭MIME被误标未知；空GET响应明确记失败。源检查、阶段、布尔与数值均纳入诊断导出白名单。

传输诊断复用原操作身份：`PlayCastVideo` 返回接受请求的 `request_id` 和 `cast_session_id`，前端确认、Seek 来源／结果、后台切换及结束记录显式关联该会话；转发使用独立请求身份和 `parent_request_id`。`cast.stream.probe`／`probe_failed` 记录源站检查，`receiver_request` 记录首次电视请求；`first_bytes` 与 `response` 分别管理首次字节和首次完成，按 playlist／key／init／media／unknown 分类，保留方法、Range 存在、状态、耗时及字节。未知响应不作为媒体数据证明。失败记录最多八条；接收端身份、已撤销资源、并发超限保留有界分类汇总。媒体进展清除跳过分片的历史故障，已取流后的暂时错误标为 degraded，客户端中止不作为源站故障。

起播超时由 Go 定时器持有，四十五秒仍无媒体字节时区分无请求、仅探测、仅列表或密钥／初始化段等阶段；再给七十五秒恢复，仍未收到媒体则释放连接。HEAD／列表不能绕过此期限；显式暂停挂起起播期限，恢复播放重新计时，已收到媒体不根据静默推断暂停故障，空闲及绝对期限仍生效。电视原始 `transport_status` 与本地 `relay_health`、`confirmation_state` 分开；`position_state` 区分零、缺字段、解析失败、不支持及超时。后端拥有时钟推进与回退规则，前端直接消费 `seek_ready`；手动确认不开放无时钟 Seek，也不写入未验证进度。`ReleaseCastSession` 按设备和会话撤销，迟到清理不释放新会话；切模式由后端释放全部旧会话。

诊断导出白名单保留上述固定字段和阶段，原生 `cast-native-*.jsonl` 沿既有有界 writer 与导出入口保留启动、权限／系统拒绝、CPU／Wi-Fi 锁结果、停止与释放。StartForegroundService 请求被接受不作为服务实际启动证明；原生日志采用独立时钟并携带会话身份。构建记录 `build_revision`／`build_modified` 和 build_identity_source；Android overlay 构建关闭自动VCS戳，因此 build.ps1 显式注入实际Git提交与工作区状态，缺失时明确 unknown。前端与后端时钟分开，不靠时间接近拼接因果。直连模式 `relay_health=unobservable`，不把缺少取流日志当作电视未请求；不记录能力 URL、完整媒体地址、凭据或响应正文，首批字节和 HTTP 成功仍不等于电视解码或显示成功。

`app/cast/service.go` 只对明确的兼容错误作有限处理：SetAVTransportURI 返回 UPnP 402／714 时，用空元数据重发一次；Play 返回 701 时最多尝试三次，间隔 300ms，发送操作总超时为 12 秒。Seek 返回 501／701 且再次查询确认仍为本次媒体的 PAUSED_PLAYBACK 时，执行一次 Play → 等待 PLAYING → Seek → Pause，确认电视恢复用户原先的暂停状态；主请求限时 8 秒，取消后的暂停恢复也受应用生命周期和 2 秒上限约束，恢复失败继续报告。其他媒体、停止、未知身份与网络错误不触发此兼容路径。超时、取消和其他错误不盲目重发。这些 SOAP 回退复用同一局域网媒体会话；不通过无界重试或提前 Seek 掩盖取流失败。

MuMu 日志中同一媒体暂停时 Seek 返回 501、播放时返回 200；[Kodi 21.3 OnSeek](https://github.com/xbmc/xbmc/blob/21.3-Omega/xbmc/network/upnp/UPnPRenderer.cpp#L683) 明确只接受 PLAYING 状态。这是暂停拖动失败的已确认条件，不能以模拟器限制为由略过。

关闭面板不取消正在交接的投屏请求，也不结束接收端会话；应用可见时继续查询状态，息屏／进入后台停止前端查询，返回前台重新读取电视状态。临时查询失败不恢复手机视频。系统入口明确标为“屏幕镜像”，并说明它同步手机画面、需要亮屏，与可息屏的 DLNA 视频投屏不同；电视只支持镜像时不能通过发送端代码把它变成独立视频接收端。实体电视息屏持续播放、返回前台、暂停／继续及失败恢复仍需用户验收。

投屏交接先记录手机位置，接收端时钟确认后执行一次续播 Seek；用户主动确认则保持从头播放；失败保留真实错误并允许继续遥控。快进／快退以电视返回的当前时间为基准，每次 10 秒，不读取已暂停的手机时间。暂停状态同样持有本地播放保护。接收端进度用于播放页展示、检查点和既有历史保存节流；停止投屏前再读取一次电视时间，手机定位到最后有效位置并保持暂停，由用户主动播放。位置与时长同时为零时视为没有可用时间；PLAYING 仅是接收端报告的状态，不单独作为自动交接依据；临时缺失时保留最后有效检查点。设备发现界面复用播放器抽屉深色表面，小屏初始高度 60%；已确认的独立遥控页占满可用区域，原生全屏遥控直接适配播放器宿主，短横屏不再沿用小屏紧凑密度。

上述会话与控制分离参考 2026-10-29 对 MuMu 已安装 B站 9.17.0、抖音 40.9.0 的 REA 方法级分析：抖音 `PlayerController.play(PlayerInfo)` 将 URL、接收设备交给选定引擎，暂停／继续是独立的接收端命令；B站 `ProjectionClient` 独立提供状态、进度与事件流，业务层按来源身份维护播放约束。证据位于 `dist/audit/cast-rea/`。分析使用原始 DEX 切片，未确认完整私有协议、底层渲染器的所有分支或任意电视兼容性。

夸克 10.20.0.1175 的官方 Android 包来自[夸克官网](https://www.quark.cn/)提供的下载入口，静态 REA 证据保留在 `dist/audit/cast-current/browser-rea/`。`LeboProjectionService.startProjectionImpl$lambda$17` 将 URL、LelinkServiceInfo、时长和起始位置交给乐播 SDK；`ProjFlutterPlugin.H`／`I` 分别发布 `cast_player_state`／`cast_position_update`，通过 transaction recordId 关联会话。`ProjManager.l0` 在 Connecting／Projecting 状态持有 CPU 唤醒锁，其他状态释放；`VideoCastDaemonService.b` 提供“视频正在增强投屏中，请保持应用开启”的前台通知。其 `enhance.fallback.d.o`／`p` 和 `fallback.b.b` 还具有本地文件、HLS、TS 的 HTTP／Range 服务，因此不能把当时 CCZJ 的电视直连模型推广为浏览器的全部投屏路径。尚未追完整个私有 SDK、远程请求头、增强与回退策略，不将这些静态方法作为任意设备可连接或息屏播放通过的证明。补充运行观察已通过真实设备列表连接 Kodi，接收端实际解码 90 秒 MP4，约 12 秒处返回有效时钟；手机出现独立遥控页，返回网页后视频区显示投屏占位及悬浮控制入口。此轮仅借鉴会话／状态与资源所有权的代码分工，手机界面采用前述两层遥控，未采用夸克的圆盘页面。当前 CCZJ 的局域网媒体传输另由本项目的 CastRelay 与原生后台服务持有，不依赖上述私有 SDK。

本轮 MuMu 的 B站 9.17.0 对照已实际连接 Kodi，接收端播放出视频，Kodi 返回约 10 秒／12 分 30 秒的有效媒体时间。手机采用两层遥控呈现：播放页原视频区域变为设备、换设备、播放／暂停和进度控件，下方简介、评论保留；展开后提供独立深色遥控页，集中设备、前后 10 秒、播放／暂停及媒体选项，返回播放页仍保留遥控区。布局和控制入口可借鉴；本次手机进度持续显示 00:00/00:00，Kodi 在同模拟器退后台后暂停，因此不把进度同步、暂停命令效果或实体电视息屏播放算作通过。原始截图及状态在 `dist/audit/cast-current/competitors/`。官方 TV 接收端原包因 ARM32 ABI 与当前 MuMu 不匹配而未能安装，未修改 APK。抖音 40.9.0 未登录时，搜索明确提示“请登录后重试”，当前短视频长按、分享、侧栏及有限视频切换未取得投屏入口；静态 REA 定位到横屏播放器标题栏及媒体资格检查，但本轮未取得其连接后的手机界面。第三方设备要求和媒体服务不同，不能用静态分析替代运行结果。

2.5.1 的竖屏遥控曾在 MuMu 验收包与 Kodi 上实测，但当时遗漏了原生全屏投屏布局；原有结果不能证明全屏外观已通过。此前已核对：新原生接口发现设备、真实取流、匹配媒体后本地暂停；紧凑／展开界面、系统返回、至少 44px 触摸目标和展开主按钮 80px 均已核对。实际按钮／进度控件核对到接收端 Pause speed=0、Play speed=1、Seek 到 40 秒且手机同步；接收端前台保持 Surface 时 Stop 成功，手机交还至 14 秒并能手动继续。强制关闭接收端后，停止失败显示明确断开提示，主动断开后手机恢复播放。证据在 `dist/audit/cast-current/remote-ui-retry/` 和 `remote-final/`。同一模拟器中的 Kodi 切后台会暂停，早期暂停 Seek 发生 501、播完后 Stop 发生超时；暂停 Seek 兼容处理依据下述接收端源码和后续实测维护；不能把前台接收端结果写成实体电视息屏验收通过。手机真实 Wi-Fi／路由器、实体电视编码兼容和息屏持续播放仍需用户验收。复用既有脚本的 setup／finally 并通过 ADB／CDP 调试操作，未修改脚本；旧脚本完整断言及其“全通过”结论不适用于此轮界面和后台规则。

`app/cast/discovery.go` 在每个经原生快照或兼容入口确认的有效私网 IPv4 组播接口上发送 MediaRenderer、AVTransport、根设备和全部设备查询，间隔 1.1 秒重发一次，接收窗口 4.5 秒，总任务最多 8 秒。描述获取使用三路有界队列，按地址去重；只接受与回复发送端同 IP 的私网 HTTP 描述和控制地址，不跟随跳转。请求取消或宿主退出会关闭套接字、停止描述请求并等待工作结束。Android Wi-Fi 组播锁仅在搜索期间持有；原生读取或准备阶段失败和关闭面板也必须释放搜索状态。

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

本地修正版在 MuMu Android 15 发送端和独立 Windows Kodi 21.3 接收端复验生命周期，原包使用隔离的 portable_data，未修改协议、未安装手机中转服务。通过真实设备列表选择接收端、发送原始 MP4 地址后：显式熄屏且系统报告 Asleep 时接收端从 2.540 秒前进到 14.730 秒；返回桌面从 15.334 到 27.386 秒；调用应用原生 finishAndRemoveTask 退出从 28.590 到 40.434 秒；强制停止发送端且 pidof 确认进程不存在时从 40.643 到 52.684 秒。各项观察约 12 秒，接收端 speed 始终为 1。随后在相同独立接收端通过全屏真实触摸拖到 50 秒，接收端与手机都为 50 秒且保持暂停；停止后接收端活动播放器列表为空，手机定位在 50 秒并能主动继续播放。控制证据在 `dist/audit/cast-fullscreen/lifecycle/independent-controls.json`，续播证据在 `dist/audit/cast-fullscreen/lifecycle/results.json`。这是独立进程接收端、90 秒 H.264/AAC MP4 的短期续播验收，不覆盖实体电视、手机省电策略、受鉴权源或长时间 HLS 稳定性。正常后台返回保留遥控和本地暂停保护；强制停止后冷启动回首页，不自动重建遥控，需要重新进入视频选择设备，不能把电视续播通过表述为冷启动会话恢复也已实现。

投屏诊断按 `cast_device_id` 和 `cast_session_id` 串联各次控制请求，RPC 的 request／operation 身份保留。`cast.discovery.*` 记录接口、查询次数、收到／拒绝的回复与耗时；`cast.description.*` 区分发现后设备描述失败；`cast.soap.*` 记录操作、HTTP 状态、UPnP 错误码、响应大小／摘要和耗时；`cast.status` 记录接收端播放状态、媒体归属及位置。重新发现同一设备保留已有会话和目标媒体身份。空设备列表不等于视频错误，SOAP 成功或媒体匹配不等于解码成功；持续 STOPPED／ERROR_OCCURRED、其他媒体与无法取得状态分别显示。日志不保存原始视频响应和完整带签名查询地址。

## 卡顿与内存指标

Android 宿主使用 `android.webkit.WebView`，实现来自 [Chromium Android WebView](https://chromium.googlesource.com/chromium/src/+/HEAD/android_webview/README.md)；Windows 使用以 Microsoft Edge 为引擎的 [WebView2](https://learn.microsoft.com/en-us/microsoft-edge/webview2/)。移动卡顿不能直接归因于 WebView2。排查特定设备时需记录系统版本、WebView 提供程序及版本；Android 官方提供 [WebView 包版本接口](https://developer.android.com/develop/ui/views/layout/webapps/managing-webview#version-api)，实现和版本差异需要结合现场判断。

本项目 `app/resource/native_android.go` 的 RSS／PSS／私有驻留仅覆盖应用主进程，不包含隔离的 WebView 渲染进程；`native_windows.go` 的总提交内存则汇总可读取的宿主进程树。根据 [Chromium WebView 架构](https://chromium.googlesource.com/chromium/src/+/main/android_webview/docs/architecture.md)，GPU 和 Network Service 在宿主进程内运行，不能写成独立 GPU 进程；图形驱动／系统共享服务的分配仍有统计边界。RSS 包含驻留的共享页，PSS 按共享进程分摊，Windows 私有提交表示已提交的私有内存，并不等于当前物理驻留。定义见 [Android 内存诊断](https://developer.android.com/tools/dumpsys#meminfo)和 [Windows 进程内存计数器](https://learn.microsoft.com/en-us/windows/win32/api/psapi/ns-psapi-process_memory_counters_ex)。所以移动约 200 MB 与桌面约 1 GB 不能直接比较，也不存在据此判断是否流畅的固定阈值；应连同系统可用内存、同一操作前后的增长和未覆盖进程判断。Go 堆与浏览器 JS 堆均为局部指标，不与这些总量相加。

Android 宿主通过 `CczjMemoryDiagnostics` 补充系统代采的主进程 PSS、私有驻留、Java／Native／Code／Stack／Graphics 等分类与 Swap，以及 WebView 包名／版本。`memoryState` 只返回带查询时间的缓存快照，按需投递单个后台任务，最多每 5 秒发起一次；关闭桥接时关闭采样器。来源为 [ActivityManager.getProcessMemoryInfo](https://developer.android.com/reference/android/app/ActivityManager#getProcessMemoryInfo(int%5B%5D)) 和 [Debug.MemoryInfo](https://developer.android.com/reference/android/os/Debug.MemoryInfo)，不阻塞 UI 线程做内存转储。分类不与总 PSS 相加，Graphics 为系统报告值，0 不意味着没有图形分配。Android 还会限制该 API 的采样频率并返回较早快照，其原始采集时间不可得，因此 `queried_at_ms` 只表示查询时间，不保证数据新鲜度；趋势以 procfs 当前读数为准。前端诊断导出保留 `android_memory` 的查询时间、来源、范围和分类，旧宿主缺少桥接时显示不可用。视频及阅读模式录制均保留这些分类。

普通 Android 10+ 应用的 `getProcessMemoryInfo` 只能读取自身 UID，其余 UID 返回零；公开 WebViewRenderProcess 接口不提供 PID／内存。扩展诊断走独立的 `CczjProcessMemory` adapter：明确授权后，从应用身份执行系统转储，按 `dumpsys activity processes <包名>` 的 packageList 确认主进程及隔离 renderer 的唯一 PID，再逐个执行 `dumpsys meminfo --local <PID>`，前后核对 PID 与 ProcessRecord 身份后汇总 Total PSS。RSS 单列，不相加；不把全机 WebView 进程都算入，也不按 PPID 推断归属。最多 8 个进程，每条输出上限 512KiB、单命令 1.5 秒、整轮 4 秒；退出终止子进程和计时器。授权不足、进程变化、漏采或超限时只保留失败码，不显示部分合计，不保留其他应用的原始转储。该合计称为“已确认归属进程 PSS”，并非全部 GPU 物理内存。

`DUMP` 是 `signature|privileged|development` 权限，不能用普通运行时授权框申请；本项目按用户要求声明该可选权限。系统转储还要求 `PACKAGE_USAGE_STATS` 及对应 AppOp，依据 [Android 权限定义](https://android.googlesource.com/platform/frameworks/base/+/refs/heads/main/core/res/AndroidManifest.xml)和 [DumpUtils 权限检查](https://android.googlesource.com/platform/frameworks/base/+/refs/heads/main/core/java/com/android/internal/util/DumpUtils.java)。诊断页提供实际系统“使用情况访问”入口与复制 ADB 命令；正常使用不依赖这些授权。在已授权调试的设备上执行，下列包名替换为实际安装身份：

```powershell
adb shell pm grant com.cczj.video.acceptance android.permission.DUMP
adb shell pm grant com.cczj.video.acceptance android.permission.PACKAGE_USAGE_STATS
adb shell appops set com.cczj.video.acceptance GET_USAGE_STATS allow
# 撤销扩展诊断
adb shell pm revoke com.cczj.video.acceptance android.permission.DUMP
```

获授权且采样成功后，内存主卡片、曲线与进程明细使用归属进程合计 PSS；未授权则保留主进程指标。权限或采样范围变化时重置趋势基线，避免把口径变化误判为泄漏。主进程系统分类仍单列，查询时间和各进程数值一并进入录制／导出；Android 权限被拒绝或撤销后不继续显示已授权的旧合计。

60 Hz 屏幕每帧约 16.67 ms，浏览器自身也需要时间；超过 50 ms 的主线程任务属于长任务，但短于 50 ms 仍可能错过动画帧。优化时区分交互／布局／绘制阻塞、图片解码及视频缓冲／解码，不能用单一内存读数定位。依据 [Chrome 渲染性能](https://web.dev/articles/rendering-performance)和[长任务优化](https://web.dev/articles/optimize-long-tasks)。

移动主路由的跟手位移由 `MobileRouteStage` 按动画帧合并，仅更新当前页、预览页和背景的 transform；高频坐标不进入 Vue 响应式状态，避免每次触摸都更新路由壳层与页面插槽。手势宽度在起点缓存，帧内先计算再写样式，不交错布局读取。标签切换的非 passive touchmove 监听仅在可能发生横向手势时注册，识别为纵向后立即解除，结束、取消和卸载也会清理；不能把整个应用长期挂在可取消的移动监听下，原因见 [Chrome 触摸滚动说明](https://developer.chrome.com/blog/scrolling-intervention/)。`RemoteImage` 保留正常换图的淡出／淡入并提供异步解码提示；淡入结束后移除已完成动画，以最终 opacity 样式保持显示，避免长期保留 forwards 动画。解码提示见 [HTML 图片解码规范](https://html.spec.whatwg.org/multipage/images.html#image-decoding-hint)。回顶按钮仅在已提交且可见的页面绑定滚动容器，初次读取位置放在首帧绘制之后；滚动中复用容器，手机按钮显隐不再启动桌面藤蔓计时器。

页面的可见性与工作资格分开：`MobileRouteStage` 提供真实已提交路由的工作状态，`useViewActivity` 的 `committed` 范围供轮播、诊断、日志及阅读发现请求使用。预览可以显示保留内容和近视口图片，但取消滑动不会激活轮播或发现请求；隐藏／退后台立即取消所属工作，恢复后沿用已有数据。漫画只为当前展示的频道加载或刷新，切换频道取消不可见频道的在途读取。短剧空态／剧场不加载 `VideoPlayer`；确有播放内容才异步装配同一个播放器，不增加第二套播放实现。跨内容模式继续以整文档切换释放旧模式运行资源，仅传递轻量检查点，避免同时保留三个模式的页面树。

同日补充实测：在验收包授予 DUMP、PACKAGE_USAGE_STATS 与 GET_USAGE_STATS AppOp 后，应用自身成功读取宿主与 renderer 的合计 PSS。撤销 DUMP 或拒绝 Usage AppOp 后，旧合计消失，主卡片／曲线回退到主进程口径；重新授权后恢复合计且重置曲线基线。“打开使用情况访问”实际进入 Android 的 AppUsageAccessSettingsActivity。小说／漫画从书架拖出首页预览并取消，发现操作数和模式网络请求均为 0；提交进入后，小说三个展示分组完成 3 次 BrowsePage，漫画指定 honkai 频道只完成 1 次 BrowsePage。短剧空态没有 video 节点，也没有加载 VideoPlayer／HLS／Upscaler 分块。开启授权采样和录制的八次切页中，rAF P95 约 16.7ms，出现一次 71ms 长任务及一次约 66.7ms 回调间隔；不能据此宣称完全无掉帧。证据为同一 `dist/audit/android-performance/` 下的 `permissions-and-series.json`、`mode-work-activity.json`、`design-ui.json`、`design-runtime-profile.json` 和授权录制文件。

短剧播放器和设置动效的条件样式使用普通 `html[状态] .组件类` 祖先选择器，让 Vue 给最后的组件类加 scoped 标记。此前 `:global(html[状态]) .组件类` 在当前编译器中丢掉了后半段，短剧样式加载后把 `html` 设为最小 72×72px，影响所有路由和触摸命中；设备与编译产物均复现。已修正播放器、动效预览和基本设置同类写法，保持既有数值。作用域机制见 [Vue SFC CSS](https://vuejs.org/api/sfc-css-features.html#global-selectors)。

2026-10-28 经用户授权，在 MuMu Android 15／Chromium WebView 110／x86_64 Debug acceptance 2.4.8 实测：已有真实快照含 5 源、88 条目录、3 个收藏，首页 75 张卡片；进入短剧后页面仍为 360×616 CSS 像素，搜索、收藏及短剧空态双向切页通过。连续 8 次主页面滑动中，rAF 间隔 P95 约 16.7ms、最大 33.3ms，无超过 50ms 的长任务；首页上下滚动有一次约 66.7ms 的 rAF 间隔，无长任务。触摸取消和中途反向提交后，仅保留一个活跃场景且位移样式已清除。视频诊断开始／结束录制与自动导出完成，JSONL 中含 3 处 `android_memory` 快照。采样时已确认归属的主进程 PSS 约 154.4MiB、renderer 约 127.9MiB，合计约 282.3MiB；并非同一主进程的重复相加。原始数据及截图在 `dist/audit/android-performance/`，真实目录回归结果在 `dist/audit/real-mobile/`。rAF 是回调时序，不能冒充硬件 FPS；旧安装包／空目录与本轮包不是受控 A/B，不据此宣称性能提升百分比。尚未验证实体手机、长时间播放及全部图片失败／恢复路径。

主标签的顶部半透明渐变由共享路由 viewport 持有，跟随主题颜色更新；详情等不透明次级场景在自己的背景上绘制同款渐变，普通播放场景除外，使页头和内容随页面共同移动。图片背景仍按既有取景规则平移。顶部状态栏从同一共享渐变取色，底部系统导航栏独立读取应用底栏表面；短剧实际推荐播放时，应用底栏复用观看区深色板，系统底部区域同时按应用底栏的实色背景与半透明表面合成取色；剧场和离开短剧后恢复主题。Android 窗口及内容后备色保持主题画布色。Android 15 edge-to-edge 下 Window.setStatusBarColor 不直接上色，因此已消费的上下 inset 由 CczjInsetSurface 分别绘制，颜色不变时不重建 Drawable；启动背景缓存更新也不再清空当前系统栏取色，避免短暂回退到画布色。平台行为见 [Android 15 系统栏变更](https://developer.android.com/about/versions/15/behavior-changes-15#edge-to-edge)。切换主题仍更新实际画布，进入和退出全屏保持原有保存／恢复流程。离场场景的视觉状态在 after-leave 才释放，已提交资格立即停止后台工作；短剧显示状态读取本场景注入的路由，避免全局路由变成目标页时把剧场提前改成推荐页。搜索推荐的同源刷新保留原列表节点，只在首次加载时显示占位，并复用请求 scope 和 generation 取消旧读取；来源切换清空旧源内容。

本次 MuMu 验收覆盖 green、blue、d-blue 三个主题下，收藏、短剧推荐空态、短剧剧场分别离场，共 9 组逐帧观察：场景的渐变均为 none，共享 viewport 渐变每组保持同一个值，剧场离场全程保持 block，不再中途切成推荐页。另录制短剧剧场／收藏下六次主题颜色切换及连续导航，检查帧序列；视频首页平滑返回从 0 经连续中间值到 930px。原始数据和录像位于 `dist/audit/android-performance/gradient-after.json`、`gradient-after.mp4`、`theme-switch-after.json`、`theme-switch-after.mp4`。这些结果覆盖模拟器已有收藏和短剧空态，实际短剧播放帧、全部自定义壁纸及实体手机尚待用户验收。

短剧返回首页的近程图片保留复用 `imageRelease`，由原先 750ms 延长为最长 15 秒共享退役窗口，并限制最多 64 个待释放节点，超限先释放最早离场项；后台和卸载仍立即释放，不建立永久图片缓存。750ms 原先在邻页刚打开时就清空首页封面，使立即返回仍经过透明占位和加载事件。MuMu 上三次首页／短剧往返的 133 个首页可见采样帧，原封面始终 loaded 且 opacity 为 1，应用底栏背景始终为白色。315 帧录像的栏位固定背景像素仅有 1–2 级编码波动；截图顶部 RGB 为 249/253/251，与紧邻页面顶部一致，底部为 255/255/255。证据为 `dist/audit/android-performance/system-bars-after.json`、`system-bars-after.mp4`、`system-bars-pixels.json`。这覆盖当前浅色主题的短剧空态往返，尚未覆盖实际短剧播放及全部自定义主题。 移动路由的图片观察以所在 `.main-content` 为 root，避免预览页先被外层窗口裁剪、封面直到拖到一半才开始准备；观察器按实际滚动根复用，等待项归零后断开并移除，隐藏页面仍取消观察，不增加常驻预取。

路由交接参考 [Ionic transition](https://github.com/ionic-team/ionic-framework/blob/main/core/src/utils/transition/index.ts) 的目标视图准备与完成清理，以及 [Vant Swipe](https://github.com/vant-ui/vant/blob/main/packages/vant/src/swipe/Swipe.tsx) 的拖动／归位分离。预览的懒加载完成并挂载后才提交归位，目标加载失败回到原页；归位等待场景和背景实际的 transform 过渡结束，不按固定 230ms 提前移除旧场景。跟手过程中取消未完成的自动过渡，导航、取消与卸载清理待执行帧；不等待子内容的轮播或无限转圈。路由动画与 KeepAlive 的组合见 [Vue Router 官方示例](https://router.vuejs.org/guide/advanced/transitions.html)。此前关闭移动端多种动画和首页平滑返回的修改已撤回，其测量不能作为当前方案通过的依据。

实现优先检查可见区域外的列表节点、重复响应式计算、隐藏页面工作和布局读写交错。大列表沿用既有窗口化能力；大型只读数据确有开销时可用浅层响应式，但更新必须替换根值。动画优先使用 `transform`／`opacity`，模糊和阴影的绘制成本及合成层数量需结合实际设备剖析，不无差别添加 `will-change`。参考 [Vue 性能指南](https://vuejs.org/guide/best-practices/performance.html)及 [Chrome 动画指南](https://web.dev/articles/animations-guide)。这些是官方建议和代码静态分析依据，尚不能证明用户现场的具体根因或优化幅度；流畅度仍由用户在 MuMu／真机上验收。

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

手机视频底栏参考用户提供的 B 站截图，图标下恢复本地化文字；普通图标 16px、搜索图标 18px，其余入口文字 10px，中央搜索按用户要求采用 40×40px、8px 圆角的方形按钮，上移 12px，按用户要求去掉搜索可见文字，保留可访问名称。链接保留本地化 aria-label、当前页语义和至少 44px 触摸高度；顶部内边距 3px，底部 20px 加系统安全区，下方留白大于上方。背景伪元素 opacity 为 0.7，图标和文字不一起变淡。系统底栏取色合成时同时计入该 opacity。共享画布纯色放在固定 stage 上，只有壁纸与遮罩参与大画布位移，避免无壁纸时仍把整块纯色交给移动层。短剧右滑的残余闪烁仍在定位，不能以这项层次调整或底栏预览宣称已解决。 推荐播放的静默刷新另行实查：刷新期间始终只有一个 video 节点，没有切回占位页或出现加载图；应用底栏和截图底部像素均为 RGB(24,25,28)。全屏按钮实际进入 short-drama 区域，并通过同一按钮退出。第 1 集到第 2 集的交接采到 7 个位移样本，进入面从约 446px 连续到 0、退场面同步向上移，结束后 Canvas 宽度归零；源站失败时旧内容继续保留。证据为 `dist/audit/android-performance/feed-silent-refresh-final.json`、`feed-fullscreen-result.json`、`feed-series-paging.json` 和对应录像。该结果不代替全部源站或真机验收。

## 移动路由共享背景

上述短剧录像对应此前等待首帧再交接的版本。当前针对用户反馈调整为：主动上下翻页按动画时长完成，手势绑定到固定短剧区域，静默刷新仍可保留旧画面；进度条抬高并扩大触摸区，拖动放大轨道与滑块，选集紧邻简介，全屏位于右下角。暂停播放图标与失败页的非按钮区域也允许开始翻页。此轮只进行编译、类型、样式与文档静态检查及安装交付，尚待用户在 MuMu／真机确认拖动、长按、连续上下翻页、缓冲／失败后的恢复与全屏行为，不能用此前录像宣称当前手感已验收。

后续交互调整将标题改为纯文本，简介在下方直接展示至多两行并跳转已有详情页；时间常驻进度条下方，全屏图标缩为 16px，保留 44px 触摸范围。拖动预览复用现有目标帧采样器，仅在拖动时创建预览解码器。手机首页可提前准备首部推荐，推荐模式随机换剧并预热下一部，剧场模式按集顺序播放并预热下一集；没有上一项时大幅下拉触发刷新。会话、网络额度与媒体缓存边界见[短剧播放](development.md#短剧播放)。本次仅完成静态检查和 APK 构建；打包时 ADB 没有在线设备，未完成覆盖安装或设备行为复验。最新构建日志在 `dist/audit/android-performance/drama-queue-final-build.log`，此前录屏不作为这些新交互的通过依据。

手机播放器按钮位置回归使用 `python scripts/test-mobile-player-layout.py --device emulator-5554`，只操作 acceptance 包。复用原生绑定与媒体服务，验证普通播放／全屏的右上角线路入口、底部弹幕开关、线路抽屉和 320／360px 控件边界；结束恢复来源、设置与页面。界面截图使用无时间码的本地验收片，弱网测试片自带的时间码不属于播放器界面。截图与结果位于 `dist/audit/mobile-player-layout`。

系统栏安全区统一由 `MainActivity` 的根视图消费，WebView 不重复预留顶部高度；栏色和图标明暗跟随当前应用模式的主题画布，短剧剧场与空态保持主题，已提交的实际播放场景让应用底栏与系统底部区域跟随播放器深色板，退出全屏后重新应用当前场景。手机次级页面标题栏保持透明，沿用共享主题背景，不再铺设固定白色／黑色横条；返回键与标题保留。主题页通过左侧悬浮按钮编辑当前主题，不在卡片列表上方占用操作区。日志导出生成在应用私有 `exports/logs` 下，随后直接打开系统分享界面；只有在分享目标中保存后才会出现在用户选定的文件夹中，取消分享不代表已写入下载目录。

自动切页的栏色过渡只读取一次页面时长和曲线；底栏由浏览器动画运行，系统栏由现有 ValueAnimator 运行，旧宿主仅按时间插值颜色，不逐帧读取布局。同表面颜色不启动栏色动画；手势直接传入进度，取消／新目标清理原动画。全屏会话以请求身份拥有四阶段状态，重复释放共享退出承诺，迟到进入确认不能重开正在退出的会话；进入失败的回滚也等待原生布局恢复。详情和播放选集通过共享 VirtualGrid 有界挂载，固定高度及独立滚动保留。上述实现的真机栏色、全屏及连续操作仍须用户验收，MuMu 的短时采样不代表所有设备。

视频底栏不再参与舞台 Flex 高度分配；主标签场景按底栏实测高度预留空间，打开次级页时离场页保持原几何。全屏 custom view 仍覆盖正常 WebView，但底层 WebView 保持可见以保留显示面；窗口进出旋转暂用 `ROTATION_ANIMATION_JUMPCUT`，避免系统旋转动画与浏览器布局交接叠加，恢复原生尺寸后还原窗口原策略。此策略的含义见 [Android WindowManager.LayoutParams](https://developer.android.com/reference/android/view/WindowManager.LayoutParams#rotationAnimation)。MuMu 录屏中曾出现的全黑帧需按同一媒体、相同进出操作前后比较，不能用 DOM 尺寸吻合代替像素检查。



对应隔离回归运行 `python scripts/test-mobile-feedback-android.py`，先安装当前前端对应的 acceptance Debug 包。它验证 XML 源搜索、浅色／深色主题与编辑入口、短剧推荐及剧场不覆盖外壳与原生栏色，以及导出后打开系统选择器并在取消后保留分享入口；测试来源、网络许可和主题恢复到进入前状态。截图位于 `dist/audit/mobile-feedback`，模拟器结论不代替华为真机验收。

`MobileRouteStage` 在路由场景之外保留一个背景层，页面本身透明。视频模式沿用用户主题图片和遮罩，路由通过有限、稳定的取景坐标移动同一背景；背景使用五列、两行的固定画布：五个视频主路由等宽分区，子页使用稳定的第二行取景；小说／漫画按各自三个主路由划分。只动画 transform，预览页不加边缘阴影，避免同一张图上出现竖向色带。手势预览插值坐标，取消恢复原取景；设置子页、返回和连续导航不重建图片层。背景与内容同向但位移更小，页面内容改为完整滑出，避免透明页面相互叠字。桌面维持原绘制方式，小说／漫画保留各自画布色，阅读器及播放器继续拥有不透明的内容底色。系统减少动态效果、页面动画关闭都禁用背景过渡。

移动路由采用横向 transform 过渡，不叠加页面根节点的 opacity 淡入；内容列表、图片换图、轮播及操作反馈保持各自正常动效。已展示的缓存内容再次进入时，不重放首次入场动画：场景记录完成或离场中断的指定入场动画，重新插入同一 DOM 时将这些动画推进到完成位置；新元素仍按原样入场，不改全局动画开关。该记录使用 WeakMap 随内容节点释放，排除转圈、进度、图片换源及主动操作反馈。

滚动目标由 router 统一记录并交给 `restoreModeScroll`；点击导航返回视频首页时保留从顶部平滑回到原位置的动画，场景只在首次激活时建立动画起点。跟手滑动提交首页已在预览中展示，不再重置起点或重播进入动画。恢复响应内容尺寸变化并受截止时间约束，用户触摸、滚轮或再次导航均可取消。

首页离场只取消在途推荐读取，保留当前源的轮播与推荐；重入时先显示保留内容，移动路由进入动画结束后后台刷新，不先清空再重建，也不在未提交的滑动预览中开始读取。离场使排队刷新失效，来源切换仍立即清空旧源内容。短剧已确认的空结果在后台复查期间继续显示，复查仍会发现新采集内容及来源错误，不反复替换为加载占位。`RemoteImage` 的未加载／已释放节点使用共享透明像素，避免每张尚未进入视口的海报创建 Chromium 破图替代文字布局；其透明占位不视为实际图片加载成功，真实图片仍按原有可见性、代理和生命周期规则加载与释放。MuMu 调试数据保存到 `dist/audit/page-polish/mobile/`，WebView 帧间隔和主线程长任务用于定位，不能等同于真机硬件帧率。

移动视频卡片复用已有共享可见性观察器，在视口外 240px 内才创建海报、角标和播放遮罩；此前保留海报比例与文字尺寸，避免通过改变卡片高度节约节点。手机卡片不再等待二次淡入才显示文字；桌面的可见性边界与呈现保持原行为。两种边界最多各持有一个共享观察器，卡片销毁时解除观察。

重复验收：安装与当前 `frontend/dist` 匹配的独立 acceptance Debug 包后，运行 `python scripts/test-mobile-backdrop.py --device emulator-5554`。脚本在宿主中临时展示已有内置水墨背景，不持久修改用户主题；检查单背景节点、跨页、滑动取消、动画逐帧边缘覆盖、减少动态效果和模式隔离，结束恢复模式和页面。证据放 `dist/audit/shared-backdrop/`；逐帧检查同时确认主路由间位移等于一个视口宽度、滑动预览没有边缘阴影；帧回调不是硬件 FPS，MuMu 验证不能代替鸿蒙真机。

移动端日志复用共享 LogPanel：概览和级别／来源筛选默认收起，搜索与记录列表靠前；输出级别和导出格式使用统一 Segment，日志详情使用统一 Modal 并支持系统返回关闭。列表保留固定 76px 双行窗口化渲染，完整长记录在详情中换行，历史文件和原始文本共用原有读取、取消与导出接口。回归入口 `scripts/test-mobile-logs-layout.py` 覆盖窄屏、中英文、浅深主题、筛选、长记录和历史查看；导出系统交互沿用 `scripts/test-mobile-feedback-android.py`。

设置首页省去总说明，分组及工具页不重复提供右上角搜索／设置链接；弹幕源页复用宿主标题。移动小／中／大按钮使用公共密度变量按屏宽变化，最小高度分别在 36～40px、40～44px、44～48px 间调整，内容或字体变大时允许自然撑高；页面留白使用共享 section-padding，不能把每个控件强制改成同一高度。

视频壁纸的主路由取景向下偏移 6% 视口高度。短剧的禁止横向切页标记仅随实际播放场景挂载，空态、错误和加载态可以切换主路由。`backdropSystemBars` 复用有界图片像素读取，按当前取景顶部、背景透明度和页面遮罩计算原生栏色；Java 桥接只更新临时外观及系统图标明暗，不覆盖 SQLite 主题或开屏缓存。原生根容器同时更新，兼容系统将状态栏设为透明的情况。

真实目录验收使用 `scripts/test-real-catalog-android.py --snapshot <桌面库只读快照>`，只向 acceptance 包装载视频来源、目录、收藏和历史，并在首次操作前保留该包原库。真实桌面库不写入，目录数据不打入 APK。检查首页、搜索、收藏、短剧空态双向滑动，以及原生栏色桥接和开屏缓存隔离；结果及截图归入 `dist/audit/real-mobile/`。

五项底部导航的当前尺寸与留白见上文底栏说明。搜索按钮的装饰图标不单独捕获触摸，由链接承担点击与焦点；选中时沿用主题强调色。MuMu 当前计算样式为普通图标 16px、文字 10px、搜索图标 18px、底栏上下内边距分别为 3px／20px，总高 67px。最新有数据剧场右滑及收藏、搜索往返的 249 帧样式采样中，共享渐变保持一个值、底栏 opacity 为 1（表面伪元素为 0.7）；录屏为 `dist/audit/android-performance/drama-current.mp4`。这不覆盖用户原先无短剧数据的空态闪烁，不能宣称该问题已经解决。

移动源站搜索只在选中至少一条结果时显示底部保存栏；取消最后一项、保存成功、翻页或切换来源后收起。出现时在整个搜索页末尾预留空间，分页属于预留范围，不能只给结果网格增加底部留白。桌面搜索保留原有工具栏。

播放状态验收运行 `scripts/test-playback-state-android.py`：使用独立来源验证前四集 99%、第五集 15% 的继续观看、收藏夹选择、重启后收藏，以及弹幕字号滑杆与保存。退出时删除本次来源并恢复设置和 WebView 缓存。真实目录导入前归档旧进度缓存，导入后清掉测试目录遗留的编号缓存，从复制的 SQLite 历史重建进度，避免相同 global_id 命中旧测试片段。此脚本只用于 acceptance 包，不用于真机 preview 数据。

手机详情页的选集区直接展示集数，不挂载 `PlayLinePicker` 的来源探测和重试卡片；播放时通过右上角线路抽屉选择。桌面详情页保留线路选择。

主题恢复完成前，启动屏复用启动缓存底色，App不将尚未恢复的默认主题覆盖到原生启动缓存。主题选择不插入瞬时保存文案，保存失败继续显示真实错误与重试。支持ViewTransition的宿主使用浏览器扩散，Android旧WebView使用下述原生圆形揭示；仅旧宿主桥接缺失时按声明时长插值颜色，减少动态效果仍跳过。普通主题保留染色表面，“简约”独立提供黑白灰背景、强调色及按钮。搜索历史普通与编辑状态均不为右上角删除按钮预留内容宽度，保持44px触摸高度；进度条填充和缓冲层的缩放保留垂直居中变换。

移动媒体功能抽屉采用固定深色观看配色，应用主题不再改变倍速／线路／选集／更多设置的表面和按钮。遮罩点击只关闭当前面板并恢复控制栏，不冒泡为视频单击；直接视频单击也先处理打开的面板。选集进度置右上角，排序和关闭图标收紧，保留触摸目标。主题切换由共享themeReveal拥有：Android缺少ViewTransition时，经api/themeReveal与WailsJSBridge请求CczjThemeReveal保存有界旧画面，新主题DOM视觉提交后由原生圆孔扩大露出；不是颜色渐变替代。局部预览裁剪边界，前后端4秒限时与身份隔离，取消／换页／后台／旋转／低内存／销毁移除遮罩并释放位图。无原生桥接才保留颜色渐变，关闭／减少动效跳过。移动画布、顶部渐变、控件和导航保留原壳层配色，仅壁纸卡片默认78%不透明、自定义contentAlpha保留可读边界；主题外观页单独去除分组外层实色白框，卡片外缘开始揭示画布；播放器固定深色和阅读模式独立保留。简约启动图使用grey资源并灰度呈现。紧凑回顶部复用MotionTransition slide-up进入与退出；桌面藤蔓保留原动画，关闭或减少动效时跳过。

主标签快速反复点击时，MobileRouteStage保留当前滑动直至完成后提交最新等待目标，避免两页中途反向重入导致画面间隙；收藏与搜索的返回刷新在路由落位后执行。主题扩散默认1000ms，原生与浏览器同用(.4,0,.2,1)曲线；显式时长偏好与减少动态效果仍优先。

启动异常先按系统ApplicationExitInfo的PID核对现有applog会话，区分Java异常、信号终止、用户停止与升级安装。CczjStartupTrace在原生库加载前记录有界白名单阶段并异步保存，接续前端首屏／开屏关闭及前后台阶段；Go日志单独记录数据库、策略、插件与模式初始化。启动记录最多保留4次，由现有诊断导出收集；异步写入可能丢失崩溃前最后几条，不能仅根据最后阶段断言底层根因。
