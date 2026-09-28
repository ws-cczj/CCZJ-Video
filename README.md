# CCZJ Video

Current release: **v2.1.0**

<p align="center">
  <strong>多源视频资源聚合桌面应用</strong><br>
  基于 Wails v3 + Vue 3 + Go 构建的跨平台桌面客户端
</p>

<p align="center">
  <img src="https://img.shields.io/badge/Go-1.25+-00ADD8?style=flat-square&logo=go&logoColor=white" alt="Go">
  <img src="https://img.shields.io/badge/Vue-3.x-4FC08D?style=flat-square&logo=vue.js&logoColor=white" alt="Vue">
  <img src="https://img.shields.io/badge/Wails-v3.0.0--beta.24-EB2F2F?style=flat-square&logo=data:image/svg+xml;base64,&logoColor=white" alt="Wails">
  <img src="https://img.shields.io/badge/TypeScript-5.x-3178C6?style=flat-square&logo=typescript&logoColor=white" alt="TypeScript">
  <img src="https://img.shields.io/badge/Platform-Windows-0078D6?style=flat-square&logo=windows&logoColor=white" alt="Platform">
</p>

---

## ✨ 功能特性

### 核心功能

- **多源聚合** — 支持添加多个视频 API 数据源，统一管理所有资源
- **在线播放** — 内置 HLS 播放器（基于 xgplayer），支持多集连播、播放进度记忆、同一部片的多条线路切换与测速
- **字幕** — 播放器手动加载本地 `.srt` / `.vtt` 文件并自己叠加渲染，支持显示/隐藏/更换/移除；不支持 `.ass`/`.ssa`，也不从源站取内嵌轨
- **智能搜索** — 全局关键词搜索 + 源内模糊搜索 + 分类/年份/地区多维筛选
- **离线下载** — 多线程分片下载引擎，支持暂停/恢复/取消，下载进度实时展示
- **收藏 & 历史** — 视频收藏同步、观看历史自动记录，支持断点续看
- **跨源合并影片库** — 同一个身份（豆瓣 ID 或归一化标题+年份+类型）在多个源里的多份记录并成一张卡片，卡片上标「N 个源有货」，详情页可换源看同一部
- **数据备份与恢复** — 收藏、历史、设置、采集源定义和已补全的全局元数据打包成单个文件导出，换机/重装后导入合并；升级前的数据库快照也能直接读回来（详见「设置与诊断」的高级分组）
- **回收站** — 删除视频只是挪进回收站，收藏和历史跟着隐藏不丢，随时恢复；确认无误再彻底删除
- **源健康度巡检** — 每轮采集收尾和后台每 6 小时的主动巡检各给每个源记一条样本（成功率、延迟、连败次数），「设置 → 诊断」用点阵展示并按连败阈值提示「可能已失效」。巡检只请求 `sources.api_url` 的一页列表，不会逐个校验影片的播放地址

### 画质增强

- **Anime4K 动画增强** — 移植自 Anime4K v4.0，支持 S/M/L 三档模型的 WebGL2 CNN 实时 2× 超分辨率，为动画/动漫视频提供高质量画面增强
- **Film 影视增强** — WebGL2 上的 FSRCNNX 卷积超分 + CAS 自适应锐化，这两步总是执行。着色器里另外写了去隔行、降噪、时间混合、HDR 色调映射四段，但播放器用 `FILM_PRESET`（这四项默认关闭、且运行期没有任何入口能改）实例化，所以 Film 模式现在只做「超分 + 锐化」，那四段是待接线的半成品
- **换集自动重建** — 切集/切线路时增强管线会被销毁并重启，同时显式归还 WebGL 上下文（每个页面的上下文数量有上限，不还就会静默拿不到新上下文）
- 两个增强器都实现了 `updateOptions`，`FilmUpscaler` 还有按帧率降级的 `adaptQuality`；三者在当前 UI 里都没有调用点，`autoQuality` 恒为 false

