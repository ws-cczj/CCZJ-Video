# 开发与复用指南

执行规范见 [AGENTS.md](../AGENTS.md)。本页负责给出真实代码入口和维护命令，避免在新页面重新实现已有能力。

开始修改时，先按 [关联影响与完整体验](../AGENTS.md#关联影响与完整体验) 确定操作链和共享消费者，从 [功能关联索引](feature-relations.md) 定位跨页面、跨模块关系，再使用下方入口查找实现；设置入口、状态、存储、平台装配与业务页面应一起纳入影响分析。

## 目录职责

Windows 的 SQLite 位于 `%APPDATA%/CCZJ Video`。浏览器资料目录由 `app/storagepaths.WindowsWebview` 选择一次并写入同目录的 `webview-profile.json`；首次升级时会接续最近使用的正式版 WebView2 目录，此后 EXE 文件名变化仍使用选定目录。该记录指向原目录，迁移时不复制运行中的 Chromium 数据。Android 数据按 APK 内的 `applicationId` 隔离；APK 文件名不决定数据目录，同一包身份和签名的覆盖安装使用原目录。

| 目录／文件 | 用途 |
| --- | --- |
| `main.go`、`app.go` | 程序入口、装配与应用生命周期 |
| `app/service` | Wails 绑定适配；业务规则进入所属领域模块 |
| `app/*` | 视频、阅读、下载、更新、存储和共享基础能力 |
| `frontend/src/modes` | 模式选择、切换和壳层装配 |
| `frontend/src/api` | 绑定、事件、请求策略与 DTO 解码 |
| `frontend/src/platform`、`frontend/src/mobile` | 宿主能力与移动呈现 |
| `frontend/src/novel`、`comic`、`reading`、`player` | 独立领域体验与适当共享的阅读／播放能力 |
| `frontend/src/components/ui`、`styles` | 公共交互控件、工具类与样式 |
| `frontend/build`、`build` | 前端构建插件源码、原生宿主与发布脚本 |
| `scripts`、`frontend/tests`、Go `*_test.go` | 验证入口与回归测试 |
| `docs`、`reports` | 长期维护说明、验收与历史依据 |
| `.cache`、`bin`、`dist` | 本地缓存、可重建产物、运行证据与发布输出，默认不提交 |

## 复用入口

持久化 Key 的前端入口为 [`constants/cczj.ts`](../frontend/src/constants/cczj.ts)，例如 `CCZJ.Storage.episodeProgress`、`CCZJ.Settings.applicationMode` 和 `CCZJ.Keys.playbackTime(key)`；Go 入口为 [`app/cczj/keys.go`](../app/cczj/keys.go)，以 `CCZJ` 作为导入别名。集中定义保留原始字符串和历史双前缀播放进度兼容，不需要清缓存或重建数据库。旧模块公开的 Key 常量保留为兼容别名。JSON 传输字段和历史迁移 SQL 保持各自原契约。

[`platform/storage.ts`](../frontend/src/platform/storage.ts) 的 `readStorageRecord` 按领域提供的校验器过滤缓存条目；详情、海报与图片代理使用同一读取规则。缓存上限仍由各模块管理，详情在加载旧快照时也收敛条数。[`platform/debouncedWrite.ts`](../frontend/src/platform/debouncedWrite.ts) 负责合并写入、失败脏状态、页面后台补写和监听释放：`cancel()` 用于删除缓存前取消待写，`dispose()` 用于所有者销毁时补写并解绑；在 Vue／Pinia scope 内自动释放，模块单例由文档生命周期持有。

图片代理的内存缓存、持久副本、失败冷却和请求合并集中在 [`utils/imageProxy.ts`](../frontend/src/utils/imageProxy.ts)，真实消费者直接导入该模块，`utils/index.ts` 保留旧导出兼容。这里不接管小说、漫画封面各自的来源请求上下文。首屏引导的 `index.html` 在模块加载前直接读取 `cczj.boot.background.v1`，与 `CCZJ.Storage.bootBackground` 保持相同拼写。

| 要解决的问题 | 先查看 |
| --- | --- |
| 按钮、加载、禁用、弹窗、下拉、数字输入、空状态 | [`components/ui/index.ts`](../frontend/src/components/ui/index.ts) |
| 图标、带请求上下文的图片 | [`Icon.vue`](../frontend/src/components/Icon.vue)、[`RemoteImage.vue`](../frontend/src/components/RemoteImage.vue) |
| 通用布局、主题变量与动效 | [`cczj-utilities.css`](../frontend/src/styles/cczj-utilities.css)、[`stores/theme.ts`](../frontend/src/stores/theme.ts)、[`motion`](../frontend/src/motion/) |
| 通用业务绑定和请求策略 | [`api/app.ts`](../frontend/src/api/app.ts)、[`requestPolicies.json`](../frontend/src/api/requestPolicies.json)、[请求说明](request-pipelines.md) |
| 数据校验、结构化错误与取消传播 | [`api/decode.ts`](../frontend/src/api/decode.ts)、[`response.ts`](../frontend/src/api/response.ts)、[`errors.ts`](../frontend/src/api/errors.ts) |
| 页面取消、迟到结果和 KeepAlive 活跃状态 | [`requestScope.ts`](../frontend/src/api/requestScope.ts)、[`useGeneration.ts`](../frontend/src/composables/useGeneration.ts)、[`useViewActivity.ts`](../frontend/src/composables/useViewActivity.ts)；后台读取／轮播／轮询使用 `committed` 范围，图片继续使用可见性范围，避免滑动预览激活工作 |
| 后端事件订阅 | [`api/events.ts`](../frontend/src/api/events.ts)，返回的解绑函数由订阅者持有 |
| 下载任务、协议执行、暂停恢复与资源所有权 | [`download/engine`](../app/download/engine/)、[`下载与请求上下文`](request-pipelines.md)；宿主注入目录、断点存储、网络预算与进度回调 |
| 视频列表、搜索、详情、线路、收藏与历史 | [`media`](../app/media/)、[`detail`](../app/detail/)、[`MediaStore`](../app/db/mapping.go)、[请求与所有权](request-pipelines.md#视频查询详情与用户记录)；复用应用详情实例和请求上下文 |
| 设置写入与浏览器缓存 | [`settingsQueue.ts`](../frontend/src/api/settingsQueue.ts)、[`platform/storage.ts`](../frontend/src/platform/storage.ts)、[`debouncedWrite.ts`](../frontend/src/platform/debouncedWrite.ts) |
| 文件分享、安装器、平台选择 | [`platform`](../frontend/src/platform/)、[`build/android`](../build/android/) |
| iOS 本地宿主、待签名 IPA 与模拟器启动检查 | [`build/ios`](../build/ios/)、[iOS 开发](ios.md) |
| 阅读目录工具、桌面输入设置 | [`ChapterTools.vue`](../frontend/src/reading/ChapterTools.vue)、[`ReaderInputPreferences.vue`](../frontend/src/reading/ReaderInputPreferences.vue)，通过模式和偏好显式传入 |
| 阅读热缓存、搜索返回快照 | [`contentCache.ts`](../frontend/src/reading/contentCache.ts)、[`useSearchReturn.ts`](../frontend/src/reading/useSearchReturn.ts)，按阅读器／页面拥有，失败、取消与卸载清理 |
| 阅读持久化、书目与来源 | [`app/readingstate`](../app/readingstate/)、[`app/library`](../app/library/)、[`app/provider`](../app/provider/)、[`app/source`](../app/source/) |
| 来源变更与采集 | [`app/source`](../app/source/)、[`app/collection`](../app/collection/)，同源准入、取消等待、原子导入和统一收尾 |
| 执行上下文 | [`operation.OwnedContext`](../app/operation/identity.go)，独立执行身份、父请求关联与资源所有者 |
| 备份一致性与恢复事务 | [`backup`](../app/backup/)、[`db.ReadBackupSnapshot`](../app/db/backup_snapshot.go)、[`db.WriteBackup`](../app/db/backup_writer.go)；导出同一只读快照，恢复各类数据同一事务提交 |
| 文件读取、复制与发布 | [`api/fileTransfer.ts`](../frontend/src/api/fileTransfer.ts)、[`files/stream.go`](../app/files/stream.go)；文本／Data URL 读取限额与取消，唯一文件或原子替换，失败清理自身临时文件 |
| 宿主级长请求 | [`runHostRequest`](../app/service/requests.go)，用于更新下载和备份，跨模式保留，退出取消并等待 |
| 豆瓣后台补全 | [`douban.Updater`](../app/douban/updater.go)、[`douban.Scheduler`](../app/douban/scheduler.go)，模式上下文贯穿限速等待、搜索、详情与存储 |
| 工作生命周期、固定批次、在途合并 | [`app/lifecycle`](../app/lifecycle/)、[`app/parallel`](../app/parallel/)、[`app/workflight`](../app/workflight/) |
| 全应用出站限流、预算与统计 | [`app/netpolicy`](../app/netpolicy/)、[`app/netbudget`](../app/netbudget/)、[`app/netstats`](../app/netstats/) |
| 公网代理池、局域网直连、本机代理回退 | [`app/outbound`](../app/outbound/)、[`app/netaddress`](../app/netaddress/)、[路由配置与验收](request-pipelines.md#公网代理池) |
| 阅读五路 HTTP 闸门与检测批次 | [`quality/gate.go`](../app/library/quality/gate.go)、[`quality/dispatch.go`](../app/library/quality/dispatch.go)，由 [`library/service.go`](../app/library/service.go) 共享装配 |
| 错误、日志、资源、文件边界 | [`app/apperror`](../app/apperror/)、[`app/applog`](../app/applog/)、[`app/resource`](../app/resource/)、[`app/files`](../app/files/) |
| 模拟器 WebView 控制 | [`scripts/android_webview.py`](../scripts/android_webview.py)，复用连接、进程就绪等待与端口释放 |

入口表用于定位，不代表所有场景都要多包一层。新增职责放在所属模块，修改共享接口时同步所有消费者和验证。

## 界面动效与设置

视频、小说、漫画的桌面与移动壳层共用 `mobile/MobileRouteStage.vue`，通过 `routeMotion.ts` 中的屏幕身份、主标签关系与历史位置选择过渡。移动主标签横向移动，桌面视频并列入口以整页高度纵向推移，设置分组以短距离纵向移动；整页推移使用 `--cczj-motion-section`（默认 800ms），分组使用 `--cczj-motion-group`（默认 320ms）；详情等层级页面覆盖／返回，旧层在退出期间不可交互。桌面视频播放场景自身固定覆盖整个应用客户区，从右侧进入（默认 800ms）并向右侧退出（`--cczj-motion-dismiss`，默认 600ms），仅改变 transform，不再为播放页计算点击原点或圆形裁切；舞台保持原尺寸，不自动进入系统显示器全屏。桌面侧栏及转场共同消费 `modes/videoNavigation.ts` 的真实条目顺序，含动态扩展入口；目标条目在上方时从上向下进入，在下方时从下向上进入，不以历史栈位置替代空间关系。桌面壁纸按应用画布裁切，场景包含规则与播放器全屏同步适配。阅读器内同一作品的章节／话次切换仍由阅读器负责，不再叠加一次全页转场。路由只查询已登记入场节点的动画；可见预览和已提交页面分别控制图片准备与业务工作，已呈现的缓存主标签不重新遮挡。

同级页面同时进入和退出，保持同一 800ms 位移与 `cubic-bezier(.4,0,.2,1)` 曲线，不能将两页串行播放而暴露空画布。时长按视觉范围分配，进入与退出可不同；参考 [Atlassian 动效调度](https://atlassian.design/foundations/motion/applying-motion)和 [Fluent 动效原则](https://fluent2.microsoft.design/motion)，保留本项目已选择的方向滑动。桌面首页完成首屏加载后，在可见空闲时预先加载共享 `modes/videoViews.ts` 的详情代码模块；不挂载页面或预取业务／媒体数据，离场和销毁取消待执行调度。该优化减小首次导航的模块加载工作，不能代替设备帧率验证。

`stores/motion.ts` 是动效偏好的所有者，`allows()` 同时考虑总开关、分类开关、用户减少动态效果与系统偏好。减少动态效果也同步根 CSS 标记，覆盖只消费样式的页面、图标与反馈；加载指示保留。基础设置的总开关不依赖扩展包是否启用；内置 motion-effects 包继续提供设置分组及样式。修改时一起维护 MotionPanel 预览、两份语言文案、路由、公共覆盖层和装饰反馈。关闭效果保留加载指示及静态选中状态，不冻结业务工作。

MotionPanel 以真实操作展示结果：导航预览经 `routeTransition` 选择当前宿主方向和共享路由 CSS，播放预览使用相同进入／退出名称；弹窗直接装配公共 Modal，抽屉装配 CczjMobilePlayerDrawer。主题卡片经 `createThemeReveal` 的具名局部快照复用点击原点、半径和曲线，不改写实际主题或存储；宿主不支持 ViewTransition 时直接更新配色。没有媒体请求或自动预览循环，离场关闭覆盖层、取消揭示并恢复临时快照名称。旧风格／速度／强度不再作为界面选项，旧存储值仍参与未覆盖时长和曲线，用户恢复默认时才重置。新的 reduceMotion 与 durations 字段沿用原 motionOptions Key、串行保存、失败回滚；duration 仅接受已知名称的有限数值，限制 100–2500ms。展开项显示实际配置毫秒数，桌面提供 section／dismiss，手机提供 slow，双方提供 group／normal／reveal；normal 同时被常规面板和阅读翻页消费，小说不再独自计算该时长。

主题颜色计算集中在 `utils/colors.ts`，由 `stores/theme.ts` 分配功能角色。主操作与文字／图标配对，自动前景优先使用 OKLCH 同色相的深浅候选、降低色度适配 sRGB，再检查 4.5:1 对比度，不足才回退黑白；输出普通十六进制色，不要求宿主支持 CSS oklch。成功／警告／危险的色相语义保持稳定，最小化和最大化使用低饱和主题色阶，关闭悬停保留危险角色。btnMin／btnHide CSS 名称保留兼容，显式预设 tint 和用户自选色仍优先；ThemeSample 同步展示主操作及窗口动作色片。已知透明表面按声明底色合成后计算辅助文字，不读取壁纸像素，不能声称任意壁纸、任意透明度均符合对比度。黑底播放器继续使用本地深色控件板，小说／漫画阅读配色不改成视频样式。颜色空间依据见 [CSS Color 4](https://www.w3.org/TR/css-color-4/#color-conversion-code)，角色划分参考 [Material 色彩角色](https://developer.android.google.cn/design/ui/mobile/guides/styles/color?hl=en)；不引入新依赖。

公共 Modal 由一个 `modal` 过渡同时持有遮罩和面板，退出完成后释放焦点所有权。ReaderSheet 的所有调用方在稳定父级持有 `sheet` 过渡，内容组件不再持有会先于退场销毁的内部过渡；未实现拖动关闭，因此不展示装饰把手。两者通过 `composables/useOverlayFocus.ts` 共用最上层 Escape／Android 返回、焦点约束和归还，监听在最后一层释放时移除。下拉展开方向与实际定位一致，跟踪动画只查询触发点的祖先；隐藏或离场时关闭并释放跟踪。

明确选择主题时，`stores/theme.ts` 使用 `motion/themeReveal.ts` 从点击点揭示真实新外观；键盘选择以卡片中心为起点。主题揭示使用 `--cczj-motion-reveal`（默认 1200ms），桌面播放入场使用 `--cczj-motion-section`（默认 800ms），退出使用 `--cczj-motion-dismiss`（默认 600ms），分场景时长可分别调整；未覆盖项保留旧版速度／风格的计算结果。关闭页面动效或启用减少动态效果时跳过。实现参考 [MDN ViewTransition 示例](https://developer.mozilla.org/en-US/docs/Web/API/ViewTransition#examples)与 [Telegram Android 主题交接](https://github.com/DrKLO/Telegram/blob/3de37c8cfb039452be7398909461da6288babf15/TMessagesProj/src/main/java/org/telegram/ui/LaunchActivity.java#L7480)，没有复制原生窗口动画或引入截图依赖。仅支持该 API 且允许页面动效时启用；连续选择以最新选择为准，后台、离开与销毁清理临时过渡。持久化按输入顺序进入原设置队列，失败继续显示重试。编辑器的颜色输入保持即时预览，不重复播放整页扩散；选择页和编辑器复用 ThemeSample，布局预览直接读取 layout store 的真实网格规则。

桌面窗口轮廓由 `app/window/appearance_windows.go` 的 `InstallRoundedCorners` 所有，`app.go` 在创建主窗口时装配：Windows 11 优先使用 DWM 圆角偏好，旧版系统使用 8 DIP 半径的原生窗口区域，按尺寸、DPI、最大化与全屏状态变化更新；相同几何不重建区域。成功设置的区域交给 Windows 释放，失败时释放未转交句柄。前端 `App.vue` 只绘制静态边缘高光，不再叠加独立客户区圆角裁切；`TitleBar.vue` 使用根目录应用图标并同步最大化状态。最大化和系统全屏使用矩形边界，手机不装配此能力。系统限制及区域所有权参见 [Windows 桌面圆角](https://learn.microsoft.com/en-us/windows/apps/desktop/modernize/ui/apply-rounded-corners)与 [SetWindowRgn](https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-setwindowrgn)。原生四角、调整尺寸、DPI、恢复和全屏仍需设备验收。 缺少 GetDpiForWindow 时改用短期设备上下文读取系统 DPI，获取后立即释放。此窗口 API 回退不代表整个安装包支持旧系统：当前 Wails 3.0.0 声明 Go 1.26.0，[Go 1.21 起最低 Windows 10](https://go.dev/wiki/MinimumRequirements)，[WebView2 109 是最后支持 Windows 8/8.1 的版本](https://blogs.windows.com/msedgedev/2022/12/09/microsoft-edge-and-webview2-ending-support-for-windows-7-and-windows-8-8-1/)。Windows 10/11 为本包目标；Win8 需要独立评估可用工具链与宿主，不能仅修改 manifest 或外观就宣称兼容。

Settings 的桌面 Grid 显式分配标题、搜索、分组和内容区域，窄屏改为单列，原生移动设置由 block 宿主呈现。搜索范围为设置分组名称和说明，不是全字段检索；动态扩展包分组同样参与搜索。桌面分组保留草稿和各自滚动位置，仅实际切换分组时播放短距离位移，返回设置页不重播分组动画，基本设置区块不再独立错峰入场；手机子分组继续沿路由与模式快照恢复。选集律动消费播放器真实播放／缓冲状态，并受面板可见性与动效偏好控制；空状态不再持续漂浮，Toast 退出时保留几何后脱离布局，让其他消息同时移动。`api/events.ts` 的解绑可重复调用，初始化失败清理事件合并资源，开发热更新会移除旧订阅；共享订阅的业务所有者仍负责正常释放，不能绑到首个消费组件的销毁周期。

上述行为已进入实现，Windows WebView2、Android WebView 和 iOS 的视觉交接、焦点、系统栏、全屏及连续操作仍需分别由用户验收；不以构建或静态检查声称设备流畅、无闪烁或没有内存泄漏。共享元素从封面展开到详情等更复杂的交接没有作为本次通用路由的承诺。

## 视频播放与来源反馈

桌面和移动首页返回时等待同级场景的有限过渡结束后再刷新数据，包含播放层从静止首页侧向退出的情况；离场 generation 使等待结果失效，不遍历场景后代动画。播放器销毁通过 `releaseAnimations.ts` 在根节点设置 `data-cczj-player-releasing`，公共动画样式同时关闭该子树及伪元素动效，再取消已有动画对象，不逐节点写入样式；该标记仅用于已销毁实例。

主播放与缩略预览共用分片缓存，但使用不同用途的 HLS loader。预览不写入主播放命中率、网络失败和吞吐诊断；主播放发起网络分片请求时会取消正在拉取的预览，预览本身不抢占后台预取。加载面板的速度使用实际媒体网络字节累计值，不用内存缓存占用增长代替下载量；磁盘命中和缩略预览不计入该速度。IndexedDB 连接在分片读取期间复用，空闲五秒关闭，打开失败或版本变化后允许重新建立连接。

HLS worker 可同步转移进度回调和成功回调收到的 ArrayBuffer。loader 必须在转发前保留独立的缓存副本；命中时先复制再结束 loader，复制失败逐项失效并回退实际网络。内存读取与预取判定同时核对 buffer 实际长度和登记容量，淘汰时修正账本，磁盘仅接收缓存所有的字节。`setEpisodes` 有意跨页面保留全局 LRU，因此退出／重进不能代替坏条目恢复。该所有权链覆盖主播放、预览、非流式和字节区间响应；2.4.9 主播放关闭 worker，2.5.0 启用后暴露了转移后复用缓存的问题，日志与源码支持此链路，现场恢复仍待设备验收。

页面返回保留并直接恢复原滚动位置，不从顶部动画扫过列表；恢复期间的触摸、滚轮和滚动键输入会交还用户控制。主动返回顶部遵守动效偏好，并可由新输入打断。首页轮播只有在页面已提交、窗口前台且轮播进入视口时自动运行；离屏取消计时、图片等待和未完成的切换，保留当前海报。轮播跟手与播放器抽屉调整高度每帧最多提交一次更新，停止、离场和卸载会清理待处理帧。

`VirtualVideoGrid` 服务首页目录、普通搜索／本地补搜结果和短剧剧场，按所属滚动容器挂载可见行及前后三行；复用 VideoCard，按实际行高记录占位，响应列数／宽度变化，键盘焦点所在行单独保留。批量选择的源站搜索结果尚沿用原列表，避免丢失选择操作语义。普通视频及预览 HLS 使用随当前 hls.js 打包的独立 Worker 文件处理转封装，失败由 SDK 回退主线程；它不取代硬件视频解码，也不承担 Vue 布局。构建输出应包含 `hls.worker-*.js`。

移动横滑允许从普通按钮／卡片开始，横向列表、输入框、播放器手势及系统边缘返回仍各自拥有操作；短距离快速甩动或超过屏宽 22% 可提交切页。路由手势交接保留无过渡状态至目的页绘制提交，系统栏过渡复用一次判定的页面颜色身份，避免每帧重复查询全部后代。2.4.9 已有路由直接 transform 和生命周期区分，但不代表所有真机闪烁均已验收。

卡片的移动端准备观察使用预览页自己的 `.main-content`，与 RemoteImage 共用观察入口。`routeVisualMotionKey` 传递手势／交接状态；RemoteImage 默认关闭独立淡入淡出，VideoCard 不再以整卡透明度和入场位移隐藏已存在的内容，hover 限于精确指针。公共 `ui/Skeleton.vue` 提供静态文字、列表及卡片占位，结构参考 [Vant Skeleton](https://github.com/youzan/vant/blob/main/packages/vant/src/skeleton/README.zh-CN.md)。尚未呈现过的主标签可在入场／手势准备时使用占位，已呈现的缓存页直接恢复；首页和短剧剧场自己的首次读取、失败与重试仍由业务负责，骨架不代表数据成功。Skeleton 内容替换不附带第二次入场；短剧骨架与真实列表共用响应列定义、间距和 2:3 海报比例，不显示列表中不存在的额外标题。桌面保留 ShortDrama 剧场实例，离场仍按活动范围取消请求、释放媒体。页面动效关闭或系统减少动态效果时跳过视觉交接。实际闪烁与触摸手感由用户设备验收。

视频缓存没有按应用版本编号分区。2.4.0 与 2.4.9 的浏览器详情键同为 `cczj_detail_cache_v3`（最多 12 条），媒体数据库同为 `tscache` v2（160 MiB／两天），分片键归一化规则未变。详情和集表仍按来源身份读取；2.4.9 增加返回详情的 vod_id／global_id 校验，旧缓存身份不匹配会被拒绝。后端详情热缓存和 m3u8 文本缓存只在进程内保留，重启后不能把重新请求清单等同于磁盘分片已丢失。实际分片按归一化媒体 URL 与字节区间命中，同一视频 ID 并不足以证明不同源、集数、线路、码率或剪辑的字节相同；两个源指向同一份可识别的媒体资源时可以命中相同分片键。Android 覆盖升级要求相同 applicationId 和签名；卸载／清理 WebView 或不同包身份会隔离浏览器缓存。当前版本对照尚未定位用户实际未命中的存储记录，不以盲目移除来源隔离或延长 TTL 代替诊断。

用户补充的两版本并存对比实际发生在 Windows。2.4.0 尚未显式设置 WebView 资料目录，旧环境产生了按 EXE 名称区分的目录；2.4.2 开始由 `storagepaths.WindowsWebview` 固定选择资料目录，2.4.9 沿用该选择。本机 `webview-profile.json` 选择 `cczjVideo.exe`，同时仍有两个名称含 2.4.0 的旧目录及独立 Local Storage。相同缓存键处于不同浏览器资料目录中，详情与媒体缓存不会互读；这是本次跨版本对比已确认的存储隔离因素，不代表缓存格式不兼容。固定目录只能约束采用该实现的版本，不能改变已发布旧 EXE 的行为。旧目录的来源数据需要与当前目录区分，不能覆盖或拼接运行中的 LevelDB／IndexedDB，也不因旧目录仍有 Local Storage 就断言媒体分片仍在其中。

播放面板默认预览两条已命中线路，选集预览当前集附近最多十二集；两处均可独立展开。探测按钮与源列表展开按钮并列，未命中／失败候选不占用线路列表；当前正在播放的线路始终保留。详情集表使用主题 `--btn-solid` 填充已播集，底部进度条使用 `--btn-solid-text` 反色并常驻显示。桌面播放黑底面板使用柔和灰蓝已播状态、明亮蓝色当前集；移动侧面板及选集抽屉使用根主题解析后的专用集数变量，已播填充及反色进度不受抽屉局部深色变量覆盖。页面保留滚轮、触摸和键盘滚动，隐藏滚动条；展开列表由外层面板滚动，避免源列表与选集重复限高。

换源接力只在已核对同剧同集后传递现场进度；播放器等新媒体接入再允许待处理跳转，避免把时间写到旧媒体。短剧远端列表沿用后端 25 秒预算，前端最多等待 30 秒，超时取消并显示重试入口。采集源删除显示在途状态，15 秒未收到应答则取消等待，再读来源列表核对提交结果；读取失败仍反馈错误。

目录探测结果与视频播放能力分别表达。本机代理在响应头之前返回 EOF 时，只对无请求体的 GET／HEAD 沿同一路由重试一次；HTTP 错误、正文读取错误和写操作不走此重试，仍受原请求取消、超时及冷却约束。上述行为需由用户进行桌面及 MuMu／真机验收，编译检查不代表运行验收通过。

## 短剧播放

`/short-drama` 在桌面侧栏和移动底栏首页右侧提供入口；移动历史记录保留兼容路由，不再占用底栏。桌面默认进入海报剧场，点击卡片后进入单剧播放器并收起侧栏；随机播放通过 `?view=feed` 进入推荐流。移动默认刷推荐，顶部切到 `?view=theatre` 展示两列选剧列表。`ShortDramaTheatre.vue` 复用 `VideoCard` 和 `useVideoList` 的游标分页、搜索与错误状态，返回剧场保留列表和滚动位置，隐藏列表通过 `enabled` 取消在途请求。新页面不复制首页数组或另建列表请求队列。

列表通过既有 `GetVideoList.short_drama` 可选字段进入媒体查询，分页和总数计算前即筛掉普通影片；与 `GetRecommend.short_drama` 共用目录中的分类边界，包含来源分类及全局映射，并遵守可见、未回收和当前来源规则。未提供短剧字段的旧列表调用保持普通列表语义。缺失更新时间的来源仍可使用带 id 决胜的游标翻页。`player/shortDrama/useShortDramaFeed.ts` 拥有本地每批最多五部、远端每批最多一页的推荐、单部详情、集数及请求取消；剧场显示或页面隐藏时释放播放器，重新进入后恢复所选剧目与集数，不按标题猜测短剧，也不以普通影片填充不足的数量。

短剧从当前来源的 CMS `list` 获取真实分类，按类型名或已有全局映射识别短剧及子类；界面统一展示，不提供子分类选择或手动归属弹窗。剧场、推荐、本地可见性与采集准入消费同一自动范围，父级禁用包含子类，作品标题不能判断短剧身份。旧 GetShortDramaCategories／SetShortDramaCategories 绑定及 manual_short_drama 导入导出字段保留兼容，但不再扩大短剧的实际范围，也不删除原数据。显式单剧保留自身来源，普通剧场与随机推荐始终锁定当前源。

远端短剧复用 `source_types(source_key,source_type_id) → global_types` 的分类和禁用规则；分类浏览和分类内搜索沿用已声明的类型／关键词参数。`GetShortDramaPage` 在 category_id 中返回有界不透明游标，记录来源、接口、筛选与分类范围摘要、各类页码和下一读取位置；调用方原样续传，不把它当类型 ID。通常每次读取三个分类页，前面无内容时最多尝试五个，整次限时 25 秒；范围最多 128 个分类、游标至多 64 KiB、单次返回至多 100 条。查询结果按实际分类及用户禁用规则校验，再仅缓存目录字段；详情和媒体仍按需获取。列表总数表示本地当前可见缓存条数，不冒充源站全量统计。取消和失败不提交前端游标，已正常写入的目录仍可复用。筛选、来源或分类范围改变后需从首批重建游标。

来源层级补充由 `collect/category_parents.go` 负责。优质来源 `list` 中有“短剧大全”父类，但 `t=83` 不聚合子类，多 ID 参数也只读取第一个；其[官方教程](https://get.1080zyk17.com/help/)指定的爬虫入口能读取[父类导航](https://get.1080zyk17.com/?m=vod-type-id-83.html)。适配仅作用于已确认的 API 主机，将官网父类导航的 ID 和名称同时与当前 API 类型表核对后补父级，不按编号区间或作品名推断。API 已提供父级时不额外读取网页；补充页面限 1 MiB，使用现有采集传输、网络预算和请求取消。普通来源仍仅按自身协议识别，不静默切换其他来源。

缓存优先与静默更新：剧场先显示当前源的 SQLite 目录，已有内容时不重新遮挡骨架；刷新合并同身份元数据，保留已加载位置和分页，失败保留缓存并提供重试。推荐先从 SQLite 调用已有目录随机采样算法；远端补充之后仍重新经过该算法，不直接拿远端第一页冒充推荐。共享 `SHORT_DRAMA_FRESH_MS`（5 分钟）控制重新进入时的刷新间隔，不创建轮询器。推荐后台更新只补目录，不替换当前作品或播放位置；离场、换源和作用域结束取消工作。GetShortDramaPage 使用 refresh 请求策略，共享相同在途读取并在更新完成后隔离旧读结果。

短剧推荐刷新仅读取当前源的 SQLite 候选，再按需发现同源远端分类。详情、集数和队列准备成功后一起提交，刷新或换剧失败保留当前播放器并反馈错误，同身份重入复用已加载详情。发现过程总限时 30 秒，单次选剧详情限时 15 秒；离场取消请求、计时器和迟到提交。首次推荐以静态提示和剧场入口等待，后台刷新不清空已有画面。

`player/shortDrama/session.ts` 拥有首页与播放器共用的有界会话：进入手机视频首页即可准备首部推荐的数据与开头片段，不挂载播放器或自动出声；离开首页和短剧播放范围取消准备工作。推荐批次随机打乱，每次播放完毕或上划进入另一部短剧，不因标题点击意外切成连续剧模式；剧场路由及主动选集进入同剧顺序队列。播放期间只准备下一项：推荐模式准备下一部的详情和第一集，单剧模式准备下一集；预先确定的目标由实际翻页直接消费，预取远端页的游标仅在使用该目标时提交。队列续接保留至多前 19 项与新一批，初始远端页受 100 条上限约束，不保留无限播放历史。向下大幅拖动且没有上一项时触发推荐刷新，小幅拖动回弹；加载／空态复用同一手势模块，刷新能取消并重开在途推荐请求。

媒体预加载扩展既有 `ts-cache/prefetch.ts`，复用同一个清单缓存、来源／作品／集数身份和分片 LRU，不创建另一套缓存或隐藏解码器。首页预热一项，播放时等待当前缓冲充足且没有前台请求，再串行预热下一项 HLS 的初始化段与开头两片，单项新增媒体字节不超过 8 MiB，单次传输限时 12 秒；前台请求、切换、离场和来源变化均可取消。已完成分片仍受共享 LRU 预算约束，下一项播放通过原有 HLS loader 命中。多码率按最低带宽变体预热；字节区间流、非 HLS 不走这条整片预热路径，播放仍由原有管线负责。失败记录诊断并保留正常播放／重试路径，不把预取失败显示成播放失败。视图在视觉离场结束前保留自己的呈现身份，防止首页准备结果回写离场中的视频或历史。

竖屏翻页由 `useShortDramaPaging` 拥有跟手偏移、回弹与交接动画；单个活动播放器配合最多 720×1280 的临时 Canvas 和一份 inert 控件快照。推荐批次内翻页命中预备详情时直接消费；未命中时先提交条目身份和加载页，动画不等待详情或媒体缓冲，继续滑动可取消旧详情并选择批次内其他条目。详情失败保留重试和可用的前后项；获取新推荐批次与主动静默刷新仍保留原有有界请求策略。静默刷新才等替换媒体首帧 loadeddata，canplay 作为兼容补充，后续触摸可立即释放刷新快照。快照不导出、不缓存，不增设第二个解码器；完成、取消、失败或视觉离场结束时清理 Canvas、DOM、动画与计时器；已提交离场立即停止工作，仍可见的快照保留至离场结束，刷新媒体等待超过 15 秒释放快照并反馈超时。手势监听绑定到稳定的短剧区域，避免随视频移到屏幕外；缓冲中的媒体仍允许翻页。尊重已有页面动画开关和减少动态效果设置。手机全屏／退出入口位于进度条右侧的右下角，复用既有全屏控制器，以整个短剧区域为宿主，选集和设置保留在该区域中。

播放器恢复历史与媒体源准备并行，自动播放仍需等待历史定位决策和媒体就绪，避免先出声再跳转。既有 `playing` 首播统计保留；支持视频帧回调的宿主另记 `player_first_presented_frame`，表示 playing 后首次观察到的呈现帧，不冒充实际硬件首帧。短剧诊断记录详情准备是否命中、详情等待和临时画面捕获耗时，经共享有界队列提交元数据，不另开后台采样器。

播放恢复沿用当前媒体所有者：`PlaybackCheckpoint` 显式携带时间、正常倍速、暂停、静音和元素音量，同一集重试或切线路时应用到新媒体装载，不依赖周期落盘的历史。播放身份包含来源、作品ID和真实集号，避免不同源的同号作品共用浏览器检查点；旧本地键不删除，SQLite历史仍作为已有进度的恢复来源。同源线路先确认唯一集号；跨源先读取候选详情，核对规范化作品名、唯一集号和集名（同名单集影片允许单集对应），再发布新详情。对应不明确、候选失败、换集或离场取消时保留当前播放，不自动跳第一集。已验证换源的 router.replace 保留播放器页面实例和现场接力，普通 push／返回仍按路由身份处理。提交前等待旧源历史保存，新媒体完成定位后的有效进度立即保存到新源；开头五秒仅在显式换源时允许写入，失败反馈并限频重试。旧源历史保留，继续观看选择最新保存的来源。分片边界不同仍按媒体时间定位；片头、广告或剪辑差异不能据秒数保证同一场景，换源后提示使用已有时间输入校准。

桌面普通播放的右栏由 `PlayerInformationPanel` 展示完整源列表和集表，移除展开状态，整栏纵向滚动；移动概览／评论分组保持独立。`PlayerPanelToggle` 在媒体全屏时卸载。顶部原生拖拽命中区保留稳定 DOM，点击和双击停止向媒体操作冒泡。应用 `Sidebar` 仅导航区域滚动，底部当前来源保持固定。启动图 `SplashScreen` 将 cyan 系映射到 skyblue 图，bitblue 使用自己的资源。

HLS内部重试结束后，`playbackSource` 对一次连续网络故障最多追加两次恢复，每次等待8秒；恢复从当前时间开始，同一时刻只有一份恢复等待。暂停取消倒计时，手动跳转丢弃旧恢复，媒体替换／退出取消监听和迟到回调；重新播放优先交给该恢复所有者，普通暂停后的缓冲恢复仍走既有 `resumeStreaming`。重新出现 playing 后重置恢复预算。持续失败保留媒体现场和重试／切线入口，不自动跨源。恢复提示延迟1.5秒，失败或恢复后清除；系统 online 只用于唤醒尚未耗尽的恢复，不能当作来源已可用的证明。

短剧推荐控制复用同一会话和下一项预热：`recommendationPreferences.ts` 按来源与作品ID持久保存“不感兴趣”，最多300项；使用既有SQLite设置 `short_drama_recommendations_v1` 和串行写队列，运行时校验版本、字段和数量。写入成功才生效，失败可再次操作，原记录不删除。界面提供撤销最近一次屏蔽与恢复全部已屏蔽推荐，空候选时同样可恢复；显式剧场／单剧入口不受推荐排除影响。“追这部”进入已有单剧顺序队列，返回推荐恢复发现语义。最近观看只在本会话保留40项，实际进度事件后记入，优先选择没看过的候选；不足时允许近期内容，明确屏蔽与当前作品不放宽。过滤覆盖选剧、后续翻页及下一项预备，避免消费旧的已屏蔽准备结果。无其他候选时保留当前画面并给出说明；不无限翻远端页寻找内容。

`VideoPlayer` 的 `presentation="portrait"` 共用 HLS、平台传输、缓存和续播，装配 `PortraitPlayerControls`；手机不挂载横屏控制条，桌面在观看区宿主装配同一播放器的精简控制条。竖屏固定从正常速度播放，长按临时加速并在释放／取消时恢复；不写入横屏倍速偏好。控件渐变背景不接收指针，只有实际按钮与进度条拦截触摸，避免透明留白吞掉长按与上下滑动。`useEpisodeHistory` 持续保存 SQLite 历史（位置和总时长）并反馈写入失败。媒体初始化、等待续播确认及跳转期间不覆盖进度；详情的“继续观看”显式恢复数据库位置，普通选集仍遵守播放偏好。没有总时长的旧记录不伪造百分比。标题是不可点击的纯文本；简介去除与剧名规范化相同的重复内容，缺失或重复时不占位，最多两行并提供展开／收起，在本页限高滚动阅读；展开文本和抽屉消费自己的键盘／滚轮输入，不触发换集，手机选集紧邻简介并使用原有选集抽屉，桌面选集统一在底栏提供。进度条参考 [Vant Slider](https://github.com/youzan/vant/blob/main/packages/vant/src/slider/README.zh-CN.md) 的滑块与拖动状态，时间显示参考 [Video.js](https://videojs.org/docs/framework/html/reference/components/time)，适配既有 range 控件，不引入新组件库。手机轨道左右内缩 18px，保留 44px 触摸区及下方留白，常驻当前时间／总时长；全屏图标缩为 16px，触摸区域仍为 44px。拖动时轨道从 3px 加粗到 8px，滑块从 8px 放大到 20px；复用 `useThumbnailScrubber` 与 `CczjMobileGestureOverlay` 显示目标帧小屏和时间，不用主视频画面冒充目标位置。预览解码器仅在拖动时创建，移动端闲置两秒释放；无法解码时显示既有不可用提示。按下和移动只更新预览，释放提交，取消不跳转；换媒体与卸载释放指针捕获。

播放队列按入口区分：推荐流上下切换、滚轮／键盘及播放结束均切到不同短剧；点击剧场卡片、剧名或在抽屉选择任一集（含当前集）进入单剧队列，所有切换与自动连播只在本剧集数内进行，末集播放结束后停止并显示已播完。两端都提供返回剧场和返回推荐入口；手机通过画面上下滑动切换，选集提供可见的触摸入口，观看区不常驻上下集按钮。`getPlayerPath` 根据视频分类将短剧详情的播放入口送往 `/short-drama?source=…&vod=…&global_id=…&episode=…&line=…`；已由数据库确认分类的剧场卡片通过 `getShortDramaPath` 进入同一播放器，保留来源身份、集数索引及所选线路。直接进入时不请求推荐，也不改写全局当前来源。隐藏再进入保留本次选集，换具体剧目或返回推荐时取消旧请求。

短剧选剧列表、空态及加载失败状态沿用应用主题；已提交的实际播放场景让手机底栏与系统底部区域复用播放器深色板，切回剧场或离场恢复主题。已有播放内容时，`player/shortDrama/presentation.css` 仅为观看区和 Teleport 选集／简介抽屉提供局部中性色板，不修改全局主题变量、启动背景或用户偏好。`shortDramaScene` 只负责选剧／播放状态与导航布局。播放器抽屉将普通属性转发到遮罩根节点，使传入的局部色板在 Teleport 后仍然有效，其他抽屉保留原来的背景默认值。

桌面视频保持 9:16 并优先铺满可用高度，两侧使用当前视频整帧的低分辨率虚化背景，最长边不超过 160px；背景复用播放器进度和 media-ready 回调，最多每 400ms 绘制一次，不新建解码器、定时循环、图片导出或网络请求。切换媒体清空旧画面，页面离场、不可见或文档隐藏时停止绘制，画布随观看场景移除。背景通过固定的饱和度、亮度和遮罩控制对比，不读取或导出视频像素。桌面剧名与最多两行简介／当前集数收在观看区左下方，细进度条横贯桌面底部状态栏上沿；状态栏复用 `PlayerDesktopControls` 的精简模式，提供播放／暂停、时间与时间跳转、队列状态、唯一的桌面选集、画中画和全屏入口；播放时复用 `usePlayerChrome` 的三秒闲置隐藏，鼠标接近观看区底部恢复；悬停底栏、暂停、操作时间输入或展开场景面板时保留。标题简介独立保留位置，不随底栏隐藏而上下跳动。按最初的竖屏交互要求，不挂载清晰度、倍速及音量面板，视频高度预留状态栏空间，上下切换与收藏／简介沿右侧同一竖轴排列，按钮明确标注上一部／下一部或上一集／下一集，避免仅靠页头解释队列语义。底部时间轨道与状态栏连续排列，保留桌面鼠标命中区；精简控制条提供静音／取消静音按钮，不额外挂载音量或倍速面板。`VideoPlayer.portraitControlsHost` 允许把同一份竖屏底部控件 Teleport 到观看区，拖动状态和媒体控制仍归原播放器所有；长按提示、中央暂停入口仍在视频内部。全屏复用既有呈现控制器，通过可选的 `fullscreenRoot` 指向整个短剧观看页，使底栏与选集抽屉留在全屏内；画中画仍由原媒体实例及平台适配持有。桌面右侧提供收藏与简介入口；简介在同一观看页复用抽屉，抽屉内提供完整详情导航，收藏复用 `usePlaybackFavorites` 和收藏夹弹窗，显式使用播放来源身份。弹窗挂载到独立的观看页覆盖层宿主，避免 Teleport 到其自身父节点时破坏视图切换锚点。两端页头覆盖渐变保护层。桌面页头只保留当前推荐／追剧模式，当前集号及总集数放在左下信息区，避免相同状态在页头重复。桌面观看时页头与右栏降低不透明度，悬停或键盘聚焦恢复；与底栏显隐分开，暂停和抽屉打开时保持清晰。小字号元数据、工具栏与操作标签使用系统 UI 字体，作品标题保留应用字体。移动端保持底部信息和进度条在画面内，保留底栏，实际竖屏内容填充观看区，横屏内容仍完整显示。移动端时间常驻进度条下方，拖动时另外显示目标帧预览与时间，媒体就绪后暂停才显示中央继续播放入口。滚轮连续事件限制为一次切换，上下方向键／PageUp／PageDown 按当前队列切换；与普通播放器共用 `useDesktopPlaybackKeys` 的文档监听和作用域，空格／K 暂停，左右键跳转，F 全屏。弹层、表单和原生按钮键盘激活保留自己的输入行为，离场解绑。全屏宿主包含同一状态栏，不额外挂载另一套控件。桌面抽屉限制在观看区内，展开时视频、底部信息与进度条一起让位。

设计参考：[抖音短剧入口](https://www.douyin.com/series)、[官方短剧播放器文档](https://developer.open-douyin.com/docs/resource/zh-CN/mini-app/develop/industry-plugin/playlet-plugin/playlet/playlet-extension)。官方文档明确支持透明导航、播放进度续播和选集事件；其同剧集滑动语义属于小程序官方播放器，不能据此声称抖音所有推荐页都采用同一流程。本项目按用户确认的入口语义区分推荐流和单剧队列。公开历史截图仅用于信息层级参考，未作为当前抖音原生 App 的逐项实测结论。

选集每 20 集分组，使用来源真实集数，支持正倒序和定位当前；分组用文字与下划线区分，集数网格通常五列、窄屏自动减少列数，当前集用主题色描边和小圆点标记，完整的有意义集名在网格上方补充展示。抽屉固定保留剧名、计数和分组，网格独立滚动；移动端初始高度为视口的 72%，可通过既有拖动手柄调整。自动定位只横向移动分组条，纵向滚动由集数网格承担，避免定位标签后再次遮住当前集。

浏览器替身验收运行 `python scripts/test-short-drama-browser.py --channel chrome`（需要 Python Playwright 和 FFmpeg），覆盖桌面／移动布局、真实竖屏媒体、长按、进度条、80 集长标题抽屉、320px 窄屏当前集完整可见及文字不溢出、页面离开与重入、空状态和请求失败重试。独立 acceptance APK 的原生验收运行 `python scripts/test-short-drama-android.py --device emulator-5554`，复用既有 Android CDP／绑定入口，验证真实 CMS 导入、Go 推荐、HLS 播放、触摸、SQLite 进度与返回键；测试创建的临时来源和设置会在结束后清理／恢复。截图与测试媒体输出到 `dist/audit/short-drama`。

短剧页每次激活重新核对视频来源；来源移除后清空旧队列、错误和播放身份。保留的推荐条目在详情返回 `NOT_FOUND` 时重新查询目录一次；恢复仍失败则展示错误，重试重新取推荐，不循环请求已退役 ID。显式单剧入口保留其身份，不自动跳到别的作品。原生针对性回归可加 `--catalog-recovery`：实际播放后清空该测试源目录，检查空态，重新导入并重试播放，再删除来源检查无源状态；清理阶段同步刷新来源 store，避免留下测试源引用。

## 小说发现与阅读

小说书城与搜索共用 `novel/components/NovelBookCard.vue` 的列表变体，展示来源实际提供的简介、题材、状态和字数。书城保留真实频道及分页，首页显示有限预览；最近续读使用既有按来源保存的进度。频道读取失败在原页重试，保留已读取内容，不通过重载应用恢复。布局参考七猫实际书城的信息层级、作品详情参考掌阅的元数据顺序，界面继续使用 CCZJ 控件与小说主题；本地研究证据见 `reports/reading-redesign-2026-10-21.md`。

移动书城从搜索框开始，不挂载重复的品牌／搜索顶部栏；书架、设置及详情保留各自导航。桌面书城仍由宿主顶部导航提供页面切换。书源整改以[产品与路由设计](reading-source-routing-design.md)为目标，当前供给分层、候选折叠、正文预验与尚待设备验收的范围见[实现记录](reading-source-routing-implementation.md)。

`library/source_evidence.go` 统一解释独立操作证据，源页不以搜索成功覆盖正文状态。`NovelPrepareSourceSwitch` 拥有小说恢复候选的身份核对、章节对齐和目标正文验证；前端最终选择使用准备结果，准备失败不导航。`novel/searchGroups.ts` 仅派生显示关系，继续使用现有来源自然键和 SQLite 阅读进度；书城、搜索及恢复保持各自加载和取消所有权。

书城与找书共用 `room-search` 组合输入框，焦点由外框表达；手机找书页使用宿主标题，避免重复标题挤占结果。共享搜索状态行使用 `Spinner` 的 `inline` 模式，整页加载继续使用默认留白。频道节点使用频道身份作为 key，查询参数切换不应停用当前页面的请求所有权；封面加载不重复淡入。

阅读偏好首层提供亮度、字号、背景和翻页方式，字体排版与键盘／滚轮选项使用可展开分组。设置页保留正文预览；阅读中的字号、行距、字体、边距及流式／分页切换会尝试记录当前可见文字并在重排后恢复，无法取得文字锚点时沿用章内比例。这是同一章节内的重排定位，跨重启进度仍由既有章节身份与比例恢复。日间背景偏好随现有 SQLite 阅读偏好保存，旧记录默认纸色；切回日间时恢复上次的纸色或白色。

小说阅读的左右触摸区包含正文外侧留白：分页模式翻一页，滚动模式移动一屏并保留部分重叠，只有到章界才切章；菜单两端使用同一翻页行为。中间呼出菜单，选字、操作控件及设置面板优先消费事件；关闭面板不向正文传递点击。阅读入口不与正文首轮排版同时执行页面滑动和入场动画扫描，相邻章预取在首轮绘制后启动，切章及卸载会取消尚未启动的工作。

`reading/chapterPrefetch.ts` 保留上一章和下一章的完整读取结果及在途请求，已完成结果保留最多 60 秒；`take` 将请求及取消所有权交给当前章，避免切章重新等待上下文 IPC。目录刷新、换书及离开阅读会清理未交接的缓冲。`NovelReader` 最多保留三章的排版节点，当前章与相邻章按章节身份复用，正文交给 `NovelChapterText`，减少进度变化时重新生成段落。此窗口沿用原有正文缓存和五路网络预算，不持久化第二份进度。

正文磁盘记录单独保存 `checkedAt`，读取只更新存储层的最近访问时间，不将缓存命中写成新的源站成功观测。旧记录仍可读取，缺少验证时间或超过 24 小时标为 stale；阅读器首轮排版后尝试刷新当前章，同一次阅读失败后至少间隔五分钟才自动重试，设置中可主动刷新或取消。`LibraryRefreshChapterText` 与普通读取复用按来源规则／作品／章节身份的 workflight，绕过磁盘正文；失败不清空原文，带异常警告的新内容不能覆盖原本无警告的缓存。刷新结果核对章节身份和请求代号，等待翻页交接结束后再提交；内容相同不重排，变更时尝试恢复原段落及字符锚点，找不到锚点时保留比例。切章和离场取消刷新。相邻章页数缓存同时检查正文对象及视口／字号／字体／行距／边距签名；字体装载完成会重新排版，不清除正文或进度。自动缓存仍不是用户主动离线下载。

横向分页参考已核实的起点 7.9.480“左右”模式，使用整页覆盖：前翻移动当前页、下一页固定在下；后翻让上一页从左侧覆盖当前页，移动页右缘带阴影。页眉、正文、留白及页脚同属一张页面，阅读控制栏保持独立覆盖层；CSS 文字分栏仍承担实际排版，不再以整条正文列的平移充当翻页动画。滚动阅读保留原有滚屏行为，不改变用户已保存的翻页方式。

收起菜单时的返回入口与本章名称同属页面页眉，随整页翻动。起点 7.9.480 的 REA `ReadHeadView` 构造方法及 APK 布局 `view_read_page_head` 表明：16dp 图标后紧接 12dp 标题，返回点击由独立的 48dp 宽透明热区承接，标题没有绑定返回操作；ADB 界面树也确认图标与标题边界直接相接。本项目复用现有 Icon 和 Button，将图标设为 16px、标题使用 12 号工具类，移除额外间距；返回热区独立覆盖在页眉左侧，宽 48px、高 44px，不挤开标题或将整段标题变成按钮。长标题可省略，后台预备页与翻页临时页保持 inert；翻动期间不执行返回，避免点中临时页下方的真实入口。展开菜单后的工具栏仍由独立控制层持有，桌面沉浸时保留返回入口。原始 REA 输出与资源解码保存在本地 `.cache/novel-research/qidian-head-*`，竞品运行证据位于 `dist/audit/novel-continuity/qidian-reader-head.xml` 及既有左右翻页中间帧。

`useNovelPageTurn` 拥有跟手、松手速度、反向回拖、回弹和提交交接；同一时刻最多两张 inert 的临时 DOM 页面，只在翻页期间存在，不挂载另一份 Vue 阅读器或发起额外正文请求。页面克隆包含该章已排版的正文节点，以保留浏览器真实分栏几何；它不是只复制当前屏文字的逐字分页。完成、取消、尺寸变化及退出均释放页面、动画与帧回调。章内与已预备相邻章复用同一动效，跨章提交等新正文定位后再撤去临时页；排版设置、目录跳转和离场作废旧交接，期间的连点不排队跨多页。

短滑和明确反向回拖回到原页，手势取消及多指触摸不提交；鼠标拖动由阅读区持有指针捕获。未准备好目标正文时不拖出空白预览，达到翻页意图后沿原有读取链加载，保留当前正文和原页脚位置，正文就绪后再覆盖交接。超过 180ms 才显示等待提示，失败进入已有错误与重试／换源流程。持续时间跟随动效偏好，减少动态效果时直接定位。上述新行为经过编译和静态检查后，仍由用户进行 MuMu／真机验收；不能以构建成功声称与起点的手感已完全一致。

首次拖动的临时页先完成分栏布局，再以极低不透明度在原页上方预绘制两帧，随后呈现并跟随最新手势位置；松手早于预绘制时等待同一准备过程，取消会释放等待和帧回调。加载后才启动的跨章覆盖在准备期间保留真实旧页，避免新章先闪现再回到旧页动画。此调整针对用户报告的拖动首帧闪烁，是否消除设备上的闪烁仍需用户实测确认。

逐作品候选缓存入口为 `NovelBookSources` 和 `useNovelSourceCache`。详情与换源面板先读 SQLite，再发静默复核；正文读取的成功／失败直接更新相同持久化记录，不另建浏览器缓存。缓存寿命、5 分钟冷却、三站点补充目标及样章验证范围见[来源实施记录](reading-source-routing-implementation.md)。

Android 小说阅读通过 `platform/readingSystemBars.ts` 和宿主桥接独立持有系统栏沉浸会话，不请求视频全屏、不改变屏幕方向。进入隐藏系统状态栏与导航栏，退出释放；宿主根据会话身份防止旧页面恢复新阅读页的系统栏，焦点恢复时重新应用。阅读面板保持同一会话，避免打开面板改变视口和页码；屏幕切口仍由原生 inset 所有者处理。阅读控制栏通过透明度与位移进出，隐藏完成后才设 visibility，隐藏期间立即 inert，避免键盘进入不可见控件。

## 漫画发现与图片加载

漫画发现页复用快看 8.25.0 实际观察到的搜索、续读和内容分层思路，保留 CCZJ 的来源身份与主题。手机发现页不重复挂载品牌／搜索顶栏；已有进度的续读先于推荐展示。`ComicHome` 先读取一个频道供首屏推荐使用，其余频道在所属区域进入滚动容器外扩 320px 的范围后再请求，显式选择频道则立即读取。骨架保留未请求区域的几何，避免空区域挤在首屏一起触发读取；隐藏取消实际请求并作废迟到结果，频道初始化失败在原页重试。

漫画图片继续由既有 Go 图片代理持有 256 MiB／最后访问后三天的磁盘缓存，页表继续使用 `comicReaderSession` 的 12 条／2 MiB／五分钟热缓存，不另建浏览器持久缓存。`ComicReader` 在当前图片完成加载并让出两次绘制回调后，复用 `reading/chapterPrefetch.ts` 按阅读边界预取相邻话的上下文和页表，最多两份、完成后保留 60 秒，不提前下载整话图片。切话交接请求及取消所有权，离场、隐藏、目录显式刷新和图片地址刷新取消未交接缓冲。靠近作品末尾的目录更新先于相邻话预取，避免旧目录上的预取被立即推翻。

条漫滚动每帧最多处理一次定位，复用当前话的图片节点引用并二分查找；切话、页表刷新和阅读方式切换清理引用，图片高度仍从当前 DOM 读取。页状态只遍历新旧预取窗口，同一页内滚动不扫描整话；仍只挂载当前页前后各两张图片。窗口外不创建隐藏的加载／错误控件。上述为实现边界，设备上的滚动、快速跳页、切话、失败重试、返回恢复和后台重进仍由用户在 MuMu／真机验收。

## CCZJ 工具类与组件

先选公共组件，再组合工具类，最后补领域样式：

缺少可复用样式时扩展 `cczj-utilities.css`，在真实调用处使用并重新生成手册／编辑器数据；单个页面独有的排版、配色和动画留在领域样式。新增类先检查同义定义，不维护平行工具库。

```vue
<script setup lang="ts">
import { Button } from '../components/ui'
</script>

<template>
  <div class="toolbar cczj-flex cczj-items-center cczj-gap-4">
    <Button variant="primary">{{ $t('common.save') }}</Button>
  </div>
</template>

<style scoped>
@media (max-width: 640px) {
  .toolbar { @apply cczj-flex-col cczj-items-stretch; }
}
</style>
```

基础布局不再写第二份 `display:flex` 或 `align-items:center`。`@apply` 在构建时按原位置展开，保留 scoped、媒体条件和声明顺序。阅读分页尺寸、特定排版、模式主题和关键帧由对应领域保留，不能为了通过检查抹平设计。

样式检查会拒绝存在精确公共等价类的显示、对齐、方向、换行、收缩、溢出、盒模型和零最小尺寸声明。尺寸、颜色、关键帧与 `!important` 不作机械替换；这不代表允许用它们绕过复用。所有尺寸的实际值查[速查表](cczj-utilities-cheatsheet.md)。

## 更新提示与下载状态

桌面与 Android 的启动装配均在每次进程启动后执行一次 `update.Service.StartupCheck`，同意条款后才出网；不是每日只检查一次。关闭弹窗仅抑制当前文档重复提示，保留后端本次发现结果；只有“跳过此版本”写入 `CCZJ.SettingIgnoredVersion`。下一次启动重新查询，仍有未跳过新版本时再次提示；离线或检查失败不保证发现更新。手动检查保持原有主动检查语义。

后台下载与弹窗共用 `stores/updateState.ts`。最小化后，`UpdateModal.vue` 通过 Teleport 展示常驻应用内进度小窗，桌面／Android 均可查看连接、百分比、大小、速度、校验、完成或失败并点击展开；失败在详情中重试。小窗尊重全局反馈动效开关和移动安全区。安装继续使用既有桌面安装／退出安装与 Android 系统安装入口，不在移动端显示桌面退出安装行为。实际进程退出或系统终止会停止当前任务，应用内浮窗不代表系统后台服务。

## 诊断证据与轻量采集

桌面转场在路由提交后记录 `visual_transition`：挂载准备耗时、一次 requestAnimationFrame 回调等待和过渡完成／取消时长。它不逐帧采样，也不等于呈现帧率；页面离场和舞台销毁取消待执行回调。启动快照附带 `ui_build` 编译时间标识，用于区分同版本号的本地包，不采集运行文件路径。全局异常仅保存规范化 API 码或白名单错误类型，ErrorEvent 附数字行列，不采集错误文本、堆栈或 URL；明确取消的 Promise 拒绝记录为生命周期取消，不计作操作失败。短剧本地读取失败与源空列表事件同时登记到后端允许列表；新增前端事件须核对该允许列表，不能只看前端入队成功。

诊断使用三个独立层次。日常事件只保留本机内存窗口；用户开始复现后才记录低频资源和事件文件；CPU、堆、协程剖析通过独立按钮按需抓取。视频、小说、漫画及桌面、移动设置入口复用 [DiagnosticSessionPanel.vue](../frontend/src/components/DiagnosticSessionPanel.vue)，不为每种宿主另建记录器。

- [diagnostics/session.ts](../frontend/src/diagnostics/session.ts) 是无 API／Pinia 依赖的元数据队列，最多 128 条、128 KiB，单条不超过 2 KiB；按 2 秒合批，一批最多 32 条、60 KiB。提交超时 5 秒，连续三次失败丢弃该批并保留计数；重试沿用事件 ID，由后端当前窗口去重。队列为空不保留提交计时器。设置 Key 为 `CCZJ.Settings.diagnosticsCapture`／`CCZJ.SettingDiagnosticsCapture`，保存成功才更新运行状态；关闭日常记录会清除当前内存窗口，显式复现可继续记录。
- 操作入口记录固定控件语义，不读取标签文本、输入值或链接地址。导航记录声明的路由模板；[requestPipelines.ts](../frontend/src/api/requestPipelines.ts) 保留前端请求／合并飞行身份，[requestChain.ts](../frontend/src/api/requestChain.ts) 给组合操作中后续请求补显式关系。诊断自身的 RPC 不再次记录。普通异步任务不继承已经结束的点击分发身份。
- [app/diagnosticrecording](../app/diagnosticrecording/) 保留最多 2048 条、1 MiB 序列化事件数据；这是数据预算，不是进程 RSS 上限。录制写入队列最多 128 条，生产者不等待文件 I/O。复现每 15 秒采样，3 分钟、10000 条写盘事件或 32 MiB 时结束；停止会排空可写队列并报告丢弃。自动原始录制保留最近 5 份，剖析保留最近 3 份；用户主动导出的 JSON、摘要和 ZIP 不自动删除。
- 原生资源面板仅在可见时每 15 秒轮询，与后台录制共用 10 秒有效的 [resource.Sampler](../app/resource/snapshot.go) 快照。日常观察不扫描 DOM；展开资源覆盖详情时才抓取一次附着元素计数。录制中的前端数值快照每 15 秒一次，支持 Long Tasks 的宿主仅在录制期间连接 PerformanceObserver；其他宿主报告能力缺失。
- CPU 剖析最长 15 秒，堆与协程为单次抓取，每个文件最多 8 MiB，不强制 GC。应用生命周期拥有剖析；退出或模式切换会取消，已完成文件按所属模式加入导出。它们只观察 Go 宿主，不能代替 WebView／解码器／GPU 性能证据。
- [diagnostics_export.go](../app/service/diagnostics_export.go) 导出格式 v2：过滤后的元数据 JSON、可读摘要、已经关闭的录制 JSONL 与最多三份已完成剖析，ZIP 源数据累计最多 64 MiB。普通导出不额外采集剖析，不自动上传。录制期间导出是窗口快照，包含 `recording_active_snapshot`，不把仍在写入的 JSONL 打包为完整文件；结束后再次导出包含关闭后的文件。

导出会过滤 URL、具体路径、请求参数、作品内容及自由文本日志；原日志页面仍保留详细日志。格式只保证固定元数据，不将任意新增字符串字段自动暴露给报告。窗口淘汰、前端排队／提交失败、后端拒收和写盘丢弃分别记录；进程突然结束可能丢失最后一批，日常窗口不跨进程恢复。

规则提示是观测条数，不是去重故障数或自动根因诊断。前端操作／请求拥有明确身份；Wails 成功返回目前没有公开的后端调用身份，仅结构化错误的请求号能够准确跨层关联，其他后端操作保留独立证据。`at_ms` 为后端接收时间，`client_at_ms` 为独立前端时钟，不按相近时间拼接因果。导出窗口与界面最近 200 条查询范围不同；界面只展开最近 40 条，完整导出仍受全局窗口上限约束。开销、页面往返后的资源释放及设备支持情况由用户进行设备对照验收，编译与静态检查不能证明无泄漏或零影响。

## 开发与检查

架构总图可以在根目录运行 `python scripts/build-architecture.py --validate` 重建到 `dist/audit/architecture/index.html`。结构扫描、人工功能链与画布模板保存在 `scripts/architecture`；生成器会拒绝未归入功能链的公开绑定。图示为同一可缩放／平移画布，并提供模块依赖、生命周期及数据归属视图。交互验证需要现有浏览器测试依赖和 Chrome；`--channel` 可选择已安装的浏览器通道。图上的覆盖数量表示结构清单，业务验证结论以整改台账为准。

Go 版本以 `go.mod` 为准（当前 1.26），Node 以 `frontend/package.json` 的 engines 为准（推荐 24）。Go 与 JS 的 Wails 版本保持一致，当前均为 3.0.0，CLI 同步为 3.0.0；本机 Go 1.26.2 保持不变。

### 依赖升级候选与兼容边界

2026-10-31 已将 Wails Go 模块、JS runtime 与 CLI 同步到 3.0.0，hls.js 固定为 1.7.4。`go.mod` 最低版本仍为 1.26.0，本机工具链仍为 Go 1.26.2；依赖解析仅将 Wails 必需的 x/image 提升到 0.45.0，其余候选保持原版本。本节区分当前实现与未实施候选；编译、类型与打包检查不能代替设备行为验收。

| 优先级 | 当前实际版本 → 候选 | 依据与联查范围 |
| --- | --- | --- |
| 保持 | Go 1.26.2 | `go.mod` 的 1.26.0 是最低要求，Wails 3.0.0 可由当前工具链编译。本轮不升级 Go；后续工具链维护独立安排。[Go 发布记录](https://go.dev/doc/devel/release) |
| 已升级 | Wails Go／JS／CLI 3.0.0 | 官方明确建议 beta.30 及以下升级，另修复 Windows 菜单和 `applyDarkMode` 崩溃。同步 Go 模块、前端 runtime、CLI、生成绑定及 Android 构建副本，不能只改 `go.mod`。[官方发布说明](https://github.com/wailsapp/wails/releases/tag/v3.0.0) |
| 已升级 | hls.js 1.7.4 | `package.json` 和锁文件均精确固定，替换原 canary；主模块与 worker 从同一包构建。共享缓存 loader 实现官方 `Loader<LoaderContext>`，配置和回调接受类型检查；主播放和缩略预览共用此契约及 worker 配置。缓存、预取、拖动、音轨、错误恢复及销毁仍需设备验证。[官方发布记录](https://github.com/video-dev/hls.js/releases)、[迁移说明](https://github.com/video-dev/hls.js/blob/v1.7.0/MIGRATING.md) |
| 优先 | vue-i18n 9.14.5 → 11.4.16 | v9 已停止维护，官方审计命中 `vue-i18n/@intlify` 同一依赖链的 4 个包，不是 4 个独立漏洞。已有 `legacy:false`，仍需核对插值、复数、回退与语言切换。[维护状态](https://vue-i18n.intlify.dev/guide/maintenance)、[安全公告](https://github.com/intlify/vue-i18n/security/advisories/GHSA-83rm-6c8r-g728)、[v11 迁移](https://vue-i18n.intlify.dev/guide/migration/breaking11) |
| 常规补丁 | Vue 3.5.42 → 3.5.44；Vite 8.3.4 → 8.3.5；PostCSS 8.5.30 → 8.5.32；vue-tsc 3.3.13 → 3.3.14 | 按工具链组合分批核对，不跟随 RC／alpha；保留现有 WebView 语法目标。[Vue 发布记录](https://github.com/vuejs/core/releases)、[npm 官方元数据](https://registry.npmjs.org/) |
| 配套评估 | brotli 1.2.1 → 1.2.7；x/net 0.61.0 → 0.62.0；x/image 0.45.0 → 0.47.0 | Wails 必需的 x/image 0.45.0 已配套升级；更高版本和其他候选尚未实施，不把所有可更新版本都称作已命中的安全问题。[Go 官方代理](https://proxy.golang.org/)、[Wails 依赖说明](https://github.com/wailsapp/wails/releases/tag/v3.0.0) |
| 独立兼容工作 | SQLite 1.52.0 → 1.61.0；Pinia 3.0.4 → 4.1.0；Router 4.6.4 → 5.4.0；TypeScript 5.9.3 → 7.0.2 | 有新版本不代表立即切换。SQLite 与 libc 配套，联查原地迁移、备份、取消和 Android；状态库／路由／类型工具按真实收益评估，不为减包盲目换大版本。[SQLite 文档](https://pkg.go.dev/modernc.org/sqlite)、[Pinia 4](https://github.com/vuejs/pinia/releases/tag/v4.0.0)、[Router 5](https://github.com/vuejs/router/releases/tag/v5.0.0) |
| 保持 | markdown-it 15.0.3、plugin-vue 6.0.10、Vitest 5.0.5、sqlx 1.4.0、cascadia 1.3.5、x/sys 0.49.0、x/text 0.43.0、go-pinyin 0.21.0 | 核对时已为相应模块／主版本路径的 latest；没有凭版本号更换的必要。`@types/node` 配合实际 Node 24，不机械追 Node 26 类型。 |

Android 的 `security-crypto:1.1.0-alpha06` 在 `WailsBridge.securePrefs()` 确实使用，可评估稳定 1.1.0；其 API 已弃用，长期迁移平台 Keystore 时必须保留 `wails_secure` 旧密文兼容，不能直接删库或换 Key。[官方 Security 说明](https://developer.android.com/jetpack/androidx/releases/security)。Webkit 1.9.0 → 1.18.0、AppCompat 1.6.1 → 1.8.0 可排期核对 minSdk／compileSdk／AGP 和真实宿主能力；Webkit 兼容库升级不会替用户升级系统 WebView 内核。Biometric 1.1.0 仍是稳定版本，不追 1.4 alpha。[Webkit](https://developer.android.com/jetpack/androidx/releases/webkit)、[AppCompat](https://developer.android.com/jetpack/androidx/releases/appcompat)、[Biometric](https://developer.android.com/jetpack/androidx/releases/biometric)。Material 依赖是否必要还需查 Gradle 传递关系和资源引用，不能仅凭没有直接 import 就删除。

Wails 3.0.0 的移动端仍属实验性支持；官方披露 Android bridge 的 frame 可见范围及其他 iframe 限制，升级不能替代不可信内容隔离。本项目未检出 iframe 模板，Markdown 使用 `html:false`，但这不是完整漏洞可利用性证明。`prepare-android-runtime.mjs` 对结构化错误、取消交接和 Android 退出清理有严格源码补丁；3.0.0 的源码对比确认这些补丁仍需要，上游 HTTP 错误包装修复不等于 JNI 已保留 CallError。副本身份同时包含 SDK 版本、模块清单与修补后源码，避免不同版本共用旧副本。现有更新器、设备控制、文件选择与分享继续由所属模块管理，不因框架提供同名功能就替换。

npm 官方审计原始结果、官方版本查询和临时符号分析保存在 `.cache`。本机 npm 镜像没有 audit 接口，调查使用单次 `--registry=https://registry.npmjs.org`，没有修改全局配置。未安装或执行 Go 全调用漏洞扫描，不能将上述清单称为完整安全审计。

### 安装包体积与减重入口

以下是升级 Wails／HLS 之前的本地 2.5.0 成品静态基线，单位 MiB；压缩实验只处理内存字节，不改源码、不替换成品、不运行设备。

| 项目 | 测量值 | 结论 |
| --- | --- | --- |
| Windows EXE／便携 ZIP | 48.56／24.73 | Release 已使用 `-s -w`；缩短变量名、注释或删除 node_modules 不会继续减小已生成二进制。 |
| Android APK／其中 Go so | 53.75／48.51 | so 占 APK 约 90.3%，仅一个 arm64 ABI；没有再塞一份完整前端 assets。优先缩减被 Go 嵌入的内容。 |
| 前端资源总计 | 13.76 | 图片 10.35，JS／CSS 3.40；图片是最大前端热点。 |
| 三份大 JSON | 原始 8.04，gzip 后 1.02 | `quality/bulk.v1.json` 及小说／漫画源目录压缩实验合计减少 7.02；仍是业务真实消费者，不应当作无用验证材料删除。 |
| JS／CSS 文本 | 原始 3.40，gzip 1.05，Brotli 0.90 | Brotli 实验少 2.50，但需要构建嵌入、AssetFS／响应头、Android JNI asset 路径和 worker 联动适配。Vite 日志的 gzip 列不是实际嵌入大小。 |

建议按以下顺序实施，并在每一步重新测量原始 EXE、ZIP、APK 与安装后占用，不把资源节省量直接承诺为全部成品降幅。

1. 大 JSON 已实施按需解压：`quality/bulk.v1.json`、`source_library_novel.json`、`source_library_comic.json` 保留原文，仅 `.json.gz` 嵌入程序；三份原文合计 8,432,026 字节，gzip 共 1,079,607 字节，内嵌数据减少 7,352,419 字节。读取复用 `payload.UnpackBytes`，语料解压上限 64 MiB、源目录 32 MiB，校验 gzip 完整性并继续原有 JSON／领域解码；解压临时字节不进入模块缓存。维护操作使用可取消 `AuditCorpusContext`，兼容入口 `AuditCorpus` 保留。`audit_catalogs/*.gz` 已压缩，不再包一层。
2. 图片已由资源维护流程替换，保留当前文件名和主题指纹恢复契约；前述图片体积只代表旧基线，不代表替换后的资源。图片视觉质量与透明度由用户验收。
3. JS／CSS 不增加 gzip／Brotli 存储或解码流程，沿用现有生产 minify、tree shaking、懒加载和独立 worker。
4. Android R8 与资源裁剪。Release 的 `minifyEnabled` 当前为 false，DEX 10.19 MiB（APK 中约 3.78 MiB），Android 资源原始 1.59 MiB，是进一步候选。启用 R8／shrinkResources 前补齐 JNI、`@JavascriptInterface`、反射与动态资源 keep 边界；桥接、安装、文件选择、权限和退出需设备验收，不能只看构建通过。[官方优化说明](https://developer.android.com/topic/performance/app-optimization/enable-app-optimization)。

`libwails.so` 当前在 APK 中不压缩，便于直接映射。静态 gzip 实验约为 24.07 MiB，但改成压缩 so 会改变下载、提取占用与启动路径，不能只为 APK 数字更小就直接切换。[Android 体积说明](https://developer.android.com/topic/performance/reduce-apk-size)。符号表中的 32 MiB 加密缓冲属于 BSS，不占同等文件字节；符号别名也会重叠，不能把 nm 大小直接累加为安装包来源。SQLite 等依赖的功能替换不列为首批减重方案。

HLS light 构建会裁掉字幕、多音轨、DRM 等能力，不能用它冒充无损减包；本项目主播放、字幕与预览需一起核对。[hls.js 官方说明](https://github.com/video-dev/hls.js)。Go 链接器 `-s -w` 只移除符号与调试信息，不移除运行时必需元数据。[Go 链接器](https://pkg.go.dev/cmd/link)。

已完成 Go 应用编译、`go vet ./...`、稳定版绑定生成（无警告，226 个方法）、前端类型／生产构建、依赖边界／语言键位／样式／文档静态检查，以及 Windows EXE／便携 ZIP 和 Android ARM64 Release 打包。版本、既有签名、不可调试标记和 SHA256 校验通过，前端主 HLS 模块与独立 worker 均包含 1.7.4 版本。未运行自动化测试或操作设备；实际启动、请求取消、播放、拖动、切线、弱网恢复、缓存／字节区间、预览与退出仍由用户验收。

升级基线保留 `dist/releases/v2.5.0` 的原始本地成品和 `dist/audit/wails3-hls174/packages/v2.5.0` 的验证包；这些产物没有作为正式版发布。当前预发布使用 `v2.5.0.beta1` 标签和 `dist/releases/v2.5.0.beta1` 产物，版本隔离与发布流程见[预发布版](releases.md#预发布版)。验证期间前端构建与 Go 嵌入必须串行：Vite 会替换带 hash 的 dist 文件，并行读取会产生缺失文件或不一致包。JSON 按需解压已实施；JS／CSS 不做进一步预压缩，图片使用当前已替换的文件。

压缩维护入口为 `node scripts/compress-embedded-json.mjs`；`--check` 验证 gzip 解压后与原始 JSON 逐字节一致。脚本校验 JSON 和容量，仅在内容变化时原子替换 gzip；Windows／Android 打包及 Taskfile 生成资源，静态检查拒绝陈旧压缩副本。直接 `go build` 不自动执行生成器，修改原文后先运行生成入口或 `go generate ./app/library`。原始 JSON 与 gzip 同步纳入版本管理，但只有 gzip 被嵌入；不写用户目录、不改变 SQLite、Key、来源身份或目录导入格式。

JSON 按需解压会增加首次读取的 CPU 与临时内存；容量和取消沿 `payload` 处理，未建立全量解压常驻缓存，实际设备开销仍待用户验收。

### 生成事件契约

beta.33 的事件变化对应 [PR 6307](https://github.com/wailsapp/wails/pull/6307)：新增可导入的 `bindings/events/index.js` 事件名常量，并引入原有载荷创建和类型扩展；不是新建一套事件总线。只发现直接调用 `application.RegisterEvent[具体类型](编译期常量名)` 的注册，动态名字与泛型包装不能靠生成器自动发现。beta.30 已有载荷类型生成与注册校验，beta.33 主要补齐可导入入口。

当前业务代码未注册事件，最近一次绑定生成报告为 `0 Events`。事件结构应由所属领域拥有；只有在具体调用链存在契约重复维护或类型漂移、且生成入口确实降低维护成本时，才采用类型化生成，不因框架提供新能力而全项目强制迁移。采用时在 Wails 装配处直接注册一次，经 `frontend/src/api/events.ts` 适配生成入口，页面不导入生成目录，并保持现有事件名和 JSON 字段兼容。编译期类型不能替代运行时校验；request／operation／mode／task 身份、迟到结果过滤、进度合并和订阅解绑仍由应用管理。不要为每个事件机械添加所有身份字段，按真实任务和会话语义决定。

```powershell
npm --prefix frontend ci
wails3 dev -config ./build/config.yml -port 9245

npm --prefix frontend run check:css-reuse
npm --prefix frontend run check:css-data
node scripts/check-document-links.mjs
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/verify.ps1
```

修改公共工具定义后运行 `npm --prefix frontend run gen:css-data`，随后校验生成文件。速查表纳入 Git；`.vscode` 下的编辑器数据和个人设置仅本地保留，检查允许全新检出时缺少编辑器数据，但会核对已有数据是否过期。绑定签名变化后运行 `scripts/generate-bindings.ps1`，维护请求策略，前端调用继续经 `api`。

界面变更检查桌面和移动的实际控件、长文案、空状态、已有数据、重复进入以及操作后新增状态；涉及手机文件、权限、全屏、分享和安装器时运行原生验收。可先使用浏览器替身定位问题，但单独报告其验证范围。Android 命令见 [Android 开发](android.md)，打包见 [发布流程](releases.md)。

## 清理生成物

`scripts/clean-generated.ps1` 默认只预览已识别的旧构建、临时截图／日志、Android 中间产物与下载的临时编译工具；传 `-Apply` 才删除。它校验目标仍在工作区内，拒绝包含已跟踪文件的目标，跳过正在运行的程序。保留发布目录、最新测试包、当前验收证据、依赖环境、源配置、原始交接包及 `bin/data` 旧数据；不会把 `frontend/build` 插件源码当作构建产物。

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/clean-generated.ps1
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/clean-generated.ps1 -Apply
```

发布使用 `dist/releases/v版本号/`，避免为每次小修复重复复制整套产物。清理保留当前发布的关键验收目录；目录中发现数据库、密钥、压缩包或恢复快照时整项跳过，需核实用途后单独处理。清理结果写入 `dist/audit/cleanup/result.json`；不能把预览或脚本生成当作删除完成。`reports/`、`.vscode/` 和 `.idea/` 仅本地保存，由 `.gitignore` 排除。

反复构建后的 Go 编译缓存可用 `go env GOCACHE` 定位，确认没有构建任务后按需执行 `go clean -cache`；后续构建会重新编译。它与 Go 模块下载、Android SDK／NDK、签名密钥及应用数据库分开，不要把这些目录一并清空。

收藏夹列表和影片归属由 `stores/favoriteFolders.ts` 共用 SQLite 设置与 `settingsQueue`，首次读取导入旧 WebView 值并保留原值供失败重试。详情、普通播放器和短剧共用 `usePlaybackFavorites` 与 `FavFolderModal`；确认按钮在保存期间显示忙碌状态，成功后关闭，失败时保留选择。

观看位置与可选总时长保存在 `watch_history`；2.4.4 以迁移 22 追加 `duration`，旧记录的位置保持原值，未知时长为 0，不据此编造百分比。旧绑定省略时长的写入保留已知时长，备份读写同样兼容缺省字段。浏览器的 `cczj_ep_prog_v1` 和 `vp_t_` 键保持原格式，并兼容旧的双前缀读取。SQLite 历史和浏览器检查点按 UTC 时间比较；恢复位置、等待续播选择或媒体尚未就绪时不回写起始进度。无需更改缓存身份／格式的修复不清空缓存。
