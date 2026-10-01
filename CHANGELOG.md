# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

2.2.0 之后一直在工作树里的那一批：扩展包体系落地、发布链路与安全补齐、错误处理与生命周期收拢，
以及把几个三四千行的巨型文件拆开。数据库继续走顺序迁移（新增 v7~v10），启动时自动补齐并在迁移前留快照。

### Added

- **扩展包**：`%APPDATA%\CCZJ Video\plugins` 下的文件夹即包。四类——采集适配（声明 URL 拼法、信封与字段名）、
  着色器（mpv hook 语法的 `.glsl` 进画质增强下拉）、主题（追加预设与包内背景图）、前端注入
  （`script.entry` 拿到 `cczj` 接口：加页面、加侧栏入口、加设置分组、注入样式、订阅事件、`intercept` 应用自己的方法）。
  边界是内核不动：碰不到 Go 侧，也覆盖不了内置页面与内置分组。规范见 `docs/plugins.md`，理由见 ADR 0007 / 0008。
- **改数据与出网要先授权**：`permissions: ["write", "network"]` 的包第一次真去调非只读绑定或往外 `fetch` 时弹一次确认，
  允许与拒绝记进设置、重启仍在、可撤销。应用自己的请求与 Wails IPC 不算出网；这是授权闸门，不是沙箱。
- **拖放安装与卸载**：把包文件夹拖到扩展卡片上即装（先校验后落位，逐个给回执），失败整包判 `invalid` 并展开机器可读的原因码；
  「卸载」删掉那个目录，应用自带的三个包不给按钮、Go 侧也拒绝删。
- **日志、诊断、动画改成三个内置包**：删掉那几个文件夹，设置页就真的少了那几项；`motion.css` 带着全应用的动效时长与缓动，
  另有「开启动画 / 关闭动画」总开关。
- **缓存改为过期先展示**：详情、豆瓣热榜、评论、首页轮播与推荐都是 stale-while-revalidate——旧数据立刻给界面，
  新数据在后台取，抢不到限速名额就跳过这一轮；「设置 → 基本」新增数据新鲜度档位，改完下一次读就生效。
- **海报改本地磁盘缓存**（TTL 7 天淘汰），豆瓣 img9 失败时改走镜像主机，前端加载改淡入不再硬切。
- **诊断面板加三块实时读数**：本次会话的播放质量、按用途拆分的出网吞吐与耗时、Go 与前端各缓存的命中率（新增 `app/netstats` 计数层）。
- **发布链路补齐**：`build/windows/package.ps1` 产出带图标的 exe 与 `checksums.txt`，`build/windows/info.json` 的版本资源
  经校验（语言键、FileVersion/ProductVersion 字符串），更新下载后按 SHA-256 校验清单才安装。
- **迁移 v7~v10**：地区写法归一、标题去画质尾巴后重算身份、同豆瓣 ID 跨类型合并、`sources.auto_disabled_at`
  区分自动停用与手动关闭。

### Changed

- 错误统一到 `app/apperror` 的少量稳定码（ADR 0009）：只有离开包边界的错误带码，包内继续 `%w` 串链，
  分类只走 `CodeOf` / `errors.Is` / `errors.As`。`scripts/verify.ps1` 加守卫，禁止再按 `err.Error()` 文本分支。
- 取消上下文贯通采集、下载与豆瓣补全；关停不再提前关库，`RestartApp` 等旧进程退出再接管。
- SQLite WAL 在关停时 checkpoint 并设 `journal_size_limit`，日志文件不再无限增长。
- 生产构建剥离 `console.*`；前端补可访问性（Select 键盘导航与 ARIA、可点击 div 补 `role`/`tabindex`、图标按钮补 `aria-label`）。
- 巨型文件按域拆分。Go 侧只做同包整块搬移，逐行核对过内容与行数：`app/douban/crawler.go` 1454 行拆成
  crawler（抓取与搜索）/ anticrawl（退避与静默）/ parse（HTML 解析与打分）/ detail（详情解析）四份，最大 646 行；
  `chart.go` 971 → 634 + chart_parse / chart_match；`app/service/download.go` 1394 → 任务生命周期 614 + download_http（直连多线程）
  / download_hls（m3u8）；`app/db/douban.go` 1180 → 294 + douban_chart / enrich / match / cooldown；
  `app/updater/updater.go` 1052 → 454 + speedtest / update_download / install。