### 自动化引擎

- **采集调度器** — 后台定时自动采集，支持全量/增量模式、可配置循环间隔
- **豆瓣数据补全** — 自动爬取豆瓣评分、封面、简介等信息，丰富视频元数据
- **启动补采** — 应用启动时自动补采离线期间遗漏的数据

### 相似推荐

- **详情页内嵌区** — 「相似推荐」区块自己拉同类型的前 100 条当候选池算出结果，最多显示 7 条；结果超过 7 条时才出现「查看更多」，点它带着 `sourceKey/vodId/vodName` 跳到整页。前端算不出任何一条时，退回后端按类型给的兜底列表
- **整页 `/recommendations`** — 侧栏没有这一页，只能从详情页进。候选池自己拉当前源的前 200 条（不分类型），不依赖别的页面留下的列表，所以直接改 URL 进来也能用
- **算法是内容交集，不是协同过滤** — 两处共用 `frontend/src/utils/recommend.ts` 的 `computeRecommendations`：拿种子片的演员/导演/类型/年份/地区与候选逐条比对打分，卡片上标匹配分和命中的维度。**它完全不读观看历史和收藏**，所以这里没有"个性化"，只是"和这一部像不像"

### 版本更新

- **启动自动检查更新** — 每次启动延迟 3 秒检查一次，没有"每天一次"的缓存，所以反复重启会反复请求 GitHub
- **多渠道版本获取** — 支持 GitHub API、GitHub Raw、jsdelivr CDN、Gitee 等多个版本信息源
- **更新日志展示** — 版本升级后自动展示更新内容，支持历史版本查看。启动时比对的是「安装版本标记」与编译进去的版本，两者不一致就保留标记（热替换后 exe 里的版本号还是旧的，靠标记才认得出真实版本）

### 设置与诊断

原先独立的后台管理面板（`/dev-admin`）已整体移除——它的每一项能力都已在侧栏页面中存在。运维能力改为集中在「设置」页的分组里：

| 分组 | 功能 |
|------|------|
| 基本设置 | 窗口尺寸、字体与语言、主题外观、关闭行为、数据入库策略；豆瓣补全轮询间隔；日志保留天数 |
| 主题外观 | 深浅色跟随、预设与自定义主题、主色与派生色 |
| 缓存管理 | 四栏占用：TS 片段内存缓存、TS 磁盘缓存（IndexedDB）、IndexedDB 总量、localStorage——数字全部由前端统计，Go 侧的 `GetCacheInfo` 目前没有调用点。分类清理只给「TS 内存」「TS 磁盘」两项；「一键清理」调 Go 的 `ClearCache(memory)`，Go 清完自己的详情/热榜/评论缓存后广播 `cache:invalidate`，前端的详情、海报、TS 片段跟着一起清 |
| 日志 | 实时跟随、级别切换与过滤、关键字搜索、历史文件分页查看、导出与复制 |
| 诊断 | 运行状态快照，默认只读：版本与安装标记、运行时长、Go/Wails 运行时与平台、协程数/GC/堆内存、数据目录与可执行文件路径、后台任务计数；数据库与磁盘缓存体积、各表行数；采集调度状态与最近启停；豆瓣补全进度、反爬静默与 302 验证题求解结果；数据健康度（缺评分、缺条目 ID、冷却中、重复分组——重复分组可以在这一页直接发起合并）；每个源的健康度点阵与「失效」提示。两个动作是例外：底部「探测采集源」会真的发请求（并把样本记进巡检健康度），合并会写库，两者动手前都弹确认 |
| 高级 | 数据备份与恢复（导出/导入备份文件、迁移前快照恢复）、豆瓣补全队列统计、手动触发一轮补全、图片代理私网放行开关 |

### 用户体验

