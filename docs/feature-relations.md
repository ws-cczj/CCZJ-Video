# 功能关联索引

本页记录当前源码中的主要能力、共享所有者和需要联查的入口，供修改前确定影响范围。维护要求统一见 [AGENTS.md：关联影响与完整体验](../AGENTS.md#关联影响与完整体验)。表中“联查”包含直接调用、共享状态和同一用户流程，不表示每个文件都直接依赖同一个模块，也不表示这些路径已完成设备验收。

先按下表选主题，再从对应小节的真实入口和检索词追踪实际调用方；关键文件的导入、导出、事件、Key 和平台装配仍需检索。这里列出的是查找起点，不能将未列出的新消费者排除在修改范围之外。通用复用入口与维护命令见 [开发指南](development.md)，公开绑定及模块依赖可参考[现有架构画布生成入口](development.md#开发与检查)；本页补充设置、体验和状态之间的联系，不另建一套接口清单。

| 准备修改 | 首先联查 |
| --- | --- |
| 路由、弹窗、列表或图标动画 | [动效](#动效与过渡)、[导航与模式](#导航与模式)、[页面活动与异步工作](#页面活动与异步工作) |
| 主题、字号、密度、壁纸、语言 | [外观](#外观与布局)、[设置入口](#设置入口与分组)、[平台](#平台与原生能力)；阅读偏好另查[阅读](#小说漫画与阅读共享能力) |
| 播放、选集、倍速、全屏或弹幕 | [播放器](#播放器与播放偏好)、[短剧](#短剧)、[历史与收藏](#历史进度与收藏)、[缓存](#缓存与失效) |
| 章节、话次、换源、书架或阅读进度 | [阅读](#小说漫画与阅读共享能力)、[历史与收藏](#历史进度与收藏)、[来源与网络](#来源采集与网络) |
| 删除、修改来源，清库或导入数据 | [来源](#来源采集与网络)、[缓存](#缓存与失效)、[存储与恢复](#key存储与备份恢复)、[下载](#下载任务与文件输出) |
| 请求、事件、取消、性能或资源泄漏风险 | [页面活动](#页面活动与异步工作)、[平台](#平台与原生能力)、[诊断](#错误日志与诊断)，再查所属业务小节 |
| 扩展包、设置分组或第三方脚本 | [扩展包](#扩展包与动态入口)、[设置](#设置入口与分组)、[动效](#动效与过渡)、[存储与恢复](#key存储与备份恢复) |
| 更新、安装、文件选择或分享 | [更新](#更新与安装)、[平台](#平台与原生能力)、[页面活动](#页面活动与异步工作) |

## 动效与过渡

| 关系 | 当前入口与所有者 |
| --- | --- |
| 设置与参数 | [SettingsBasicGroup.vue](../frontend/src/views/settings/SettingsBasicGroup.vue) 提供不依赖扩展包启停的总开关，[MotionPanel.vue](../frontend/src/components/MotionPanel.vue) 提供参数；[stores/motion.ts](../frontend/src/stores/motion.ts) 负责加载、保存、回退及系统减少动态效果，消费者通过 `allows()` 判断是否播放，[motion/preferences.ts](../frontend/src/motion/preferences.ts) 定义参数、默认值与变量换算。 |
| 动态分组与样式 | [内置 motion-effects 包](../app/plugin/builtin/motion-effects/) 注册设置分组并提供样式；[plugins/registry.ts](../frontend/src/plugins/registry.ts)、[plugins/runtime.ts](../frontend/src/plugins/runtime.ts) 和 [stores/plugins.ts](../frontend/src/stores/plugins.ts) 关联加载与卸载。不能只在 Settings.vue 的内置分组中寻找动效页。 |
| 路由与设置页自身 | [DesktopVideoShell.vue](../frontend/src/modes/DesktopVideoShell.vue)、[MobileRouteStage.vue](../frontend/src/mobile/MobileRouteStage.vue)、[routeMotion.ts](../frontend/src/mobile/routeMotion.ts)、[route-motion.css](../frontend/src/mobile/route-motion.css)；小说／漫画壳层另见导航小节。[Settings.vue](../frontend/src/views/Settings.vue) 的分组导航使用动效规则，SettingsBasicGroup 不再叠加独立区块入场。 |
| 覆盖层与反馈 | [MotionTransition.vue](../frontend/src/components/ui/MotionTransition.vue)、[MotionList.vue](../frontend/src/components/ui/MotionList.vue)、[Modal.vue](../frontend/src/components/ui/Modal.vue)、[Select.vue](../frontend/src/components/ui/Select.vue)、[ReaderSheet.vue](../frontend/src/reading/ReaderSheet.vue)、[PlayerDialogFrame.vue](../frontend/src/player/views/PlayerDialogFrame.vue)、[CczjMobilePlayerDrawer.vue](../frontend/src/mobile/CczjMobilePlayerDrawer.vue)；反馈还需查 [Button.vue](../frontend/src/components/ui/Button.vue)、[Empty.vue](../frontend/src/components/ui/Empty.vue)、[Icon.vue](../frontend/src/components/Icon.vue)、[ErrorToastStack.vue](../frontend/src/components/ErrorToastStack.vue)。 |
| 非组件内的动态行为 | [animations.css](../frontend/src/styles/animations.css)、[BookCarousel.vue](../frontend/src/components/BookCarousel.vue)、[BackToTop.vue](../frontend/src/components/BackToTop.vue)、[restoreScroll.ts](../frontend/src/modes/restoreScroll.ts)，以及下文的短剧翻页、小说翻页与播放器媒体状态。 |

[MotionPanel.vue](../frontend/src/components/MotionPanel.vue) 复用 routeTransition／路由 CSS、公共 Modal、CczjMobilePlayerDrawer，以及 themeReveal 的具名局部快照。局部主题预览不写入 themeID 或 themeCustoms；离场关闭弹层并释放快照。`reduceMotion`／`durations` 扩展已有 motionOptions，联查 store 的读取、串行保存、回滚、reset，以及页面、阅读翻页、覆盖层和纯 CSS 消费者。旧 style／speed／intensity 保持读取，不再展示抽象选项；显式场景时长优先。

桌面侧栏与切页共同读取 [videoNavigation.ts](../frontend/src/modes/videoNavigation.ts) 的实际顺序，动态扩展入口一起参与方向计算；并列路由、设置分组和播放侧向覆盖共用 `--cczj-motion-section`。主题揭示单独消费 `--cczj-motion-reveal`；修改时联查基础 CSS、内置包、motionVariables、durations、旧偏好恢复、重置及用户／系统减少动态效果。播放页从右侧进入／退出，不计算圆形展开原点。

移动路由由 MobileRouteStage 直接移动实际页面，加载状态由页面自身拥有；不在切页期间隐藏整页换成通用骨架，也不在落位后再播放一轮内容入场。次级页面的页头和正文共用顶部渐变（播放页保留媒体表面）。`useBackdropSystemBars` 在路由提交、旧媒体清理之前保存颜色交接起点；过渡期间不反复重采样主题；拖动直接消费手势进度，自动落位不逐帧读取布局或动画树；同深浅表面跳过颜色动画。底栏用 WAAPI 运行，原生栏色向 MainActivity 的 `animateSystemBarColors` 交付目标、过渡时长和曲线，ValueAnimator 在 UI 线程运行。联查取消／新目标／销毁的直接设置栏色、旧宿主回退和系统栏对比度。底栏文字和搜索按钮颜色随动效令牌过渡；手机主标签的下一次点击导航等待当前滑动结束，等待中的旧目标被较新请求替换，手势提交不等待自身；离场释放守卫和有界等待。收藏与搜索返回刷新复用sceneTasks，在路由落位后执行，取消隐藏页面待执行任务；入场动画仅在 animationstart 登记对象，离场完成已登记对象，缓存返回不扫描子树。视频底栏绝对定位并由 CczjMobileNavigation 的 ResizeObserver 发布实际高度；主标签场景始终预留该高度，次级页占满舞台，底栏挂载／卸载不再拉伸离场页。观察器随底栏卸载释放，保留舞台高度供缓存场景恢复。联查首次进入、缓存返回、短剧离场、拖动取消、关闭动效和页面加载失败；模拟器录屏只证明对应操作，不代替真机性能验收。

桌面并列路由按整页高度推移，播放场景独自 fixed 覆盖窗口；舞台／viewport 不建立阻挡该场景的堆叠上下文，不随播放进入退出扩张尺寸。整页使用 section 800ms，同步进入退出；设置分组使用 group 320ms，播放退出使用 dismiss 600ms，主题 reveal 1000ms 独立保留。联查底层虚拟网格、壁纸、侧栏、标题栏及全屏祖先规则。Settings 的 `animatedGroup` 只对实际分组变更设置，Skeleton 不额外播放内容入场，避免叠加与返回重播。

联查重点：设置入口可见性、包启停、参数与文案、关闭／减少动态效果、旧页面与目标页面并存、父级 `v-if` 是否提前销毁退出动画、预览是否错误启动业务工作、手势取消和重复操作后的收尾。改变路由动画时同时检查设置页与阅读／播放覆盖层的适用性；同一偏好不等于同一种动画。

Modal 退出由自身稳定过渡持有；ReaderSheet 则由 NovelBook、NovelReader、ComicReader 调用方的 `MotionTransition preset="sheet"` 持有。焦点／返回键所有权共用 [useOverlayFocus.ts](../frontend/src/composables/useOverlayFocus.ts)，修改覆盖层结构时同时检查该监听栈的注册、退出和清理。当前行为见[界面动效与设置](development.md#界面动效与设置)。

检索词：`useMotionStore`、`allows`、`MOTION_PACK_ID`、`--cczj-motion-`、`data-motion-`、`prefers-reduced-motion`、`MotionTransition`、`routeVisualMotionKey`、`animate(`、`requestAnimationFrame`、`@keyframes`。仅搜索组件名会遗漏 CSS 和直接控制的动画。

## 外观与布局

桌面窗口轮廓由 [appearance_windows.go](../app/window/appearance_windows.go) 的 InstallRoundedCorners 持有，通过 [app.go](../app.go) 装配；Windows 11 用 DWM，Windows 10 用 DPI 感知的窗口区域，句柄转交和失败释放由该模块负责。[App.vue](../frontend/src/App.vue) 仅绘制 8 DIP 高光，[TitleBar.vue](../frontend/src/components/TitleBar.vue) 使用根应用图标并同步最大化状态。联查调整尺寸、DPI、最大化、恢复、系统全屏与固定播放覆盖层；手机与非 Windows 壳层不调用 Windows 几何 API。旧系统的 API 回退与安装包最低运行要求分开维护，当前 Go/Wails/WebView2 组合不支持 Win8；兼容边界见开发指南。

| 关系 | 当前入口与所有者 |
| --- | --- |
| 主题选择、编辑与保存 | [SettingsThemeGroup.vue](../frontend/src/views/settings/SettingsThemeGroup.vue) → [ThemeEditorModal.vue](../frontend/src/views/settings/ThemeEditorModal.vue)／[stores/theme.ts](../frontend/src/stores/theme.ts) → [settingsQueue.ts](../frontend/src/api/settingsQueue.ts)；点击揭示由 [themeReveal.ts](../frontend/src/motion/themeReveal.ts) 持有临时过渡，选择卡片直接展示主题，编辑器实时作用于当前界面。编辑器即时预览、取消恢复、正式保存及失败重试需一起追踪；选择主题不插入瞬时保存文字，失败和重试保留。Android无ViewTransition时由themeReveal经api/themeReveal、WailsJSBridge与CczjThemeReveal完成原生快照圆形揭示与释放；扩散支持和减少动态效果独立于配色风格。 |
| 启动与宿主背景 | [main.ts](../frontend/src/main.ts)、[index.html](../frontend/index.html)、[bootAppearance.ts](../frontend/src/platform/bootAppearance.ts)、[backdropSystemBars.ts](../frontend/src/platform/backdropSystemBars.ts)、[readingSystemBars.ts](../frontend/src/platform/readingSystemBars.ts)；模式切换会准备下一模式的启动底色。 |
| 字号、字体、密度与内容布局 | [SettingsBasicGroup.vue](../frontend/src/views/settings/SettingsBasicGroup.vue)、[typography.ts](../frontend/src/platform/typography.ts)、[stores/layout.ts](../frontend/src/stores/layout.ts)；联查 [VideoCard.vue](../frontend/src/components/VideoCard.vue)、[VirtualVideoGrid.vue](../frontend/src/components/VirtualVideoGrid.vue)、[Home.vue](../frontend/src/views/Home.vue)、[Search.vue](../frontend/src/views/Search.vue)、[Favorites.vue](../frontend/src/views/Favorites.vue) 及短剧剧场。 |
| 公共视觉与语言 | [cczj-utilities.css](../frontend/src/styles/cczj-utilities.css)、[App.vue](../frontend/src/App.vue)、[公共 UI](../frontend/src/components/ui/)、[locales/index.ts](../frontend/src/locales/index.ts)、[zh-CN.ts](../frontend/src/locales/zh-CN.ts)、[en.ts](../frontend/src/locales/en.ts)；样式边界见 [CCZJ 样式复用](cczj-styles.md)。 |

`minimal` 预设使用独立黑白灰TINT_MINIMAL，应用背景、按钮与强调色均区别于绿色；仍保留旧自定义覆盖。紧凑回顶部复用MotionTransition slide-up，overlays关闭／减少动效时不播放，桌面禁用该额外过渡保留藤蔓；视频、小说／漫画页和目录共用。Android缺少ViewTransition时通过原生快照圆孔揭示，前后端身份握手、视觉提交确认、4秒超时与取消清理统一；MotionPanel只覆盖局部预览，无桥接才颜色渐变。移动壳层继续沿用既有画布、顶部渐变、控件、导航和壁纸遮罩，仅卡片适度混入画布色，壁纸默认78%不透明，自定义沿contentAlpha保留68%–94%边界。主题外观分组外层单独透明化，样本卡保留示例色，点击从卡片外缘揭示画布；不为此改变其他页面渐变。播放器及阅读表面保留独立归属。简约启动图明确映射grey并灰度呈现。

颜色计算由 [colors.ts](../frontend/src/utils/colors.ts) 提供有界 OKLCH 色阶、sRGB 输出和对比检查，规则与角色归属在 theme store。普通主题染色表面与独立简约预设、主要／次要操作、选择和选集状态分别定义；公共 Button／Tag／Modal、导航、Detail、PlayerInformationPanel 与播放选集抽屉消费相应角色。桌面黑底侧栏使用 viewing 角色，移动页内侧栏使用应用主题、媒体功能抽屉使用固定观看配色；画面控件、小说／漫画与短剧保持显式场景配色。`readableForeground` 为强调及状态文字按实际已知底色配对，透明表面不扫描壁纸。

编辑器的基础与高级配色共用 theme store，`roleColors` 和 `colorVersion:2` 沿既有 `theme_customs` JSON 保存／备份／恢复，没有新增 Key；加载只接受已知角色的有效色值。旧标签与选集底色仍可读，编辑时固定原选集颜色再分离，取消不回写；复制保持完整角色。预览保留原预设 tint 身份，临时身份不进入正式保存。生成默认保留手动颜色，显式替换与逐角色恢复有真实行为；联查预设／自定义／扩展包、图片取色、暗色切换、取消、保存失败及重试。btnMin／btnHide 保留窗口动作兼容角色。

图片主题的点击与拖放导入由 ThemeEditorModal 的同一入口持有，图片区声明 Wails `data-file-drop-target` 并停止 DOM drop 冒泡；扩展包监听只查询自身 `.ext-card`。[themeImage.ts](../frontend/src/utils/themeImage.ts) 持有可终止 worker，[themeImage.worker.ts](../frontend/src/utils/themeImage.worker.ts) 校验原图并做无损 PNG 裁剪（原文件／裁剪文件各 32 MiB、8000 万像素、30 秒超时），未裁剪时原始文件字节保持不变，不缩放或重编码。图片预览右上角提供编辑／裁剪／移除，加载与保存反馈复用 Spinner 和 Button。已删除数据库、压缩规则等界面说明及对应中英文键。[ThemeImageCrop.vue](../frontend/src/views/settings/ThemeImageCrop.vue) 使用独立 Modal，桌面左图右数值、窄屏上下排列；默认居中 16:9 选区，像素数值、框内拖动和八方向缩放共用边界约束。最小边以 128 原图像素和 88 显示像素共同约束，小图以自身边长为限；底层 worker 复核像素下限，不裁到图外、不缩放像素。长图按宽度显示并在左侧滚动，拖动临近边缘可滚动，选区随 ResizeObserver 更新，取消指针恢复本次拖动前位置，退出释放捕获与观察器。裁剪后 `backgroundOriginal` 保存原图，重新打开仍可重裁或还原。取消／关闭／卸载释放运行时资源，不删除正式保存的原图。取色清理非手动的高级角色覆盖，保留手动设置；保存失败保留预览和稳定身份。[themeWallpaper.ts](../frontend/src/platform/themeWallpaper.ts) 随 theme store 持有当前背景和一个候选背景：将 data URL 转成短 Blob URL，解码后才提交 CSS，避免 CSS 变量的长度上限使大图更新被拒绝。替换取消在途工作、拒绝迟到结果，失败保留旧背景并允许保存重试；清除／退出／HMR 撤销对象 URL，不将 Blob URL 写入数据库。取消编辑可直接复用仍在显示的原背景。保存等待背景准备完成再提交，临时预览卡片不进入正式卡片列表；浅／深色分组的预设和自定义卡片共用背景加载状态，失败时点击当前卡片重试。编辑复制不序列化整份图片。

主题 JSON 沿 `theme_customs` 保存到应用数据目录 `cczj_video.db`。[setting_codec.go](../app/db/setting_codec.go) 仅在单张原图或无损裁剪文件达到 1 MiB、且含版本头的压缩结果至少节省 5% 时，对主题 JSON 使用 GZIP 二进制封装；大小判断排除 data URL 头与 base64 膨胀，多张小图合计不触发。上限为解压后的 60 MiB。全部小于门槛或收益不足时保留原文，旧明文可直接读取，不做清库或全库迁移。读取检查 GZIP 完整性及展开上限；写入／批量写入／备份事务共用编码，GetSetting／备份快照／迁移归档读取共用解码。便携备份仍输出原 JSON，原图数据随备份往返；旧程序直接打开采用新封装的数据库不能解码，应使用便携备份交换。已被旧版本有损保存的数据无法补回细节，需要重新导入原文件。

开屏配色由 [splashAppearance.ts](../frontend/src/platform/splashAppearance.ts) 按主题身份／实际主色确定稳定资源键，自定义覆盖不再沿用原预设色。保存自定义主题写入 `splashKey`；正式切换依次保存 `theme_splash_v1`（主题 ID＋资源键）与 theme ID，元数据不匹配或损坏时在显示前按恢复的主题推导，不在动画中重新取色。预览和取消不提交开屏键；App 的启动底色缓存也跳过预览与待提交／失败写入。main.ts 允许主题恢复与挂载后的开屏并行，store 合并重复 load；[SplashScreen.vue](../frontend/src/components/SplashScreen.vue) 等恢复完成后一次选图，同时等待 themeWallpaper 准备并复用已解码的短 URL，正常离场等背景准备完成，失败可离场，总等待仍为 2200ms。图片、监听、计时器随开屏退出／卸载释放，壁纸解码只记录固定阶段与时长，不记录图片内容。小说／漫画保留模式图标；Android 原生启动记录同步允许 `frontend_wallpaper_decode`。无损存储增加大文件传输及解码内存，未承诺压缩率或设备耗时；桌面大 PNG、Android 低内存／切后台／返回、保存失败重试、备份恢复、裁剪还原与冷启动仍待用户验收。

联查重点：主题揭示的能力检测与中断、预览与保存失败、取消编辑、冷启动与切模式底色、系统栏、壁纸对比度、长文本与字号重排、列表高度与滚动恢复。小说／漫画有独立阅读偏好，不把应用主题与阅读背景、应用字号与正文排版混为一套设置。

检索词：`useThemeStore`、`useLayoutStore`、`applyFontSize`、`applyFontFamily`、`__preview__`、`bootBackground`、`useBackdropSystemBars`、`createReadingSystemBars`、`setLocale`。

## 设置入口与分组

- 视频设置入口在 [Settings.vue](../frontend/src/views/Settings.vue)，内置内容在 [views/settings](../frontend/src/views/settings/)；移动设置分组的路由处理还涉及 [settingsNavigation.ts](../frontend/src/mobile/settingsNavigation.ts)。小说／漫画分别由 [NovelSettings.vue](../frontend/src/novel/views/NovelSettings.vue)、[ComicSettings.vue](../frontend/src/comic/views/ComicSettings.vue) 提供偏好入口；阅读中的面板另见阅读小节。
- 动态设置来自 [pluginSettingsTabs 与 RESERVED_SETTINGS_IDS](../frontend/src/plugins/registry.ts)；内置日志、诊断和动效分组还要查 [app/plugin/builtin](../app/plugin/builtin/)。日志／诊断面板宿主由 [builtinPanels.ts](../frontend/src/plugins/builtinPanels.ts) 选择，不只检查桌面组件。
- 设置写入沿实际所有者追踪：主题使用 [settingsQueue.ts](../frontend/src/api/settingsQueue.ts)，动效有自己的串行保存和回退，阅读使用 [reading/persistence.ts](../frontend/src/reading/persistence.ts)。公共设置出口是 [facade_settings.go](../app/service/facade_settings.go) → [settings.Service](../app/settings/service.go)；部分 Key 还会触发模式切换或网络策略生效，不能把所有设置当作无副作用字符串写入。

手机设置索引保留分组搜索，桌面从分组导航直接开始；搜索、桌面分组滚动位置和切换方向由 Settings.vue 管理；组名／说明来自现有元数据，动态分组被移除时清理其位置记录。它不检索所有字段，扩展搜索范围时应继续使用实际设置定义。`styles/views/settings.css` 的桌面网格排除 `cczj-mobile-settings`，手机根节点独立使用全宽块布局，避免懒加载样式顺序将索引压入隐式网格列。

手机主题编辑入口在右侧并与 BackToTop 垂直错位；[CczjMobileEditActions.vue](../frontend/src/mobile/CczjMobileEditActions.vue) 的取消／完成也在左侧，共用于首页续播、视频历史／收藏、小说书架和漫画书库。搜索历史单独消费useHoldToEdit的dismissOnOutside/onEnter选项：长按仅当前气泡显示右上角绝对定位红底圆形删除按钮（18px圆底、10px图标、44px触摸区），点击直接删除，点外侧或Escape／返回收起；不装配确认／取消浮栏，不为角标预留文字或页面空间。其他编辑入口继续原行为。位置规则在 `styles/mobile/settings.css` 与 `mobile/route-motion.css`，联查页面场景的固定定位包含块、安全区及底栏占位。

联查重点：分组 ID、保留 ID、排序、入口文案、移动 query、保留挂载的面板、滚动位置、加载与保存状态、重试和重置。新增控件需追到实际生效处；调整偏好结构需反向寻找所有快捷入口。

检索词：`GROUPS`、`pluginSettingsTabs`、`RESERVED_SETTINGS_IDS`、`settingsTab`、`query.group`、`createSettingsQueue` 及具体 `CCZJ.Settings` 字段。

## 导航与模式

- 路由定义、模式守卫和返回滚动在 [router/index.ts](../frontend/src/router/index.ts)；模式识别在 [definitions.ts](../frontend/src/modes/definitions.ts)，切换入口及流程在 [ApplicationModeSwitch.vue](../frontend/src/modes/ApplicationModeSwitch.vue)、[applicationMode.ts](../frontend/src/modes/applicationMode.ts)。
- 壳层联查 [App.vue](../frontend/src/App.vue)、[VideoModeShell.vue](../frontend/src/modes/VideoModeShell.vue)、[DesktopVideoShell.vue](../frontend/src/modes/DesktopVideoShell.vue)、[CczjMobileShell.vue](../frontend/src/mobile/CczjMobileShell.vue)、[NovelShell.vue](../frontend/src/novel/NovelShell.vue)、[ComicShell.vue](../frontend/src/comic/ComicShell.vue)；侧栏／底栏在 [Sidebar.vue](../frontend/src/components/Sidebar.vue)、[CczjMobileNavigation.vue](../frontend/src/mobile/CczjMobileNavigation.vue)。
- 桌面与移动页面缓存、视觉层、预览与已提交状态由 [MobileRouteStage.vue](../frontend/src/mobile/MobileRouteStage.vue) 共用，关系判定在 [routeMotion.ts](../frontend/src/mobile/routeMotion.ts)；手势、快照和滚动联查 [useTabSwipe.ts](../frontend/src/mobile/useTabSwipe.ts)、[modeSessions.ts](../frontend/src/modes/modeSessions.ts)、[sessionRuntime.ts](../frontend/src/modes/sessionRuntime.ts)、[restoreScroll.ts](../frontend/src/modes/restoreScroll.ts)。后端模式任务在 [application_mode.go](../app/service/application_mode.go)，应用退出顺序在 [lifecycle.go](../app/service/lifecycle.go)。

联查重点：直接链接、旧别名、模式恢复、设置子分组、返回栈、预览与已提交页面、切模式后的后台任务和下载。不能只改路由表而遗漏底栏、主标签集合 `primaryTabs`、快照身份和业务路径构造函数。

路由场景的实体背景还关联桌面壁纸裁切和窗口全屏：DesktopVideoShell 启用舞台的 `wallpaper` 呈现，`route-motion.css` 持有场景画布几何，[player.css](../frontend/src/styles/components/player.css) 同步全屏祖先的尺寸和包含规则。调整容器、transform 或 contain 时同时检查这些消费者；几何观察与帧回调归舞台释放。

首页首屏完成后的详情模块预载复用 [videoViews.ts](../frontend/src/modes/videoViews.ts) 与路由的同一个加载入口，不发起业务或媒体请求；Home 可见性与卸载取消空闲调度。诊断启动快照的 ui_build 与版本号一起核对实际运行构建，不能只按 2.5.0 判断安装内容。

检索词：实际路由字符串、`routeMode`、`modeHome`、`routeScreenKey`、`routeTransition`、`primaryTabs`、`modeSessions`、`routeWorkActivityKey`、`getDetailPath`、`getPlayerPath`。

## 页面活动与异步工作

- 视图活跃范围在 [useViewActivity.ts](../frontend/src/composables/useViewActivity.ts)，请求与迟到结果在 [requestScope.ts](../frontend/src/api/requestScope.ts)、[useGeneration.ts](../frontend/src/composables/useGeneration.ts)；事件订阅／合并在 [events.ts](../frontend/src/api/events.ts)、[eventPipeline.ts](../frontend/src/api/eventPipeline.ts)，正常解绑由订阅所有者调用，入口负责幂等、初始化失败及 HMR 清理。可见预览与已提交页面的工作权限不同；多消费者共享监听不属于首个组件。
- 前端请求继续查 [requestChain.ts](../frontend/src/api/requestChain.ts)、[requestPipelines.ts](../frontend/src/api/requestPipelines.ts)、[requestPolicies.json](../frontend/src/api/requestPolicies.json)、[network/requestPolicy.ts](../frontend/src/network/requestPolicy.ts)；后端由 [requests.go](../app/service/requests.go)、[operation](../app/operation/)、[lifecycle](../app/lifecycle/) 接到领域 I/O。宿主长请求使用 `runHostRequest`，不是所有请求都属于当前模式。
- 图片准备还涉及 [RemoteImage.vue](../frontend/src/components/RemoteImage.vue)、[imageVisibility.ts](../frontend/src/utils/imageVisibility.ts)；媒体、轮播、轮询和诊断具有各自所有者。后台并发继续查 [parallel](../app/parallel/)、[workflight](../app/workflight/)、[netbudget](../app/netbudget/)。

联查重点：创建、激活、停用、后台、销毁和 HMR；共享请求最后订阅者退出后的取消；停止定时器、帧、Observer 和事件订阅；清空后迟到写回。沿创建者和释放函数追踪，不能仅凭存在 `onUnmounted` 判定整个能力已释放。

回顶部的 compact 呈现不读取桌面藤蔓高度，也不注册只用于藤蔓的 resize 监听；桌面非 compact 保留测量、用户打断滚动及动画释放。相关消费者为 VideoModeShell、NovelShell、ComicSeries 及阅读目录面板中的 BackToTop。

检索词：`useViewActivity`、`committed`、`createRequestScope`、`onBackendEvent`、`onActivated`、`onDeactivated`、`onScopeDispose`、具体事件名、`runHostRequest`、`runRequest`。详细语义见[请求通道](request-pipelines.md)。

## 视频目录详情与搜索

- 列表入口在 [Home.vue](../frontend/src/views/Home.vue)、[Search.vue](../frontend/src/views/Search.vue)、[Recent.vue](../frontend/src/views/Recent.vue)，共享 [useVideoList.ts](../frontend/src/composables/useVideoList.ts)、[stores/video.ts](../frontend/src/stores/video.ts)、[VideoCard.vue](../frontend/src/components/VideoCard.vue) 与虚拟网格；[ShortDramaTheatre.vue](../frontend/src/player/shortDrama/ShortDramaTheatre.vue) 也是列表消费者。
- [VirtualGrid.vue](../frontend/src/components/VirtualGrid.vue) 拥有可见行、缓冲行、行高测量、焦点保留、跨虚拟边界 Tab、当前项定位和滚动恢复；[VirtualVideoGrid.vue](../frontend/src/components/VirtualVideoGrid.vue) 保留视频卡片适配，详情、播放侧栏与播放抽屉直接消费同一网格。滚动和 ResizeObserver 合并到单帧测量，隐藏／销毁解绑；长标题和字号变化按实测行高更新。网格通过 CSS 变量发布当前实测行高与行间距；详情选集按内容自然收缩、至少一行高度、最多三行后独立滚动，不为少量集数保留固定大空白。短剧选集已有每组20集的边界，保持分组语义。
- 详情由 [Detail.vue](../frontend/src/views/Detail.vue)、[api/video.ts](../frontend/src/api/video.ts) → [facade_video.go](../app/service/facade_video.go) → [media](../app/media/)／[detail.Service](../app/detail/service.go) 获取；播放器和短剧也消费详情与线路，不能只检查详情页。
- Search 的 `toggleSourceSearchMode` 同时取消旧请求、清空两种结果及完成标记；已有搜索词和已执行记录时立即通过 `doSearch` 执行目标模式。空提示分别受模式、完成、加载及错误状态约束，请求失败提供原模式重试。切源、改词、离场及返回仍由既有请求身份和页面活动管理。
- 来源与分类筛选联查 [stores/source.ts](../frontend/src/stores/source.ts)、[Sources.vue](../frontend/src/views/Sources.vue)、[VideoTypes.vue](../frontend/src/views/VideoTypes.vue)；榜单、相似推荐、评论和补全继续查 [facade_douban.go](../app/service/facade_douban.go)、[douban](../app/douban/)。删除／回收还涉及 [RecycleBin.vue](../frontend/src/views/RecycleBin.vue)；身份人工合并入口在 [VideoDiagnosticsPanel.vue](../frontend/src/components/VideoDiagnosticsPanel.vue)，需沿 `MergeGlobalVideoIdentities` 检查目录、收藏、历史引用及失效通知。

XML 详情兼容由 [FetchVideoDetailWithStrategy](../app/collect/fetcher.go) 拥有：标准 CMS 的 `ac=detail` 返回 XML 元数据且没有目标播放地址时，使用原 ID 参数和额外参数补一次 `ac=videolist`，仍严格核对返回身份；JSON、声明式响应和自定义动作保持原契约。普通详情、短剧、播放及线路探测经同一入口。

联查重点：来源身份、分类映射、分页游标、刷新与旧结果回写、详情到播放／下载、删除后的已打开列表、收藏／历史里的引用。榜单原始正文和基于本地目录的派生匹配缓存不是同一份数据。

检索词：`GetVideoList`、`GetVideoDetail`、`FindSourcesByGlobalId`、`short_drama`、`DeleteVideo`、`source_key`、`global_id`、`PublishCatalogChange`。

## 播放器与播放偏好

移动媒体功能面板由 `CczjMobilePlayerChrome → CczjMobilePlayerDrawer → mobile/player/presentation.css` 共用固定深色配色；倍速、线路、选集、投屏与更多设置不继承应用主题。面板的背景／文字／次要按钮／选中／进度角色在局部作用域定义，底部和全屏侧向布局共用，页内简介与侧栏仍属于应用主题。选集百分比绝对定位右上角，按钮对称内边距、内容居中，不为百分比预留排版空间；保留底部进度条和至少44px触摸目标，排序与关闭使用紧凑文字／图标。抽屉遮罩点击停止冒泡，close恢复控制栏；直接点击视频时优先关闭已打开面板并取消待处理单击，之后新的点击才切换控制栏。取消、系统返回、倍速选择和换集共用close；原焦点归还与控件自动隐藏计时保留。


- HLS 分片补货由 [prefetch.ts](../frontend/src/utils/ts-cache/prefetch.ts) 持有连续未播放库存／冗余和最多三路预取，播放器协助另计；[transfer.ts](../frontend/src/utils/ts-cache/transfer.ts) 统一资源／字节区间所有权、消费者、真实请求次数与限频重试。联查普通播放、短剧、详情／下一项预热、缩略预览、暂停／拖动／清晰度切换、缓存失效与退出释放。坏片提示经 [playbackSource.ts](../frontend/src/player/hls/playbackSource.ts) 的依赖核对到 [VideoPlayer.vue](../frontend/src/components/VideoPlayer.vue)；至少五次实际重试、明确阻塞和后续下载证据同时满足才允许用户选择跳过。统计与诊断复用 `usePlaybackTelemetry` 和既有播放器快照；其中 `cachedPlaybackRanges` 按当前清单时间索引匹配完整内存分片，`player/mediaRanges.ts` 合并实际缓冲和缓存时间区间供 VideoPlayer／PortraitPlayerControls 的轨道显示，保留缺口，useVideoInfo 的可播秒数只取媒体元素当前位置连续缓冲；重载清空旧索引，投屏不沿用手机缓冲。原生 HLS 不宣称启用 JS 调度。当前参数、预算和设备验收边界见[视频播放与来源反馈](development.md#视频播放与来源反馈)。

- HLS 进度回调的数据可被 worker 同步转移；[loader.ts](../frontend/src/utils/ts-cache/loader.ts) 必须先拥有缓存副本再转发，联查内存缓存、磁盘写入、非流式与字节区间响应和缩略预览。命中验证还涉及 [memory-cache.ts](../frontend/src/utils/ts-cache/memory-cache.ts) 的读取／预取判定；缓存有意跨页面保留，坏条目必须逐项失效并回退拉取，不能仅修新写入而让重新进入继续挂起。详情集数的已看色／底部进度由 [detail.css](../frontend/src/styles/views/detail.css) 维护，不依赖 hover 才可见。桌面播放场景独自覆盖整窗、保留底层侧栏与缓存页面原尺寸的装配由 [DesktopVideoShell.vue](../frontend/src/modes/DesktopVideoShell.vue) 拥有，系统栏进度的行程由实际进入动画决定，次级返回只有四分之一屏；页面展开与收拢见 [routeMotion.ts](../frontend/src/mobile/routeMotion.ts)／[MobileRouteStage.vue](../frontend/src/mobile/MobileRouteStage.vue)；移动端保留自己的路由策略。

- 播放退出还联查 [releaseAnimations.ts](../frontend/src/player/releaseAnimations.ts) 与公共 animations.css 的根释放标记，覆盖子树和伪元素，防止重置进度时重新启动动效。Home 的 `scheduleHomeRefresh` 与 Detail 的返回进度刷新共用 [sceneTasks.ts](../frontend/src/motion/sceneTasks.ts)：消费舞台通过 `sceneMotionKey` 发布的进入／离开／取消及手势状态，结束后合并刷新，兜底等待最多2秒，不查询同级 DOM 动画；停用、再次调度与销毁取消计时器、帧及等待，检查当前路由与迟到结果。
- Detail 的进度、可见性和返回事件合并到 `scheduleDetailRefresh`；只读取当前作品的进度，百分比未变化时保留引用，不在读取路径强制写存储。历史读取共享在途请求，推荐及关联来源在过渡结束后再读取；停用／换作品／销毁取消视图请求，代次拦截迟到结果，取消不报请求失败；移除定时补刷。全屏期间内嵌 WebView 保持可见并位于不透明 custom view 后面，不在退出时重建其显示面；MainActivity 保存窗口旋转策略，进出时使用 jumpcut 避免系统额外缩放／黑帧动画，原生恢复确认后还原原策略。全屏原生请求在 `api/mobilePlayer.ts` 持有进入／全屏／退出／关闭状态，重复 release 返回同一个完成承诺，退出期间忽略迟到 entered；平台回滚等待同一身份的原生方向／布局确认后再退出浏览器，不能释放新请求。
- 换源接力同时检查 `Player.loadFromSource` → `VideoPlayer.continuation` → `usePlaybackResume`：已验证的来源替换由 MobileRouteStage 的 `secondaryInstanceKey` 保留页面现场；身份仍按真实来源／作品／集号更新。旧源保存完成后提交新详情，新媒体定位后的有效进度由 `pendingSourceHistory` 强制写入目标来源，失败反馈且限频重试；历史最新来源影响首页继续观看、详情与历史页，不删除旧源记录。片头或剪辑偏移通过现有时间输入人工校准。
- 桌面右栏 [PlayerInformationPanel.vue](../frontend/src/player/views/PlayerInformationPanel.vue) 的简介、选集、播放源依次占可用高度的 27%／45%／28%，分区不随异步来源、线路和集数增减改变。`player-information.css` 用零最小尺寸 Grid 分配高度；`side-source-scroll` 和 `side-episode-scroll` 独立纵向滚动，分区标题、排序与探测操作保留在滚动区外。简介超出固定区域时内部滚动；窗口缩放按同一比例重算，折叠仍由原面板控制。完整列表不设置展开状态，移动分组和抽屉不采用此桌面分区。媒体全屏状态由 VideoPlayer 暴露给 PlayerPanelToggle；全屏隐藏右栏的选择器覆盖其 scoped Grid 布局，同时隐藏面板开关，退出后沿用原折叠状态；顶部拖拽区拦截 click／dblclick 冒泡。应用 [Sidebar.vue](../frontend/src/components/Sidebar.vue) 的导航滚动容器与固定来源底栏分开。
- 普通播放和短剧共享 [useDesktopPlaybackKeys.ts](../frontend/src/player/page/useDesktopPlaybackKeys.ts) 的文档监听与输入作用域；[usePlayerChrome.ts](../frontend/src/player/usePlayerChrome.ts) 拥有自动隐藏计时器。短剧外层全屏宿主的 pointermove 在接近底部时恢复底栏，宿主变更／卸载解绑；暂停、底栏悬停和时间输入保持可见，桌面选集只在底栏提供一处，移动保留简介旁入口。联查 PortraitPlayerControls 的 Teleport 宿主、指针穿透和 PlayerDesktopControls 的 engaged。
- 页面编排在 [Player.vue](../frontend/src/views/Player.vue)，媒体组件在 [VideoPlayer.vue](../frontend/src/components/VideoPlayer.vue)；会话与控制联查 [playbackSession.ts](../frontend/src/player/playbackSession.ts)、[playbackControl.ts](../frontend/src/player/playbackControl.ts)、[preparation.ts](../frontend/src/player/preparation.ts)、[hls](../frontend/src/player/hls/)。[ShortDrama.vue](../frontend/src/views/ShortDrama.vue) 是共享播放器的另一业务入口。
- 详情 `Detail.loading` 从挂载前即成立，防止共享 video store 的旧条目先露出再被加载替换。手机次级 KeepAlive 上限为一个页面并包含 Detail，返回复用大集表和滚动位置；Detail 消费 committed 活动状态，隐藏时不刷新进度，激活时经 video store 的 `hasCurrentDetail` 校验来源／作品身份，必要时恢复原路由数据，并刷新历史／收藏。详情 `.episodes-grid` 与手机播放 `.side-episode-scroll` 持有有界滚动视口；标题、排序保留在外。日期集的紧凑布局由 EpisodeLabel 统一，联查详情、播放侧栏和手机抽屉。手机 PlayerInformationPanel 仅在有效豆瓣 ID 存在时装配评论页签，并处理 ID 消失回到简介。
- 普通手机 VideoPlayer 的暂停续播入口在右下角，小屏／全屏图标为 40px／64px；共享 ResizeObserver 读取控制栈实际高度，经 `--cczj-player-control-height` 避让，宿主卸载时一并释放。图标位移消费覆盖动效的进入／退出曲线和关闭规则。独立 mini-progress 只在播放、控制栏隐藏且未锁定时出现，不截获拖动。联查 CczjMobilePlayerControls、fullscreen 控件换行、暂停／恢复、投屏及短剧独立控件。
- 集数状态同时关联详情 [detail.css](../frontend/src/styles/views/detail.css)、桌面黑底面板 [player-information.css](../frontend/src/styles/components/player-information.css)、移动侧面板 [media.css](../frontend/src/styles/mobile/media.css) 与 Player 的选集抽屉；手机颜色在 [presentation.css](../frontend/src/mobile/player/presentation.css) 的主题根解析，避免抽屉局部调色板覆盖。详情及移动已播填充使用主题实色／反色进度，桌面黑底单独适配；进度数据仍共用历史及 `getEpWatchPct`，不能按样式另存一套已播状态。
- HLS 版本在 [package.json](../frontend/package.json) 与锁文件精确固定；主播放 [playbackSource.ts](../frontend/src/player/hls/playbackSource.ts)、预览 [useThumbnailScrubber.ts](../frontend/src/player/useThumbnailScrubber.ts) 共用 [worker.ts](../frontend/src/player/hls/worker.ts) 和 [ts-cache/loader.ts](../frontend/src/utils/ts-cache/loader.ts)。升级需同步 loader 配置／回调、缓存与字节区间、主播放优先、取消和 worker 构建产物，不使用独立 CDN worker 或另一版本的 loader。
- 偏好由 [usePlaybackPreferences.ts](../frontend/src/player/usePlaybackPreferences.ts)、[settings.ts](../frontend/src/player/settings.ts) 管理；桌面控件在 [player/views](../frontend/src/player/views/)，手机控件在 [CczjMobilePlayerChrome.vue](../frontend/src/mobile/CczjMobilePlayerChrome.vue)、[CczjMobilePlayerControls.vue](../frontend/src/mobile/CczjMobilePlayerControls.vue)、[CczjMobilePlayerDrawer.vue](../frontend/src/mobile/CczjMobilePlayerDrawer.vue)。改变设置项还需检查两端显示值和快捷键／手势入口。
- 全屏、画中画、音量、亮度和投屏在 [player/platform](../frontend/src/player/platform/)、[platform/fullscreen.ts](../frontend/src/platform/fullscreen.ts)、[api/mobilePlayer.ts](../frontend/src/api/mobilePlayer.ts)；移动视频尺寸由 [videoLayout.ts](../frontend/src/utils/videoLayout.ts) 用容器单位随宿主布局同步变化，旧 WebView 保留像素回退；增强画布继续跟随同一视频几何。原生退出确认联查 `prepareDeviceFullscreen.release`、`MainActivity.finishFullscreenExit` 和 `fullscreenPhase`，包含按钮、系统返回、超时、销毁和新所有者。增强处理在 [enhancementPipeline.ts](../frontend/src/player/enhancementPipeline.ts)，字幕在 [useSubtitles.ts](../frontend/src/player/useSubtitles.ts)。弹幕样式由 `player/danmaku/textStyle.ts` 统一测量／绘制和预览，`typeface.ts` 文档持有官方思源黑体按需加载与HMR撤销；`preferenceStore.ts` 共享偏好与settingsQueue，联查PlayerDanmaku面板、DanmakuCanvas、timeline及DanmakuSources的freshDays提交，保留原Key与旧值、失败脏状态。滚动速度按视口归一且同屏有界，字号连续缩放，顶部／滚动／底部共同消费显示区域与避让；手机竖屏、桌面与原生全屏复用，短剧未传danmaku-key时不装配弹幕。字体来源、授权、APK静态证据及设备验收边界见[弹幕排版与设置](cczj-styles.md#弹幕排版与设置)。弹幕涉及 [player/danmaku](../frontend/src/player/danmaku/)、[DanmakuSources.vue](../frontend/src/views/DanmakuSources.vue)、[api/danmaku.ts](../frontend/src/api/danmaku.ts) 与 [app/danmaku](../app/danmaku/)。
- DLNA 会话由 [cast.Service](../app/cast/service.go) 持有目标媒体和设备身份；[discovery.go](../app/cast/discovery.go) 刷新设备时保留会话，[status.go](../app/cast/status.go) 通过 TrackURI／CurrentURI 返回 `media_state`。传输出口在 [service/cast.go](../app/service/cast.go)，手机 [CczjMobileCast.vue](../frontend/src/mobile/CczjMobileCast.vue) 统一持有 [CczjMobileCastRemote.vue](../frontend/src/mobile/CczjMobileCastRemote.vue) 的 compact／page／fullscreen 呈现，内嵌视图 Teleport 到播放器，展开复用 CczjMobilePlayerDrawer 的 expanded 布局和 header 插槽；原生全屏确认后收起选择面板，退出／系统返回交给既有 fullscreen 控制器且保留投屏。息屏／后台停止前端状态查询，Go 局域网媒体转发与 Android connectedDevice 前台服务继续执行，正常后台返回保留遥控；强制关闭后冷启动不自动重建遥控，需重新选择设备。联查横屏短视口的纵向层次、标题栏命中区域、本地锁屏／续播／增强分割线的避让；经 CczjMobilePlayerChrome 的 connected／stopped／status 连接 VideoPlayer 的本地暂停保护、HLS suspend、电视进度和历史保存。Android 发现通过 `platform.cast.interfaces → readCastInterfaces → WailsJSBridge.playerCastInterfaces → CczjPlayerDevice.castInterfaces` 获取 ConnectivityManager／LinkProperties 的 Wi-Fi／以太网 IPv4 与索引，校验后进入 `DiscoverCastDevicesOnInterfaces`；Go 绑定本地地址并设置多播接口，不依赖 Android 受限的 netlink 接口枚举。联查原生网络信息失败、旧宿主缺桥接回退、组播锁释放、取消、关闭及再次搜索；桌面和旧宿主保留 `DiscoverCastDevices`。会话还需联查旧媒体状态、有效时钟推进的自动确认、无有效进度（含地址已匹配）的用户主动确认、重新发送／更换设备／系统镜像入口、关闭面板、切后台、停止与再次进入；`matched` 仅表示地址归属，不单凭 PLAYING／地址回显自动交接；相邻时钟样本随重发、跳转、查询失败和前后台切换重置。未确认和 `other` 不写入本片进度，自动续播 Seek 等待本次媒体时钟推进的 seek_ready，手动确认保持从头播放；查询失败不恢复本地视频；停止失败后的显式断开释放手机保护，不冒称电视已经停止。[readiness.go](../app/cast/readiness.go) 按设备串行发送／控制并按 session 跟踪起播证据，发现刷新保留状态；Seek 前重新核对本次媒体、有效时钟和实际推进，拒绝时不发送 SOAP Seek。Play 接受、Seek 目标及 seek_ready 进入原操作日志。SOAP 402／714 元数据回退、Play 701 有界重试及暂停 Seek 501／701 的恢复播放／跳转／恢复暂停由后端拥有，不在视图另建重试队列。[CastRelay](../app/proxy/cast_relay.go) 复用手机 [HLSService](../app/proxy/hls.go) 的客户端／Cookie／请求头，将子列表、分片、密钥和初始化段改写到受会话及接收端 IP 约束的局域网地址，保留 Range／HEAD，TrackURI 通过实际登记资源核对归属；[app.go](../app/service/app.go) 装配，最后一个会话经 [cast_host_android.go](../app/service/cast_host_android.go) 停止原生前台服务与 CPU／Wi-Fi 锁，[lifecycle.go](../app/service/lifecycle.go) 关停等待。发现保存实际接收接口用于绑定，重发／停止／过期撤销资源；源站检查、电视首个请求／字节与有界失败日志覆盖实际取流，原始电视状态与 relay_health／confirmation_state／position_state 分开，seek_ready 由后端统一判断；45秒无媒体阶段超时由Go拥有，120秒仍无媒体撤销，显式暂停挂起起播期限；媒体推进清理可恢复历史故障，首字节与完成分别记录。PlayCastVideo／PlayCastVideoWithOptions 返回 request_id／cast_session_id；选项按会话选择手机转发或电视直连，直连标为 unobservable；默认从头发送，可选择下次续播。[cast_probe.go](../app/proxy/cast_probe.go) 在转发45秒仍无媒体且未暂停时抽样检查一条HLS链（最多6次、8秒、约770KiB），源检查与电视请求分别记录，不能据此认定接收端兼容；ReleaseCastSession 按会话撤销；切模式由 application_mode.go 释放会话。诊断导出联查 diagnostics_export.go／diagnosticrecording/timeline.go 的固定字段及 cast-native 原生证据。联查手机本地／iOS 共享 HLS、后台、原生权限、失败重试与应用退出。未新增转码或私有 SDK；根因日志、协议边界及第三方 REA 依据见 [Android 投屏说明](android.md#投屏发现与接收协议)。

联查重点：新旧媒体会话交接、缓冲与暂停、换源同集确认、续播与进度落盘、设备亮度恢复、全屏返回、字幕对象 URL、Canvas／GPU／弹幕资源释放。修改倍速、画面适配、自动连播或快捷键时，联查普通播放与短剧，不能只改一份控制条。

检索词：`pendingSourceHistory`、`secondaryInstanceKey`、`useDesktopPlaybackKeys`、`controlsVisible`、`createPlayerSettings`、`usePlaybackPreferences`、`PlaybackCheckpoint`、`createPlayerPlatform`、`usePlayerShortcuts`、`useMobilePlayerGestures`、`playerHoldSpeed`、`playerPictureFit`、`useDanmaku`。

## 短剧

- 入口 [ShortDrama.vue](../frontend/src/views/ShortDrama.vue) 按平台和 query 选择推荐流、剧场或单剧；[scene.ts](../frontend/src/player/shortDrama/scene.ts)、[session.ts](../frontend/src/player/shortDrama/session.ts) 与 [useShortDramaFeed.ts](../frontend/src/player/shortDrama/useShortDramaFeed.ts) 管理场景、候选和当前剧集。[ShortDramaTheatre.vue](../frontend/src/player/shortDrama/ShortDramaTheatre.vue)、[ShortDramaCatalog.vue](../frontend/src/player/shortDrama/ShortDramaCatalog.vue) 提供列表入口。
- 翻页在 [useShortDramaPaging.ts](../frontend/src/player/shortDrama/useShortDramaPaging.ts)，推荐排除及恢复在 [recommendationPreferences.ts](../frontend/src/player/shortDrama/recommendationPreferences.ts)；共享播放器、历史、收藏与分片缓存见相应小节。后端分类和推荐继续查 [app/media/short_drama.go](../app/media/short_drama.go)。
- 桌面观看布局由 ShortDrama 的 `drama-rail`／`drama-info` 和 [PortraitPlayerControls.vue](../frontend/src/player/views/PortraitPlayerControls.vue) 共同呈现；简介先与剧名规范化去重，再在原页展开或复用 CczjMobilePlayerDrawer，选集入口仍在底栏。`introExpanded`／drawer 与 VideoPlayer 的 `portraitControlsPinned` 连接显隐；展开、换剧、离场、滚轮与快捷键作用域一起检查。底栏隐藏保留布局占位及作品信息，鼠标接近底部才恢复；右栏和页头独立消费观看状态、hover／focus。普通播放继续使用自己的 mousemove 恢复规则，PlayerDesktopControls 的悬停与时间输入共用 engaged 状态。
- `paintBackdrop` 从同一媒体按最长边 160px 获取完整画面，复用有效进度／media-ready 回调并限制每 400ms 一次；文档隐藏、离场与不可见不绘制，换媒体重置画布及失败状态，绘制失败保留静态深色背景。不新增解码器、逐帧轮询、图像导出或媒体请求。桌面底栏静音入口复用既有 commands.mute；移动端保留原时间与全屏触摸区，完整 9:16 和播放身份不因背景或控件布局改变。
- 默认源与分类链：source store 的 currentSourceKey → api/video → GetShortDramaPage，首轮获取真实 list 类型表；[cms_xml.go](../app/collect/cms_xml.go) 保留类型元数据，[category_parents.go](../app/collect/category_parents.go) 针对已确认来源补齐官方层级，[source_categories.go](../app/db/source_categories.go) 管理 SQLite 层级与范围。剧场、推荐、父级禁用、远端准入与本地可见性共用自动范围，不按标题推断，不提供子分类界面。
- [short_drama_cursor.go](../app/media/short_drama_cursor.go) 拥有有界分类分页游标；联查 GetShortDramaPage、useVideoList、useShortDramaFeed 的续传、去重、筛选、取消与旧游标失效。来源 ID、接口、筛选及分类范围均参与身份，不能跨来源或改变筛选后继续旧分页。
- 缓存与随机：剧场经 useVideoList 先读目录再静默补充，推荐经 useShortDramaFeed 先调用 GetShortDramaRecommendations；远端补目录后重新经过同一内置采样器，不直接以源站第一页作为推荐。SHORT_DRAMA_FRESH_MS 共用刷新间隔，GetShortDramaPage 的 refresh 请求策略合并在途工作；静默失败保留内容，换源、离场及销毁停止提交和预取。旧手动分类绑定、字段及迁移 26 保留数据兼容，不再影响自动范围；源导入导出仍携带已知父级。
- 桌面 KeepAlive 保留 ShortDrama 剧场，离场仍停止媒体和请求；骨架与真实网格共用 `--theatre-columns`、间距和海报比例，公共 Skeleton 使用 `--cczj-skeleton-columns` 适配。`remoteEmpty` 仅表示无筛选的已识别分类返回空；与关键词无匹配、分类禁用和请求失败分别反馈，不把没有缓存等同于需要先采集。

联查重点：`view=feed`／`view=theatre`、平台默认场景、切单剧再返回、下一项预备、屏蔽后已预备结果、翻页取消、快照释放与首帧准备。推荐偏好、来源分类和全局当前源是不同状态，不能通过修改另一状态间接“修好”当前列表。

检索词：`shortDramaScene`、`useShortDramaSession`、`useShortDramaFeed`、`useShortDramaPaging`、`short_drama`、`GetShortDramaPage`。

## 小说漫画与阅读共享能力

- 大 JSON 原文与 `.gz` 副本由 [compress-embedded-json.mjs](../scripts/compress-embedded-json.mjs) 同步，调用方在 [source_catalog.go](../app/library/source_catalog.go)、[quality/corpus.go](../app/library/quality/corpus.go) 和 [source_maintenance.go](../app/library/source_maintenance.go)；按需解压复用 [payload/decode.go](../app/payload/decode.go)。联查社区源预览、质量检测、拼音检索的已有语料消费者及 Windows／Android 构建、静态检查，禁止原文与 gzip 同时嵌入或全量解压常驻。

| 关系 | 当前入口与所有者 |
| --- | --- |
| 小说发现、详情与数据 | [NovelHome.vue](../frontend/src/novel/views/NovelHome.vue)、[NovelCatalog.vue](../frontend/src/novel/views/NovelCatalog.vue)、[NovelSearch.vue](../frontend/src/novel/views/NovelSearch.vue)、[NovelBook.vue](../frontend/src/novel/views/NovelBook.vue) → [novelLibrary.ts](../frontend/src/novel/data/novelLibrary.ts)、[api/novelBrowse.ts](../frontend/src/api/novelBrowse.ts)、[api/library.ts](../frontend/src/api/library.ts)。 |
| 漫画发现、详情与数据 | [ComicHome.vue](../frontend/src/comic/views/ComicHome.vue)、[ComicCatalog.vue](../frontend/src/comic/views/ComicCatalog.vue)、[ComicSeries.vue](../frontend/src/comic/views/ComicSeries.vue) → [repository.ts](../frontend/src/comic/data/repository.ts)、[api/comicBrowse.ts](../frontend/src/api/comicBrowse.ts)、api/library；[ComicTrending.vue](../frontend/src/comic/components/ComicTrending.vue) 是另一内容入口。 |
| 小说阅读与偏好 | [NovelReader.vue](../frontend/src/novel/views/NovelReader.vue)、[NovelSettings.vue](../frontend/src/novel/views/NovelSettings.vue)、[NovelReaderPreferences.vue](../frontend/src/novel/components/NovelReaderPreferences.vue)；[novel/reader](../frontend/src/novel/reader/) 持有会话、排版、分页、翻页和进度，[readingState.ts](../frontend/src/novel/readingState.ts) 持有书架、进度和偏好。 |
| 漫画阅读与偏好 | [ComicReader.vue](../frontend/src/comic/views/ComicReader.vue)、[ComicSettings.vue](../frontend/src/comic/views/ComicSettings.vue)、[ComicReaderPreferences.vue](../frontend/src/comic/components/ComicReaderPreferences.vue)；[comic/reader](../frontend/src/comic/reader/) 持有会话、页面和进度，[state.ts](../frontend/src/comic/state.ts) 持有追漫、进度和偏好。 |
| 目录、换源与预取 | [ChapterTools.vue](../frontend/src/reading/ChapterTools.vue)、[ReaderSheet.vue](../frontend/src/reading/ReaderSheet.vue)、[useCatalog.ts](../frontend/src/reading/useCatalog.ts)、[useSourceSwitch.ts](../frontend/src/reading/useSourceSwitch.ts)、[api/readingSources.ts](../frontend/src/api/readingSources.ts)、[chapterPrefetch.ts](../frontend/src/reading/chapterPrefetch.ts)、[contentCache.ts](../frontend/src/reading/contentCache.ts)。换源面板分别在 [NovelSourceSwitch.vue](../frontend/src/novel/components/NovelSourceSwitch.vue)、[ComicSourceSwitch.vue](../frontend/src/comic/components/ComicSourceSwitch.vue)。 |
| 输入、亮度与持久化 | [ReaderInputPreferences.vue](../frontend/src/reading/ReaderInputPreferences.vue)、[inputPreferences.ts](../frontend/src/reading/inputPreferences.ts)、[useReadingBrightness.ts](../frontend/src/reading/useReadingBrightness.ts)、[reading/persistence.ts](../frontend/src/reading/persistence.ts) → [facade_reading_state.go](../app/service/facade_reading_state.go) → [readingstate](../app/readingstate/)／[db/reading_state.go](../app/db/reading_state.go)。 |
| 内容与图片出口 | [facade_library.go](../app/service/facade_library.go)、[facade_reading_sources.go](../app/service/facade_reading_sources.go) → [library](../app/library/) → [provider](../app/provider/)；漫画图片还涉及 [proxy/image_context.go](../app/proxy/image_context.go)、[proxy/imgproxy.go](../app/proxy/imgproxy.go)。 |

联查重点：设置页预览与阅读中设置是否同步、排版重算后可见文字恢复、章／话跳转和翻页交接、相邻内容准备、目录刷新和缓存、失败换源、亮度所有权、退出保存。书架与追漫入口分别在 [NovelShelf.vue](../frontend/src/novel/views/NovelShelf.vue)、[ComicLibrary.vue](../frontend/src/comic/views/ComicLibrary.vue)。章节文本、漫画图片和页码定位具有不同语义，联查共享规则不意味着合并两种阅读器。

检索词：`useNovelReading`、`useComicState`、`readingPersistence`、`readingRecord`、`refKey`、`LibraryNovelContext`、`LibraryComicContext`、`LibraryRefreshChapterText`、`LibraryChapterPages`、`useNovelPageTurn`、`systemBrightness`。身份匹配与来源供给另见[阅读设计](reading-source-routing-design.md)及[实现说明](reading-source-routing-implementation.md)。

## 历史进度与收藏

- 视频收藏：详情／普通播放／短剧 → [usePlaybackFavorites.ts](../frontend/src/player/page/usePlaybackFavorites.ts)／[FavFolderModal.vue](../frontend/src/components/FavFolderModal.vue) → [facade_favorite.go](../app/service/facade_favorite.go) → media／数据库。[Favorites.vue](../frontend/src/views/Favorites.vue) 还消费 [favoriteFolders.ts](../frontend/src/stores/favoriteFolders.ts) 与 [favoritesSync.ts](../frontend/src/stores/favoritesSync.ts)，收藏归属和收藏本身需一起检查。FavFolderModal 的 player 外观由普通播放和桌面短剧显式传入，经公共 Modal.panelClass 作用于实际浮层；固定深色、320px宽及紧凑留白，详情入口保留默认应用主题与尺寸，数据、确认和取消共用原控制器。
- 视频进度：媒体状态 → [useEpisodeHistory.ts](../frontend/src/player/page/useEpisodeHistory.ts)、[usePlaybackProgress.ts](../frontend/src/player/usePlaybackProgress.ts)、[usePlaybackResume.ts](../frontend/src/player/usePlaybackResume.ts)、[episodeProgress.ts](../frontend/src/utils/episodeProgress.ts) → [facade_history.go](../app/service/facade_history.go)。[History.vue](../frontend/src/views/History.vue)、详情集表、普通播放与短剧共同显示或消费进度；浏览器现场进度与 SQLite 观看历史并存，删除时逐项核对含义。
- 小说／漫画书架、追漫、最近阅读和偏好使用上一节的模式 store → readingPersistence → SQLite 链；旧浏览器值仅作为迁移来源。不要将视频收藏刷新信号或同名作品当作阅读身份。

联查重点：新增／取消收藏、移动或删除收藏夹、清历史后的续播和集表提示、换源同集映射、进度保存失败与退出收尾、已打开页面的刷新。恢复备份也会改变这些数据，应联查存储与恢复小节。

检索词：`bumpFavoritesRefresh`、`favRefreshTick`、`AddFavorite`、`RemoveFavorite`、`SaveWatchHistory`、`GetHistoryPosition`、`ClearAllHistory`、`epProgressKey`、`playbackTime`、`ReadingStateApply`。

## 缓存与失效

| 层次 | 当前入口与关联 |
| --- | --- |
| 数据变更的跨端失效 | [app/cache/invalidate.go](../app/cache/invalidate.go) 的 `PublishCatalogChange`／`cache:invalidate` → [stores/cacheInvalidate.ts](../frontend/src/stores/cacheInvalidate.ts)；订阅在 [main.ts](../frontend/src/main.ts) 启动，后端 publisher 和详情实例由 [service/app.go](../app/service/app.go) 装配。 |
| 视频详情、海报与图片 | [detail.Service](../app/detail/service.go)、[detailCache.ts](../frontend/src/stores/detailCache.ts)、[posterCache.ts](../frontend/src/stores/posterCache.ts)、[imageProxy.ts](../frontend/src/utils/imageProxy.ts)、[RemoteImage.vue](../frontend/src/components/RemoteImage.vue)；详情持久副本由 detailCache.ts 同时收敛条数、单条和总字节预算，旧快照读入同样过滤，失效取消迟到压缩结果；图片来源上下文还需查后端 proxy。 |
| HLS 内存与磁盘 | [tsCache.ts](../frontend/src/utils/tsCache.ts)／[ts-cache](../frontend/src/utils/ts-cache/)、[tsCacheDisk.ts](../frontend/src/utils/tsCacheDisk.ts)、[player/hls](../frontend/src/player/hls/)；disk-cache.ts 持有最近分片待提交键及访问时间表；prefetch.ts 的 setTransportActive 与 playback-lifetime.ts 的最后播放器释放共同控制自动维护暂停／空闲提交，换集前由 api.ts 捕获旧位置附近分片。结束／重播由 playbackSource.ts 接入；新传输中止维护，显式失效取消待写。磁盘 256 MiB／七天／2048 片，旧 v2 无 accessedAt 回退 ts；清单最长 60 秒、401／403 限频刷新。前后端媒体代理与下载是不同使用者，不能只改 IndexedDB 名称就认为所有缓存已迁移。 |
| 阅读与来源证据 | [contentCache.ts](../frontend/src/reading/contentCache.ts)、[useNovelSourceCache.ts](../frontend/src/reading/useNovelSourceCache.ts)、[library/novel_source_cache.go](../app/library/novel_source_cache.go)、[db/novel_source_cache.go](../app/db/novel_source_cache.go) 及 [library/content.go](../app/library/content.go)。 |
| 清理与容量展示 | [app/cache/service.go](../app/cache/service.go) 的 `GetInfo` 经 [service/diagnostics.go](../app/service/diagnostics.go) 提供数据库与日志占用；[VideoDiagnosticsPanel.vue](../frontend/src/components/VideoDiagnosticsPanel.vue) 另行读取前端缓存计数和 `TsCache.diskCacheInfo()`。清理从各模块的 `clear`／`clearAll`／`dropAllSegmentCache` 等实际导出追踪调用方，不假设存在统一的清缓存界面或绑定。浏览器内存、IndexedDB、后端磁盘与 SQLite 业务记录不能混为同一容量或同一清理范围。 |

联查重点：身份、层次、容量／TTL、旧快照加载、失败冷却、在途请求和待写定时器。删除来源、采集提交、修改影片、清库与恢复数据后，分别确认哪些派生缓存失效、哪些原始业务记录保留；清理后阻止迟到结果回填。不能将视频失效事件假定为阅读缓存的通用清理协议。

检索词：`cache:invalidate`、`PublishCatalogChange`、`dropDetailCache`、`dropSourceCache`、`dropVideoCache`、`clearAll`、`invalidate`、`createDebouncedWriter` 及具体 CCZJ Key。

## 来源采集与网络

- 视频管理在 [Sources.vue](../frontend/src/views/Sources.vue)、[sources/SourceCard.vue](../frontend/src/views/sources/SourceCard.vue)、[stores/source.ts](../frontend/src/stores/source.ts)；后端经 [source façades](../app/service/facade_source.go)、[facade_source_admin.go](../app/service/facade_source_admin.go)、[facade_source_import.go](../app/service/facade_source_import.go) → [app/source](../app/source/)。分类还涉及 VideoTypes 与 sources／source_types／global_types。
- 采集和定时任务经 [facade_collect.go](../app/service/facade_collect.go)、[facade_scheduler.go](../app/service/facade_scheduler.go) → [collection](../app/collection/)／[collect](../app/collect/)；模式任务负责启停。删除、停用、改地址和导入后都要考虑在途采集、搜索、已有目录与缓存失效。
- 小说／漫画源入口在 [NovelSources.vue](../frontend/src/novel/views/NovelSources.vue)、[ComicSources.vue](../frontend/src/comic/views/ComicSources.vue) → [useBuiltinSources.ts](../frontend/src/reading/useBuiltinSources.ts)、[readingSources.ts](../frontend/src/api/readingSources.ts) → [source_directory.go](../app/library/source_directory.go)、[source_maintenance.go](../app/library/source_maintenance.go)、[quality](../app/library/quality/)；质量、获取与多源搜索共用出站额度。
- 全局请求节奏设置在 [NetworkPolicyPanel.vue](../frontend/src/components/NetworkPolicyPanel.vue)、[network/requestPolicy.ts](../frontend/src/network/requestPolicy.ts) 与 [facade_settings.go](../app/service/facade_settings.go)；[netpolicy](../app/netpolicy/)、[netbudget](../app/netbudget/)、[outbound](../app/outbound/)、[netaddress](../app/netaddress/) 影响 collect／provider／proxy／download／douban。公网代理池设置与普通频率设置有不同生效时机，以 setter 和装配处为准。

联查重点：共享 sources 表中的模式过滤、启停／删除后的在途工作、来源身份和展示名称、网络权限、限流与冷却、私网图片与投屏边界。来源检测成功不等于搜索、目录、正文、图片或播放全都可用；候选详情与实际内容证据分开处理。

检索词：`source_key`、`LibrarySetSourceEnabled`、`RunSourceAction`、`StartCollect`、`NetworkGate`、`AllowPrivateNetwork`、`NETWORK_POLICY_KEY`、`outbound.SettingsKey`。具体供给和质量语义见[阅读质量基准](reading-source-quality-benchmark.md)及[请求通道](request-pipelines.md)。

## 下载任务与文件输出

- 详情发起下载，入口联查 [Detail.vue](../frontend/src/views/Detail.vue)、[Downloads.vue](../frontend/src/views/Downloads.vue)、[stores/download.ts](../frontend/src/stores/download.ts)；目录偏好还出现在 [SettingsBasicGroup.vue](../frontend/src/views/settings/SettingsBasicGroup.vue)。进度通过 `download:progress` 回到同一 store。
- 后端 [facade_download.go](../app/service/facade_download.go)／[service/download.go](../app/service/download.go) → [download/engine](../app/download/engine/)；[download](../app/download/) 提供目录、执行和断点契约，[db/download_tasks.go](../app/db/download_tasks.go) 管理任务持久化，[files](../app/files/) 管理文件边界。

联查重点：请求准入与持续传输的所有者、排队／暂停／恢复／取消、重启后任务状态、模式切换、关库前停止、断点与临时文件、改目录后的旧任务、删除来源或影片后现有任务的身份与输出策略。下载字节不是 HLS 播放缓存，不能互相代替容量和完成状态。

检索词：`useDownloadStore`、`download:progress`、`GetDownloadDir`、`downloadDirectory`、`Registry`、`Executor`、`checkpoint`。

## Key存储与备份恢复

- Key 定义在 [constants/cczj.ts](../frontend/src/constants/cczj.ts) 及同目录职责文件、[app/cczj/keys.go](../app/cczj/keys.go)；浏览器解码与防抖写在 [platform/storage.ts](../frontend/src/platform/storage.ts)、[debouncedWrite.ts](../frontend/src/platform/debouncedWrite.ts)。HTML 启动底色在前端模块初始化之前读取，独立执行环境需核对兼容值。
- 备份入口 [SettingsAdvancedGroup.vue](../frontend/src/views/settings/SettingsAdvancedGroup.vue) → [DataBackupPanel.vue](../frontend/src/components/DataBackupPanel.vue) → [facade_backup.go](../app/service/facade_backup.go) → [backup](../app/backup/) → [db/backup_snapshot.go](../app/db/backup_snapshot.go)、[backup_writer.go](../app/db/backup_writer.go)、[reading_backup.go](../app/db/reading_backup.go)。旧数据与 schema 提示还关联 [legacydata.go](../app/service/legacydata.go)、[schemanotice.go](../app/service/schemanotice.go) 和应用中的提示组件。
- SQLite 演进在 [migrations.go](../app/db/migrations.go)，浏览器旧阅读记录导入在阅读持久化链；WebView 资料选择在 [storagepaths/webview.go](../app/storagepaths/webview.go)。相同 Key 在不同资料目录或包身份下并不共享数据。

联查重点：读／写／删除／重置、动态身份、默认值、旧值导入和重试、恢复事务、内存 store 和缓存的后续处理、文件选择与分享、备份中的脚本信任边界。新增业务数据时明确是否进入备份及恢复流程；不要把常量集中定义误当作迁移完成。

检索词：具体 Key 原值及 `CCZJ` 字段、`GetSetting`、`SetSetting`、`ReadingStateLoad`、`BackupExport`、`BackupImport`、`BackupRestoreArchive`、`bootBackground`、`WindowsWebview`。

## 扩展包与动态入口

- 界面从 [ExtensionsPanel.vue](../frontend/src/components/ExtensionsPanel.vue)、[stores/plugins.ts](../frontend/src/stores/plugins.ts) 进入 [facade_plugin.go](../app/service/facade_plugin.go)／[app/plugin](../app/plugin/)；前端执行、注册与撤销在 [plugins/runtime.ts](../frontend/src/plugins/runtime.ts)、[registry.ts](../frontend/src/plugins/registry.ts)、[pendingWork.ts](../frontend/src/plugins/pendingWork.ts)。
- 权限和信任还涉及 [permissions.ts](../frontend/src/plugins/permissions.ts)、[trust.ts](../frontend/src/plugins/trust.ts)、[scriptTrust.ts](../frontend/src/plugins/scriptTrust.ts)、[api.ts](../frontend/src/plugins/api.ts)；拖入安装在 [dropInstall.ts](../frontend/src/plugins/dropInstall.ts)，内置包资源在 [app/plugin/builtin](../app/plugin/builtin/)。主题包、动效、设置分组、日志与诊断是这里的真实关联消费者。

联查重点：安装、启停、热更新、卸载时的 CSS／脚本／注册项／异步工作清理；面板已打开时取消注册、资源缓存、权限拒绝和信任重置；备份恢复不得自动授予脚本信任。动态入口不能只靠静态组件导入查找。

检索词：`readyPacks`、`syncPluginScripts`、`settingsTab`、`pluginSettingsTabs`、`RESERVED_SETTINGS_IDS`、`cczj.components`、具体扩展包 ID。契约见[扩展包规范](plugins.md)。

## 更新提示与后台下载

- 全局宿主 [UpdateModal.vue](../frontend/src/components/UpdateModal.vue) 在 [App.vue](../frontend/src/App.vue) 挂载，各模式共用；手动检查来自 [SettingsAboutGroup.vue](../frontend/src/views/settings/SettingsAboutGroup.vue)，条款确认来自 [LicenseGate.vue](../frontend/src/components/LicenseGate.vue)。自动检查由 [service/app.go](../app/service/app.go) 启动、[update/service.go](../app/update/service.go) 执行；Windows／Android 每次进程启动检查，iOS 保留商店安装流程。
- 暂时关闭只记录当前文档的 `dismissedVersion`，不写跳过 Key、不清除待提示结果；下次启动再次检查。仅显式 `IgnoreVersion` 持久写入 `CCZJ.SettingIgnoredVersion`，成功后撤销待安装安排并清理该版本下载展示。检查失败不等于没有更新，离线不能保证发现新版本。
- 下载与进度归 [stores/updateState.ts](../frontend/src/stores/updateState.ts) 和宿主请求所有，不因弹窗关闭停止。`update:download:progress` 经 [api/events.ts](../frontend/src/api/events.ts) 进入同一状态；后台浮窗在桌面／Android 显示连接、下载百分比／大小、速度、校验、完成和失败，点击重开详情，失败可沿原下载按钮重试。浮窗不使用会自动消失的通知计时器，移动端预留底部导航及安全区。
- 安装继续经过 [platform/updateInstaller.ts](../frontend/src/platform/updateInstaller.ts)，联查 Android 安装权限／取消恢复、桌面退出安装、跳过版本、新版本替换、模式切换和进程退出。浮窗是应用内状态，进程被系统终止后不能保证继续下载或显示系统通知。

检索词：`startupCheckUpdate`、`StartupCheck`、`IgnoredVersion`、`dismissedVersion`、`backgroundVisible`、`downloadUpdateArtifact`、`update:download:progress`、`openAndroidUpdateInstaller`。

## 平台与原生能力

- Wails 升级同时核对 [go.mod](../go.mod)、[package.json](../frontend/package.json)、CLI、生成绑定、[prepare-android-runtime.mjs](../scripts/prepare-android-runtime.mjs) 与 [Android 构建](../build/android/build.ps1)；联查 JNI 结构化错误、取消交接、退出清理及移动装配。版本和验证范围见 [开发指南](development.md#依赖升级候选与兼容边界)。
- 运行环境先经 [api/runtime.ts](../frontend/src/api/runtime.ts) 初始化，再由 [platform/presentation.ts](../frontend/src/platform/presentation.ts) 和 [main.ts](../frontend/src/main.ts) 选择宿主；前端布局宽度不决定原生能力。
- 播放／阅读设备操作经 [api/mobilePlayer.ts](../frontend/src/api/mobilePlayer.ts)、[player/platform](../frontend/src/player/platform/) 和 [platform](../frontend/src/platform/)；Android 联查 [MainActivity.java](../build/android/app/src/main/java/com/wails/app/MainActivity.java)、[WailsJSBridge.java](../build/android/app/src/main/java/com/wails/app/WailsJSBridge.java)、[WailsBridge.java](../build/android/app/src/main/java/com/wails/app/WailsBridge.java)、[CczjPlayerDevice.java](../build/android/app/src/main/java/com/wails/app/CczjPlayerDevice.java)、[main_android.go](../build/android/main_android.go) 及 [service/mobile.go](../app/service/mobile.go)。
- 文件读取前端共用 [api/fileTransfer.ts](../frontend/src/api/fileTransfer.ts)，原生文件与分享继续查 [service/mobile_storage.go](../app/service/mobile_storage.go)、[service/share_ios.go](../app/service/share_ios.go)、[app/files](../app/files/)；导出展示由 [platform/diagnosticFiles.ts](../frontend/src/platform/diagnosticFiles.ts) 选择系统入口。桌面窗口能力在 [facade_window.go](../app/service/facade_window.go)／[window](../app/window/)。

联查重点：前端可见入口、能力声明、桥接签名、原生注册、权限和取消、Activity／文档生命周期、系统返回、亮度与全屏恢复。文件选择同时服务来源、备份、主题取色和字幕等功能；修改公共读取行为要逐项追踪。Android、iOS、Windows、浏览器替身的能力和验收结论分别处理。

检索词：`initializeRuntimeEnvironment`、`presentationProfile`、`isMobileRuntime`、`isIOSRuntime`、具体 bridge 方法名、`readFileDataURL`、`readFileText`、`prepareDeviceFullscreen`。平台工作流见 [Android](android.md)／[iOS](ios.md)。

## 错误日志与诊断

路由确认时长与视觉呈现分开：桌面 MobileRouteStage 向同一 session 记录 `visual_transition` 的挂载、单次帧回调等待及完成／取消时长，不连续轮询帧率。全局异常类型由 `session.ts` 的白名单确定，不导出错误文本；短剧空列表／本地读取失败等事件需要同步 `timeline.go.validKind`。核对正常、返回、快速取消及销毁后剩余回调，不能把路由导航成功当成动画流畅。

- 用户反馈经 [api/errors.ts](../frontend/src/api/errors.ts)、[response.ts](../frontend/src/api/response.ts)、[stores/error.ts](../frontend/src/stores/error.ts)、[ErrorToastStack.vue](../frontend/src/components/ErrorToastStack.vue)；后端错误映射在 [app/apperror](../app/apperror/)，操作身份与日志在 [app/operation](../app/operation/)／[app/applog](../app/applog/)。
- 诊断入口 [RuntimeDiagnostics.vue](../frontend/src/views/RuntimeDiagnostics.vue)、[DiagnosticsPanel.vue](../frontend/src/components/DiagnosticsPanel.vue)、[VideoDiagnosticsPanel.vue](../frontend/src/components/VideoDiagnosticsPanel.vue)、[ReadingDiagnosticsPanel.vue](../frontend/src/components/ReadingDiagnosticsPanel.vue)，以及 [mobile/plugins](../frontend/src/mobile/plugins/)；录制在 [diagnostics/recording.ts](../frontend/src/diagnostics/recording.ts) → [service/diagnostics_recording.go](../app/service/diagnostics_recording.go)／[diagnosticrecording](../app/diagnosticrecording/)。Android早期阶段由CczjStartupTrace在原生库加载前开始记录，异步有界写入applog目录的固定元数据JSONL，最多64事件／32KiB每次、保留最近4次；phase、pid、时间和写入/丢弃/失败统计不含地址和内容。startupPhase桥接承接前端startup.ts白名单阶段，ServiceStartup沿applog记录数据库／网络策略／插件／模式及完成阶段，按PID联查，不能以最后日志推断具体崩溃调用。记录可在崩溃尾部缺失，不把入队等同落盘；诊断包复用现有导出最多附带4个合规原生启动文件。导出另查 [diagnostics_export.go](../app/service/diagnostics_export.go) 与平台文件入口。

- 元数据队列与控件／窗口错误监听在 [diagnostics/session.ts](../frontend/src/diagnostics/session.ts)，主装配在 [main.ts](../frontend/src/main.ts)，导航模板在 [router/index.ts](../frontend/src/router/index.ts)，请求／组合链在 [requestPipelines.ts](../frontend/src/api/requestPipelines.ts) 与 [requestChain.ts](../frontend/src/api/requestChain.ts)，错误提示在 [stores/error.ts](../frontend/src/stores/error.ts)。共同依赖不得反向引入 API 或 Pinia 造成启动循环。
- [DiagnosticSessionPanel.vue](../frontend/src/components/DiagnosticSessionPanel.vue) 供三种模式、桌面与移动宿主复用；日常开关需联查前后端 CCZJ Key、[facade_settings.go](../app/service/facade_settings.go)、启动恢复、录制状态、清空与迟到批次。
- 后端窗口／预算在 [timeline.go](../app/diagnosticrecording/timeline.go)，录制与非阻塞队列在 [recorder.go](../app/diagnosticrecording/recorder.go)，显式剖析在 [profiles.go](../app/diagnosticrecording/profiles.go)，操作观察在 [requests.go](../app/service/requests.go)。联查模式切换、应用退出、采样复用、原始文件轮转及失败文件排除。完整契约见 [诊断证据与轻量采集](development.md#诊断证据与轻量采集)。

联查重点：公开错误码、取消与失败的区分、重试行为、用户消息与诊断原因、记录中的模式／任务／请求身份、轮询停止及录制关闭、移动与桌面导出、元数据过滤、排队与丢弃统计、显式关联能力及独立时钟。开始／结束录制和普通导出不得隐式开启剖析；有数量提示不等于已经定位根因。改变请求或后台任务结构时检查诊断归属，不能继续将预览、取消或磁盘命中计作真实网络失败／吞吐。

检索词：`MarshalError`、`normalizeApiError`、`fromError`、`diagnosticMode`、`recordDiagnosticEvent`、`recordingStatus`、`recordTimeline`、`SubmitDiagnosticsEvents`、`GetDiagnosticsSession`、`CaptureDiagnosticsProfile`、`operation.Meta`、`retryAfterSeconds`。

## 更新与安装

- 入口在 [SettingsAboutGroup.vue](../frontend/src/views/settings/SettingsAboutGroup.vue)、[UpdateModal.vue](../frontend/src/components/UpdateModal.vue)、[stores/updateState.ts](../frontend/src/stores/updateState.ts)；安装适配在 [platform/updateInstaller.ts](../frontend/src/platform/updateInstaller.ts)。
- 后端 [app/update/service.go](../app/update/service.go) → [updater](../app/updater/)；Windows 交接在 [handoff](../app/handoff/)，Android 安装还要查上一节的桥接与 Activity。下载等待属于应用级工作，关联 `runHostRequest`、退出收尾与待安装状态。
- 正式版本发现同时读取 GitHub `releases/latest` 和 `main/version.json` 的 Raw／CDN／Gitee 镜像；预发布源码及版本清单只推送独立分支和标签，不覆盖 `main` 的稳定清单。`fetchReleaseContext` 拒绝 prerelease／draft，`fetchVersionInfoContext` 仅接收正式版本；`compareVersions` 保证 beta 可升级到同版本正式版。

联查重点：检查版本、下载、取消／恢复、校验、安装权限、重启、安装结果和回滚；关于页、弹窗和后台下载展示同一状态。安装包路径、WebView 资料身份与用户数据目录分别管理，不能因更新改名使用户数据看似丢失。流程见[发布与自动更新](releases.md)。

检索词：`DownloadUpdate`、`updateDownload`、`runHostRequest`、`pendingInstall`、`Install`、`handoff`，并从实际导出函数继续搜索调用方。

移动反馈相关链：App在theme.loaded后更新bootAppearance，SplashScreen恢复前使用启动缓存，避免默认主题覆盖缓存。主题编辑浮钮右侧与回顶部垂直错位；搜索历史仅编辑态保留删除热区。VideoPlayer进度填充／缓冲缩放保留translateY居中，迷你进度独立。短剧远端页已按来源分类筛选，useShortDramaFeed直接复用返回视频，减少重复推荐查询；本轮失败候选不立即重复详情请求。

弹幕与共享拨号：danmaku.Client保留超时／取消错误链，SafeDialContext在DNS校验全部地址后按剩余期限分配单地址尝试预算（最多3秒），防止一个不可达地址耗尽请求；TLS仍保留原主机名，SSRF规则不变。共享消费者包括HLS、图片、线路探测及阅读资源，原请求取消继续控制全链。该改动不证明平台网络或鉴权一定可达。