- 前端同样拆开：`app/service/facade.go` 一域一文件，`VideoPlayer.vue` / `Settings.vue` / `Sources.vue` / `tsCache.ts` 的
  视图逻辑收进 `frontend/src/player` 与 `frontend/src/utils` 下的小模块；超过 250 行的行内 `<style scoped>` 搬到
  `frontend/src/styles/{views,components}/*.css`，SFC 用 `<style scoped src>` 引回来（scoped 哈希照旧，只是文件分家）——
  Player.vue 2043 → 1266、Search.vue 1779 → 1124、Detail.vue 1678 → 1154、Settings.vue 与 LogPanel / Favorites /
  Downloads / BookCarousel / ExtensionsPanel / BackToTop / DoubanComments 同批处理。
- 撤掉从未接线的图标依赖：`unplugin-icons` 的 vite 插件与 `@iconify-json/carbon` / `@iconify/vue`（28 个包），
  界面图标一直是 `components/Icon.vue` 里手写的内联 SVG。
- 清掉一批无人调用的代码（逐项对过 HEAD 与当前）：`app/db` 里按片名取物的 9 个旧查询
  （`GetGlobalVideoByName` / `GetWatchHistory` / `GetWatchHistoryByVod` / `GetWatchedEpisodes` /
  `Add·Remove·IsFavorite` / `SaveWatchHistory` / `DeleteHistoryByVodName`）、`collect.NewEngine`（只留 `NewEngineV2`）、
  handler 的 `GetMode` / `MarkDone`、`proxy` 的无上下文取图入口，以及整个 `app/util` 包与随之而来的 snowflake 依赖。
- `scripts/verify.ps1` 与前端约定脚本各加一条体量守卫，防止拆完重新长回去：非测试 Go 文件不超过 900 行，
  SFC 的行内 `<style scoped>` 不超过 260 行。
- 补测试：`app/cache` 从 0% 到 89.7%（回收站/恢复/彻底删除/清空/身份合并各自发出的失效事件契约、无发布者时不炸、
  统计行键稳定），`app/handler` 从 16.2% 到 27.5%（采集调度配置的脏值回落与钳制、时间戳读写、事件载荷键名、
  暂停/恢复/停止在无引擎时的返回值）。

### Fixed

- 播放器的换集/换源竞态与生命周期（多次 `loadHls` 交叠、上下文归还不及时）。
- 设置项不生效（窗口尺寸与布局类改动没落到 layout store）。
- 权限闸门把脚本包的第一个分片请求打死（WebSocket 补丁误伤）。
- 启用/停用扩展包时界面闪烁与抖动。
- 轮播图自动播放时缺起始帧导致硬切；详情页一次打开抓两趟远程；`Recent` 页的 `onActivated` 是死代码。
- 豆瓣评论缓存后台刷新落地的测试竞态（替身位先返回再写缓存，偶发读到旧的一页）。
- 设置页分组改成「挂载一次 + `v-show`」：切走再切回来不再丢掉主题编辑器改到一半的草稿和打开着的弹窗，
  各分组的取数也不会从「每次进页面一遍」变成「每次切 tab 一遍」。删掉正在看的扩展包分组会退回「基本」，不再留一块空白面板。
- 深色主题下「新建主题」卡片是白的（写死的浅灰底与虚线边框，不看当前主题）。
- 视频卡片只能用鼠标点：补 `role="button"` / `tabindex` 与 Enter / 空格打开（空格原先会把整个网格往下滚），
  键盘聚焦时画出描边。

## [2.2.0] - 2026-09-28

按评审清单做的一轮系统性修复，覆盖数据完整性、查询与写入性能、豆瓣抓取、播放代理与缓存、前端架构，以及一批功能补齐。

### Added