- **深色/浅色主题** — 跟随系统或手动切换，全局 CSS 变量自适应
- **滚动位置记忆** — 页面切换后自动恢复滚动位置
- **组件缓存** — KeepAlive 缓存列表页面，避免重复加载
- **自定义标题栏** — 原生无边框窗口 + 自定义标题栏（最小化/最大化/关闭）
- **图片代理** — Go 后端代理远程图片请求，绕过 CORS 限制

---

## 🏗️ 技术架构

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
│  │  xgplayer     │    │  Download Engine      │  │
│  │  Anime4K      │    │  Image Proxy          │  │
│  │  FilmUpscaler │    │  Auto Updater         │  │
│  └───────────────┘    └────────────────────────┘  │
└─────────────────────────────────────────────────────┘
```

### 前端

| 技术 | 用途 |
|------|------|
| Vue 3 Composition API | UI 框架 |
| TypeScript | 类型安全 |
| Pinia | 状态管理（只放跨页面共享的状态） |
| Vue Router | Hash 路由 + 滚动位置恢复 |
| CSS 变量 + `cczj-*` 工具类 | 主题与原子样式（`src/styles/cczj-utilities.css` 手写，未接入 UnoCSS/Tailwind 构建链路） |
| xgplayer + hls.js | 视频播放（HLS 流） |
| Anime4K WebGL2 | 动画画质增强 |
| FSRCNNX WebGL2 | 影视画质增强 |
| Vite | 构建工具 |

### 后端

| 技术 | 用途 |
|------|------|
| Go 1.25+ | 主语言 |
| Wails v3 | 桌面应用框架 |
| SQLite (modernc) | 纯 Go 嵌入式数据库 |
| Brotli / Gzip | 数据压缩（导入导出） |
| Snowflake | 分布式 ID 生成 |

---

## 📁 项目结构

```
CCZJ Video/
├── main.go                         # 入口：只负责 buildApp() 与 app.Run()
├── app.go                          # 装配根：embed 前端产物、注册服务、主窗口、系统托盘
├── app/                            # 业务代码全部在这里，根目录不再散落 app_xxx.go
│   ├── service/                    # 唯一的 Wails 绑定服务（App 结构体）
│   │   ├── app.go                  # App 结构体、NewApp、启停生命周期、下载 DTO
│   │   ├── facade.go               # 转发方法：只做参数校验与转调子包
│   │   ├── diagnostics.go          # 诊断页只读快照、采集源探测
│   │   ├── logs.go                 # 日志查询/导出/级别切换
│   │   ├── download.go             # 下载引擎（直连多线程 + m3u8）
│   │   └── direct_resume.go        # 断点续传清单
│   ├── apperror/                   # 统一错误码（前端 normalizeApiError 依赖它）
│   ├── applog/                     # 日志系统：环形缓冲 + 按天滚动 + 订阅
│   ├── backup/                     # 全量备份导出、导入合并、迁移前快照恢复
│   ├── cache/                      # 统一缓存失效层（写库后按视频/整源广播）与占用统计
│   ├── collect/                    # 采集引擎（fetcher → processor → strategy）
│   ├── collection/                 # 采集/豆瓣调度器
│   ├── db/                         # SQLite 数据层（source / video / douban / catalog / recycle / diagnostics）
│   ├── detail/                     # 详情按需补全
│   ├── douban/                     # 豆瓣爬虫、热榜、评论、302 验证题自解
│   ├── download/                   # 下载注册表与目录策略
│   ├── files/                      # 文件与导入导出
│   ├── handler/                    # 请求处理器（collect / scheduler / source / video）
│   ├── lifecycle/                  # 后台任务组（Go/Stop/Context 统一管理）
│   ├── media/                      # 媒体元数据处理
│   ├── model/                      # 数据模型
│   ├── proxy/                      # 图片代理与 HLS 代理
│   ├── settings/                   # 持久化设置
│   ├── source/                     # 数据源领域逻辑
│   ├── update/ + updater/          # 更新服务与 GitHub Release 检测/下载/安装
│   ├── util/                       # 压缩 / 加密 / ID 生成
│   └── window/                     # 窗口尺寸与关闭行为
├── build/                          # Taskfile、版本提取、图标权重脚本
├── scripts/verify.ps1              # 边界守卫 + go vet + go test + 前端构建
├── scripts/check-frontend-conventions.mjs  # i18n 中英键位 / 未引用键 / cczj-* 类定义检查
├── frontend/
│   ├── bindings/                   # Wails 自动生成的 JS 绑定（勿手改）
│   ├── src/
│   │   ├── api/                    # 前端访问绑定/运行时/事件的唯一出口
│   │   ├── components/             # 公共组件 + ui/ 基础组件
│   │   ├── locales/                # zh-CN.ts / en.ts，键必须严格对齐
│   │   ├── platform/               # localStorage 等平台能力封装
│   │   ├── player/                 # 播放器控制逻辑
│   │   ├── stores/                 # Pinia
│   │   ├── styles/                 # 主题变量与 cczj-* 工具类
│   │   ├── utils/                  # Anime4K / FSRCNNX 着色器与权重、推荐算法、字幕解析
│   │   └── views/                  # Home / Search / Recent / MergedLibrary / Detail / Player /
│   │                               #   Sources / VideoTypes / Favorites / History /
│   │                               #   Downloads / Recommendations / RecycleBin / Settings
│   └── package.json
├── docs/adr/                       # 架构决策记录
├── go.mod / go.sum
├── Taskfile.yml / wails.json
├── CHANGELOG.md
└── version.json                    # 版本号来源，构建时注入 updater.Version
```

---

## 🚀 快速开始

### 环境要求

- **Go** 1.25+
- **Node.js** 18+
- **Wails v3 CLI**（beta.24）：`go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.24`
- **Task**（可选，用于 `Taskfile.yml` 里的构建任务）
- **Windows 10/11**（WebView2 运行时）

### 安装依赖

```bash
# Go 依赖
go mod tidy

