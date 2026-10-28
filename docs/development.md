# 开发与复用指南

执行规范见 [AGENTS.md](../AGENTS.md)。本页负责给出真实代码入口和维护命令，避免在新页面重新实现已有能力。

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

## 短剧播放

`/short-drama` 在桌面侧栏和移动底栏首页右侧提供入口；移动历史记录保留兼容路由，不再占用底栏。桌面默认进入海报剧场，点击卡片后进入单剧播放器并收起侧栏；随机播放通过 `?view=feed` 进入推荐流。移动默认刷推荐，顶部切到 `?view=theatre` 展示两列选剧列表。`ShortDramaTheatre.vue` 复用 `VideoCard` 和 `useVideoList` 的游标分页、搜索与错误状态，返回剧场保留列表和滚动位置，隐藏列表通过 `enabled` 取消在途请求。新页面不复制首页数组或另建列表请求队列。

列表通过既有 `GetVideoList.short_drama` 可选字段进入媒体查询，分页和总数计算前即筛掉普通影片；与 `GetRecommend.short_drama` 共用目录中的分类边界，包含来源分类及全局映射，并遵守可见、未回收和当前来源规则。未提供短剧字段的旧列表调用保持普通列表语义。缺失更新时间的来源仍可使用带 id 决胜的游标翻页。`player/shortDrama/useShortDramaFeed.ts` 拥有本地每批最多五部、远端每批最多一页的推荐、单部详情、集数及请求取消；剧场显示或页面隐藏时释放播放器，重新进入后恢复所选剧目与集数，不按标题猜测短剧，也不以普通影片填充不足的数量。

源站分类和搜索能力分别判断。短剧推荐与剧场优先读取本地目录；本地无匹配项时，通过 `GetShortDramaPage` 有界读取启用视频源的短剧分类，先尝试当前源，最多尝试五个源，不修改全局当前来源。2026-10-28 对验收包现有四个不同接口地址作只读探测：yyzy_tv 返回“短剧大全”ID 83，但该分类列表为空；rycjapi 的短剧 ID 46 可浏览，分类内关键词命中已知作品，虚构关键词返回 0；茅台 ID 37 可浏览，但关键词请求返回 code 1002（API 禁止搜索）；无尽 ID 41 可浏览，关键词请求 HTTP 200 却返回 HTML 文本“暂不支持搜索”。源返回的总数及 ID 是当时接口状态，不作为常量固化。原始摘要在 `dist/audit/android-performance/short-drama-category-probe.json`、`short-drama-search-probe.json` 和 `short-drama-search-probe-followup.json`。

远端短剧复用 `source_types(source_key,source_type_id) → global_types` 的已有分类与禁用规则，并读取源返回的分类表；同名不同源不合并 ID，不按作品标题判断分类。通用 CMS 请求已有 `FetchOptions.TypeID/Keyword` 和声明式 `type_param/keyword_param`，不需要因为 ID 不同就编写一份源代码适配。能力要区分分类浏览、普通关键词搜索和分类内搜索；只对已知分类内的结果开放短剧展示，忽略 t 参数的源需在取回后按来源分类校验。本地目录保留关键词检索，已选远端来源拒绝搜索时明确提示清空关键词继续浏览，不把当前远端第一页过滤冒充完整搜索；现有 `fetchCMSSuggestions` 仅在明确拒绝关键词时尝试通用候选接口并批量补详情，该路径仍需逐源实测，不能假定每站都有。没有搜索能力的源仍可按短剧分类分页采集，遵守用户分类开关、统一网络额度和取消。当前实现通过策略的分类元数据入口读取 CMS `ac=list`，结合已有来源分类识别短剧；列表及分类搜索沿用声明的参数名。每次最多返回 100 条，单源限时 6 秒、整次限时 25 秒，分页固定来源和分类，保留来源身份用于详情、播放、历史与收藏。结果经过分类及用户禁用规则校验，按现有搜索规则仅缓存目录字段，回收条目保持隐藏，不批量采集详情或媒体。近期筛选通过来源的小时参数传递；分类和页码不符合请求时返回错误。关键词被源拒绝时显示实际错误，不将当前页过滤冒充完整搜索。Android 验收包实查茅台源剧场首屏 20 条、翻页后 40 条，首部详情 77 集，实际播放两秒推进约 1.97 秒、解码 52 帧；rycjapi 在该 Android 网络上连接被对端重置，分类内搜索仅有前述源站探测结果，不能声称 Android 搜索通过。原始证据在 `dist/audit/android-performance/drama-theatre-loaded.png`、`drama-provider-logs.json` 和 `drama-playing-current.png`。