- **跨源同片识别**：同一身份（豆瓣 ID，或归一化后的标题+年份+类型）在各源里的多份记录会被认成同一部片，详情页可换源看同一部。诊断页可发起身份合并（2~8 个一组，迁移收藏与历史，动手前弹确认）。
- **回收站**：删除影片改为软删除并进入回收站，收藏和历史跟着隐藏不丢，可随时恢复；确认无误再彻底删除。
- **数据备份与恢复**：收藏、历史、设置、采集源定义和已补全的全局元数据打包成单个文件导出，换机/重装后导入合并；迁移前自动留的数据库快照也能从这里读回来（设置 → 高级）。
- **多线路与测速选源**：解析源站用 `$$$` 打包的多条播放线路，播放页可切线路并对各线路并发测速，按实测结果推荐。
- **字幕**：播放器手动加载本地 `.srt` / `.vtt` 并自己叠加渲染（画质增强开启时 WebGL canvas 会盖住原生 `<track>`），支持显示/隐藏/更换/移除。
- **季/集结构化**：从集名解析真实集号（S01E05、第 X 集/话/期/章/卷、EP05、纯数字，含全半角与中文数字归一），认不出才回落列表序号。播放进度、TS 缓存键、下载与换线续播统一按集号认集。
- **源健康度历史与巡检**：每轮采集收尾自动给该源记一条样本，采集源页的探测由用户点一下才发起（同一个源 5 分钟内只认一次），设置 → 诊断用点阵展示并按连败阈值提示「可能已失效」；连败到阈值自动停用该源，到期允许重试。
- **数据库顺序迁移**：以 `PRAGMA user_version` 记版本 + 有序迁移列表（v1~v6：采集游标表、`name_norm` 回填、重复身份合并、索引重建、豆瓣轮转时钟、源健康度表），启动时自动补齐落后版本，替代过去"版本标记不匹配就归档删库"。迁移前自动留快照。
- **关键索引**：补 `(source_key, lifecycle, vod_time)`、`global_id`、`douban_id`、`(global_id, ep_num)`、`source_health(key,time)` 等，并用 `EXPLAIN QUERY PLAN` 回归测试钉住。
- **前端约定守卫**：新增 `scripts/check-frontend-conventions.mjs`，把 `verify.ps1` 里此前只写在文档上的约定变成真检查——i18n 中英键位对齐、`t()` 键引用有效、`cczj-*` 工具类确实存在。
- **强制单实例**：重复启动不再开出第二份进程，而是把已在跑的窗口叫到前台并记一条日志。两份进程会同时写同一份 SQLite、同一批缓存目录，豆瓣限速闸门也是按进程算的。「关于」页的自动重启改成先等旧进程退出再接管，不会被自己判定成第二实例而静默退出。

### Changed

- 采集增量水位线改成 per-source 独立游标，一个源落后不再拖累或伪造其它源的进度。
- 采集期不再每页载入全表、也不再跑 O(N²) 编辑距离；身份匹配收拢到 `app/db/identity.go` 一处实现，写库改为批量事务。
- 豆瓣限速收成单一闸门：搜索/详情、评论、热榜共用同一个时间基准（交互 15~40 秒、批量补全 60~150 秒随机抖动，只会更保守），等不起的请求放弃而不是插队。
- 豆瓣匹配打分引入季/部与全半角、繁简归一，阈值能够真的拒绝候选，错挂不再向同系列扩散。
- 播放代理透传 `Referer` / `Cookie`，校验 `Range` 转发与 `206` / `BYTERANGE`；媒体与图片代理访问私网/回环地址改成「设置 → 高级」里的显式开关，改完即时生效不用重启。
- TS 缓存键剥离时效签名、淘汰改为真 LRU、缓存命中不再伪造带宽估计。
- 缓存失效收拢到统一层（`app/cache/invalidate.go`）：Go 侧清完广播 `cache:invalidate`，前端的详情、海报、TS 片段缓存跟着一起清。
- AI 画质增强在换集/换线路时自动重建管线并显式归还 WebGL 上下文。
- 下载补上失败重试，并正确处理 m3u8 的 `EXT-X-KEY` / `EXT-X-MAP`。
- 前端 `videoStore` 拆分，列表状态按视图持有（`frontend/src/composables/`），请求 generation / abort 与错误反馈通道统一；`<html lang>` 跟随语言切换。
- README 逐条与实现对齐：撤掉"推荐基于观看历史和偏好"的说法（实际是内容交集，不读历史与收藏），如实描述影视增强的待接线分段、缓存管理四栏与清理范围、诊断页的两个非只读动作，并删除页面数与方法数等会过期的硬数字。
- 单条写入路径（导入、详情页补录）的身份解析不再载入全表候选：精确与归一化两档直接查索引，落空才回落到别名与相似度档。
- 豆瓣评论缓存加上容量上限并按最旧条目淘汰，此前是一个只增不减的 map。
- 清掉死代码与未接入的依赖：删除无人调用的旧接口、移除并未真正接线的 UnoCSS 依赖（前端样式是手写的 `cczj-*` 工具类）。