# 前端依赖
cd frontend && npm install
```

### 开发模式

```bash
task dev            # 等价于 wails3 dev -config ./build/config.yml -port 9245
```

前端改动走 Vite HMR 即时生效；**Go 改动不会自动重启进程**，需要手动退出再 `task dev`。

### 构建生产版本

```bash
task build          # go mod tidy -e → 前端构建 → go build 注入版本号
task verify         # 边界守卫 + go vet + go test ./... + 前端 vue-tsc & vite build
```

`task build` 实际执行的链接参数是
`-ldflags="-w -s -H windowsgui -X cczjVideo/app/updater.Version=<version.json 里的版本>"`，
版本号唯一来源是仓库根目录的 `version.json`（由 `build/get_version.ps1` 读取）。产物在 `bin/cczjVideo.exe`。

### 发布新版本

1. 改 `version.json`：`version` 决定构建注入的版本号；`desc` 是更新弹窗里展示的本次说明；`history` 是历史版本说明，只有比用户当前版本更新的条目才会出现在弹窗里。
2. 同步 `CHANGELOG.md`：把 `[Unreleased]` 落成 `## [x.y.z] - 日期`。
3. 构建并把产物改名：`task build` 得到 `bin/cczjVideo.exe`，发布时命名为 `cczjVideo-vX.Y.Z-release.exe`（更新器按扩展名挑附件）。
4. 打标签并创建 GitHub Release：`git tag vX.Y.Z && git push origin vX.Y.Z`。Release 正文是更新弹窗优先读取的说明来源，`version.json` 只在 Release API 全部失败时兜底。
5. 把 `version.json` 推上 `main`：回退通道读的是 GitHub Raw / jsdelivr / Gitee 上的 `main` 分支，不推上去兜底就会拿到旧版本说明。

更新检查的顺序是：GitHub Release API 直连 → `gh-proxy.org` 等三个代理（每个源最多重试 3 次）→ 多渠道 `version.json`。安装流程见「设置 → 关于」中的更新弹窗，不会自动下载，需要你手动点。