短剧推荐刷新先读取当前源、当前播放源及其余启用视频源的已有 SQLite 推荐，再按需发现远端分类；来源身份与排除列表分别绑定，最多查看五个源。详情、集数和队列准备成功后一起提交，刷新或换剧失败保留当前播放器并反馈错误，同身份重入复用本页已加载详情。发现过程总限时 30 秒，单次选剧详情限时 15 秒；离场取消请求、计时器和迟到提交。首次推荐以静态提示和剧场入口等待，后台刷新不再清空画面或显示整页转圈。

`player/shortDrama/session.ts` 拥有首页与播放器共用的有界会话：进入手机视频首页即可准备首部推荐的数据与开头片段，不挂载播放器或自动出声；离开首页和短剧播放范围取消准备工作。推荐批次随机打乱，每次播放完毕或上划进入另一部短剧，不因标题点击意外切成连续剧模式；剧场路由及主动选集进入同剧顺序队列。播放期间只准备下一项：推荐模式准备下一部的详情和第一集，单剧模式准备下一集；预先确定的目标由实际翻页直接消费，预取远端页的游标仅在使用该目标时提交。队列续接保留至多前 19 项与新一批，初始远端页受 100 条上限约束，不保留无限播放历史。向下大幅拖动且没有上一项时触发推荐刷新，小幅拖动回弹；加载／空态复用同一手势模块，刷新能取消并重开在途推荐请求。

媒体预加载扩展既有 `ts-cache/prefetch.ts`，复用同一个清单缓存、来源／作品／集数身份和分片 LRU，不创建另一套缓存或隐藏解码器。首页预热一项，播放时等待当前缓冲充足且没有前台请求，再串行预热下一项 HLS 的初始化段与开头两片，单项新增媒体字节不超过 8 MiB，单次传输限时 12 秒；前台请求、切换、离场和来源变化均可取消。已完成分片仍受共享 LRU 预算约束，下一项播放通过原有 HLS loader 命中。多码率按最低带宽变体预热；字节区间流、非 HLS 不走这条整片预热路径，播放仍由原有管线负责。失败记录诊断并保留正常播放／重试路径，不把预取失败显示成播放失败。视图在视觉离场结束前保留自己的呈现身份，防止首页准备结果回写离场中的视频或历史。

竖屏翻页由 `useShortDramaPaging` 拥有跟手偏移、回弹与交接动画；单个活动播放器配合最多 720×1280 的临时 Canvas 和一份 inert 控件快照。推荐批次内翻页命中预备详情时直接消费；未命中时先提交条目身份和加载页，动画不等待详情或媒体缓冲，继续滑动可取消旧详情并选择批次内其他条目。详情失败保留重试和可用的前后项；获取新推荐批次与主动静默刷新仍保留原有有界请求策略。静默刷新才等替换媒体首帧 loadeddata，canplay 作为兼容补充，后续触摸可立即释放刷新快照。快照不导出、不缓存，不增设第二个解码器；完成、取消、失败或视觉离场结束时清理 Canvas、DOM、动画与计时器；已提交离场立即停止工作，仍可见的快照保留至离场结束，刷新媒体等待超过 15 秒释放快照并反馈超时。手势监听绑定到稳定的短剧区域，避免随视频移到屏幕外；缓冲中的媒体仍允许翻页。尊重已有页面动画开关和减少动态效果设置。手机全屏／退出入口位于进度条右侧的右下角，复用既有全屏控制器，以整个短剧区域为宿主，选集和设置保留在该区域中。

播放器恢复历史与媒体源准备并行，自动播放仍需等待历史定位决策和媒体就绪，避免先出声再跳转。既有 `playing` 首播统计保留；支持视频帧回调的宿主另记 `player_first_presented_frame`，表示 playing 后首次观察到的呈现帧，不冒充实际硬件首帧。短剧诊断补充详情准备是否命中、详情等待和临时画面捕获耗时，沿既有录制开关写入，不另开后台采样器。