### Fixed

- 日志面板的「导出」在 WebView2 里点了既不弹另存为窗口也不报错，等于没有导出：改为由后端落到数据目录 `exports/logs`，成功后给出实际路径和「打开导出位置」。
- 「今天 / 几天前」这类相对时间把数据库里的 UTC 裸串按本地时区解析，整体偏移了一个时区差。
- 类型页空状态的提示文字放在了组件并不存在的插槽上，永远不会渲染出来。

- 采集取页失败此前不进任务结果，看起来像"跑完且全绿"；现在计入失败率并写进源健康度样本。
- 收藏与历史的部分查询未过滤软删除条目。
- 历史页的观看进度徽章把 `position`（秒）当百分比显示，"看了 30 秒"会画成"30%"；现在按统一的进度约定换算。
- 豆瓣非 HTTP 错误不再被升格成全局反爬静默，本地闸门拦下的请求也不再被判成豆瓣真的封了 IP。

## [2.1.0] - 2026-09-27

自 2.0.3 发布以来的全部改动，包含未单独发布过的 2.0.4。主题是把散落的运维能力收进「设置」、收敛 Go 侧目录，以及修掉一批采集与搜索的真实缺陷。

### Added

- 设置页新增「诊断」分组：只读展示运行时环境、内存与协程、数据库与各表行数、采集与豆瓣调度状态、反爬静默与 302 验证题求解结果、数据健康度（缺评分 / 缺条目 ID / 冷却中 / 同豆瓣 ID 重复分组），以及采集源逐个连通性探测。
- 设置页新增常驻「日志」分组：实时跟随输出、按级别过滤、关键字搜索、翻页查看历史文件、导出与复制；Go 侧日志补上环形缓冲与订阅，前端统一走日志 store。
- 设置页「基本设置」补齐可配置项：豆瓣补全轮询间隔、日志保留天数、采集时是否恢复已删除条目，保存即生效。
- 豆瓣 302 验证题改为本地解 proof-of-work，不再依赖登录 Cookie。
- 搜索远程关键字支持子串匹配：先按源站的 `vod_name LIKE '<wd>%'` 取结果，取不到时用 `%kw%` 重试一次，解决"大臣"搜不到"是，大臣"。

### Changed

- 移除独立的后台管理面板（`/dev-admin`）：它的能力与侧栏页面完全重叠，运维入口改由「设置」承载。
- Go 侧目录收敛：根目录只保留 `main.go`（入口）与 `app.go`（装配根），原先散在根目录的 `app_facade.go` / `app_download.go` 拆进 `app/service` 包并按职责分成 `app.go` / `diagnostics.go` / `logs.go` / `download.go` / `direct_resume.go`，Wails 绑定随之生成到 `frontend/bindings/cczjVideo/app/service/`。
- 「后台采集调度」从设置页迁到「采集源」页：全局开关、采集间隔、源/页节流、启动补采与首次全量改成调度状态条下的「调度设置」面板，与每源的定时器放在同一页，设置页不再保留重复入口。
- 豆瓣补全轮询间隔从 10 分钟放宽到 30 分钟，降低被判定为异常流量的风险。
- 搜索页随导航保活：切走再切回不再重跑远程请求，只有在册可见性规则变化或数据刷新后才重新搜索，滚动位置也随之保留。
- Wails 升级到 v3.0.0-beta.24；移除未使用的 `@vueuse/motion`，列表与弹窗动效改为自绘的 `MotionList` / `MotionTransition`。
- 中英文案按命名空间对齐：采集调度相关文案从 `settings` 迁到 `sources`，脚本核对后 zh / en 键位差异与未定义引用均清零。
- `scripts/verify.ps1` 增加根目录守卫：除 `main.go` / `app.go` 外不允许再出现散装 Go 文件。

### Fixed

- `db.DoubanDuplicateGroup` 缺少 sqlx `db` tag，导致诊断页「重复豆瓣 ID」查询在真机上报 `missing destination name douban_id`；已补齐并加回归测试。
- `LogPanel.vue` 直接 `import '@wailsio/runtime'`，绕过了前端绑定出口约束；改走 `src/api/runtime.ts`。
- 日志面板读不出历史日志文件的内容；现在按保留天数列出并可翻页查看。
- 采集页 `vod_total` 按字符串解析导致总数丢失，且单页保存失败被吞成"成功"；现在类型正确、失败会如实上报。
- 采集调度器在停止与重启之间存在竞态窗口，可能同时跑起两个引擎；已加互斥并让停止等待当前页收尾。
- 下载文件名截断时切在 UTF-8 多字节中间导致 panic；改为按 rune 边界截断。
- 目录列表与统计未过滤软删除条目，已删除的影片仍会出现在部分视图里。
- 列表组件存在重复加载与 `:key` 冲突，切换筛选时会闪现上一条目的数据。
- 清理无人监听的 Wails 事件与后端遗留入口。