---

## 📖 使用说明

### 添加数据源

1. 点击侧边栏「采集源」
2. 点击「添加源」，输入源名称、唯一 Key、API 地址
3. 保存后返回首页，数据将自动加载

### 采集数据

- **手动采集**：侧栏「采集源」→ 源卡片上的「开始采集」或「增量采集」
- **自动采集**：全部在「采集源」页配置。顶部调度状态条的「调度设置」管全局：启用开关、采集间隔、源/页节流、启动补采、首次全量；每个源卡片上的时钟按钮单独设置该源的定时器（定时器需全局开关启用后才会跑）

### 豆瓣数据补全

- 默认使用匿名请求，不依赖仓库内的登录 Cookie；网络错误、验证页和重定向不会把条目冷却 24 小时。
- 如确有需要，可在启动程序前通过 `CCZJ_DOUBAN_COOKIE`（或 `DOUBAN_COOKIE`）提供当前有效的 Cookie。Cookie 只从运行环境读取，不应提交到仓库。

### 播放视频

1. 首页浏览、搜索页搜关键词，或在「合并影片库」按身份翻整库
2. 点击进入详情页查看简介、选集
3. 选择集数进入播放器

#### 多条播放线路与测速

maccms 类源站用 `$$$` 在 `vod_play_url` 里并列多条「同名集表」（`vod_play_from` 用同一个
分隔符给出线路名）。详情接口按 `$$$` 先切线路、再在每条线路内按 `#` 切集，线路数大于 1 时
详情页和播放页都会出现线路条：点芯片换线路，播放中换线路会按 `ep_num` 找回当前这一集继续播，
全屏不丢；「测速」按钮对每条线路各取一小段样本比吞吐和延迟，最快的标「最快」，探测失败的标
「不可用」，不会自动把你切走。

只有单条线路时线路条整块隐藏 —— 本机当前配置的 `yyzy_tv`、`maoyanapi` 每个影片都只有一条
线路（`1080zyk` / `mym3u8`），要看到线路条需要接入本身并列多条线路的聚合源。

#### 集号而不是序号

源站集表的顺序并不可信（新集常排在最前，也有源按热度混排），所以采集时从集名里抠真实集号：
`S01E05` / `第5集` / `5话` / `EP5` / 纯数字都认，数字支持全角和中文数字；`HD中字`、`1080P`、
`BD国语` 这类画质标记不会被误读成"第 1080 集"。认不出来才回落到列表序号。观看进度、TS 缓存键、
下载任务、换线路续播全都以 `ep_num` 认集，所以「看到第 24 集」在下次采集后仍然是同一集。

#### 字幕

播放控制条上的字幕按钮 → 「加载字幕」选本地 `.srt` / `.vtt` 文件，面板里可以隐藏/显示、更换、移除。
几个边界：

- **必须手动选文件**，Go 侧没有字幕入口，也不解析源站内嵌或外链的字幕轨
- 只支持 SRT/VTT。`.ass` / `.ssa` 不认（Chromium 的 `<track>` 也渲染不了它们）
- 字幕由播放器自己叠加一层 div 渲染，不走原生 `<track>`——画质增强开着时画面上盖的是 WebGL canvas，原生轨会被挡住
- **换集/换线路会清掉字幕**：字幕文件是对着某一集的时间轴配的，换集后必然错位，留着比清掉更误导人，需要重新加载

### 合并影片库

入口在侧栏「合并影片库」（`/merged-library`），读路径是 `app/db/catalog_union.go` 的 `GetCatalogUnionPage`。

- **一张卡片 = 一个身份**。身份是豆瓣 ID，没有豆瓣 ID 时是「归一化标题 + 年份 + 类型」；同一身份
  在 N 个源里的 N 行目录并成一张卡，卡片标「N 个源有货」，`N` 取 `COUNT(DISTINCT source_key)` 且不数软删除行
