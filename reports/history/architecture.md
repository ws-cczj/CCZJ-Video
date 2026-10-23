# 实现说明、讨论与更新历史

> 归档于 2026-10-22。以下保留当时的方案、判断和验证结果，部分内容已过时。当前规范见 [AGENTS.md](../../AGENTS.md)，维护入口见 [文档索引](../../docs/README.md)。旧“禁止”“待拍板”和外部技能引用不是现行开发要求。

## 原始记录
- [`docs/implementation-notes.md`](#origin-implementation-notes)
- [`docs/pending-decisions.md`](#origin-pending-decisions)
- [`docs/update-reliability.md`](#origin-update-reliability)

---

<a id="origin-implementation-notes"></a>

## docs/implementation-notes.md

<a id="origin-implementation-notes--实现与兼容性记录"></a>

### 实现与兼容性记录

当前升级与验收入口：`reports/upgrade-phase3-2026-10-18.md`。阅读个人数据现由 SQLite 管理，下载使用逐任务日志；旧章节描述属于历史实施背景。统一开发规范仍为根目录 AGENTS.md。


这是开发和故障定位参考。日常使用从[项目首页](../../README.md)开始。

<a id="origin-implementation-notes---功能特性"></a>

#### ✨ 功能特性

<a id="origin-implementation-notes--核心功能"></a>

##### 核心功能

- **多源聚合** — 支持添加多个视频 API 数据源，统一管理所有资源
- **在线播放** — 自研播放器（`<video>` + hls.js），支持多集连播、播放进度记忆、同一部片的多条线路切换与测速
- **字幕** — 播放器手动加载本地 `.srt` / `.vtt` 文件并自己叠加渲染，支持显示/隐藏/更换/移除；不支持 `.ass`/`.ssa`，也不从源站取内嵌轨
- **智能搜索** — 全局关键词搜索 + 源内模糊搜索 + 分类/年份/地区多维筛选
- **离线下载** — 多线程分片下载引擎，支持暂停/恢复/取消，下载进度实时展示
- **收藏 & 历史** — 视频收藏同步、观看历史自动记录，支持断点续看
- **跨源同片** — 同一个身份（豆瓣 ID 或归一化标题+年份+类型）在多个源里的多份记录会被认成同一部片，详情页可换源看同一部；独立的「合并影片库」页面已下线
- **数据备份与恢复** — 收藏、历史、设置、采集源定义和已补全的全局元数据打包成单个文件导出，换机/重装后导入合并；升级前的数据库快照也能直接读回来（详见「设置与诊断」的高级分组）
- **回收站** — 删除视频只是挪进回收站，收藏和历史跟着隐藏不丢，随时恢复；确认无误再彻底删除
- **源健康度巡检** — 每轮采集收尾自动给该源记一条样本；「采集源」页的探测由你点一下才发起，同一个源 5 分钟内只认一次。样本记成功率、延迟、连败次数，「设置 → 诊断」用点阵展示并按连败阈值提示「可能已失效」，连败到阈值还会自动停用该源。巡检只请求 `sources.api_url` 的一页列表，不会逐个校验影片的播放地址，也没有任何后台定时器会自己出去探测

<a id="origin-implementation-notes--画质增强"></a>

##### 画质增强

- **Anime4K 动画增强** — 移植自 Anime4K v4.0，支持 S/M/L 三档模型的 WebGL2 CNN 实时 2× 超分辨率，为动画/动漫视频提供高质量画面增强
- **Film 影视增强** — WebGL2 上的 FSRCNNX 2× 超分 + CAS 自适应锐化。已移除未接入的去隔行、降噪、时间混合、HDR 和自动降档分支及其闲置资源。
- **换集自动重建** — 切集/切线路时增强管线会被销毁并重启，同时显式归还 WebGL 上下文（每个页面的上下文数量有上限，不还就会静默拿不到新上下文）
- 增强管线按每次媒体装载独立管理；切集、换线路或关闭时作废在途初始化，迟到的实例释放资源，不再启动。

<a id="origin-implementation-notes--扩展包"></a>

##### 扩展包

规范在 [`plugins.md`](../../docs/plugins.md)，理由记录在 ADR 0007（声明式那一层）与 ADR 0008（放开脚本这一步）。
四类包，前三类只提供配置与素材、由内置引擎解释，第四类带前端 JS/CSS、由应用在界面挂载之后注入：

- **采集适配包** — 声明「怎么拼 URL、怎么读返回信封、字段叫什么」，在「采集源 → 编辑 → 高级选项 → 采集适配」里选中，
  写进该源的 `strategy_config`；包删了已配置好的源照旧可用。
- **着色器包** — mpv hook 语法的 `.glsl`，进播放器「画质增强」下拉成为自定义档位；只做同分辨率处理，
  想改分辨率的 pass 在校验阶段就被拒；编译失败只隔离这一档并回落原高清。
- **主题包** — 追加主题预设与包内背景图，配色仍由内置派生算法算，`tint` 只覆盖明确指定的通道。
- **前端注入包** — `script.entry` 指向的 JS 拿到一份 `cczj` 接口：加页面、加侧边栏入口、加设置页分组、
  注入全局样式、订阅后端事件、调任意后端能力，还能用 `intercept` 包住应用自己的方法改行为。这是个人使用的软件，
  界面随你改；边界是内核不动——包碰不到 Go 侧，也覆盖不了内置页面或内置分组（撞路名、撞分组 id 都直接报错）。
- **改动数据与出网要先授权** — `plugin.json` 里声明 `permissions: ["write", "network"]`，包第一次真去调非只读的
  后端绑定、或往应用外面 `fetch` 时弹一次确认；允许还是拒绝记进设置，重启后仍在，「设置 → 扩展包」那一行
  能撤销（回到「还没问过」，下次用到再问）。没声明的权限一律拦掉并写日志；应用自己的请求不算出网——播放取分片、
  探测、图片代理，连 Wails 那条递给应用自己的 IPC（`fetch` 到 `wails.localhost`）都不经过闸门。这只回答「谁能动手」，**不是沙箱**：
  包 JS 跑在应用自己的权限里，`XMLHttpRequest` / `WebSocket` 那类旁路没拦（播放器正在用它拉分片），细节见 [`plugins.md`](../../docs/plugins.md) §2.1。
- **设置页的「动画」「日志」「诊断」本身就是三个随应用自带的包**（`app/plugin/builtin/`）。它们只在第一次启动时落进
  `plugins\` 一次，之后应用不再碰：删掉那几个文件夹，设置页就真的少了那几项。「动画」那一份还带着全应用
  的动效时长与缓动（`motion.css`），改它就能把动画调快调慢；设置 → 动画里另有「开启动画 / 关闭动画」总开关，
  关掉之后所有过渡退化成直接切换。
- **卸载有两条路**：卡片上的「卸载」按钮直接把那个包目录删掉（不进回收站、不留备份），或者自己删文件夹。
  上面那三个内置包不给这个按钮，Go 侧也拒绝删它们。
- 校验失败整包判 `invalid`，设置页直接展开机器可读的原因码与人话说明；单个坏包不影响其他包，也不影响内置功能。
  脚本包跑挂了更彻底：它这一轮注册的东西全部撤销，本机记下原因并停用，改好脚本点「重试」再注入一次。

<a id="origin-implementation-notes--自动化引擎"></a>

##### 自动化引擎

- **采集调度器** — 后台定时自动采集，支持全量/增量模式、可配置循环间隔
- **豆瓣数据补全** — 自动爬取豆瓣评分、封面、简介等信息，丰富视频元数据
- **启动补采** — 应用启动时自动补采离线期间遗漏的数据

<a id="origin-implementation-notes--相似推荐"></a>

##### 相似推荐

- **详情页内嵌区** — 「相似推荐」区块自己拉同类型的前 100 条当候选池算出结果，最多显示 7 条；结果超过 7 条时才出现「查看更多」，点它带着 `sourceKey/vodId/vodName` 跳到整页。前端算不出任何一条时，退回后端按类型给的兜底列表
- **整页 `/recommendations`** — 侧栏没有这一页，只能从详情页进。候选池自己拉当前源的前 200 条（不分类型），不依赖别的页面留下的列表，所以直接改 URL 进来也能用
- **算法是内容交集，不是协同过滤** — 两处共用 `frontend/src/utils/recommend.ts` 的 `computeRecommendations`：拿种子片的演员/导演/类型/年份/地区与候选逐条比对打分，卡片上标匹配分和命中的维度。**它完全不读观看历史和收藏**，所以这里没有"个性化"，只是"和这一部像不像"

<a id="origin-implementation-notes--版本更新"></a>

##### 版本更新

- **启动自动检查更新** — 每次启动延迟 3 秒检查一次，没有"每天一次"的缓存，所以反复重启会反复请求 GitHub
- **多渠道版本获取** — 支持 GitHub API、GitHub Raw、jsdelivr CDN、Gitee 等多个版本信息源
- **更新日志展示** — 版本升级后自动展示更新内容，支持历史版本查看。启动时会比对「安装版本标记」与编译进去的版本：标记高于二进制只剩一种解释——上次热替换根本没换成，于是记下警告并**清掉标记**，让下一次检查照常把这个更新提示出来，而不是替失败的安装保密（标记一律在启动时清空，不做跨启动保留）
- **装没装上，界面会说** — Windows 用独立脚本热替换 exe，本进程必须让开所以拿不到它的退出码；脚本把结论回写 `cczj_video_update_result`（`ok` / `swap_failed` / `verify_failed` / `rollback_ok` / `rollback_failed`），下次启动一次性读掉并以日志 + 弹窗回执呈现，失败一律以 `_failed` 结尾
- **一键退回上一版** — 热替换动手前先 `copy` 一份 `cczjVideo.exe.old`（这份副本常驻，启动不再清理，下一次安装会覆盖它，所以机器上最多只落后一个版本）。有可用副本时「设置 → 关于」才出现「退回上一版」，用另一个 ASCII 脚本把副本换回去并重启（这一档同样每一行 `start` 都带交接参数——它「换不回去」那一支里本进程按定义还活着）；副本没有任何版本标记，所以界面只报大小和时间，不编版本号
- **安装时机两档** — 「立即更新」当场关程序、换好、自动重开；「退出时安装」只把 `{包路径, 目标版本}` 记进 `pending_install`，由关停流程的最后一步起脚本，而那一档的脚本正文里一行 `start` 都没有，所以换完**不**自动打开。重开不是想当然：脚本 `move` 连败 30 次（约 60 秒）后会走「先把还在跑的旧 exe 改名让位、再换入新版」这一支——给运行中的映像改名是 Windows 允许的——于是 `start` 拉起新程序时旧进程往往还没退场，而应用强制单实例，新进程会被判成重复启动、自己静默退出，用户看到的就是「只关不开」而回执写着 `ok`。现在**两档脚本里的每一行 `start` 都带交接参数**（旧进程 PID），新进程在抢单实例锁之前先等那个 PID 消失，上限 30 秒、超时照常启动；普通双击启动没有这个参数，一秒都不多等。参数只保得住"会传参数的旧进程"那一跳——装着已发布 2.3.0/2.3.1 的机器，脚本由那一版自己生成、正文里不带这个参数，所以新进程还认第二条线索：结论文件是 15 秒内写的 `ok` 就认定自己是被替换脚本刚拉起来的，改等**同名映像**的其它实例退场（2.1.0/2.2.0 连结论文件都不写，那一跳仍只能手动装一次）。等待问的是"谁还占着这把单实例锁"，读的是系统进程快照（`CreateToolhelp32Snapshot`），**不起 `tasklist`**：本应用是 `-H windowsgui` 的 GUI 进程，起一个控制台程序就闪一个黑窗，200ms 一轮足以把桌面刷屏。等待期间桌面上只有**一个**窗口，就是换本体那 60 秒常驻的脚本控制台，它不藏——应用窗口已经关了，那是唯一看得见的进度（真机对照量过：改前 3 秒的等待新开 3 个控制台窗口，改后 0 个）。重启（`RestartApp`）不兑现这份安排——新进程会立刻重新锁住同一个 exe，硬换只留下一条用户没同意过的 `swap_failed`，于是安排原样留着等一次真正退出。两档都在点下按钮时就过一遍产物闸门（路径 + 下载时记的摘要），「立即更新」取代安排，跑着的版本已等于当初安排的目标版本时启动即清记录。为什么不做成默认档位、为什么没有第三档「下次启动再装」，见 `adr/0010-install-timing-tiers.md`；为什么交接参数用冒号不用等号、为什么不在脚本里轮询进程，见 `adr/0012-relaunch-handoff.md`
- **升级后看不到数据有两种成因，现在都会说出来** — ① 2.1.0 把数据落点从 `<exe>\data` 挪到 `%APPDATA%\CCZJ Video` 却没搬旧库，而新版第一次启动就在新目录建了空库，自动迁移从此停在「两处都有库，谁都不动」：现在会报出旧库位置、大小和能找回多少收藏/历史/采集源，点一下才并进来（只增不删、旧库以 `query_only` 打开、设置一概不动），「设置 → 高级」长期留着同一栏，首启提示只说一次。② 库的结构版本比这个程序新（拿旧 exe 开新库，或用「退回上一版」把老程序换回来）：多出来的一列会让 `SELECT *` 在 sqlx 严格映射下直接报错，界面看着就是数据没了——现在启动时比一次 `PRAGMA user_version`，只在库更新时换成忽略未知列的那份连接、不动 schema 也不跑迁移，读写照常，同时说清「这份数据是更新的版本写的，本版本读不到它新加的字段」，并给出「打开数据目录」与「检查更新」两个出口；宽松映射只在这一种情形生效，平时仍是严格映射。这一条救不了已经发出去的 2.1.0/2.2.0。为什么不拦、为什么不装退回前的结构闸门：`adr/0011-read-newer-library.md`

<a id="origin-implementation-notes--设置与诊断"></a>

##### 设置与诊断

原先独立的后台管理面板（`/dev-admin`）已整体移除——它的每一项能力都已在侧栏页面中存在。运维能力改为集中在「设置」页的分组里：

| 分组 | 功能 |
|------|------|
| 基本设置 | 窗口尺寸、字体与语言、主题外观、关闭行为、数据入库策略；豆瓣补全轮询间隔；日志保留天数 |
| 主题外观 | 深浅色跟随、预设与自定义主题、主色与派生色 |
| 扩展包 | 列出 `%APPDATA%\CCZJ Video\plugins` 下扫到的包、把包文件夹拖到卡片上即装（先校验后落位，逐个给回执）、按包启停、「卸载」直接删掉那个包目录（应用自带的三个包不给卸载按钮）、显示校验失败的原因码与说明、包声明的 `write`/`network` 权限与授权状态（可撤销，撤销后下次用到再问）、脚本包跑挂时的隔离原因与「重试」、图标与「几个文件 · 多大 · 最后改动」、复制/打开目录、手动重扫 |
| 日志 | 实时跟随、级别切换与过滤、关键字搜索、历史文件分页查看、导出与复制 |
| 诊断 | 两层读数。**实时**（每 2 秒读一次内存计数器）：本次会话的播放效果（首播等待、卡顿时长、丢帧率、当前线路规格、实际观看时长、播放错误）、按用途拆分的出网吞吐（请求数、失败、字节、均耗时、峰值速率）、Go 与前端各缓存的命中率（详情线路、豆瓣热榜、豆瓣评论、热榜匹配、TS 分片、海报、图片代理）。**快照**（点「刷新」才跑）：版本与安装标记、运行时长、Go/Wails 运行时与平台、协程数/GC/堆内存、数据目录与可执行文件路径、后台任务计数、数据库体积与日志占用、各表行数；采集调度状态与最近启停；豆瓣补全进度、反爬静默与 302 验证题求解结果；数据健康度（缺评分、缺条目 ID、冷却中、重复分组）；每个源的健康度点阵与「失效」提示。唯一的写动作是「疑似重复身份」的合并，动手前弹确认；探测采集源在「采集源」页 |
| 高级 | 数据备份与恢复（导出/导入备份文件、迁移前快照恢复）、豆瓣补全队列统计、手动触发一轮补全、图片代理私网放行开关 |

<a id="origin-implementation-notes--用户体验"></a>

##### 用户体验

- **深色/浅色主题** — 跟随系统或手动切换，全局 CSS 变量自适应
- **滚动位置记忆** — 页面切换后自动恢复滚动位置
- **组件缓存** — KeepAlive 缓存列表页面，避免重复加载
- **自定义标题栏** — 原生无边框窗口 + 自定义标题栏（最小化/最大化/关闭）
- **图片代理** — Go 后端代理远程图片请求，绕过 CORS 限制

---

<a id="origin-implementation-notes---技术架构"></a>

#### 🏗️ 技术架构

```
┌─────────────────────────────────────────────────────┐
│                  Wails v3                           │
│  ┌───────────────┐    ┌────────────────────────┐  │
│  │   Frontend     │    │    Backend            │  │
│  │   (WebView2)   │◄──►│    (Go)               │  │
│  │               │    │                        │  │
│  │  Vue 3 + TS   │    │  SQLite (modernc)     │  │
│  │  Pinia        │    │  Collect Engine       │  │
│  │  Vue Router   │    │  Douban Crawler       │  │
│  │  cczj-* CSS   │    │  Scheduler            │  │
│  │  hls.js       │    │  Download Engine      │  │
│  │  Anime4K      │    │  Image Proxy          │  │
│  │  FilmUpscaler │    │  Auto Updater         │  │
│  └───────────────┘    └────────────────────────┘  │
└─────────────────────────────────────────────────────┘
```

<a id="origin-implementation-notes--前端"></a>

##### 前端

| 技术 | 用途 |
|------|------|
| Vue 3 Composition API | UI 框架 |
| TypeScript | 类型安全 |
| Pinia | 状态管理（只放跨页面共享的状态） |
| Vue Router | Hash 路由 + 滚动位置恢复 |
| CSS 变量 + `cczj-*` 工具类 | 主题与公共样式；模板类和本地 PostCSS 组合共用 `src/styles/cczj-utilities.css` |
| hls.js | HLS 流媒体播放（挂在原生 `<video>` 上，播放器 UI 自研） |
| Anime4K WebGL2 | 动画画质增强 |
| FSRCNNX WebGL2 | 影视画质增强 |
| Vite | 构建工具 |

<a id="origin-implementation-notes--后端"></a>

##### 后端

| 技术 | 用途 |
|------|------|
| Go 1.25+ | 主语言 |
| Wails v3 | 桌面应用框架 |
| SQLite (modernc) | 纯 Go 嵌入式数据库 |
| Brotli / Gzip | 数据压缩（导入导出） |

---

<a id="origin-implementation-notes---项目结构"></a>

#### 📁 项目结构

```
CCZJ Video/
├── main.go                         # 入口：只负责 buildApp() 与 app.Run()
├── app.go                          # 装配根：embed 前端产物、注册服务、主窗口、系统托盘
├── app/                            # 业务代码全部在这里，根目录不再散落 app_xxx.go
│   ├── service/                    # 唯一的 Wails 绑定服务（App 结构体）
│   │   ├── app.go                  # App 结构体、NewApp、启停生命周期
│   │   ├── facade.go               # 分区索引 + 跨域的 JSON 参数助手
│   │   ├── facade_*.go             # 绑定方法按域一分一文件（video / source / douban / plugin …）
│   │   ├── diagnostics.go          # 诊断页快照与实时指标、采集源探测
│   │   ├── logs.go                 # 日志查询/导出/级别切换
│   │   ├── download.go             # 下载传输编排与进度；控制在 download_control.go，检查点/落位在 download_journal.go
│   │   ├── download_http.go        # 直连下载：多线程分片 + 断点重试
│   │   ├── download_hls.go         # m3u8 下载：解析、密钥获取、分片并发
│   │   └── direct_resume.go        # 断点续传清单
│   ├── apperror/                   # 统一错误码（前端 normalizeApiError 依赖它）
│   ├── applog/                     # 日志系统：环形缓冲 + 按天滚动 + 订阅
│   ├── backup/                     # 全量备份导出、导入合并、迁移前快照恢复
│   ├── cache/                      # 统一缓存失效层（写库后按视频/整源广播）与各缓存的命中率读数
│   ├── collect/                    # 采集引擎（fetcher → processor → strategy）
│   ├── collection/                 # 采集/豆瓣调度器
│   ├── db/                         # SQLite 数据层（source / video / douban / catalog / recycle / diagnostics）
│   ├── detail/                     # 详情按需补全
│   ├── douban/                     # 豆瓣爬虫、热榜、评论、302 验证题自解
│   ├── download/                   # 下载注册表与目录策略
│   ├── files/                      # 落盘路径策略（保存目录越界写/删的守卫）
│   ├── handler/                    # 请求处理器（collect / scheduler / source / video）
│   ├── lifecycle/                  # 后台任务组（Go/Stop/Context 统一管理）
│   ├── media/                      # 媒体元数据处理
│   ├── model/                      # 数据模型
│   ├── netstats/                   # 出网计数层：按用途统计请求数、失败、字节与速率，供诊断页读
│   ├── plugin/                     # 扩展包：扫描与校验、注册表、拖放安装与卸载、权限闸门；builtin/ 是随应用自带的三个包
│   ├── proxy/                      # HLS 播放代理、线路测速、海报图片磁盘缓存
│   ├── settings/                   # 持久化设置
│   ├── source/                     # 数据源领域逻辑
│   └── update/ + updater/          # 更新服务与 GitHub Release 检测/下载/安装
│   └── window/                     # 窗口尺寸与关闭行为
├── build/                          # Taskfile、版本提取、图标权重脚本
├── scripts/verify.ps1              # 边界守卫 + go vet + go test + 前端构建
├── scripts/check-frontend-conventions.mjs  # i18n 中英键位 / 未引用键 / cczj-* 类定义检查
├── frontend/
│   ├── bindings/                   # Wails 自动生成的 JS 绑定（勿手改）
│   ├── src/
│   │   ├── api/                    # 前端访问绑定/运行时/事件的唯一出口
│   │   ├── components/             # 公共组件 + ui/ 基础组件
│   │   ├── composables/            # 按视图持有的列表状态与请求竞态
│   │   ├── locales/                # zh-CN.ts / en.ts，键必须严格对齐
│   │   ├── platform/               # localStorage 等平台能力封装
│   │   ├── player/                 # 播放器控制逻辑（hls/、设置、进度、快捷键、缩略图拖拽）
│   │   ├── plugins/                # 扩展包前端侧：注入运行时、cczj API、权限闸门、拖放安装
│   │   ├── router/                 # Hash 路由与滚动恢复
│   │   ├── stores/                 # Pinia
│   │   ├── styles/                 # 主题变量、cczj-* 工具类，以及大页面的 scoped CSS
│   │   │                           #   （views/ 与 components/ 一一对应，SFC 用 <style scoped src> 引回去）
│   │   ├── types/                  # 共享类型
│   │   ├── utils/                  # Anime4K / FSRCNNX 着色器与权重、推荐算法、字幕解析、TS 缓存
│   │   └── views/                  # Home / Search / Recent / Detail / Player /
│   │                               #   Sources / VideoTypes / Favorites / History /
│   │                               #   Downloads / Recommendations / RecycleBin / Settings
│   └── package.json
├── docs/                           # plugins.md（扩展包规范）、source-strategy-v2.schema.json、
│                                   #   examples/（四类样例包）、adr/（架构决策记录）
├── go.mod / go.sum
├── Taskfile.yml / wails.json
├── CHANGELOG.md
└── version.json                    # 版本号来源，构建时注入 updater.Version
```

---

<a id="origin-implementation-notes---快速开始"></a>

#### 🚀 快速开始

<a id="origin-implementation-notes--环境要求"></a>

##### 环境要求

- **Go** 1.25+
- **Node.js** 24（与 CI 一致）
- **Wails v3 CLI**（beta.24）：`go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.24`
- **Task**（可选，用于 `Taskfile.yml` 里的构建任务）
- **Windows 10/11**（WebView2 运行时）

<a id="origin-implementation-notes--安装依赖"></a>

##### 安装依赖

```bash
# Go 依赖
go mod tidy

# 前端依赖
cd frontend && npm install
```

<a id="origin-implementation-notes--开发模式"></a>

##### 开发模式

```bash
task dev            # 等价于 wails3 dev -config ./build/config.yml -port 9245
```

前端改动走 Vite HMR 即时生效；**Go 改动不会自动重启进程**，需要手动退出再 `task dev`。

<a id="origin-implementation-notes--构建生产版本"></a>

##### 构建生产版本

```bash
task build          # go mod tidy -e → 前端构建 → go build 注入版本号
task verify         # 版本/边界守卫 + 前端回归测试与构建 + go vet + go test ./...
```

`task build` 实际执行的链接参数是
`-ldflags="-w -s -H windowsgui -X cczjVideo/app/updater.Version=<version.json 里的版本>"`，
版本号唯一来源是仓库根目录的 `version.json`（由 `build/get_version.ps1` 读取）。产物在 `bin/cczjVideo.exe`。

<a id="origin-implementation-notes--发布新版本"></a>

##### 发布新版本

1. 改 `version.json`：`version` 决定构建注入的版本号；`desc` 是更新弹窗里展示的本次说明；`history` 是历史版本说明，只有比用户当前版本更新的条目才会出现在弹窗里。
2. 同步 `CHANGELOG.md`：把 `[Unreleased]` 落成 `## [x.y.z] - 日期`。
3. 打包：`task package`（= `build/windows/package.ps1`）。它先要求 `frontend/dist` 不比源码旧，
   再产出三件东西到 `dist/`——裸 exe `cczjVideo-windows-<arch>-<版本>.exe`、同名 portable zip、
   `checksums.txt`（coreutils 格式的 SHA-256 清单）。裸 exe 的名字里必须带平台标记：更新器按
   扩展名挑附件，认不出就会去下 zip。
4. 核对版本资源：`task windows:check` 会把版本号从编好的 exe 里读回来（`scripts/verify.ps1`
   也会静态检查 `build/windows/info.json` 的语言键与 FileVersion/ProductVersion 字符串）。
5. 打标签并创建 GitHub Release：`git tag vX.Y.Z && git push origin vX.Y.Z`。Release 正文是更新弹窗
   优先读取的说明来源，`version.json` 只在 Release API 全部失败时兜底。
6. **三个产物都要传到那个 Release 上**，包括 `checksums.txt`：下载完会按它校验，取不到清单或
   校验不过都不装。
7. 把 `version.json` 推上 `main`：回退通道读的是 GitHub Raw / jsdelivr / Gitee 上的 `main` 分支，不推上去兜底就会拿到旧版本说明。

更新检查的顺序是：GitHub Release API 直连 → `gh-proxy.org` 等三个代理（每个源最多重试 3 次）→ 多渠道 `version.json`。安装流程见「设置 → 关于」中的更新弹窗，不会自动下载，需要你手动点。

---

<a id="origin-implementation-notes---使用说明"></a>

#### 📖 使用说明

<a id="origin-implementation-notes--添加数据源"></a>

##### 添加数据源

1. 点击侧边栏「采集源」
2. 点击「添加源」，输入源名称、唯一 Key、API 地址
3. 保存后返回首页，数据将自动加载

<a id="origin-implementation-notes--采集数据"></a>

##### 采集数据

- **手动采集**：侧栏「采集源」→ 源卡片上的「开始采集」或「增量采集」
- **自动采集**：全部在「采集源」页配置。顶部调度状态条的「调度设置」管全局：启用开关、采集间隔、源/页节流、启动补采、首次全量；每个源卡片上的时钟按钮单独设置该源的定时器（定时器需全局开关启用后才会跑）

<a id="origin-implementation-notes--豆瓣数据补全"></a>

##### 豆瓣数据补全

- 默认使用匿名请求，不依赖仓库内的登录 Cookie；网络错误、验证页和重定向不会把条目冷却 24 小时。
- 如确有需要，可在启动程序前通过 `CCZJ_DOUBAN_COOKIE`（或 `DOUBAN_COOKIE`）提供当前有效的 Cookie。Cookie 只从运行环境读取，不应提交到仓库。

<a id="origin-implementation-notes--播放视频"></a>

##### 播放视频

1. 首页浏览或搜索页搜关键词
2. 点击进入详情页查看简介、选集
3. 选择集数进入播放器

<a id="origin-implementation-notes--多条播放线路与测速"></a>

###### 多条播放线路与测速

maccms 类源站用 `$$$` 在 `vod_play_url` 里并列多条「同名集表」（`vod_play_from` 用同一个
分隔符给出线路名）。详情接口按 `$$$` 先切线路、再在每条线路内按 `#` 切集，线路数大于 1 时
详情页和播放页都会出现线路条：点芯片换线路，播放中换线路会按 `ep_num` 找回当前这一集继续播，
全屏不丢；「测速」按钮对每条线路各取一小段样本比吞吐和延迟，最快的标「最快」，探测失败的标
「不可用」，不会自动把你切走。

只有单条线路时线路条整块隐藏 —— 本机当前配置的 `yyzy_tv`、`maoyanapi` 每个影片都只有一条
线路（`1080zyk` / `mym3u8`），要看到线路条需要接入本身并列多条线路的聚合源。

<a id="origin-implementation-notes--集号而不是序号"></a>

###### 集号而不是序号

源站集表的顺序并不可信（新集常排在最前，也有源按热度混排），所以采集时从集名里抠真实集号：
`S01E05` / `第5集` / `5话` / `EP5` / 纯数字都认，数字支持全角和中文数字；`HD中字`、`1080P`、
`BD国语` 这类画质标记不会被误读成"第 1080 集"。认不出来才回落到列表序号。观看进度、TS 缓存键、
下载任务、换线路续播全都以 `ep_num` 认集，所以「看到第 24 集」在下次采集后仍然是同一集。

<a id="origin-implementation-notes--字幕"></a>

###### 字幕

播放控制条上的字幕按钮 → 「加载字幕」选本地 `.srt` / `.vtt` 文件，面板里可以隐藏/显示、更换、移除。
几个边界：

- **必须手动选文件**，Go 侧没有字幕入口，也不解析源站内嵌或外链的字幕轨
- 只支持 SRT/VTT。`.ass` / `.ssa` 不认（Chromium 的 `<track>` 也渲染不了它们）
- 字幕由播放器自己叠加一层 div 渲染，不走原生 `<track>`——画质增强开着时画面上盖的是 WebGL canvas，原生轨会被挡住
- **换集/换线路会清掉字幕**：字幕文件是对着某一集的时间轴配的，换集后必然错位，留着比清掉更误导人，需要重新加载

<a id="origin-implementation-notes--跨源同片"></a>

##### 跨源同片

侧栏的「合并影片库」页面已下线，跨源同片现在只在详情页体现：一次按 `global_id` 的本地查询就能给出「换源看」入口，不碰网络。

- **一个身份 = 豆瓣 ID**，没有豆瓣 ID 时是「归一化标题 + 年份 + 类型」；同一身份在 N 个源里的 N 行目录算作同一部片
- **详情页换源**：`/detail/:sourceKey/:globalId`，同一身份有多个源时详情页出现「其它源也有这部」列表，
  点它换源重开；单源库（本机现状）这块整块隐藏
- **身份合并**：确实该并却没并上的（同豆瓣 ID 或同名同年重复建了身份）在「设置 → 诊断 → 重复分组」
  列出来，可以在那一页直接发起合并。合并复用迁移 v2 的存活行选择逻辑，收藏与观看历史一并迁移到存活身份，
  操作前先弹确认；候选最多 8 个身份一组

<a id="origin-implementation-notes--下载视频"></a>

##### 下载视频

1. 在详情页点击「下载」按钮选择集数
2. 侧边栏「下载管理」查看进度
3. 下载完成后点击「打开」定位文件

<a id="origin-implementation-notes--启用画质增强"></a>

##### 启用画质增强

在播放器页面点画质增强按钮，从下拉里选「原高清」「影视增强」或 Anime4K 的 S/M/L 档，其中会按当前
视频高度给一个档位标「推荐」（≥1080 推 S，≥720 推 M，更低推 L）；选择记在设置里，跨集、跨源保持，
开着增强时可以用对比滑块看左右两侧的差异。Film 模式现在只做 FSRCNNX 超分 + CAS 锐化（「画质增强」
一节写了哪几段着色器还没接线），换集时管线会重建。

<a id="origin-implementation-notes--装一个扩展包"></a>

##### 装一个扩展包

最省事的一种：把包文件夹从资源管理器**直接拖进应用窗口**——整窗都是投放区，正在看视频时
也能拖，不必先跑去设置页。应用按被拖文件夹的绝对路径读进临时目录、按规范整份校验，通过了
才放进 `plugins\`——拖进去一个写坏的包等于什么都没装，「设置 → 扩展包」会给这一拖一条回执
（成功还是失败、为什么失败）；一次拖多个文件夹会逐个装，一个坏掉不影响其余。

不想用拖的就把目录整个拷进 `%APPDATA%\CCZJ Video\plugins\`（例：`plugins\theme-dusk\plugin.json`），
到「设置 → 扩展包」点「重新扫描」就会列出——不用重启。两种装法走同一套规则，目录名要和 `plugin.json`
里的 `id` 一致，一层深度，只认 `plugins/<目录>/plugin.json`。

**卸载有两条路**：卡片上的「卸载」按钮（点下去直接删掉那个包目录，不进回收站），或者自己删那个文件夹；
只想临时关掉请点同一行的「停用」。应用自带的 `motion-effects` / `logs-panel` / `diagnostics-panel` 不给卸载按钮，Go 侧也
拒绝删它们——那是应用自己的东西，不想看到就点「停用」。这几个包只在第一次启动时落盘一次，之后只往里
补应用新带的文件（比如图标），改过的不覆盖、删掉的不复活。

**一个包要动数据或出网，得先声明再授权**：`plugin.json` 里写 `"permissions": ["write", "network"]`，包第一次真去调
写入类的后端绑定、或往应用外面 `fetch` 时弹一次确认，允许还是拒绝记进设置，重启后照旧生效；没声明的那一类
直接拦掉并写日志。递给应用自己那一跳不算出网（调后端绑定就是 `fetch` 到 `wails.localhost`），播放器取分片那一类应用请求也完全不拦。
只做界面、只读数据的包什么都不用写。卡片上会列出这个包声明了哪几项、当前是「已允许 / 已拒绝 /
还没问过」，点「撤销」回到还没问过——那不是永久拉黑，下次用到时再问一次。

每张卡片显示图标（`manifest.icon` 指向包内那张图，不写这一行时包根的 `icon.png` 也自动认）以及从磁盘上
真数出来的「几个文件 · 多大 · 最后改动」，校验失败的包同样给这行数字——那正是用来对照「我改的文件到底
进去了没有」的。四类包（采集适配 / 着色器 / 主题 / 前端注入）各自的字段与边界、以及全部校验规则见
[`plugins.md`](../../docs/plugins.md)；`examples/` 下有六份可直接拷走的样例，六份都带着自己的图标：
`script-ui-tweaks` 是一个「扩展包自检台」页面，把注入型包的能力各用了一遍；`ai-assistant` 是那套能力的
上限样本——一整页 AI 助手，把既有后端绑定当成模型的工具表来调，应用侧不改一行；`icon-gallery` 那一份专门为
看图标而写——卡片图标 + 包目录里 26 枚 PNG，整个文件夹拖进去就能一次验完「拖拽安装 → 校验 → 图标显示」。

写错了不会静默失效：整包判「校验失败」，卡片上直接展开原因码和人话说明，改好再点重扫即可。一个坏包不影响其他包，
也不影响内置功能。

<a id="origin-implementation-notes--看日志与诊断"></a>

##### 看日志与诊断

- 应用内：「设置 → 日志」实时跟随、按级别过滤、搜关键字、翻页看历史文件、导出/复制。
- 应用外：日志按天写在 `%APPDATA%\CCZJ Video\applog\cczj-YYYY-MM-DD.log`，保留天数在「设置 → 基本设置」里改。
- 「设置 → 诊断」的**实时性能**每 2 秒读一次 Go 侧内存计数器（`GetRuntimeMetrics`：不查库、不遍历目录、
  不做全表 COUNT，也不碰 `ReadMemStats`，那个会 STW），摆的是播放效果、按用途拆分的出网吞吐和各缓存命中率。
  出网数字来自计数用的 `http.RoundTripper`（`app/netstats`），速率一律由实测字节与耗时派生，没跑过的用途是空白而不是猜测值；
  TS 分片、海报、图片代理这三块缓存在 WebView 里，Go 侧只看得见它们的未命中，所以由前端自己报数。
- 其余分组是点「刷新」才生成的**快照**：运行时环境、内存与协程、数据库与各表行数、采集与豆瓣调度状态、
  反爬静默与 302 验证题求解结果、数据健康度（缺评分 / 缺条目 ID / 冷却中 / 重复分组）、每个源的采集与巡检健康度点阵。
  除了「合并重复身份」这个会弹确认的写库动作，诊断页不往外发请求；探测采集源在「采集源」页做。
- 诊断面板上的 schema 版本号读的是 `GetDiagnostics` 返回的 `env.schema_version`。Go 侧 `app:ready` 事件
  也带了 `schema_version`，但前端只在 `api/events.ts` 声明了类型、没有订阅——要拿启动期的 schema 版本得自己接。

<a id="origin-implementation-notes--备份与换机迁移"></a>

##### 备份与换机迁移

入口在「设置 → 高级 → 数据备份与恢复」，实现位于 `app/backup/`（读写层在 `app/db/backup.go`）。

- **导出**：把设置、采集源定义、已补全的全局元数据、收藏、观看历史打成一个 Brotli 压缩的 JSON
  （`*.json.br`）。不带目标路径时默认落在 `%APPDATA%\CCZJ Video\exports\backup_<时间戳>.json.br`。
  导出按钮不走系统「另存为」对话框——WebView2 里它点了既不弹窗口也不报错，只会静默失败——
  所以固定写进 `exports\`，同页的「备份文件」列表把这些文件列出来，点一下即可导入，要挪去别处交给文件管理器。
  视频库目录本体（`source_videos`，可能几十万行）不进备份，仍走「采集源」页按源导出。
- **导入**：接受 `.json` / `.json.br` / `.json.gz`（gzip 也认魔数），合并策略是**只增不删**——
  收藏按标题+年份去重、已存在的不动；观看历史按 `(global_id, source_key, 集数)` 取较新的一条，
  旧备份不会把新进度倒推回去；全局元数据只填空列，不覆盖本机已有值。
  源定义只补本机没有的 key，已有的源保持原样。
  窗口尺寸、升级记账（`database_reset_*`、`last_start_version`）、调度水位这类**机器本地**的设置键永不搬移；
  值没变的设置算「跳过」，所以把自己导出的备份再导回去，结果面板每一项都是 0。
- **身份**：库里挂的是自增 `global_id`，换机后完全对不上号，所以备份里一律写成「标题 + 年份 + 类型 + 源坐标」，
  导入时再解析回本机 id；解析不了的条目计数报告出来而不是静默丢弃。
- **迁移前快照**：`%APPDATA%\CCZJ Video\schema-backups\` 下由顺序迁移自动留下的快照，这里列出来并可恢复。
  恢复是**读出再合并**（走同一套只增不删策略），不是文件替换，所以永远不可能拿旧库盖掉当前库里的东西；
  读快照时先复制到临时目录并以 `query_only` 打开，前端只递文件名、后端重建路径并拒绝任何穿越。
  快照滚动保留 5 份。

<a id="origin-implementation-notes--回收站"></a>

##### 回收站

入口在侧栏「回收站」（`/recycle`），数据层在 `app/db/recycle.go`。

- **删除是软删**：详情页的删除只把 `source_videos` 那一行标成 `lifecycle_state='deleted'`，
  收藏和观看历史一行都不动——它们靠目录行的投影条件一起隐藏，恢复后原样回来。
- **恢复 / 彻底删除**：恢复把行放回 `active`；彻底删除只认已在回收站里的行
  （SQL 带 `lifecycle_state='deleted'` 条件），所以这两个入口都不可能误伤还在库里的条目。
  彻底删除同样不级联删收藏/历史，宁可留孤儿行也不删用户数据。
- **跨源**：列表默认汇总所有源，每行自带 `source_key`，所有操作按这对坐标下命令；
  这里故意不套「视频类型」可见性过滤，否则关掉某个类型就等于把数据锁在库里。
- **缓存失效**：单条走 `InvalidateVideo`（先解析 `global_id` 再改状态），
  清空按受影响的源整体 `InvalidateSource`——回收站可能攒上千条，逐条广播前端处理不完。

---

<a id="origin-implementation-notes---开发指南"></a>

#### ⌨️ 开发指南

<a id="origin-implementation-notes--添加新的-go-绑定方法"></a>

##### 添加新的 Go 绑定方法

1. 在 `app/service/` 里为 `App` 结构体加方法：绑定方法按域写进对应的 `facade_<域>.go`（video / source /
   douban / plugin …），业务实现放子包，`facade.go` 只留分区索引和跨域共用的 JSON 参数助手。
2. `wails3 generate bindings -clean` 重新生成绑定，输出到 `frontend/bindings/cczjVideo/app/service/`。
3. 前端**只允许**通过 `frontend/src/api/app.ts` 访问绑定；`scripts/verify.ps1` 会拦截任何直接
   `import 'bindings/...'` 或 `import '@wailsio/runtime'` 的组件。

注意：Wails 会把服务结构体上的**每个导出方法**都暴露给 WebView，所以只想给 Go 内部用的东西
不要写成 `App` 的导出方法（托盘引用就是包级变量 `service.SetSystemTray` 而不是方法，原因在此）。

<a id="origin-implementation-notes--根目录约束"></a>

##### 根目录约束

根目录只保留 `main.go` 与 `app.go`。新增 Go 代码请放进 `app/<包名>/`；
`scripts/verify.ps1` 会拒绝任何重新出现在根目录的 `app_xxx.go`。

<a id="origin-implementation-notes--前端-store-规范"></a>

##### 前端 Store 规范

- 使用 Pinia Composition API 风格（`defineStore('name', () => { ... })`）
- 导出函数和 ref，组件通过 `const store = useXxxStore()` 使用
- 只有跨页面共享的状态才进 store；单页状态留在页面里，组件可以直接调用 `src/api/app.ts`
  的适配函数。`scripts/verify.ps1` 拦的是绕过 `src/api/` 直接 import 绑定或 `@wailsio/runtime`，
  不是"组件不许碰接口"。

<a id="origin-implementation-notes--国际化"></a>

##### 国际化

`frontend/src/locales/zh-CN.ts` 与 `en.ts` 的键必须一一对应，且不允许出现未被引用的键；
`cczj-*` 工具类必须在样式里真实定义。这三条由 `scripts/check-frontend-conventions.mjs` 静态检查，
`scripts/verify.ps1` 在 `go vet` 之前执行它（`node scripts/check-frontend-conventions.mjs` 也能单独跑，
加 `--report-unused` 打印未引用键清单）。需要这层守卫的原因：locales 是无类型的 `export default {}`，
`cczj-*` 是手写 CSS，两者写错 `vue-tsc` 和 Vite 都不会报错。

组件模板里用 `t()`，模板之外（store、工具函数、prop 默认值）用 `tr()`。注意 `tr()` 只有在渲染期间
调用才会随语言切换重算——把它写进 setup 顶层常量或模块级常量就会冻结成挂载时的文案，需要静态列表
时用 `computed`。`<html lang>` 由 `locales/index.ts` 跟随 locale 自动同步。

<a id="origin-implementation-notes--样式约定"></a>

##### 样式约定

主题变量定义在 `App.vue`，全局 CSS 变量自适应深色/浅色：

```css
--bg-app, --bg-secondary, --bg-card, --bg-hover
--text-primary, --text-secondary, --text-muted
--accent, --accent-alpha-10, --accent-alpha-20, --accent-alpha-35
--border, --border-strong
--danger, --warning, --success, --info
```

公共工具类由 `src/styles/cczj-utilities.css` 手写维护，模板可以直接引用，作用域及条件规则通过本地 PostCSS 插件使用 `@apply cczj-*`。模板类由约定检查器校验，未知组合类会使构建失败；生成文档和编辑器提示也纳入同步检查。用法与兼容边界见 [CCZJ 样式复用](../../docs/cczj-styles.md)。

---

<a id="origin-implementation-notes---技术依赖"></a>

#### 📦 技术依赖

<a id="origin-implementation-notes--go-依赖"></a>

##### Go 依赖

| 包 | 用途 |
|---|------|
| `github.com/wailsapp/wails/v3` | 桌面应用框架 |
| `modernc.org/sqlite` | 纯 Go SQLite 驱动 |
| `github.com/jmoiron/sqlx` | SQL 扩展 |
| `github.com/andybalholm/brotli` | Brotli 压缩 |

<a id="origin-implementation-notes--前端依赖"></a>

##### 前端依赖

| 包 | 用途 |
|---|------|
| `vue` | UI 框架 |
| `vue-router` | 路由管理 |
| `pinia` | 状态管理 |
| `@wailsio/runtime` | Wails 运行时 API |
| `hls.js` | HLS 流媒体播放（唯一播放器依赖，UI 与控件自研） |
| `vite` | 前端构建工具 |

界面图标是手写的内联 SVG（`components/Icon.vue`），不依赖图标库。

---

<a id="origin-implementation-notes---更新日志"></a>

#### 📄 更新日志

详细更新历史请查看 [CHANGELOG.md](../../CHANGELOG.md)

---

<a id="origin-implementation-notes---开发规范"></a>

#### 📄 开发规范

当前开发规范统一在 [AGENTS.md](../../AGENTS.md)。[`adr/`](../../docs/adr) 保留历史设计与兼容背景，
不是禁止重构的约束。旧 ADR 0005、0009 已在规范合并时删除，其架构与错误处理要求以
AGENTS.md 和当前实现为准；本文件也不另立一套开发规则。

- [统一开发规范](../../AGENTS.md) — 模块职责、错误传输、数据与生命周期；历史实现允许按证据重构
- [`plugins.md`](../../docs/plugins.md) — 扩展包规范与原因码表
- [`pending-decisions.md`](architecture.md#origin-pending-decisions) — 尚未处理的问题与历史取舍记录；按当前用户指令和证据推进。

---

<a id="origin-implementation-notes---使用许可"></a>

#### 📄 使用许可

本项目**不是**可自由商用的开源软件，许可条款见根目录 [`LICENSE`](../../LICENSE)（当前条款版本 1.0）：

- 免费供**个人学习、研究和私人使用**；未经修改的副本可以不收费地私下交给其他个人。
- 不允许：商业使用、收费分发、改换名称或图标后分发、去除署名、分发修改版。
  需要这些请通过仓库 issue 联系作者取得书面许可。
- 软件不收集也不上传个人数据；媒体库、缓存、日志与设置全部留在本机数据目录
  （Windows 下 `%APPDATA%\CCZJ Video`）。网络请求只用于你自己配置的采集源、
  元数据补全和检查 GitHub 上的更新。
- 按「现状」提供，不作担保；因使用产生的数据丢失、账号受限或法律问题由使用者自担。

首次启动时应用会展示条款摘要并征求同意，同意状态记在本机设置
`license_terms_version` 里；条款升版后会重新征求一次同意。设置 → 关于 里可以随时复看。

---

<a id="origin-implementation-notes---致谢"></a>

#### 🙏 致谢

- [Wails](https://wails.io/) — 优秀的 Go + Web 桌面应用框架
- [Vue.js](https://vuejs.org/) — 渐进式 JavaScript 框架
- [hls.js](https://github.com/video-dev/hls.js) — HLS 流媒体播放
- [Anime4K](https://github.com/bloc97/Anime4K) — 动画超分辨率算法
- [FSRCNNX](https://github.com/igv/FSRCNN-TensorFlow) — 快速超分辨率卷积神经网络

---

<a id="origin-pending-decisions"></a>

## docs/pending-decisions.md

> 历史讨论与待办记录。当前开发规范见 [AGENTS.md](../../AGENTS.md)；本文中的旧“待拍板”不是独立审批规则，按当前用户指令、实际数据影响和已落地实现处理。

<a id="origin-pending-decisions--待判断事项记录"></a>

### 待判断事项记录

这里记的是「已经查清、但方向要人拍板」的事。与 ADR 的分工：ADR 记已经决定的，这一份记还没决定的；
拍板之后就把它挪进 `docs/adr/` 或直接改代码，然后从这份里删掉。

每条都分三层写：**证据**（带文件行号，可查）、**推断**（尚未证实的部分要写明）、**要拍板什么**。

<a id="origin-pending-decisions--2026-10-02"></a>

#### 2026-10-02

<a id="origin-pending-decisions--1-新旧两处都有库时该让哪一份赢"></a>

##### 1. 新旧两处都有库时，该让哪一份赢

- 起因：有 2.1.0 用户更新后反馈「进去数据全读不出来」。
- 证据：`app/service/app.go` 的 `migrateLegacyDatabase`（378 行起）只在「旧落点有库、新落点没库」时复制；
  两处都有库时刻意什么都不做，只留一行 WARN（同文件 387 行）。旧落点是 `<exe 目录>\data`，
  新落点是 `%APPDATA%\CCZJ Video`。
- 证据（这一跳的分界到底在哪一版，已查实）：改动数据目录的是 `c0fff71`「重构：服务层收拢、后台系统下线、
  诊断并入设置」，它就是 2.1.0 那次发布的内容（CHANGELOG 的 `[2.1.0] - 2026-09-27` 同一天）。
  所以 **2.0.x 及更早 = `<exe 目录>\data`，2.1.0 起 = `%APPDATA%\CCZJ Video`**，找回入口探的就是前者
  （`legacyDataDir`，app.go:366 的注释已按这个改口）。另查 `os.UserConfigDir()` 之后拼的目录名
  从 2.1.0 到现在都是 `"CCZJ Video"`，没变过——所以 2.1.0 之后的库彼此在同一处，不存在「2.2 又搬了一次」。
  2026-10-06 拿**从 GitHub 下的官方 v2.1.0 产物**在这台机器上实测过：它的 `application ready` 报的
  `data_dir` 就是 `%APPDATA%\CCZJ Video`，这条分界从推断变成了观察到的事实（顺带炸出来的问题见第 10 条）。
- 证据（别用 tag 当依据）：`v2.1.0` 指向 46ccb02（2026-07-20「大重构前更新」）、`v2.2.1` 指向 cb67bea（6 月），
  都不是对应版本的发布点。`git grep UserConfigDir v2.1.0` 查不到，只说明 tag 是坏的，不说明 2.1.0 没搬目录。
  判版本内容要按提交（c0fff71 / 5fadd03）查，见第 4 条。
- 证据（发布线上根本没有 2.0.4 这一版）：GitHub Releases 逐个看下来是 v2.0.3（2026-07-08，29,466,112 字节）
  直接跳到 v2.1.0（2026-09-27，30,279,680 字节），而 v2.1.0 的正文自己写着「包含未单独发布过的 2.0.4」。
  所以从没发布过 2.0.4：读 `<exe 目录>\data` 的最后一版是 2.0.3，2.1.0 起就在 APPDATA 了（3 次下载，确实有人在那）。
  本机这个自报 2.0.4 的 exe 不是发布产物——`go version -m bin/old-cczjVideo.exe` 给出
  `-X cczjVideo/app/updater.Version=2.0.4` 与 `mod cczjVideo v0.0.0-20260720092854-85a1e9ad3cea+dirty`，
  即 46ccb02 之后带未提交改动的一次本地构建（wails v3.0.0-alpha2.117，比现在这套旧一个 beta 段）。
- 证据（认 exe 要认 buildinfo，别认文件名 / mtime / 界面版本号）：Go 1.18+ 把提交号与 ldflags 打进二进制，
  `go version -m <exe>` 一次给全；文件名和文件时间都会被改名、复制、`cp -p` 改掉——本机那个 exe 的 mtime
  是 2026-08-06，buildinfo 说源树是 2026-07-20。applog 里 JS 报错带的前端 bundle 文件名
  （`index.e7d13ca2.js`）可以反查是哪个 exe 嵌的：Desktop / Downloads / Documents / AppData 与 D 盘顶层找下来，
  只有 `bin/old-cczjVideo.exe` 含它，2.3.0 / 2.3.1 的产物都没有；`%APPDATA%\old-cczjVideo.exe\EBWebView`
  的时间戳也正好落在那次会话上。
- 推断（不影响上面的分界，只影响这台机器怎么判）：46ccb02 那棵**已提交**的树里 `getDataDir` 还是
  `<exe 目录>\data`（`git show 46ccb02:app.go` 251 行），而它今天把日志和库都落在 `%APPDATA%\CCZJ Video`，
  说明那次 `+dirty` 已经含上了目录搬迁的改动。所以「界面写着 2.0.4」不等于「它会去读 exe 旁边的 data」——
  判一台机器该探哪个落点，要看那个 exe 的 buildinfo 与实际日志，不能只看版本号。
- 推断：热替换失败后用户手动点过一次 `CCZJ-Video-Update.exe`，若那一次已经在 `%APPDATA%` 建起一个空库，
  之后从原目录启动新版就永远走这条「不动」分支——界面看着是空的，而旧库原地未动、并没丢。
  测试机说的「从 2.1.0 开始每个版本都检测不到原有数据」按上面的分界只有两种成因：他的数据其实是
  2.0.x 时代攒的（在 exe 旁边，2.1.0 起就没人读了），或者他把 exe 换过目录（探的是现在这个 exe 旁边的 data）。
  2026-10-06 补第三种：装过 2.2.0 及以后（库被迁到 v10）再回头跑 2.1.0，源列表查询会整个失败，
  首页连带变成「暂无数据」——库好好的，看上去却像数据没了，见第 10 条。
  要证实只需要测试机当天的 applog（`%APPDATA%\CCZJ Video\applog\` 或 `<exe 目录>\data\applog\`）：
  看有没有 `[DataDir] 新旧两处都有库`、有没有 `已迁移旧数据库`，以及 `application ready` 里的 `data_dir`。
- 要拍板：要不要把它改成可判定的——新库查出 0 条视频、旧库有数据时，把新库改名 `.bak` 再搬旧库。
  反过来说这就是「允许新版覆盖一个刚建好的空库」，方向猜错会覆盖掉用户这几个月的数据，所以不擅自做。
- 已落地的替代路径（不改上面那条的取舍）：这一支不再等于数据没了。应用会把那份旧库报出来
  （位置 / 大小 / 能找回多少收藏历史与源），用户点一下才并进来，设置 → 高级 里长期留着同一栏；
  实现见 `app/service/legacydata.go` 与 `app/backup/legacy.go`，合并只读旧库、只进不出。
  「自动判定 + 自动搬」要不要做仍然没定，手工那条只是让不等用户点头时不再无路可走。
- 不改代码也能救回去（仍可用）：退出应用 → 把 `%APPDATA%\CCZJ Video\cczj_video.db` 连同 `-wal` / `-shm`
  改名 → 用 2.3.0 的 exe 覆盖原目录（旧 exe 改名留着）→ 启动，日志应出现 `[DataDir] 已迁移旧数据库`。

<a id="origin-pending-decisions--2-210-的老客户端热替换这一跳修不了了"></a>

##### 2. ≤2.1.0 的老客户端，热替换这一跳修不了了

- 证据：`git show v2.1.0:app/updater/updater.go` 里的 `buildSwapScript` 把绝对路径当字面量写进 .bat 正文，
  而 .bat 按 UTF-8 落盘；cmd 会用控制台代码页（本机实测 936）重新解释正文，中文目录能把一整行 `if`
  解析到别的命令上去。2.3.0 已改为四个路径全走 `%~1`–`%~4`、正文纯 ASCII、30 次有界重试、先改名不删除
  （`app/updater/install.go`）。
- 影响：装着 2.1.0 的机器，往 2.3.0 的第一跳只能手动覆盖 exe 一次；此后 2.3.x → 2.3.y 才走得上。
  另外 2.1.0 在替换**之前**就写了 `<exe 目录>\.installed_version`，替换失败也把 2.3.0 记上了，
  而 `EffectiveVersion` 取编译版本与标记里的较高者——所以那台机器此后不会再提示更新，
  要重新提示得删这个文件。
- 要拍板：要不要在 Release 说明或更新弹窗里，对 ≤2.1.0 单独给一句「请手动安装一次」。只改文案、不动逻辑。
- 2.3.1 已按「给一句」处理：`version.json` 的 desc 写明了手动覆盖那一次，并按第 1 条的实际情况改口——
  旧库只有在「新落点还没建库」时才会被自动搬，已经建过库时不搬也不覆盖，需要手工挪文件或等带找回入口的版本。
  若不同意这个口径，改文案不用改代码。

<a id="origin-pending-decisions--3-拖放安装的投放区范围已定随-231-发"></a>

##### 3. 拖放安装的投放区范围（已定，随 2.3.1 发）

- 要求：拖文件夹装扩展包只在「设置 → 扩展包」这一页生效。
- 落地：删掉 `document.body` 与 `.app-shell` 上那两个全局 `data-file-drop-target`，
  全应用只剩 `frontend/src/components/ExtensionsPanel.vue` 的 `.ext-card` 一处；
  `frontend/src/plugins/dropInstall.ts` 另加一道「投放区不可见就不装」的判断。
  订阅仍然挂在应用生命周期上——Go 每次拖放只把路径交上来一次，晚挂会漏。
- 已定：已发布的 2.3.0 三个附件早于这次修改，那份仍然是「到处能投」，不回头替换附件，并入 2.3.1。

<a id="origin-pending-decisions--4-远端孤儿-tag"></a>

##### 4. 远端孤儿 tag

- 证据：`v2.2.1` 有 tag、无对应 Release；2.2.0 也没发布出去，最新可见的是 v2.1.0 与 v2.3.0。
- 证据（tag 指错了提交）：本地 `v2.1.0` 指向 46ccb02（2026-07-20「大重构前更新」），`v2.2.1` 指向 cb67bea
  （2026-06-27，动画增强那次），都不是各自版本的发布点——2.1.0 的实际内容是 c0fff71。
  所以「某版有没有某个改动」要用提交查，用 `git grep <sym> v2.1.0` 会得到错答案。
- 要拍板：删不删 `v2.2.1`，以及要不要把 `v2.1.0` / `v2.2.1` 重指到正确提交（重指要强推 tag，属共享状态）。
  两件都不可逆或影响别人，所以不动。

<a id="origin-pending-decisions--5-防商用第二层"></a>

##### 5. 防商用第二层

- 第一层（静态许可条款 + 首启同意闸门）已落地。第二层是「可见水印 + 签名授权文件」。
- 要拍板：A（水印）还是 B（签名授权），以及「授权解锁到底给用户换来什么」。定了才动代码。

<a id="origin-pending-decisions--2026-10-03"></a>

#### 2026-10-03

<a id="origin-pending-decisions--6-旧数据找回只认当前-exe-旁边的-data"></a>

##### 6. 旧数据找回只认「当前 exe 旁边的 data」

- 现状（已落地）：`app/service/legacydata.go` 的候选目录只有一个——`legacyDataDir()`，也就是
  正在运行的那个 exe 所在目录旁边的 `data`。旧数据在这台机器上、且没换过安装目录时够用。
- 缺口：如果用户是把新版装进**另一个文件夹**再打开的，旧库在老 exe 旁边，这一处扫不到，
  提示和设置页都会显示「没有找到旧版本的数据库」。测试机恰恰是手动覆盖安装，正是这种形状。
- 要拍板：要不要补一个「选目录」入口。代价是先要能弹原生文件夹选择器——本项目里
  `Dialogs.SaveFile` 已实测在 WebView2 下点了不弹也不报错（见 `app/backup` 走 exports 目录的原因），
  `SelectFolder` 同样没验过。可选路径三条：A 先用 `Dialogs.SelectFolder` 真机试一次，成不成才知道；
  B 不做对话框，让用户把旧 `data` 文件夹拖进窗口（复用扩展包那套原生拖放，只读路径不装包）；
  C 让他手填路径，后端要多一道「这个目录能不能当旧库读」的校验与越界防护。
  三条都比现在多一个能收任意输入的面，所以等他定方向再动。
- 眼下能自救的路径：把旧 `data` 文件夹整个拷到新版 exe 旁边，重启应用就能被扫到。

<a id="origin-pending-decisions--7-这批修复进-231-还是-232"></a>

##### 7. 这批修复进 2.3.1 还是 2.3.2

- 证据：`dist` 里已有 2.3.1 的 exe / 便携包 / checksums.txt，`version.json` 也定版到 2.3.1；
  那一份产物不含今天这三件（旧数据找回、校验清单多通道、轮播交棒闪切）。
- 推断：GitHub Release 的 publish 仍是要人工点的（见记忆里那条），所以现在改口不等于改了已发布的附件。
- 要拍板：A 重新构建一份 2.3.1 带上这三件（改动里有 Go 新绑定与新界面，等于换掉定版产物）；
  B 直接开 2.3.2，2.3.1 保持已构建的那份，`version.json` 的 desc 按 B 轮转。
  构建要你点头，所以两边都不动。

<a id="origin-pending-decisions--8-回滚副本会常驻程序目录"></a>

##### 8. 回滚副本会常驻程序目录

- 现状（已落地）：热替换动手前先 `copy` 一份 `cczjVideo.exe.old`，启动时不再自动删（旧逻辑是
  「版本对得上就清理」，等于每次刚装上就把退路撤了）。有这份副本时，设置 → 关于 才出现「退回上一版」。
- 代价：程序目录常驻一个和 exe 同大小的文件——当前构建 31,163,904 字节（约 30 MB）。只留一份，
  下一次安装的 `copy /y` 会覆盖它，不会越攒越多。
- 要拍板：A 就这样，退路常驻；B 加一个「不保留回滚副本」开关，勾选后安装时跳过 copy，代价是那台
  机器上再也没有一键退回；C 让副本过期（新版连续正常启动 N 天后自动清理），要新增一个启动计数。
- 备注：C 盘紧张是这台机器的既有约束，但 30 MB 相对应用目录本身不算大；这条主要是给磁盘紧的用户
  一个选择，所以等他定方向再动。

<a id="origin-pending-decisions--9-退出时安装要不要变成默认档位"></a>

##### 9. 「退出时安装」要不要变成默认档位

- 现状（已落地）：下载完成后并排三个按钮——「忽略此版本」「退出时安装」（安排上了就地变成「取消退出时安装」）
  「立即更新」，两档都要人点一次，没有默认、也不会自动安排。机制见 `docs/adr/0010-install-timing-tiers.md`。
  原先那枚写着「下次启动」的按钮改掉了标签：它点的一直是「忽略此版本」，从来不会在下次启动装任何东西，
  而真有了退出时安装这一档，两个词更容易被读成同一件事（标签换了，行为没换）。
- 证据：这条链路本来就是刻意不做自动的（`important-decision-experience-a1f22f0f`：更新弹窗禁自动下载），
  所以「退出时安装」也只做成显式选择；安排上了会在按钮位显示「取消退出时安装」，随时能撤。
- 推断：真正被打断的是「每次都手点」这一步——如果他自己的用法是「下完就让它下次退出时装」，
  那做成默认能省一次点击；但代价是某次退出会装上他其实已经改主意的版本。
- 要拍板：A 保持两条都要人点（现在的样子）；B 加一个设置项「下载的更新包默认在退出时安装」，
  勾选后下载完成即自动安排，界面上仍然可以取消；C 更激进——发现新版本就后台下好并安排退出时装，
  等于把「禁自动下载」那条推翻，需要他明确改口。
- 依赖：B 只是在现有 `ScheduleInstallOnExit` 上加一个开关与一次调用，C 还要改下载闸门；
  他定方向之前不动。

<a id="origin-pending-decisions--2026-10-06"></a>

#### 2026-10-06

<a id="origin-pending-decisions--10-旧版读到新库当时不拦也不出声只在中途炸-sqlx已定--adr-0011"></a>

##### 10. 旧版读到新库：当时不拦也不出声，只在中途炸 sqlx（已定 → ADR 0011）

- 起因：本机跑了一次自报 2.0.4 的旧构建（`bin\old-cczjVideo.exe`），刚打开就弹「加载源站列表失败」。
- 证据（**从 GitHub 下的官方 v2.1.0 也一样炸**，所以这不是本机那个本地构建的专属问题）：
  12:06:59 那段日志同样三条 `missing destination name auto_disabled_at`，前端 bundle 是
  `index.e91a91f5.js`（与本地那个 2.0.4 构建不同，证明确实是另一个 exe）。它的 `application ready`
  里 `data_dir` 已经是 `%APPDATA%\CCZJ Video` —— 第 1 条那条「2.1.0 起改读用户配置目录」的分界，
  到这里从「按提交推断」升级成了「发布产物实测」。
- 证据（撞库的版本范围只有一个）：`auto_disabled_at` 这条 v10 迁移是 **e8f0ff8** 加的，而那次提交发布的
  版本号是 **2.2.0**（`git show e8f0ff8:version.json`）。所以按第 1 条的目录分界推：**2.0.x 看不见这份库
  （读的是 exe 旁边），2.2.0 及以后自己就带这一列，唯一会撞上的是 2.1.0 这一版**——今天实测吻合。
  GitHub 上 v2.1.0 有 3 次下载，所以外面确实有人处在这个位置上。
- 证据（界面后果比那枚红 toast 大）：截图里左下角是「未选择来源」、首页整块「暂无数据」，
  而库里 `global_video` 有 77 条。也就是说「打开就是初始状态」这一类反馈，有一种成因是源列表查询
  整个失败，而不是数据真没了。
- 证据（报错是哪来的）：`%APPDATA%\CCZJ Video\applog\cczj-2026-10-06.log` 里 11:39:45–11:40:02 那一段，
  三条 `missing destination name auto_disabled_at in *[]*model.Source`，界面那条 ERROR 之外，
  热榜侧跟着「获取采集源失败 / 没有可用的采集源」。`auto_disabled_at` 是迁移 v10 加的列
  （`app/db/migrations.go:39`，死源自动停用），旧版的 `model.Source` 没有这个字段，而查询是 `SELECT *`
  （`app/db/backup.go:136`、`app/db/source.go:16`）——结果集比结构体多一列，sqlx 当场报错。
  这是**旧程序读新库**，不是当前代码的缺陷。
- 证据（它读的是哪一份库）：这台机器上只有 `%APPDATA%\CCZJ Video\cczj_video.db` 带这一列
  （`PRAGMA user_version` = 10）；`bin\data\cczj_video.db` 是 v0、没有这一列，文件时间还停在 7-22，
  这次完全没被碰。
- 证据（降级为什么不报「版本太旧」）：`migrateToLatest`（`app/db/migrations.go:102`）对
  `m.version <= current` 一律跳过——库比程序新时它既不动作也不出声，程序照常起来，照常往库里写。
  那次启动确实写了 v10 库（`[DoubanChart] 入库完成: 新增 0 + 更新 10`）。
- 推断：这次只是恰好炸在 `sources` 上。同一棵旧树里任何 `SELECT *` 都可能同样炸（生产代码里 11 处）；
  更值得注意的是写路径——旧版按自己认识的列写，不会改坏新列，但会按旧语义覆盖新版在维护的数据
  （热榜缓存、源状态这类）。这一条没有真机样本可证，只能算推断。
- 证据（这条是连带查出来的，关系到刚做的「退回上一版」）：`SwapBackupStat` 只看副本在不在、多大
  （`app/updater/install.go:357`），`RollbackUpdate` 也不问库是几版（同文件 378 行起）。所以
  **哪一版加了列，从那一版退回上一版就会落进今天这个状态**——程序换回去了，库回不去。
  副本本身没有版本标记（第 8 条记过），现在也没地方能查出它认哪一版结构。
- 已定并已落地（2026-10-06，他给的是「按你的想法来，把问题解决掉」，所以这三件由我拍）：
  1. **不拦**——选了 A 档。启动时比 `user_version` 与本构建 `LatestSchemaVersion()`，库更新时照常读写，
     只把结论说出来。取舍与代价写在 `docs/adr/0011-read-newer-library.md`（B/C 两档为什么否掉也在里面）。
  2. **让查询对多出来的列免疫**——选条件版：只在「库比构建新」这一种情形把实例换成 `db.Unsafe()`
     （`app/db/migrations.go` 的 `applySchemaAhead`，`app/db/sqlite.go:123` 处接入），平时仍是严格映射，
     写错 `db` tag 照样当场报错。没有去改那 11 处生产 `SELECT *`，理由在 ADR 的 Rejected alternatives。
     单测：`app/db/schema_ahead_test.go` 两条都过（正常/升级路径不误判；未知列下严格句柄报错、
     宽松句柄读得出来且共用同一个 `*sql.DB`）。
     还是那句：**救不了已经发出去的 2.1.0/2.2.0**，只是让将来的旧客户端不再被一次加列打穿整个界面。
  3. **「退回上一版」不加结构闸门**——副本没有版本标记，加闸门要在安装时另存一份状态。改成两头都说话：
     退之前 `rollbackMsg` 补一句「退回的那版读不懂这个版本写过的新字段」（中英都补了），
     退之后由第 1 条那条提示接管。
  - 界面上多了一个启动弹窗：`frontend/src/components/SchemaNewerPrompt.vue` + 绑定 `App.SchemaNotice()`。
    排期变成四个：条款闸门 → 更新弹窗 → 旧数据找回 → 这条。旧数据那个原先把开关留在组件里，
    后面的就没法知道该不该开口，所以 `legacyPromptOpen` 提到了 `frontend/src/stores/legacyState.ts`。
    「暂时不提」存的是**库的版本号**不是布尔：库再被更新的程序写高一次，就得再提一次。
- 已核（写在这里免得下次重查）：`auto_disabled_at` 那条迁移是 `INTEGER NOT NULL DEFAULT 0`
  （`app/db/source.go:139`），带默认值，所以旧版的 INSERT 不会因为它撞约束。**将来**加不带默认值的
  NOT NULL 列时，`Unsafe()` 救不了写路径——这条只算边界，不是今天的缺陷。
- 证据（旧版写完，新版的库没坏）：紧接着 11:41 的 2.3.1 会话读同一份库一切正常：三个采集源全部列出且都是启用、
  热榜匹配 9/10、`user_version` 仍是 10。界面提示条也没落下「上次替换没生效」之类异常回执。
  样本只有这一次，所以它支撑的是「不拦」这个选择，不是「旧版写新库无害」这个结论。
- 验证到哪一步（2026-10-06 收尾时）：**只有静态验证**。`gofmt`/`go build`/`go vet` 全 0，`go test ./app/db/ ./app/service/`
  过（含 `schema_ahead_test.go` 那两条），`vue-tsc --noEmit` 与 `check-frontend-conventions.mjs` 绿（中英各 1313 键、未引用 0）。
  **那张新弹窗一次都没在真机上见过**：它要 `wails3 build` 才有产物（他现在是「不进行构建」），而带单实例锁的构建
  又要求主程序退出。同样没被真机走过的还有「旧 exe 读到新库时条件 `Unsafe()` 真的把列表捞回来了」——
  D 场景里跑的是**没有这个修复的 2.1.0**，它只能证明缺陷在，不能证明修好了。所以第 2 条那句「将来的旧客户端
  不再被打穿」目前是**代码事实 + 单测事实**，不是发布产物实测。
- 不建议做的事（写在这里免得下次又试）：不要拿旧 exe 去"回退"当前数据。真要退回旧版，用设置 → 关于
  里的「退回上一版」（那是退回**同一份库**上的旧程序，见第 8 条与 README 的那一条），或者手工换 exe 但
  别换库。这台机器上 `bin\old-cczjVideo.exe` 这种本地构建的旧 exe 容易被当成"官方旧版"再点一次，
  要不要删掉由他决定，我没动它。

<a id="origin-pending-decisions--11-新版本找不到数据库的复现现场以及为什么拿-230-升级试不出来"></a>

##### 11. 「新版本找不到数据库」的复现现场，以及为什么拿 2.3.0 升级试不出来

- 起因：他问怎么复现测试者那句「从 2.1.0 开始每个版本都检测不到原有数据，打开就是初始状态」，
  打算下载 v2.3.0 再升一次试试。
- 结论先说：**这一跳试不出来**。2.3.0（e14a435）与 2.3.1（6a883ac）同数据目录、同结构版本（都 10），
  而更新是原地换 exe、不动目录——从 2.3.0 到 2.3.1 没有任何能触发「找不到库」的分支。
  真能触发的只有跨 2.1.0 那道目录分界，或者旧 exe 撞上新库（第 10 条）。
- 证据（版本能力矩阵，逐提交 `git grep -l` 核过，不是按 tag）：

  | 版本（定版提交） | 数据目录 | 自动搬旧库 `legacyDataDir` | 强制单实例 | v10 迁移 `auto_disabled_at` |
  |---|---|---|---|---|
  | 2.0.x 及更早 | `<exe>\data` | 不需要 | 无 | 无 |
  | 2.1.0（c0fff71 代码 / b0408b9 定版） | `%APPDATA%\CCZJ Video` | **无** | 无 | 无 |
  | 2.2.0（5fadd03 定版；e8f0ff8 仍写 2.2.0） | 同上 | **无** | **有**（5fadd03） | e8f0ff8 起才有 |
  | 2.3.0 / 2.3.1（5a9bdae 起） | 同上 | **有**（5a9bdae） | 有 | 有 |

- 证据（2.2.0 那一行有半格是空的）：`auto_disabled_at` 所在的 e8f0ff8 与自己声明的 version.json 都是 2.2.0，
  而 5fadd03 里没有这条迁移，所以我无法只凭仓库断言 GitHub 上那份 v2.2.0 资产带不带 v10（tag 本来就指错，见第 4 条）。
  这条直接影响第 10 条「唯一会撞库的是 2.1.0」的范围：如果发布的那份 2.2.0 早于 e8f0ff8，撞的就是 2.1.0 **和** 2.2.0。
  判据很便宜：拿一份 v10 库的**副本**喂给手上那份 v2.2.0 资产，日志出 `missing destination name auto_disabled_at` 就说明它不带 v10。
- 证据（沙箱为什么能把两份 exe 都关进去）：`os.UserConfigDir()` 在 Windows 上取的就是 `%APPDATA%`，
  所以给启动那条命令设 `APPDATA=D:\...\roaming`，数据目录（`app/service/app.go:363`）、日志目录
  （`app/applog/applog.go:106`，落在 `%APPDATA%\CCZJ Video\applog`）与 WebView2 的用户数据目录会一起进沙箱，
  主的 `%APPDATA%\CCZJ Video` 一个字节都不动。之前 `D:\视频测试\roaming\CCZJ Video` 就是这么来的。
- 证据（沙箱并行跑的前提是关掉主程序）：单实例锁自 5fadd03 起声明 `UniqueID: "com.cczj.video"`（`app.go:35`），
  Wails beta.24 在 Windows 上是按这个名字开互斥体 `wails-app-com.cczj.video-sim`
  （`single_instance_windows.go:34`，无 `Global\` 前缀＝同登录会话内共享），
  第二份进程只负责把已在跑的窗口叫到前台，然后 `os.Exit` 掉（`application.go:207-212`）。
  所以 2.2.0 及以后的沙箱副本**必须等主程序退出**才起得来；2.1.0 没有这段代码，它能随时起，也不会把主窗口顶出来。
- 推断（`bin\old-cczjVideo.exe` 不能用来测跨界那一跳）：它自报 2.0.4，可今天日志里它的 `data_dir` 已经是 APPDATA
  （第 1 条），所以它读不到 `<exe>\data` 那份旧库，测不出「2.0.x 升级后数据留在原地」的原貌。
  要那个原貌得用 GitHub 上 v2.0.3 及更早的资产——那是下载动作，没他点头我不做。
- 复现的四个现场（2026-10-06 四个都跑了：A、D 先照预期重现，B、C 在他同意真机验证之后补上）：
  - **A 跨界丢数据（投诉原貌）——跑通了**。roaming 空着，把 7 月那份有数据的库放 `<exe>\data`，起**官方 2.1.0**：
    界面开出来就是空的，而它的 applog 里**一条 `[DataDir]` 都没有**（那版没这段代码，`grep DataDir` 结果为空）。
    更要紧的是它在 roaming 里自建了一份 v0 空库——这一步就是「自动搬库的前提从此不成立」的成因
    （`app/service/legacydata.go:14-19` 那段注释讲的正是这件事），也解释了为什么他试哪个版本都看不到数据。
  - **D 旧版撞新库——跑通了**。roaming 放当前库的副本（`.db` 连 `-wal`/`-shm` 一起拷），起 2.1.0：
    12:34:41 三条全数复现——`[chart.go:396] 获取采集源失败`、`[chart.go:402] 查询所有源失败`、
    `[facade.go:691] 加载源站列表失败`，文案都是 `missing destination name auto_disabled_at in *[]*model.Source`。
    而且它**往这份 v10 库里写了数据**，和第 10 条在真库上看到的一模一样。
  - **B 2.0.x 的那份库换当前构建——自动搬那一支跑通了**（16:23:46，沙箱 `D:\cczj-bc\B`）。roaming 空着，
    把 7 月那份真实的 2.0.3 库放进 `<exe>\data`，起当前构建，逐条落在日志里：
    `[DataDir] 已迁移旧数据库: D:\cczj-bc\B\app\data -> D:\cczj-bc\B\roaming\CCZJ Video`（app.go:414）→
    「迁移前已备份数据库 …\schema-backups\cczj_video_pre_migration_20261006_162346.db」→
    `已应用迁移 v1 … v10` 十步顺序走完（`user_version` 0 一路到 10）→ `application ready`，整份日志 0 条 ERROR/FATAL。
    之后 `[Updater] 版本变化: 2.0.3 -> 9.9.9`（service.go:365）——`last_start_version` 存在库的 settings 表里，
    所以这一句同时说明搬过来的那份库**被读到了、也被写回去了**，不是起了个新空库。
    行数逐张对过：watch_history 3→3、sources 1→1、favorites 0→0、global_types 13→14、source_types 13→14、
    source_videos 40→48、global_video 40→51、settings 6→9。涨的那几张在日志里点得到名
    （`[chart.go:437] 入库完成: 新增 9 + 更新 10 = 处理 19/10 条 (global_video: 40 → 49)`）：
    首次启动本来就要抓热榜、跑采集，不是搬库搬出来的。**要说清的副作用**：这一跑沙箱是真联网的，
    豆瓣 `movie.douban.com/chart` 与源站 `api.yyzy-tv.vip` 各挨了请求（约 1 秒内十来条搜索），
    那是首启 chart_match 的既有行为、不是我另外起了爬虫——记在这里免得下次以为是我加的负载。
  - **C 两处都有库那一支——后端跑通了，界面那一半没跑**（16:26，沙箱 `D:\cczj-bc\C`）。先让当前构建在
    roaming 建起空库，再把旧库放进 `<exe>\data`，重启：
    `[DataDir] 新旧两处都有数据库，保留新目录 …，未改动 D:\cczj-bc\C\app\data`（WARN，app.go:407，16:26:13.337）
    → `[Legacy] 发现可找回的旧数据库: D:\cczj-bc\C\app\data（收藏 0、历史 3、源 1）`（legacydata.go:106，16:26:14.022）。
    roaming 那份库大小前后都是 151,552 字节，没被盖掉。**没测到的那一半**：提示条真在屏幕上长什么样、
    点「合并」之后的现场。合并本身由 `app/backup/legacy_test.go` 拿真库测过（旧库 `query_only` 打开、只进不出），
    所以缺的是界面那一环的实测，不是数据正确性。
- 撤回一条我自己造出来的假缺陷（写在这里免得下次把它当事实）：第一次拿沙箱那份库和真库对行数，
  看到 `watch_history` 61 vs 62、还有一行的 `updated_at` 更旧，读起来就像「2.1.0 删了一条历史」。
  实情是**真库是活的**：`-wal` 的 mtime 在 12:37，而我的副本抄于 12:34；那条"少掉"的行 `id=18611`
  写于 2026-10-06 04:36:09 UTC（本地 12:36），就在我抄完之后。**没有任何数据丢失，丢的是我的基线。**
  教训：对着正在被使用的库做前后对比，抄完立刻记时间，比完先看 `-wal` mtime 晚于抄写时刻就不作数。
- 复制库的注意（这条仍然成立，上一条踩的就是它）：`.db` 要连 `-wal`/`-shm` 一起拷
  （程序自己搬库时就是这么做的，`copyDatabaseFiles`），而且要在他关掉主程序之后拷——否则未 checkpoint
  的近期事务还留在 WAL 里，拷出来的样本"少了最近一段"，看着像丢数据其实是我们自己抄漏。
  沙箱路径保持 ASCII。
- 已删：沙箱 `D:\cczj-repro`（两份官方 exe 的副本 + 四份库副本 + 各自的 roaming，约 110 MB）验完删掉了。
  今天补 B/C 用的是本机新构建的两份当前 HEAD 二进制（只差 `-X updater.Version`，8.8.8 / 9.9.9），
  放在 `D:\cczj-bc`，验完一并删。
- 要拍板：无新决策。第 10 条那三件已定并已落地（ADR 0011），A/B/C/D 四个现场今天都有账了；
  剩的只有 C 的界面那一半（提示条上屏 + 点合并），要不要补由下一次真机决定。

<a id="origin-pending-decisions--12-点安装后只关不开在当前代码里还走得通改名那一支换入成功start-被单实例锁吃掉已定--adr-0012"></a>

##### 12. 「点安装后只关不开」在当前代码里还走得通：改名那一支换入成功、start 被单实例锁吃掉（已定 → ADR 0012）

- 起因：他问的是「现在更新会不会出问题，还会不会出现点了安装只关软件不打开、更新后找不到数据库」。
- 结论先说：**会，而且这一条不是"已经发出去的版本救不了"那类**——它走的是当前代码里还活着的一条分支，
  已修。第 10/11 条管的是"数据看不见"，这一条管的是"程序回不来"，两件事的成因完全不同。
- 证据（机制，逐行核过 `app/updater/install.go`）：
  1. 脚本的重试预算是 30 次 × `ping -n 3`（实测 2.03 秒）≈ 60 秒，烧完进 `:RENAME_REPLACE`。
  2. `:RENAME_REPLACE` 里 `ren "%OLD%" "%BASE%.old"` 之后紧跟 `move /y "%NEW%" "%OLD%"`，成功就
     `goto :REPLACE_OK` → `echo ok>` 结论文件 → 走到 `:START_NEW` 的 `start "" "%OLD%"`。
     **给正在运行的映像改名是 Windows 允许的（删除才不允许）**，所以这一支恰好在旧进程还没退场时
     能把新版换进去——而它存在的理由正是"文件还被占着"。
  3. 换进去的那一行 `start` 启动的是新 exe，而旧进程仍持有单实例互斥体：`UniqueID:
     "com.cczj.video"`（`app.go:34`），beta.24 在 Windows 上按这个名字开 `wails-app-com.cczj.video-sim`，
     第二进程把已有窗口叫到前台后自己 `os.Exit`，随后旧进程才走。
- 证据（撞上的窗口有多宽，不是断电级冷门）：`shutdownHardStop = 60 * time.Second`
  （`app/service/app.go:47`）与脚本 ~60 秒的重试预算**重合**。也就是说一次偏慢的退出正好把 `move`
  逼进改名那一支，而那正是本进程还在的时候。
- 证据（它静默到什么程度）：结论文件里写的是 `ok`。第 195 条那条读回执的通道下次启动无话可说，
  日志里也不会有任何一行提到"被当成第二实例"——那句 `[SingleInstance] 检测到重复启动，已激活现有窗口`
  是**旧进程**打的，而它当时正在退出。用户看到的就是"点安装 → 程序没了"。
- 证据（参数格式是拿真 `cmd.exe` 跑出来的，不是推的）：原先的 `--cczj-relaunch=1234` 进脚本之后
  `%~5` 只剩 `--cczj-relaunch`，PID 掉到 `%6`——`=` 和空格、tab、`,`、`;` 一样是 cmd 的批处理参数
  分隔符。测试输出里那一行 `would-start --cczj-relaunch`（少了数字）就是当时的原文。
  所以交接格式改成冒号，`TestTargetPID` 把 `=` 的两种旧写法判死，
  `TestArgSurvivesCmdBatchParameterSplitting` 禁止 `Arg` 的输出里出现 ` \t=,;`。
  这条如果不跑真 cmd 就会一直"看起来没问题"：Go 侧 `Arg`/`TargetPID` 的单测怎么都是绿的。
- 已定并已落地（这一条没让他选方向，接着第 10 条那句「按你的想法来，把问题解决掉」我自己拍）：
  1. 跨进程约定收进新包 `app/handoff`（`Arg` / `TargetPID` / `Await`）。原先它在
     `app/service/relaunch.go`，只服务设置页那个「重启」；**换 exe 的脚本是同一个参数的第二个造它的人**，
     所以格式不能是 service 包的私事。`main.go` 第一行就是 `handoff.Await(os.Args[1:])`——
     锁在 `application.New` 里面拿，写晚了等于没写。
  2. 两档脚本里**每一行** `start` 都拼 `%HANDOFF%`：替换脚本收 `%5`、回滚脚本收 `%4`，
     `restore`（`swap_failed` / `verify_failed` 两支共用）、`:START_NEW`、`:BACK_FAILED`、`:BACK_OK`
     全部带上。`assertEveryStartHandsOff` 逐行钉住：少一行就红，一行 `start` 都没有也红（防止把
     断言写成空转）。
  3. 等待有上限 30 秒，超时照常启动并 `applog.Warn` 一句；没有交接参数时完全不等，
     `TestAwaitReturnsImmediatelyWhenTargetIsGone` 拿一个不存在的 PID 卡住"正常安装不许变慢"。
- 证据（回滚那一档为什么也得带）：`:BACK_OK` 里副本已经被 `move` 消费掉、本进程按定义早走了，
  那一支带参数只是同规矩；`:BACK_FAILED` 反过来——**本进程还活着**（`move` 撞的就是它的锁），
  不带参数就是"点退回 → 程序关了 → 没回来"。区别只在于它的回执是 `rollback_failed`，
  下次启动会说一句话，所以这一支是"会说"而不是"全静默"。
- 推断（不是证据）：#190「测试机 2.1.0→2.3.0 热替换失败、装不上」很可能就是这条。现场对得上
  （点了安装、程序没回来），但测试机的 applog 没拿到，`.old` 的时间戳也没回传，所以只能记成推断。
- 判据（15:14 那一跑 A 之后要分成两个症状，不能再混着说）：这一条的现场是**回执 `ok` + 程序没自己回来 +
  人再手动打开时看到的是新版本**（A 里本体确实换成了 `ee6d98db`，重开必然是新版）。
  所以「点了安装程序没了」是这一条；「重开之后还是老版本」不是这一条，那是**根本没换成**——
  该去查回执是不是 `swap_failed`/`verify_failed`、产物是不是压根没选中 `.exe`（上面「已核」那条第三轮）、
  或者他重开的是另一份安装位置的 exe。下次拿测试机的日志来，先按这三样分岔，别再整条猜成同一个 bug。
- 边界（写在这里免得被当成"已彻底解决"）：
  - **PID 复用**：等的那个 PID 若已被别的进程占走，新进程最多白等 30 秒再起。交接靠 PID，
    这是这套机制固有的，没有更好的信号可用（批处理脚本里查进程名列已被第 2 条规矩否掉，见 ADR 0010）。
  - 只覆盖"脚本自己 start 的那几行"。用户看到程序消失后自己双击，走的是没有交接参数的路径——
    此时旧进程必然已经不在了，否则双击本来也起不来。
  - 「退出时安装」那一档一行 `start` 都没有，与它无关；那一档的失败仍然只有结论文件说话。
- 已核（顺手把"还会不会只关不开"的其余分支一起过了一遍，写在这里免得下次重查）：
  - 产物不是 `.exe` 时，`InstallUpdate` 本来就不承诺重开：`.msi` 交给 `launchAndExit`（装完要人自己开），
    zip/7z 那一支是 `explorer` 打开下载目录 + 本进程退出——**这是设计如此，不是漏了交接参数**。
  - 那我们的更新会不会走到 zip 那一支？核了选产物的代码（`app/updater/updater.go:384` `findBestAsset`）：
    第一轮要求文件名带平台词（`cczjVideo.exe` 不带，落空），**第二轮按扩展名命中 `.exe`**，
    所以三件套里那个 exe 总被选中；只有当某个 release 的产物里 `.exe/.msi/.zip` 一个都没有时，
    第三轮才会退回 `assets[0]`——那时候选名会是 `checksums.txt` 这类，装上就是"打开下载目录 + 程序退出、
    什么都没装"。**我们自己发的每一版都带 `cczjVideo.exe`，所以这条路今天走不到**，属于边界不是缺陷；
    真要防它，判据是"选出来的产物扩展名不在 `.exe` 里就别给『立即安装』按钮"，那是界面闸门，先不做。
  - 换进去的新 exe **起来就崩**（半截文件、缺依赖）这一类，仍然只能靠「退回上一版」；
    半截文件那种已在门口被下载摘要复核挡住（`ValidateInstallArtifact`），所以剩下的都是构建本身的问题。
  - ~~救不了已经发出去的 2.1.0/2.2.0/2.3.0/2.3.1~~（这条今天被推翻了一半，见文末「已发版的那一跳」）：
    那几版发出去的脚本没有 `%5`，改名那一支撞上了照样是"只关不开"。核过 `git show` 之后分成两档——
    **2.3.0/2.3.1 能救**（它们虽然不传参数，但**在 `start` 之前会把 `ok` 写进结论文件**，新进程可以拿这条
    线索认出自己是被替换脚本拉起来的），**2.1.0/2.2.0 仍救不了**（c0fff71 / 5fadd03 / e8f0ff8 里
    `:START_NEW` 只有裸的 `start "" "%s"`，**连结论文件都不写**，新进程手上没有任何信号）。
    所以第一跳从 2.1.0/2.2.0 升上来仍然是"可能只关不开"，那一跳只能靠手动覆盖 exe 或人再双击一次。
- 实测（2026-10-06 下午，本机 Windows 10 19045，不碰他的应用）：上面第 2 条那句"改名是允许的"
  原来是按 Windows 语义写的，现在验过了。拿 `PING.EXE` 的副本起一个活进程，然后：
  `ren` 之改名为 `.old` → **成功（rc=0）**；紧接着 `move` 另一个文件到腾空的那个名字 → **成功**
  （cmd 打"移动了 1 个文件"）；再 `del /f /q` 那个已改名的活映像 → **失败**（拒绝访问）。
  改名可以、删除不行、名字一腾出来 `move` 就一次落地——这正是 `:RENAME_REPLACE` 干的事，
  所以"旧进程还活着时换入成功"从推断升级成实测。探针目录 `D:\cczj-rename-probe` 用完已删。
- 实测（等待那一半）：新增 `TestAwaitWaitsForALiveProcess`——起一个真的活 2 秒的子进程再 `Await`，
  实测耗时 2.41 秒（对照组：不存在的 PID 用 0.15 秒返回）。这条以前是空的：把 `Await` 写成
  空函数，"立刻返回"那条照样绿，而真机上就是"只关不开"。现在两个方向各钉一条。
- 他要的是「点更新 → 老版关掉 → 新版起来 → 库认得」，按现在的代码逐跳说清（**没修之前就没人逐跳过**）：
  - **2.3.x 之间升一版**（同数据目录、同结构版本）：换 exe 不动目录，库原地就是同一份，
    这一跳"数据库认不认得"本不该有问题；出问题的是"重开"那一半，也就是上面这条。
    **这一跳已在 15:12 的 B 档整条实测**（新进程把窗口开回来、`user_version` 仍是 10、十张表行数与基线逐张相同）。
  - **2.1.0 → 2.3.x**（测试者那一跳）：2.1.0 已经在 `%APPDATA%\CCZJ Video` 里**自建了一份空库**，
    于是自动搬库的前提（旧目录有库、新目录没库）从那一刻起就不成立（第 11 条现场 A 实测过）。
    所以现在**不是静默搬过来**，而是启动时提示"发现了旧版本的数据"、报出旧库位置和能找回多少，
    **点一下**才并进当前库。这一跳要说成"会问你一次"，不能说成"全自动"。
  - **2.0.x → 2.3.x**（他本机那个 2.0.4 是本地构建，不算这一档）：旧库还在 `<exe>\data`、
    新目录没被建过空库，走的是自动搬那一条。
- 实测（2026-10-06 15:11–15:18，整条应用级链路，D 盘沙箱 + `APPDATA` 重定向，他那份
  `%APPDATA%\CCZJ Video` 一个字节没动——连 WebView2 的资料目录都跟着走了沙箱，因为 go-webview2
  的默认落点是 `%APPDATA%\<exe 名>`）。两份二进制都是当前 HEAD 的真实构建，只差
  `-X updater.Version`，sha256 前 8 位 `e216c5ee`（旧）与 `ee6d98db`（新）。一个要说清的细节：这两份的
  Windows 文件版本资源仍是 `build/windows/info.generated.json` 里的 2.3.1（这一跑没重跑 `sync_info.ps1`），
  起作用的是编译进去的 `updater.Version`（8.8.8 / 9.9.9，日志与界面读的都是它）——这一跑验的是
  "换本体 + 交接"这条链，文件版本资源不在它的任何一环上。跑法就是生产的口径：
  起旧进程，再用 `installBySwapWindows` 那一条命令形状起真脚本
  （`cmd /c _update_swap.bat` + 五个参数 + `cmd.Dir` 指应用目录），脚本正文由 `buildSwapScript`
  原样落盘，一个字没改。三档：
  - **B（带交接参数，也就是修完的样子）——他要的四条全过**。`move` 连败 29 次（61 秒，逐次约 2.03 秒，
    与第 1 条那个预算对得上）→ 进 `:RENAME_REPLACE` → 旧进程还活着时改名成功、换入成功 → 回执 `ok`
    → 15:12:10.994 新进程出现，此后 **5.2 秒日志里一行 `application ready` 都没有**（它在等旧进程），
    15:12:16.213 我结束旧进程，15:12:16.449 新进程就 `application ready` 了（差 236 毫秒），
    之后连续 30 秒活着。落盘核对：本体 = 新字节、`.old` = 旧字节、暂存产物被 `move` 消费、脚本自删。
    库里 `user_version` 仍是 10，十张表的行数与动手前的基线**逐张相同**
    （favorites 3 / global_video 77 / watch_history 63 / sources 3 / settings 21 …），
    日志里没有 `[DataDir]` 迁移、没有 schema 提示、没有 `Unsafe`——原地同一份库，认得。
    新进程那一句 `[Updater] 版本变化: 8.8.8 -> 9.9.9` 顺便说明它是从库里读到的上一版，不是空库自增。
  - **A（对照组：同一份脚本，只把 `start` 那行的 `%HANDOFF%` 去掉，也就是修之前的行为）——老 bug 原样复现**。
    换本体照样成功、回执照样 `ok`；15:14:20.964 新进程出现，**311 毫秒后**旧进程打出
    `app.go:40 [SingleInstance] 检测到重复启动，已激活现有窗口`，新进程随即消失，此后没有第四行
    `application ready`。再结束旧进程，现场是：目录里躺着新版 exe，桌面上一个窗口都没有——
    「点安装 → 程序关了 → 再也没回来」，而回执写着一切正常。A/B 之间唯一的差别就是那个参数，
    所以第 3 条那句"被单实例锁吃掉"从今天起是实测，不是机制推演。
  - **C（「退出时安装」那一档，第 198 条欠的真机那一跑）——按设计成立**。`buildSwapScript(false)` 落盘的
    脚本里 `^start ` 计数为 0；跑到第 8 次重试时进程结束，约 2 秒后 `move` 落地、回执 `ok`、换入新版、
    留 `.old`、脚本自删，**随后 20 秒没有任何新进程**（它没把他刚关掉的窗口再开回来）；
    下一次手动启动读到回执（`app/update/service.go:167` 那行由 1 条变 2 条）并自报 9.9.9，回执文件被消费。
    边界要说清：这一跑的"退出"是我结束进程造成的，不是界面那个关闭按钮走 `RestartApp`/quit hook 的
    优雅关停。外部状态（进程没了、脚本还在重试）是同一个，但优雅退出那半条路径仍没被这一跑覆盖。
- 这一跑没覆盖的（别把它当"全链路已验证"）：
  - **没点过界面里的「立即安装」按钮**。要走那颗按钮得真发布一个比当前版本新的 release，
    沙箱里是从 Go 的调用口径直接把脚本起起来的。所以「检查更新 → 下载 → 摘要复核 → 点按钮」这条上游
    链路仍是单测 + 代码核过，不是实测；实测的是后半条——「脚本 → 换本体 → 交接 → 新进程起来 →
    读同一份库」，而坏掉的那半正是后半条。
  - 两份二进制是同一份代码构建的，跨"schema 真的变了"那一跳（带迁移的升级）没在这一跑里发生；
    它由第 10/11 条那些现场单独管。
  - ~~交接等满 30 秒然后超时照起那一支没实测~~ —— 今天下午真的撞到了：A2b 第一跑我把旧进程**故意留着不结束**，
    新进程 16:43:10.365 进等待、16:43:40.648 打 `[Relaunch] 等待同名实例 cczjVideo.exe超时，直接启动`
    （30.28 秒，与上限对得上）、16:43:40.659 就被单实例锁判成重复启动并退出。也就是说这一支的行为是
    「等满 → 照常往下 → 让锁决胜负」，和代码写的一致；它是**兜底失败时的样子**，不是设计目标。
- 顺手记（免得下次有人以为丢了东西）：`app/service/relaunch.go` 与它的测试搬到了 `app/handoff/`，
  搬迁用了 `git rm --cached`，所以 `git status` 里现在有两条暂存的 `D` 而**没有任何提交**。
  工作树是完整的，`go build ./...` 与相关测试全绿。
- 已发版的那一跳（今天下午补的兜底，对应他要求里最后没覆盖的那段）：
  上面那条「每一行 `start` 都带交接参数」只对**当前代码构建出来的**版本成立。装着他发出去的 2.3.0 / 2.3.1
  的机器，脚本是上一版程序生成的，正文里没有 `%5`——也就是说**第一跳升上带修复的版本时，交接参数根本不存在**，
  撞改名那一支照样"只关不开"。这条今天收了 `git show` 逐版核对（e14a435 / 6a883ac 的 `installResultPath`
  与 `buildSwapScript` 正文），不是推断。
  能用的第二信号只有一个：**那两版脚本在 `start` 之前会往结论文件写 `ok`**。所以 `handoff.Await` 改成两条线索：
  1. 带交接参数 → 等那个 PID 消失（原有那条，一字没动）。
  2. 没参数，但 `%TEMP%\cczj_video_update_result` 是 **15 秒内**写的、内容正好是 `ok` → 认定自己是
     被替换脚本刚拉起来的，于是等"同名映像"的其它实例退场。
  闸门收紧在这三处，免得把正常双击也算成更新：只看 `ok`（`swap_failed` / `verify_failed` 两支不 start，
  读到它们不该等）、只看 15 秒内的（旧的、没被消费掉的文件不算数，时间往前的也不认）、
  只有真找到同名进程才等（`TestAwaitReturnsImmediatelyWithoutArgOrReceipt` 钉住"正常启动不许变慢"）。
  文件名也收进 `handoff`（`SwapReceiptPath`），`updater.installResultPath()` 改为引用它——
  写它的是脚本、读它的是新进程，两边拼写不一致等于这条线索永远捡不到。
  「按映像名找进程」这件事第 2 条规矩（`buildSwapScript` 里那条"不按名字找进程"）否过，否的是
  **在批处理脚本里查进程名**；这里换到 Go 侧、有 15 秒闸门、有 30 秒上限，且认的是"占着同一把单实例锁的人"，
  与脚本里那个会误伤无关同名的判据不是一回事。这条写进 ADR 0012 的否决项批注，免得下次被当成自相矛盾。
- 实测（2026-10-06 16:46–16:47，D 盘沙箱 `D:\cczj-bc\A2b`，`APPDATA` 重定向，他那份库没动）：
  上面那条兜底**跑通了**。现场按生产口径搭：应用目录放着 8.8.8 的构建，暂存放 9.9.9 的构建，
  起旧进程，然后用 2.3.1 那一档脚本正文（`start "" "%OLD%"`，**不给第 5 个参数**）
  以 `cmd /c _update_swap.bat <旧exe> <新exe> <结论文件> cczjVideo.exe` 起真脚本。逐条对上：
  - 16:46:10.997 起脚本 → 60.7 秒里 `move` 一直失败（旧进程活着）→ 走进 `:RENAME_REPLACE` →
    改名、换入成功。落盘核对：本体 = 新字节（4A4D6DA0）、`.old` = 旧字节（5D26AEA7）、暂存被 `move` 消费、脚本自删。
  - 16:47:11.168 新进程打出 `[Relaunch] 等待同名实例 cczjVideo.exe退出后再抢单实例锁` —— 它没有交接参数，
    是**靠那条 15 秒内的 `ok` 回执**认出自己的来历的。
  - 16:47:14.029 第二行 `application ready`（我在新进程出现 2 秒后结束旧进程，从结束到界面起来约 2.9 秒），
    整场 `SingleInstance` 计数 **0** —— 新进程没被判成重复启动。
  - 对照的第一跑（同一份脚本，唯一区别是我**故意不结束**旧进程）：新进程等满 **30.28 秒**（16:43:10.365 →
    16:43:40.648）后照常启动，随即被单实例锁判成重复启动并退出。这就是上面那条"上限 30 秒、超时让锁决胜负"
    的真机现场，也是这一支以前只有单测的那格。
  还没覆盖的：2.1.0 / 2.2.0 那一档（不写结论文件，新进程无信号可用，见上面改口那条）；
  界面里那颗「立即安装」按钮仍没点过；C 那一跳的提示条上屏。
- 要拍板：无。这一条不需要他定方向，修完落在这里。

<a id="origin-pending-decisions--13-交接等待把桌面刷成黑窗gui-进程每-200ms-起一次-tasklist他-2026-10-06-报的"></a>

##### 13. 交接等待把桌面刷成黑窗：GUI 进程每 200ms 起一次 tasklist（他 2026-10-06 报的）

- 起因：他在旁边看着 sandbox 跑，问我到底在测什么——「每重启一次需要我手动帮你点已阅读，然后突然打开很多
  tasklist cmd，一直闪烁」。这两件事都是真机验证的副作用，第二件是**缺陷**，不是测试噪音。
- 根因（已核到行）：交接等待每 200ms 问一次"那个进程还在不在"，而早先的实现是
  `exec.Command("tasklist", "/FI", "PID eq ...")`。本应用按 `-H windowsgui` 构建，属于 GUI 进程、
  没有可依附的控制台，Windows 会为它启动的**每个控制台程序新开一个控制台窗口**——
  于是 30 秒上限内最多刷出约 150 个黑窗。他看到的就是"很多 tasklist 一直闪"。
- 这条**不是今天新引入的**，但被今天新加的那一支放大了：`git show 6a883ac:app/service/relaunch.go`
  里 published 2.3.1 的 `processAlive` 就是同一份 tasklist 轮询，也就是说**装 2.3.0/2.3.1 的机器点
  「设置 → 关于」里那个重启，一样会闪**——那一路只等旧进程，通常 1–3 秒，所以闪几下不明显；
  而更新那一跳是在"脚本刚 start 新进程"的时刻等，旧进程还在退场，等得久，就刷成一片。
- 已定并落地（他这句话就是缺陷报告，不用他选方向）：等待改读**系统进程快照**，一行子进程都不起。
  新文件 `app/handoff/procs_windows.go`：`CreateToolhelp32Snapshot(TH32CS_SNAPPROCESS)` +
  `Process32First/Next`，`processAlive` 按 PID 找、`sameImagePIDs` 按映像名找（`ExeFile` 是加载时定下的名字，
  所以脚本把那个文件改名让位的旧进程照样认得出——这正是兜底要认的那个人）。
  `procs_other.go` 给非 Windows 桩（`Await` 第一行本来就按 `runtime.GOOS` 挡掉了），
  `golang.org/x/sys` 从 indirect 转 direct（`go mod tidy`，go.sum 没变）。
  顺带改掉两处隐性依赖：不再吃 tasklist 的列格式与本地化输出，也不再需要"tasklist 不可用就退化"那套取舍。
- 实测（单测，同一批断言换实现前后各跑一次）：`TestAwaitWaitsForALiveProcess` 2.21 秒（真起一个活约 2 秒的子进程，
  与 tasklist 版的 2.41 秒同量级——说明等待时长没被改坏）；
  `TestAwaitReturnsImmediatelyWhenTargetIsGone` 从 0.15 秒降到 **0.02 秒**（少起一个进程就是少一次几十毫秒的开销）；
  `TestSameImagePIDsSeesAnotherInstanceButNotItself`、`TestAwaitWaitsForSameImageWhenTheSpawnerIsAnOlderBuild`
  两条"看得见别人、看不见自己"的断言在快照版下照样绿。`go build ./...`、`go vet`、`go test ./app/handoff/
  ./app/updater/ ./app/service/` 全绿。
- 已量（2026-10-06 真机对照，同一套 D 盘沙箱、同样的 `-H windowsgui` 链接参数，差别只有轮询实现）：
  - **对照组**（旧 tasklist 轮询版）：3 秒等待里 `CHILDREN_CONSOLE=4`、按父进程归因到本应用名下的新
    conhost 窗口 `CONHOST_WINDOWS_FROM_TARGET=3`。也就是"闪"这件事从推断变成了数出来的。
  - **修复版**（进程快照）：整段等待 `CHILDREN_TOTAL=1`，只有 `msedgewebview2.exe`（WebView2 运行时，
    不是我们起的控制台程序）；`CHILDREN_CONSOLE=0`、`CHILDREN_CONSOLE_DURING_WAIT=0`、
    `CONHOST_WINDOWS_FROM_TARGET=0`。等待期间桌面上不再出现任何由本应用开出的窗口。
  - **方法上的坑（下次别再踩）**：第一版量到的是"这一跳新开了 44 个 conhost 窗口"，那是**假的**。
    原因是这台机器的编辑器 Qoder 自己每 0.6 秒起一对 `powershell.exe` + `icacls.exe` 并各带一个 conhost
    （16 秒空闲就 25 个）。判据必须按**父进程 PID 归因**，只数目标 exe 名下的；跨进程的桌面噪声不能
    算进被测对象的账。改判据之后才有上面那组数。
  - **同一批跑顺带复验了交接本身**：两次真跳转都记了 `[Relaunch] 等待旧进程 …退出后再抢单实例锁`，
    旧进程写 `应用正常退出`，新进程 `application ready … data_dir="D:\cczj-vv\roaming\CCZJ Video"`，
    T+3 秒窗口到位；库里那条采集源和刚抓到的豆瓣热榜都读得到。他要的那句
    「点更新并重新启用新版就得把老版本关掉、启用新版本、数据库要能识别到」到这一格才算观察完。
  - **2026-10-07 他又报了一次"执行过程中仍然有很多黑窗闪"**。这一轮 150 秒按父进程 PID 归因，新建
    956 个进程里 `powershell.exe` 381、`icacls.exe` 58、`conhost.exe` 440，父进程全是 `Qoder.exe`
    （宿主自己约每 0.4 秒起一对）；而这段时间**应用根本没在跑**（同一份 CSV 里 `cczjVideo` 出现 0 次），
    所以那批闪烁一格都记不到应用账上。应用侧再数一遍：非测试代码里 `tasklist` 已无任何调用点
    （只剩 `app/handoff/procs_windows.go:15` 的注释提了一句）。我这边能收的是自己的账：探测用的
    `tasklist` / 内联 `powershell` 改成一次性隐藏脚本、优先直接读文件，不为每次查询开一个控制台。
- 界面侧复看（2026-10-07 跑了一次，结论停在"形状"上）：上一格记的"抓帧只能取到黑屏，因为窗口被
  LS-PrePost 压住"是**错的归因**。真原因是我自己的 `framegrab`：`-mode screen` 那条路 `GetDIBits`
  返回 0 行（缓冲区全零），PNG 就是纯黑且 178 帧"纹丝不动"；同一份代码去抓编辑器窗口返回 788 行、
  非零字节 4.45M/4.52M，说明工具在别的窗口上正常、在这条取数路径上一直是坏的。换成 .NET
  `Graphics.CopyFromScreen` 后 144 帧全部有内容（平均亮度 167、亮像素 80%），首页轮播两次交棒落在
  第 34 与第 89 帧，每次摊在连续 4 帧的渐变上（8×8 块均值 L1 距离 9.5 → 11.4 → 5.8 → 1.4），
  不是"单帧跳完随后归零"那种硬切。**这只说明量到的形状是渐变，不等于宣布"图片瞬间替换"已经消失**：
  约 110 毫秒一帧的采样看不见比一帧更短的闪，而他看完这段后说「你没必要测这个问题啊」，所以这一格
  停在这里，按代码侧的闸门修复交付，不再往下追。
  动画总开关、海报淡入、扩展包不抖、两张数据提示弹窗这几项仍是要看屏幕的，同样不再拿别的证据冒充"看过了"。
- 已定（他 2026-10-06 的原话：「黑窗口不藏起来，但是不能一直出现然后关闭持续闪烁啊。」）：
  这句话正好切在两种窗口上，两边都按他说的落。
  - **常驻的那一个不藏。** `_update_swap.bat` / 回滚脚本是 `exec.Command("cmd", "/c", ...)` 起的
    （`app/updater/install.go:232`、`:415`），cmd.exe 是控制台程序，所以换本体的那 60 秒里桌面上会有
    一个**一直存在**的黑色窗口，上面滚的是脚本自己的英文提示。**不加 `CREATE_NO_WINDOW`、不动
    `SysProcAttr`**：它是安装期间唯一看得见的进度——点完「立即安装」后应用窗口就没了，进度条随进程
    一起消失，我们没有别的进度通道；它至少回答"是不是卡住了"。
  - **反复出现再关闭的那种不是进度，是缺陷。** 那就是上面每 200ms 一个 tasklist 刷出来的约 150 个黑窗，
    已按进程快照修掉，不在这条决策的保护范围里。
- 顺手核过一遍（免得下次以为还有别处在刷屏）：全仓库**非测试**代码里会起进程的 `exec.Command` 只剩这些——
  `install.go:57/58`、`app/service/download.go:599/605/614`、`app/service/facade_backup.go:68/70/72` 起的是
  `explorer` / `open` / `xdg-open`，不是控制台程序，开不出黑窗；`install.go:118` 的 `launchAndExit` 只在
  `.msi` 那一支用到，起的是下载来的安装包；`app/service/app.go:117` 起的是本应用自己的 exe，按
  `-H windowsgui` 构建、没有控制台。脚本正文里的重试间隔用 `ping -n 3 127.0.0.1 >NUL`
  （`install.go:306`、`:457`），它**继承 cmd 已经有的那个控制台**，不另开窗。其余 `exec.Command` 都在
  `_test.go` 里，不进产物。所以一次"立即安装"期间桌面上的窗口数：**一个，从头到尾那一个**。
- 要拍板：无。这一条他已定，上面那格真机复看也量完了。
- 顺带钉住的（同一件事的两侧，别只改一侧）：`scripts/verify.ps1` 加了一格守卫——`app/handoff` 的
  非测试代码里再出现 `exec.` / `tasklist` 就红。刷屏既不会编译失败，也没有别的测试会管，只有这一格
  能在 `go test` 之前拦下来；拿 `app/updater` 试跑过同一套匹配，确实抓得到真起子进程的那几行，
  说明守卫是活的不是摆设。测试里那几处 `exec.LookPath("tasklist")` 跳过闸门也一起换了——它们守的是
  旧实现的真依赖，留着会让下一个读测试的人以为等待还要靠外部命令，现在统一走 `requireWindows`。

<a id="origin-pending-decisions--2026-10-18"></a>

#### 2026-10-18

起因：`reports/` 里那五份 GPT 探查报告（fqapp / Legado / Mihon / API 规范 / 源码索引）要求给
小说模式与漫画模式补真实资源载入。开工前有几条方向必须他定，逐条记在这里。

> **当天已拍板的四条**（第 15、17、18、19 条，以及第 14 条的原则部分），结论写在每条的
> 「已定」行里，下面的选项保留原样当决策依据。**总原则（他原话）**：「我的软件有我自己的特点，
> 源表可以加入新一列的字段可以改动，但我不喜欢照抄别人的东西，我可以使用某些好的，
> 但需要配合我自己的设计和软件。」→ 报告只当**原则来源**（稳定自然键、两步图片解析、
> 页状态机、generation 取消、SWR），**不当格式来源**：Legado 的书源规则语法、Mihon 的
> 扩展类加载模型都不照搬，一律走本项目已有的 `sources` + `strategy_config` 声明式那套语言。

<a id="origin-pending-decisions--14-小说漫画的真实资源到底从哪来总闸门其余四条都挂在它下面"></a>

##### 14. 小说/漫画的「真实资源」到底从哪来（总闸门，其余四条都挂在它下面）

- 现状（证据）：两个模式是 **100% 前端演示**，`app/` 下 grep 不到一行 novel/comic 代码。
  `frontend/src/novel/domain.ts:11-15` 的 `NovelRepository` 只有三个**同步**方法
  （`books/book/chapter`），实现是 `novel/data/demoRepository.ts` 对 `demoBooks.ts:4-17` 那
  12 本硬编码常量做内存 filter；正文是 `demoRepository.ts:16-31` 拿 `book.opening` 加模板
  拼出的 15 段；封面不是图片，是 `NovelCover.vue:7-13` 按 `motif` 画的 SVG。
  漫画同构：6 部作品硬编码在 `comic/data/demoRepository.ts:2-9`，「画面」是
  `ComicArtwork.vue:8-33` 一个按 `scene` 编号切图层的 480×640 参数化 SVG。
- 证据：`docs/adr/0014-application-mode-strategies.md:50-52` 已经把这个口子写死了——
  「A future network repository can replace the demo adapter, with asynchronous loading and
  error handling added at that seam.」所以要做的事本身没有争议，争议在**接什么**。
- 证据（报告推荐的起点没随包过来）：`reports/00_聚合项目开发交接.md:172-182` 让人先跑
  `examples/mock_provider.py`、`reports/04:524` 让人对照 `api-contracts/fixture.openapi.json`
  与 `evidence/api-captures.json`。`find reports -type f` 只有 5 个 `.md` 加 3 个
  player-architecture 的 `.json`，**那三样一个都不在**。要照 §8 的规范走 fixture，得自己
  按 `reports/04:526-618` 重新实现一份本地服务（规范写得够细，可实现，但是新工作量）。
- 要拍板（四条路，可组合，代价递增）：
  - **A 本地 fixture provider**：按 `reports/04` §8 自建一份合成服务，贯通
    搜索→详情→目录→正文/图片，可测、零授权风险。**代价**：用户看到的还是合成内容，
    等于把演示数据从前端常量搬到后端接口，产品价值有限，但它是 B/C/D 的测试地基。
  - **B Legado 风格书源规则子集**：`reports/02:133-137` 明确第一版只做
    HTTP GET/POST + 静态 headers + JSONPath/CSS + 相对 URL + 显式分页 + 有限替换，
    **不上 JS 与 WebView**，不支持的字段要在导入时显式报错而不是静默当空结果。
    能接真实小说站。**代价**：规则由外部提供，站点改版即失效，反爬与限流要自己扛
    （项目已有 `app/douban/anticrawl.go:19-72` 那套熔断可参考，但豆瓣那套是硬编码域名）。
  - **C 漫画源**：`reports/03:7` 与 `:46` 反复说明 Mihon 核心**没有**通用漫画搜索 API，
    站点协议全在扩展里，主仓库无法独自说明任何漫画站的实际接口。本项目也没有对应的
    扩展生态。**代价最高**：要接就得为某个站写死解析，且撞 Referer/防盗链（见第 16 条）。
  - **D 复用视频模式已有的采集源链路**：`app/model/source.go:51-73` 的 `Source` 已经有
    `strategy_config` 这个 JSON 列，`app/collect/strategy.go:19-33` 的 `SourceStrategy`
    接口 + `docs/source-strategy-v2.schema.json` 是现成的声明式适配层，前端「采集适配」
    下拉已经能选扩展包里的 adapter（`app/plugin/types.go:64-76`）。给 `sources` 加一列
    `kind TEXT NOT NULL DEFAULT 'video'` 就能把这条链路扩到小说/漫画。
    **代价**：要动 `sources` 表和迁移（v11），而视频模式正在改。
- 推断：B 与 D 不冲突——书源规则最终也是填进类似 `strategy_config` 的声明式配置，D 提供的
  是持久化、健康度（`app/db/source_health.go`，每源留 200 样本）、自动停用
  （`app/handler/source_guard.go:21,28`，连败 3 次停用、24h 冷却）这些**已经写好**的周边。
  但 `app/plugin/types.go:179-184` 的 `strategyNames` 枚举和那份 schema 都要扩，
  而 `docs/plugins.md:230-231`（§3 `kind: source`）写明适配包「**不能**指定请求打到哪台主机，
  也不能注入 header」——这正好是小说/漫画源最需要的能力，
  所以「书源做成扩展包」这条走不通，只能走内核建表 + 内核解析。
- **已定（2026-10-18）**：走 **D 为骨架**——`sources` 加 `kind` 列，复用
  `app/collect/strategy.go` 的 `ParamConfig` / `ResponseShape` / `FieldMapping` 这套**本项目自己的**
  声明式语言往下扩，而不是引入 Legado 的书源规则语法。提取层按需要新增（现有 `ResponseShape`
  只会读 JSON 信封，小说/漫画源多半是 HTML，这一层是必须补的）。B 的「规则子集」思想采纳，
  语法不采纳；C 的漫画源按同一套骨架做，不引入 Mihon 的扩展模型；A 的 fixture 降级为
  **自测夹具**（照 `reports/04` §8 的形状自己写，用来验证提取层与全链路），不当产品能力。

<a id="origin-pending-decisions--15-repository-异步化会扫到每一个小说漫画页面走哪条改法"></a>

##### 15. Repository 异步化会扫到每一个小说/漫画页面，走哪条改法

- 证据：接口是同步的，而且**在 setup 顶层被同步消费**——`NovelReader.vue:15`、
  `ComicReader.vue:13`、`NovelHome.vue:11`、`NovelCatalog.vue:12`、`NovelSearch.vue:9`、
  `NovelBook.vue:11-12`；两个 store 还拿它校验 id 有效性
  （`novel/readingState.ts:12,16-17`、`comic/state.ts:8,14`），id 查不到就静默丢弃。
  `ComicReader.vue:13` 取不到就直接落空态（模板 `v-if="series&&episode"`）。
- 要拍板：
  - **A 直接把接口改成 `Promise`**，同步改造 6 个 view + 2 个 store，加
    loading / error / empty / 取消 / generation 五态。**代价**：改动面覆盖整个小说漫画前端，
    但这正是 `reports/00:131-137` 与 `reports/03:101` 要求的东西（新 query 建 generation、
    取消旧 query、空列表只代表一次成功的「无结果」、页状态机 Queue→LoadPage→DownloadImage→Ready）。
  - **B 保留同步外壳**，异步只藏在 provider 内部（预载/缓存后同步读）。**代价**：view 不用改，
    但要额外维护一层「数据还没到」的中间状态，且 `reports/00:107` 警告的那种
    「切章慢响应覆盖新章」问题会被这层壳藏起来，更难查。
- 推断：A 更贵但更对。B 省下的改动会以调试成本还回来。这一条不定，第 14 条选了哪条都开不了工。
- **已定（2026-10-18）**：**A**。接口直接改成 `Promise`，6 个 view + 2 个 store 一并改造，
  loading / error / empty / 取消 / generation 五态齐全。

<a id="origin-pending-decisions--16-漫画图片必须过代理而现有两条代理都不够用"></a>

##### 16. 漫画图片必须过代理，而现有两条代理都不够用

- 证据（第一条不能用）：`app/service/app.go:798` 的 `ProxyImage(url)` 返回
  `data:<mime>;base64,...` **字符串**，走 Wails IPC；`app/proxy/service.go:22` 上限 16MB、
  `:228-231` 强制响应 `Content-Type` 必须 `image/*`、`:201-203` 的 Referer **只对 douban 域硬编码**、
  `:21,158-168` 有一把 300ms 全局互斥锁。一话几十页图走 base64 IPC 会撑爆，且没法传站点 Referer。
- 证据（第二条差一点）：`/__cczj/hls` 是挂在 Wails 同源 asset server 上的**真 HTTP 端点**
  （`app.go:58-61` → `app/proxy/hls.go:99-105`，URL 形状 `?u=<base64url>&ref=<base64url>`），
  流式转发不缓冲全量（`hls.go:193`）、透传客户端 Range 并回传 Content-Range（`:199-204`、
  `copyStreamHeaders:306-312`）、**Content-Type 无任何限制**（`:283-286`）、有 SSRF 防护
  （`safeDialContext:62-89` 在拨号瞬间复核，防 DNS rebinding）。它今天就能当 `<img src>` 用。
  但两个硬伤：`RefererOrigin`（`hls.go:110-116`）把 Referer **截成 `scheme://host/`**，
  路径和 query 全丢；`:213-221` 只发固定三个头（UA / `Accept: */*` / `Accept-Encoding: identity`），
  **不支持自定义 header**。
- 证据（为什么这两点是硬伤）：`reports/03:97` 明确指出在线图片是**两步解析**
  （pageList → 缺 imageUrl 时再 getImageUrl），且「source headers 可能包含 User-Agent、Referer
  或会话需要的其他字段，图片下载器不能在取得 imageUrl 后丢弃这些上下文」。
  `reports/04:681` 的排错表也把「漫画拿到 URL 却无法显示」的首查证据列为
  「相对 URL、capability、headers、实际 MIME」。
- 要拍板：
  - **A 改 `hls.go`**：加命名 header 集参数、停止截断 Referer。**代价**：动的是视频模式正在用的文件。
  - **B 新开一条 `/__cczj/img` 路由**：复用 `safeDialContext` + `ValidateTarget` + `netstats`
    计数，不碰 `hls.go`。**代价**：多一个文件、多一处 SSRF 白名单要维护，但零冲突。
  - **C 先不做**：漫画只接不需要 Referer 的源。**推断**：真实漫画 CDN 绝大多数校验完整
    Referer，C 基本等于漫画接不了真实源，只在第 14 条选 A（fixture）时成立。
- 顺带记一笔已知的更硬的墙（不是这次能解的）：`app/proxy/service.go:111-129` 原文记录了
  `img9.doubanio.com` 按 **TLS 指纹**判定、对本机 Go 客户端固定回 200 + 一段 JS 挑战页，
  curl 带同样的头却能拿到图；`go.mod` 里没有 uTLS 或任何指纹伪装库。小说/漫画源撞同类墙时，
  现有基建无解，只能换域名或改走前端 `fetch`（WebView 的指纹是真浏览器）。
  另：全仓非测试代码 11 处 `&http.Transport{}` 字面量，只有一处显式写了 `Proxy: nil`
  （`app/cast/service.go:63`），其余根本没设这个字段 → Go 侧出网完全直连、不读系统代理；
  只有 `netstats.WrapTransport(cat, nil)` 回落到 `http.DefaultTransport` 那两份
  （源探测 `app/service/diagnostics.go:471,492`、更新检查 `app/updater/updater.go:295,357`）
  带 `ProxyFromEnvironment`——同一个源「探测走代理、采集不走」，这个不一致已经存在。
- **已定（2026-10-18）**：**B**，新开 `/__cczj/img`。理由不再是不撞车（见第 19 条），而是
  职责本来就该分开：`hls.go` 要重写 m3u8 里的分片地址、要对 `.m3u8` 屏蔽 Range，这些语义
  对图片全是负担；图片路由只需要「全量 Referer + 命名 header 集 + 流式 + Range + SSRF 复核」，
  复用 `safeDialContext` / `ValidateTarget` / `netstats` 即可。TLS 指纹墙与系统代理那两条
  是既有缺口，**这轮不解**，只记在这里。

<a id="origin-pending-decisions--17-书架追漫进度的键要从演示-id-换成-provider--remoteid"></a>

##### 17. 书架/追漫/进度的键要从演示 id 换成 provider + remoteId

- 证据：`cczj.novel.shelf.v1` 存的是 `['long-wind','star-letter']` 这类演示 id
  （`novel/readingState.ts:7,11-12`），`cczj.comic.follow.v1` 存 `['moon-mail']`
  （`comic/state.ts:7-8`），两张进度表也都以单一 id 为键
  （`cczj.novel.progress.v1`、`cczj.comic.progress.v1`）。
- 证据（报告的要求）：`reports/00:99-107` 给出 `items UNIQUE(provider_id, remote_id)`、
  `chapters UNIQUE(item_id, remote_id)`、`progress UNIQUE(profile_id, item_id, media_kind)`，
  并明确「不能以书名作为远端唯一键」「同名内容跨平台可能是不同版本、翻译、卷或扫描组，
  搜索 UI 可展示候选聚类，但不要据书名自动将章节和阅读进度混在一起」。
  `reports/03:67-74` 的身份映射表进一步指出章节的稳定关联依据是
  「manga key + 远端 chapter URL/ID」，**不宜**用列表下标或浮点章号。
- 要拍板：
  - **A 现在就换复合键**，存储版本升 v2，旧演示数据一次性丢弃。
  - **B 保留 v1 兼容读**，演示书架与真实书架并存。
- 推断：A 更干净。演示内容本来就要被替换，为它维护兼容读只是负担；而且
  `novel/readingState.ts` 现在遇到查不到的 id 是**静默丢弃**，换源那天用户书架会无声清空——
  这个行为在 A 之下要改成「保留条目 + 标记来源不可用」，正好对上
  `reports/00:109`「目录删除可标记不可用，不应立即删除用户下载」。
- **已定（2026-10-18）**：**A**。换 `{providerId, remoteId}` 复合键，存储升 v2，
  旧演示数据一次性丢弃（第 18 条已定演示内容全部下线，v1 里只剩演示 id，没有保留价值）。
  同时把「id 查不到就静默丢弃」改成「保留条目 + 标记来源不可用」。

<a id="origin-pending-decisions--18-漫画的数据模型撑不住真实漫画六部演示作品留不留"></a>

##### 18. 漫画的数据模型撑不住真实漫画，六部演示作品留不留

- 证据：`comic/domain.ts:3` 的 `ComicPanel { scene:number; caption:string; dialogue:string }`
  **没有图片字段**；`:2` 的 `ComicSeries.episodes` 只是**话标题字符串数组**，
  没有每话 id、更新时间、是否需登录/付费。`reports/00:72-77` 的 Content 判别联合把漫画定义成
  `{kind:'images'; pages:{index:number; image?:Resource; resolverId?:string}[]}`，
  `reports/00:63-67` 的 `Chapter` 要求带 `order`、`access: available|login|subscription|unknown`。
  这两处模型不改，真实漫画源无处安放。
- 要拍板：现有六部演示作品（`ComicArtwork.vue` 的参数化 SVG 分镜）**留不留**？
  - **留**：`ComicPanel` 要同时支持「scene 编号渲染 SVG」和「imageUrl 加载真图」两种形态，
    多一个判别字段和一套并存的渲染分支。
  - **不留**：直接换成图片模型，演示作品下线，漫画模式在接上真实源之前是空的。
- 推断：小说侧同一个问题但更轻——`NovelChapter.paragraphs` 已经是 `readonly string[]`，
  正好对上 `reports/00:73` 的 `{kind:'text'; blocks:{id,text}[]}`，只需把来源从模板拼接换成
  provider 返回；12 本演示书可以原地保留成「离线示例源」，因为它和真实源走同一个接口。
- **已定（2026-10-18）**：**都下线**。12 本演示书与 6 部演示作品全部移除，
  `NovelCover.vue` 的 motif SVG 与 `ComicArtwork.vue` 的 scene 参数化分镜随之退役；
  `ComicPanel` 直接换成图片模型（`{index, image?:Resource, resolverId?}` 那一形），
  不留「scene 渲染 SVG」的兼容分支。小说与漫画模式在用户加源之前是**空态**，
  这是预期的：与视频模式一致，源由用户自己添加，应用不内置内容。

<a id="origin-pending-decisions--19-与正在进行中的视频模式改动的文件冲突面"></a>

##### 19. 与正在进行中的视频模式改动的文件冲突面

- 证据：`git status` 有 261 项未提交改动。`frontend/src/novel/`、`comic/`、`reading/`、
  `modes/` 四个目录是**未跟踪的新目录**，不在任何 `M` 条目里，短期不冲突。
- 证据（共享文件正在被改）：`app/service/app.go`（**唯一的 Wails service 注册点**，
  `app.go:47-49` 只注册 `application.NewService(myApp)`，新增的小说/漫画绑定方法必须挂这里）、
  `frontend/src/router/index.ts`、`frontend/src/App.vue`、`frontend/src/locales/{zh-CN,en}.ts`、
  `frontend/bindings/**`（wails3 CLI 在 dev/build 时自动重生成，Taskfile 里没有显式命令）、
  `AGENTS.md`、`docs/plugins.md`、`scripts/verify.ps1`、
  `app/plugin/types.go`。`app/db/migrations.go`、`app/db/sqlite.go`、`app/model/source.go`
  目前**是干净的**，加 v11 迁移暂时不撞。
- 要拍板：
  - **A 现在就开工**，只新建 `app/novel/`、`app/comic/`、`app/service/facade_novel.go` /
    `facade_comic.go` 与前端 provider 层，**共享文件留到最后一并改**（`app.go` 注册、
    router、locales、绑定重生成）。
  - **B 等视频模式那轮改完再动**。
  - **C 在独立 git worktree 里开一支**，最后合并。
- 推断：A 可行且冲突面很小——真正必须共享的只有 `app/service/app.go` 的方法挂载和
  `frontend/bindings/` 的重生成，两处都能压到最后一步。C 的代价是 261 项未提交改动
  要不要一起带进 worktree，反而更麻烦。
- **已定（2026-10-18）**：**前提作废**。他澄清「视频模式在被 GPT 改」的意思是**那轮还没做完、
  现在并没有在执行**，所以不需要避开任何共享文件。按 A 的省事版走：`app/service/app.go`、
  `router/index.ts`、`App.vue`、`locales/*.ts`、`app/db/migrations.go`、`frontend/bindings/`
  都直接改，只**追加**、不覆盖或回退他在工作树里的那些改动。

<a id="origin-pending-decisions--20-报告里没有可用的源提取层因此定不了形本轮改成交付调研需求书"></a>

##### 20. 报告里没有可用的源，提取层因此定不了形（本轮改成交付调研需求书）

- 他问「reports 中没有源的接口吗」。核过一遍，报告里一共五层接口，**没有一层能当内容源用**：
  - `FQ-LOCAL`（`reports/04:9`）是 fqapp 自己 Flutter→Rust 的内部 RPC，要运行时 capability
    bearer token，`:57` 明写「能力 token 由运行时提供，不能从文档生成」。
  - `FQ-UPSTREAM`（`:266-272`）是 fqapp→番茄，原话「设备会话与签名相关字段不应由公共 UI
    自行拼接…不是可保证实时有效的上游开放文档…不要从主文复制 URL 后期待 curl 独立成功」；
    `:303`「未抓取在线上游响应」。
  - `LG-CONTROL`（`:309-313`）是 Legado app 自己的本地 Web 口 `127.0.0.1:1234`，要先在它里面开服务。
  - `LG-SOURCE`（`:12`）原话「URL、headers、body、解析方式由书源决定，**具体站点响应无统一规范**」；
    书源规则是外部配置，客户端源码里不含任何站点地址。
  - `MH-SOURCE`（`:13`）原话「Kotlin suspend 函数调用，**不是 HTTP JSON 服务**」；
    `reports/03:7`「主仓库无法独自说明所有漫画站的实际接口」。
  - `FIXTURE`（`:14`）是唯一标着「可运行、可抓取、可做契约检查」的，但 `examples/mock_provider.py`
    与 `api-contracts/*.openapi.json`、`evidence/api-captures.json` **都没随包过来**，
    `find reports -type f` 只有 5 个 `.md` 加 3 个 player-architecture 的 `.json`。
- 结论：**没有真实源样本，提取层就是凭空设计 DSL**，正是他要求避开的事。报告自己也是这个态度
  （`reports/02:133`「使用自有 fixture 验证数据链」、`reports/00:187`「先落契约与 FixtureProvider，
  不急着移植三仓库代码」）。
- **已定（2026-10-18）**：
  - 提取层**两种都要**——JSON 路径与 CSS 选择器都要能表达，由源配置声明自己是哪种。
    代价认了：`go.mod` 现在 5 个直接依赖里**没有任何 HTML 解析库**，也没有 `golang.org/x/net`，
    要新增（`x/net/html` 或 `PuerkitoBio/goquery`），并且中文小说站大量是 GBK/GB2312，
    还要连带 `golang.org/x/text/encoding` 与字符集检测。
  - 本轮**不写提取层代码**，改为交付一份给 GPT 的调研需求书
    （`reports/05_调研需求_小说漫画提取层.md`），由他去开源项目里把定形所需的证据取回来。
  - 不依赖源形状的其余部分（`sources.kind` 迁移、`app/novel`/`app/comic` 骨架、provider 契约、
    正文与图片缓存、`/__cczj/img` 路由、前端异步五态、复合存储键、源管理 UI）等需求书回来一并排。

<a id="origin-pending-decisions--21-小说漫画的书架与阅读进度该不该从-localstorage-搬进-sqlite"></a>

##### 21. 小说/漫画的书架与阅读进度该不该从 localStorage 搬进 SQLite

**已解决（2026-10-19）**：采用 SQLite 权威存储，按作品身份逐条保存书架、进度和偏好；每模式一次性原子导入 v2 旧值，保留浏览器原值，备份 v2/归档支持跨库编号重建。以下是实施前的调查背景。验证见 `reports/upgrade-phase3-2026-10-18.md`。


- 现状（已落地）：六个键全在前端 localStorage——`cczj.novel.{shelf,progress,reader}.v1`、
  `cczj.comic.{follow,progress,preferences}.v1`（`novel/readingState.ts:7-9`、
  `comic/state.ts:7-9`），经 `platform/storage.ts` 的 `{version,value}` 薄封装读写。
  本轮已定升到 v2 复合键（第 17 条）。
- 证据（不一致在哪）：视频模式的收藏与历史**在 SQLite**——`favorites`
  （`app/db/sqlite.go:253-260`，`UNIQUE(global_id, source_key, vod_id)`）与 `watch_history`
  （`:262-271`，带 `position REAL`）。所以「清一次 WebView 数据，书架和阅读进度全没，
  但收藏和观看历史还在」是当前会真实发生的事。
- 当前未迁移：小说/漫画仍经 `platform/storage.ts` 使用浏览器持久化。旧 ADR 0005 已删除，
  不能作为禁止迁移的依据；SQLite 迁移需落实表结构、绑定、异步 store 与旧值导入验证，
  与第 15 条的异步化重叠但不等价。当前规范见根目录 AGENTS.md。
- 证据（报告的态度）：`reports/00:97-105` 建议的表里包含 `progress`
  （`UNIQUE(profile_id, item_id, media_kind)`），`reports/03:119-121` 另提醒
  「不要在每次图片状态变化时都写一次全量书架」，要 debounce 合并、并在退出/切章/进后台时落盘。
- 要拍板：A 保持 localStorage（与 ADR 0014 一致，代价是与视频侧不一致、清 WebView 数据会丢）；
  B 搬进 SQLite 与 `favorites`/`watch_history` 并列（一致、可备份——`app/backup` 那套
  已经会带库走，代价是两张新表 + facade + 两个 store 改写 + 两份 ADR 要改归属）；
  C 双写，localStorage 当读缓存、SQLite 当权威（最贵，但切换期最稳）。
- 推断：B 更对，因为「书架」在语义上就是「收藏」，而收藏已经在库里；而且第 15 条已经
  要把两个 store 改成异步了，搬库的边际成本比以后单独搬一次要低。**但没有拍板，
  ADR 0016 里按 A 写，并把这一条挂在这里。**

<a id="origin-pending-decisions--22-四个小说漫画-ui-脚本还按演示内容写死现在是哑弹"></a>

##### 22. 四个小说/漫画 UI 脚本还按演示内容写死，现在是哑弹

**已解决（2026-10-19）**：删除依赖已下线演示内容的四个脚本，替换为 `scripts/test-reading-browser.py` 与专用浏览器夹具，覆盖真实 Vue 页面/store 的导入、分页、筛选、清理 localStorage 后恢复、失败重试和模式隔离，并加入 CI。它使用传输替身，不冒充 Android/Wails 真机测试；真实 SQLite 迁移另有 Go 验证。以下保留原问题记录。


- 已修（键名失效）：`scripts/test-reading-comic.py`、`scripts/test-reader-layout.py`、
  `scripts/test-novel-mode.py` 原来各硬编码一串 `cczj.*.v1` 键，用来在跑之前快照、`finally` 里还原。
  本轮存储键升到 v2（`frontend/src/novel/readingState.ts:8-10`、`frontend/src/comic/state.ts:9-11`），
  这三处快照会全取到 `null`，等于**不再还原任何东西**——脚本改过的书架、进度、阅读偏好会留在真实
  WebView 配置里。现已改成按 `cczj.` 前缀枚举，跑之前记下全部、跑完连本次新建的一起清掉，
  以后再改键名不会再悄悄失效。
- 证据（目标内容不存在）：脚本导航的是演示 id 与演示选择器——`/novel/read/last-train/0`
  （`test-reading-comic.py:48`）、`/novel/book/paper-moon`（`test-novel-mode.py:64`）、
  `/comic/series/cloud-island` 与 `/comic/read/moon-mail/0`（`test-reading-comic.py:87,90`）、
  `.comic-card` 数量断言 `===2`（`:86`）、`.novel-pagination-track`（`:48`）。
  演示书籍与演示作品已下线，模式在用户加源之前是空的，这些路由与选择器都不存在了。
- 证据（不在自动门禁里）：`scripts/verify.ps1` 不调用这四个脚本，它们要连 Android WebView
  或本机 Vite 手工跑，所以全绿验证捕获不到这条失效。
- 要拍板：A 直接删掉这四个脚本（阅读模式的 UI 验证就此空缺，只剩 `frontend/tests` 的 144 个单测）；
  B 改成夹具驱动——内置一个只在测试里启用的本地源（`httptest` 已在 `app/library/library_test.go`
  证明这条路可行），脚本先加源再导航；C 先删、等真机回归时按 B 重建。
- 推断：B 才是长期对的做法，因为它同时补上「前端视图 → 真实 facade」这条目前只有 Go 侧覆盖的链路；
  C 是当下最诚实的顺序，因为本轮没跑过任何真机 UI。按你之前说的真机验证要单独同意，
  这里只把失效记下来，不自行起模拟器验证。

---

<a id="origin-update-reliability"></a>

## docs/update-reliability.md

<a id="origin-update-reliability--更新可靠性修复2026-10-12"></a>

### 更新可靠性修复（2026-10-12）

<a id="origin-update-reliability--已复现的问题与修复"></a>

#### 已复现的问题与修复

- **更新包被占用时无法下载**：Windows 文件锁测试复现了 `CCZJ-Video-Update.exe` 的分享冲突。下载不再删除或截断固定文件名；每个下载源使用独立的 `.part` 文件，校验通过、关闭句柄后才改成资源的真实扩展名并写摘要凭据。网络截断或摘要失败会清理本次临时文件，再尝试下一下载源。
- **后台浮窗始终停在连接中**：`onBackendEvent` 已经交付 `event.data`，更新组件却再次读取 `.data`。进度和更新发现事件均已改为接收真实 payload。浮窗现在显示进度、速度、校验中、下载完成等待安装、下载失败，以及退出时安装安排。下载完成后提示保留，可用鼠标或键盘重新打开安装选项。
- **旧包被误当作最新版**：新包文件名带 release 版本与随机标识。扫描只恢复目标版本且有摘要记录的包，跳过当前程序、`.part` 和版本未知的历史包；扫描不再删除可能正在被下载器发布的文件。前端不再仅凭旧 localStorage 路径和文件存在就宣称最新版已下载。历史包需要重新下载。
- **数据库迁移失败留下不完整新库**：原先主库复制成功、WAL 复制失败后，下一次启动会因新库存在而跳过迁移。现在先暂存 DB/WAL/SHM，复制成功后将主库最后发布；发布失败清理本次已发布文件。迁移失败且目标库尚未存在时，启动返回错误，不再创建空库。旧目录保留。
- **安装失败仍宣称新版已安装**：原先 `Service.Install` 在安装校验前记录版本、消费待处理更新。现在只有安装器成功接手才记录版本；校验或启动脚本失败时仍保留更新提示和当前版本。前端也只在安装器成功接手后清除下载缓存。
- **路径解析失败退回错误目录**：`EvalSymlinks` 的错误结果曾覆盖原始可执行文件路径，回退可能变成当前工作目录。现在保留原始路径供回退使用。

<a id="origin-update-reliability--验证"></a>

#### 验证

修复前运行下列测试分别看到文件占用错误、进度仍为 0、残留新数据库、失败安装宣称版本 9.1.0；修复后均通过：

```powershell
go test ./app/updater -run TestDownloadWithLockedPreviousArtifact -count=1 -v
npm.cmd --prefix frontend test -- --run tests/update.test.ts
go test ./app/service -run TestMigrateLegacyDatabaseFailureDoesNotPublishPartialDatabase -count=1 -v
go test ./app/update -run TestRejectedInstallDoesNotClaimTheNewVersion -count=1 -v
```

还验证了坏下载源回退、摘要失败清理、目标版本匹配与当前程序排除。项目 `scripts/verify.ps1` 的前端约定检查、34 个前端测试、生产构建、`go vet ./...` 和全量 Go 测试通过。

实际 Chrome 浏览器测试使用真实 Vue 组件与 Wails 事件分发，只替换绑定调用，不访问发布页、不执行安装：

```powershell
python .agents/skills/webapp-testing/scripts/with_server.py --server 'node frontend/node_modules/vite/bin/vite.js frontend --host 127.0.0.1 --port 9247 --strictPort' --port 9247 -- python scripts/test-update-browser.py --channel chrome
```

需要 Python Playwright。浏览器测试覆盖更新发现事件、旧缓存拒绝、实时进度、未知大小、校验、完成提示、键盘打开、退出时安装/取消、安装失败保留缓存与下载失败提示。

`go test -race ./app/updater ./app/update ./app/service` 在本机 Windows 工具链中启动失败（`0xc0000139`），无法确认 race 检查结果；本机没有 `golangci-lint`。普通测试与 vet 通过。

<a id="origin-update-reliability--适用范围与旧版恢复"></a>

#### 适用范围与旧版恢复

这些是源码修复，尚未发布或修改发布版本。中文与空格目录的文件锁场景已测试；测试路径包含 OneDrive 名称，但没有模拟真实 OneDrive 同步进程。数据库测试验证迁移中途失败及已有迁移用例，不代表已经诊断用户机器上的所有数据库异常。

旧更新器不能自动获得尚未运行的新代码。若旧版更新被卡住，可以完全退出应用后手动覆盖修复版，保留原有数据目录。若正在运行的程序本身就叫 `CCZJ-Video-Update.exe`，退出后将它改为正常应用名再启动，可避开旧更新器写同名文件的冲突；其他进程持有文件锁时仍需先释放占用。

避免回归的关键是维持事件 payload 类型约定、使用独立下载文件、按 release 恢复缓存，以及把迁移与安装的成功记录放在对应动作真正成功之后。