播放恢复沿用当前媒体所有者：`PlaybackCheckpoint` 显式携带时间、正常倍速、暂停、静音和元素音量，同一集重试或切线路时应用到新媒体装载，不依赖周期落盘的历史。播放身份包含来源、作品ID和真实集号，避免不同源的同号作品共用浏览器检查点；旧本地键不删除，SQLite历史仍作为已有进度的恢复来源。同源线路先确认唯一集号；跨源先读取候选详情，核对规范化作品名、唯一集号和集名（同名单集影片允许单集对应），再发布新详情。对应不明确、候选失败、换集或离场取消时保留当前播放，不自动跳第一集；人工确认不同版本及片头偏移仍需用户判断。

HLS内部重试结束后，`playbackSource` 对一次连续网络故障最多追加两次恢复，每次等待8秒；恢复从当前时间开始，同一时刻只有一份恢复等待。暂停取消倒计时，手动跳转丢弃旧恢复，媒体替换／退出取消监听和迟到回调；重新播放优先交给该恢复所有者，普通暂停后的缓冲恢复仍走既有 `resumeStreaming`。重新出现 playing 后重置恢复预算。持续失败保留媒体现场和重试／切线入口，不自动跨源。恢复提示延迟1.5秒，失败或恢复后清除；系统 online 只用于唤醒尚未耗尽的恢复，不能当作来源已可用的证明。

短剧推荐控制复用同一会话和下一项预热：`recommendationPreferences.ts` 按来源与作品ID持久保存“不感兴趣”，最多300项；使用既有SQLite设置 `short_drama_recommendations_v1` 和串行写队列，运行时校验版本、字段和数量。写入成功才生效，失败可再次操作，原记录不删除。界面提供撤销最近一次屏蔽与恢复全部已屏蔽推荐，空候选时同样可恢复；显式剧场／单剧入口不受推荐排除影响。“追这部”进入已有单剧顺序队列，返回推荐恢复发现语义。最近观看只在本会话保留40项，实际进度事件后记入，优先选择没看过的候选；不足时允许近期内容，明确屏蔽与当前作品不放宽。过滤覆盖选剧、后续翻页及下一项预备，避免消费旧的已屏蔽准备结果。无其他候选时保留当前画面并给出说明；不无限翻远端页寻找内容。