## [2.0.4] - 2026-07-21

### Added

- Added persistent poster and Douban chart caches to reduce repeated requests.
- Added a Recently Updated page with today, week, month, and type filters.

### Fixed

- Search results are no longer added to the library until explicitly imported.
- Fixed the Recently Updated navigation icon.

## [2.0.0] - 2026-06-27

> 这一节原先误记为 `1.1.1`：对应的 git tag 是 `v2.0.0`，`version.json` 的更新历史也记作 2.0.0。
> 中间的 `2.0.1` 只在 `version.json` 里留过说明，`2.0.3` 只打过 tag、两边都没有条目；
> 2.1.0 的原文就是「自 2.0.3 发布以来的全部改动」，那之后的改动记在那里。

### 新增

- **Anime4K 动画画质增强**：移植自 Anime4K v4.0，支持 S/M/L 三档模型的 WebGL2 CNN 实时 2× 超分辨率，为动画/动漫视频提供高质量画面增强
- **FilmUpscaler 影视画质增强**：基于 FSRCNNX 的全链路视频增强管线，包含去隔行(Deinterlace)、降噪(Denoise)、时间混合(Temporal)、HDR 色调映射、CAS 锐化等处理
- **智能画质增强模式**：根据 GPU 性能自动调整增强质量，帧率过低时自动降级，确保流畅播放
- **视频推荐功能**：新增相似视频推荐，详情页展示同类型视频作为兜底推荐
- **历史记录优化**：历史记录条目现在包含视频名和封面信息，便于卡片展示
- **版本更新功能**：新增「设置 → 关于 → 检查更新」功能，支持每天首次启动自动检查 GitHub 版本更新
- **多渠道版本获取**：支持 GitHub API、GitHub Raw、jsdelivr CDN、Gitee 等多个版本信息源，提高更新检查成功率

### 优化

- **应用体积优化**：从 ~35MB 减小到 ~27MB（约 22% 缩减）
- **视频列表加载性能**：优化数据库查询，使用批量补充豆瓣数据（一次 JOIN 替代 N×2 次查询）
- **视频搜索优化**：支持关键词模糊匹配（标题/演员/导演/备注/年份/地区/类型）
- **更新检查机制优化**：每个版本源最多重试 3 次，提高网络不稳定时的成功率
- **版本信息缓存**：5 分钟内重复检查使用缓存，减少网络请求
- **下载超时处理**：超过 60 分钟自动终止下载
- **ARM 平台适配**：Windows ARM 平台自动跳过更新检查（暂无 ARM 安装包）

### 修复

- **画质增强 Bug**：修复画质增强功能的已知问题，提升稳定性
- **更新检查时序问题**：确保前端挂载后再推送更新事件
- **TypeScript 类型检查**：增加空值处理，修复类型检查错误

### 其他

- 更新版本号至 2.0.0

## [1.1.0] - 2026-06-20

### 新增

- 新增视频搜索功能，支持多源搜索
- 新增视频播放功能，支持多种播放模式
- 新增视频收藏功能，本地数据库存储
- 新增主题系统，支持自定义主题配色
- 新增下载管理功能，支持批量下载视频
- 新增设置页面，包含基本设置、主题设置、播放设置等

### 优化

- 优化界面布局，响应式设计
- 优化视频列表加载性能
- 优化数据库查询效率

### 其他

- 项目初始化，基于 Wails 3 + Vue 3 + Go 技术栈

[Unreleased]: https://github.com/ws-cczj/CCZJ-Video/compare/v2.2.0...HEAD
[2.2.0]: https://github.com/ws-cczj/CCZJ-Video/compare/v2.0.3...v2.2.0
[2.0.0]: https://github.com/ws-cczj/CCZJ-Video/compare/v1.1.0...v2.0.0
[1.1.0]: https://github.com/ws-cczj/CCZJ-Video/releases/tag/v1.1.0