- **卡片显示哪一行的元数据**：取该身份 `vod_time` 最新的那一行——也就是"哪个源最近更新过这部"，
  不是按源健康度或评分优选。类型筛选按 `global_type_id` 整数走，年份/地区是跨源去重后的并集
- **详情页换源**：`/detail/:sourceKey/:globalId`，同一身份有多个源时详情页出现「其它源」列表，
  点它换源重开；单源库（本机现状）这块整块隐藏
- **身份合并**：确实该并却没并上的（同豆瓣 ID 或同名同年重复建了身份）在「设置 → 诊断 → 重复分组」
  列出来，可以在那一页直接发起合并。合并复用迁移 v2 的存活行选择逻辑，收藏与观看历史一并迁移到存活身份，
  操作前先弹确认；候选最多 8 个身份一组

### 下载视频

1. 在详情页点击「下载」按钮选择集数
2. 侧边栏「下载管理」查看进度
3. 下载完成后点击「打开」定位文件

### 启用画质增强

在播放器页面点画质增强按钮，从下拉里选「原高清」「影视增强」或 Anime4K 的 S/M/L 档，其中会按当前
视频高度给一个档位标「推荐」（≥1080 推 S，≥720 推 M，更低推 L）；选择记在设置里，跨集、跨源保持，
开着增强时可以用对比滑块看左右两侧的差异。Film 模式现在只做 FSRCNNX 超分 + CAS 锐化（「画质增强」
一节写了哪几段着色器还没接线），换集时管线会重建。

### 看日志与诊断

- 应用内：「设置 → 日志」实时跟随、按级别过滤、搜关键字、翻页看历史文件、导出/复制。
- 应用外：日志按天写在 `%APPDATA%\CCZJ Video\applog\cczj-YYYY-MM-DD.log`，保留天数在「设置 → 基本设置」里改。
- 「设置 → 诊断」是运行状态快照：运行时环境、内存与协程、数据库与各表行数、采集与豆瓣调度状态、
  反爬静默与 302 验证题求解结果、数据健康度（缺评分 / 缺条目 ID / 冷却中 / 重复分组，重复分组可以在这里合并）、
  每个源的采集与巡检健康度点阵。除了「探测采集源」和「合并」这两个会弹确认的动作，这里不发请求也不写库。
- 诊断面板上的 schema 版本号读的是 `GetDiagnostics` 返回的 `env.schema_version`。Go 侧 `app:ready` 事件
  也带了 `schema_version`，但前端只在 `api/events.ts` 声明了类型、没有订阅——要拿启动期的 schema 版本得自己接。

### 备份与换机迁移

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

### 回收站

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

## ⌨️ 开发指南

### 添加新的 Go 绑定方法

1. 在 `app/service/` 里为 `App` 结构体加方法（业务实现放对应子包，`facade.go` 只做转发）。
2. `wails3 generate bindings -clean` 重新生成绑定，输出到 `frontend/bindings/cczjVideo/app/service/`。
3. 前端**只允许**通过 `frontend/src/api/app.ts` 访问绑定；`scripts/verify.ps1` 会拦截任何直接
   `import 'bindings/...'` 或 `import '@wailsio/runtime'` 的组件。

注意：Wails 会把服务结构体上的**每个导出方法**都暴露给 WebView，所以只想给 Go 内部用的东西
不要写成 `App` 的导出方法（托盘引用就是包级变量 `service.SetSystemTray` 而不是方法，原因在此）。

### 根目录约束

根目录只保留 `main.go` 与 `app.go`。新增 Go 代码请放进 `app/<包名>/`；
`scripts/verify.ps1` 会拒绝任何重新出现在根目录的 `app_xxx.go`。

### 前端 Store 规范