`VideoPlayer` 的 `presentation="portrait"` 共用 HLS、平台传输、缓存和续播，装配 `PortraitPlayerControls`，不挂载横屏控制条。竖屏固定从正常速度播放，长按临时加速并在释放／取消时恢复；不写入横屏倍速偏好。控件渐变背景不接收指针，只有实际按钮与进度条拦截触摸，避免透明留白吞掉长按与上下滑动。`useEpisodeHistory` 持续保存 SQLite 历史（位置和总时长）并反馈写入失败。媒体初始化、等待续播确认及跳转期间不覆盖进度；详情的“继续观看”显式恢复数据库位置，普通选集仍遵守播放偏好。没有总时长的旧记录不伪造百分比。标题是不可点击的纯文本；简介位于标题下方直接展示，最多两行，点击进入已有短剧详情页查看完整内容，选集紧邻简介并使用原有选集抽屉。进度条参考 [Vant Slider](https://github.com/youzan/vant/blob/main/packages/vant/src/slider/README.zh-CN.md) 的滑块与拖动状态，时间显示参考 [Video.js](https://videojs.org/docs/framework/html/reference/components/time)，适配既有 range 控件，不引入新组件库。手机轨道左右内缩 18px，保留 44px 触摸区及下方留白，常驻当前时间／总时长；全屏图标缩为 16px，触摸区域仍为 44px。拖动时轨道从 3px 加粗到 8px，滑块从 8px 放大到 20px；复用 `useThumbnailScrubber` 与 `CczjMobileGestureOverlay` 显示目标帧小屏和时间，不用主视频画面冒充目标位置。预览解码器仅在拖动时创建，移动端闲置两秒释放；无法解码时显示既有不可用提示。按下和移动只更新预览，释放提交，取消不跳转；换媒体与卸载释放指针捕获。

播放队列按入口区分：推荐流上下切换、滚轮／键盘及播放结束均切到不同短剧；点击剧场卡片、剧名或在抽屉选择任一集（含当前集）进入单剧队列，所有切换与自动连播只在本剧集数内进行，末集播放结束后停止并显示已播完。两端都提供返回剧场和返回推荐入口；手机通过画面上下滑动切换，选集提供可见的触摸入口，观看区不常驻上下集按钮。`getPlayerPath` 根据视频分类将短剧详情的播放入口送往 `/short-drama?source=…&vod=…&global_id=…&episode=…&line=…`；已由数据库确认分类的剧场卡片通过 `getShortDramaPath` 进入同一播放器，保留来源身份、集数索引及所选线路。直接进入时不请求推荐，也不改写全局当前来源。隐藏再进入保留本次选集，换具体剧目或返回推荐时取消旧请求。

短剧选剧列表、空态及加载失败状态沿用应用主题；已提交的实际播放场景让手机底栏与系统底部区域复用播放器深色板，切回剧场或离场恢复主题。已有播放内容时，`player/shortDrama/presentation.css` 仅为观看区和 Teleport 选集／简介抽屉提供局部中性色板，不修改全局主题变量、启动背景或用户偏好。`shortDramaScene` 只负责选剧／播放状态与导航布局。播放器抽屉将普通属性转发到遮罩根节点，使传入的局部色板在 Teleport 后仍然有效，其他抽屉保留原来的背景默认值。

桌面视频保持 9:16 并优先铺满可用高度，两侧使用当前视频中央色带形成横向一致的低分辨率虚化背景；背景复用播放器进度回调，最多每 400ms 绘制一次 72×128 Canvas，不新建解码器、定时循环、图片导出或网络请求，页面隐藏即移除。桌面剧名与最多两行简介／当前集数收在观看区左下方，细进度条横贯桌面底部状态栏上沿；状态栏复用 `PlayerDesktopControls` 的精简模式，常驻播放／暂停、时间与时间跳转、队列状态、选集、画中画和全屏入口；按最初的竖屏交互要求，不挂载清晰度、倍速及音量面板，视频高度预留状态栏空间，切换胶囊靠右侧放置，避免把全部信息挤在中央视频上。`VideoPlayer.portraitControlsHost` 允许把同一份竖屏底部控件 Teleport 到观看区，拖动状态和媒体控制仍归原播放器所有；长按提示、中央暂停入口仍在视频内部。全屏复用既有呈现控制器，通过可选的 `fullscreenRoot` 指向整个短剧观看页，使底栏与选集抽屉留在全屏内；画中画仍由原媒体实例及平台适配持有。桌面右侧提供收藏、选集与简介入口，收藏复用 `usePlaybackFavorites` 和收藏夹弹窗，显式使用播放来源身份。弹窗挂载到独立的观看页覆盖层宿主，避免 Teleport 到其自身父节点时破坏视图切换锚点。两端页头覆盖渐变保护层，不重复展示总集数或悬浮计数。移动端保持底部信息和进度条在画面内，保留底栏，实际竖屏内容填充观看区，横屏内容仍完整显示。移动端时间常驻进度条下方，拖动时另外显示目标帧预览与时间，媒体就绪后暂停才显示中央继续播放入口。滚轮连续事件限制为一次切换，方向键／PageUp／PageDown 按当前队列切换、空格暂停；弹层和表单控件保留自己的输入行为。桌面抽屉限制在观看区内，展开时视频、底部信息与进度条一起让位。

设计参考：[抖音短剧入口](https://www.douyin.com/series)、[官方短剧播放器文档](https://developer.open-douyin.com/docs/resource/zh-CN/mini-app/develop/industry-plugin/playlet-plugin/playlet/playlet-extension)。官方文档明确支持透明导航、播放进度续播和选集事件；其同剧集滑动语义属于小程序官方播放器，不能据此声称抖音所有推荐页都采用同一流程。本项目按用户确认的入口语义区分推荐流和单剧队列。公开历史截图仅用于信息层级参考，未作为当前抖音原生 App 的逐项实测结论。

选集每 20 集分组，使用来源真实集数，支持正倒序和定位当前；分组用文字与下划线区分，集数网格通常五列、窄屏自动减少列数，当前集用主题色描边和小圆点标记，完整的有意义集名在网格上方补充展示。抽屉固定保留剧名、计数和分组，网格独立滚动；移动端初始高度为视口的 72%，可通过既有拖动手柄调整。自动定位只横向移动分组条，纵向滚动由集数网格承担，避免定位标签后再次遮住当前集。

浏览器替身验收运行 `python scripts/test-short-drama-browser.py --channel chrome`（需要 Python Playwright 和 FFmpeg），覆盖桌面／移动布局、真实竖屏媒体、长按、进度条、80 集长标题抽屉、320px 窄屏当前集完整可见及文字不溢出、页面离开与重入、空状态和请求失败重试。独立 acceptance APK 的原生验收运行 `python scripts/test-short-drama-android.py --device emulator-5554`，复用既有 Android CDP／绑定入口，验证真实 CMS 导入、Go 推荐、HLS 播放、触摸、SQLite 进度与返回键；测试创建的临时来源和设置会在结束后清理／恢复。截图与测试媒体输出到 `dist/audit/short-drama`。

短剧页每次激活重新核对视频来源；来源移除后清空旧队列、错误和播放身份。保留的推荐条目在详情返回 `NOT_FOUND` 时重新查询目录一次；恢复仍失败则展示错误，重试重新取推荐，不循环请求已退役 ID。显式单剧入口保留其身份，不自动跳到别的作品。原生针对性回归可加 `--catalog-recovery`：实际播放后清空该测试源目录，检查空态，重新导入并重试播放，再删除来源检查无源状态；清理阶段同步刷新来源 store，避免留下测试源引用。

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

## 开发与检查

架构总图可以在根目录运行 `python scripts/build-architecture.py --validate` 重建到 `dist/audit/architecture/index.html`。结构扫描、人工功能链与画布模板保存在 `scripts/architecture`；生成器会拒绝未归入功能链的公开绑定。图示为同一可缩放／平移画布，并提供模块依赖、生命周期及数据归属视图。交互验证需要现有浏览器测试依赖和 Chrome；`--channel` 可选择已安装的浏览器通道。图上的覆盖数量表示结构清单，业务验证结论以整改台账为准。

Go 版本以 `go.mod` 为准（当前 1.26），Node 以 `frontend/package.json` 的 engines 为准（推荐 24）。Go 与 JS 的 Wails 版本保持一致，当前 beta.30。

### Wails beta.34 兼容评估

2026-10-24 对照 [beta.31](https://github.com/wailsapp/wails/releases/tag/v3.0.0-beta.31)、[beta.32](https://github.com/wailsapp/wails/releases/tag/v3.0.0-beta.32)、[beta.33](https://github.com/wailsapp/wails/releases/tag/v3.0.0-beta.33)、[beta.34](https://github.com/wailsapp/wails/releases/tag/v3.0.0-beta.34) 的发布说明和 beta.30／34 模块源码：

| 变化 | 对本项目的处理 |
| --- | --- |
| Windows 焦点／Tab、WebView2 COM vtable、临时 COM 错误、托盘菜单和自定义问题弹窗修复 | 值得升级；升级后分别做窗口恢复、播放、托盘及弹窗回调原生验证 |
| Vite 就绪检测从 HTTP 改 TCP，InvokeSync 回调 panic 不再永久阻塞 | 有助于开发和主线程稳定性，不能替代应用资源回收 |
| 生成绑定的事件常量、独立 cancellable 导出 | 可逐步采用，现有 `api/events` 和取消组合不因升级而删除 |
| Android HTTP runtime 非 200 响应包装 JSON | 修复的是 `serveAssetForAndroid`；JNI `handleRuntimeCallForAndroid` 仍把错误转成字符串，现有结构化错误补丁仍需要 |
| Android 文件选择与状态栏颜色 | 项目已有 content URI 校验和自己的文件选择控制器，不能覆盖本地宿主；状态栏颜色按实际需求接入 |
| Android 初始化、取消注册、服务启动和窗口关闭核心语义 | 对比未发现能替代本轮生命周期整改的变化；Activity 归属、取消排队、启动失败回滚及窗口保存错误仍由项目修复 |
| 内置 updater 和 server 模式加固、macOS／Linux 修复 | 项目使用自己的 `app/updater`；按实际平台和 server 工具调用评估，不声称自动修复本项目更新流程 |

使用 `GOWORK=off` 和 `dist/audit/architecture/wails-beta34/go.mod` 做隔离依赖验证，`go test` 的 service／window／lifecycle／apperror 包通过。未改根目录依赖、前端 runtime 或全局 CLI；尚未完成 beta.34 的绑定生成、前端和 Windows／Android 原生验收。升级保留为有具体需求时的兼容工作，不作为领域整改的前置步骤；采用时一起同步 Go 模块、JS runtime、CLI 和 Android 构建副本。评估源码差异清单在 `dist/audit/architecture/wails-beta34/changed-files.json`。

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