- 使用 Pinia Composition API 风格（`defineStore('name', () => { ... })`）
- 导出函数和 ref，组件通过 `const store = useXxxStore()` 使用
- 只有跨页面共享的状态才进 store；单页状态留在页面里，组件可以直接调用 `src/api/app.ts`
  的适配函数。`scripts/verify.ps1` 拦的是绕过 `src/api/` 直接 import 绑定或 `@wailsio/runtime`，
  不是"组件不许碰接口"。

### 国际化

`frontend/src/locales/zh-CN.ts` 与 `en.ts` 的键必须一一对应，且不允许出现未被引用的键；
`cczj-*` 工具类必须在样式里真实定义。这三条由 `scripts/check-frontend-conventions.mjs` 静态检查，
`scripts/verify.ps1` 在 `go vet` 之前执行它（`node scripts/check-frontend-conventions.mjs` 也能单独跑，
加 `--report-unused` 打印未引用键清单）。需要这层守卫的原因：locales 是无类型的 `export default {}`，
`cczj-*` 是手写 CSS，两者写错 `vue-tsc` 和 Vite 都不会报错。

组件模板里用 `t()`，模板之外（store、工具函数、prop 默认值）用 `tr()`。注意 `tr()` 只有在渲染期间
调用才会随语言切换重算——把它写进 setup 顶层常量或模块级常量就会冻结成挂载时的文案，需要静态列表
时用 `computed`。`<html lang>` 由 `locales/index.ts` 跟随 locale 自动同步。

### 样式约定

主题变量定义在 `App.vue`，全局 CSS 变量自适应深色/浅色：

```css
--bg-app, --bg-secondary, --bg-card, --bg-hover
--text-primary, --text-secondary, --text-muted
--accent, --accent-alpha-10, --accent-alpha-20, --accent-alpha-35
--border, --border-strong
--danger, --warning, --success, --info
```

原子类是 `src/styles/cczj-utilities.css` 里**手写**的 `cczj-*` 工具类，没有接 UnoCSS/Tailwind
的构建链路——写一个不存在的 `cczj-xxx` 不会报错，只会静默不生效，用之前先确认已定义。

---

## 📦 技术依赖

### Go 依赖

| 包 | 用途 |
|---|------|
| `github.com/wailsapp/wails/v3` | 桌面应用框架 |
| `modernc.org/sqlite` | 纯 Go SQLite 驱动 |
| `github.com/jmoiron/sqlx` | SQL 扩展 |
| `github.com/andybalholm/brotli` | Brotli 压缩 |
| `github.com/bwmarrin/snowflake` | 分布式 ID |

### 前端依赖

| 包 | 用途 |
|---|------|
| `vue` | UI 框架 |
| `vue-router` | 路由管理 |
| `pinia` | 状态管理 |
| `@wailsio/runtime` | Wails 运行时 API |
| `xgplayer` / `xgplayer-hls` | 视频播放器 |
| `hls.js` | HLS 流媒体协议支持 |
| `unocss` / `@unocss/preset-wind` | 已声明但**未接入构建链路**；实际样式是手写 `cczj-*` 工具类 |
| `vite` | 前端构建工具 |

---

## 📄 更新日志

详细更新历史请查看 [CHANGELOG.md](CHANGELOG.md)

---

## 📄 开发规范

架构边界与开发决策请参阅 [ADR 0005](docs/adr/0005-architecture-boundaries.md)。

---

## 📄 开源协议

本项目仅供个人学习研究使用。

---

## 🙏 致谢

- [Wails](https://wails.io/) — 优秀的 Go + Web 桌面应用框架
- [Vue.js](https://vuejs.org/) — 渐进式 JavaScript 框架
- [xgplayer](https://github.com/bytedance/xgplayer) — 西瓜播放器
- [hls.js](https://github.com/video-dev/hls.js) — HLS 流媒体播放
- [Anime4K](https://github.com/bloc97/Anime4K) — 动画超分辨率算法
- [FSRCNNX](https://github.com/igv/FSRCNN-TensorFlow) — 快速超分辨率卷积神经网络
