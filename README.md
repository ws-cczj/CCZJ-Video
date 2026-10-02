# CCZJ Video

Current release: **v2.2.0**

<p align="center">
  <strong>多源视频资源聚合桌面应用</strong><br>
  基于 Wails v3 + Vue 3 + Go 构建的跨平台桌面客户端
</p>

<p align="center">
  <img src="https://img.shields.io/badge/Go-1.25+-00ADD8?style=flat-square&logo=go&logoColor=white" alt="Go">
  <img src="https://img.shields.io/badge/Vue-3.x-4FC08D?style=flat-square&logo=vue.js&logoColor=white" alt="Vue">
  <img src="https://img.shields.io/badge/Wails-v3.0.0--beta.24-EB2F2F?style=flat-square&logo=data:image/svg+xml;base64,&logoColor=white" alt="Wails">
  <img src="https://img.shields.io/badge/TypeScript-4.9-3178C6?style=flat-square&logo=typescript&logoColor=white" alt="TypeScript">
  <img src="https://img.shields.io/badge/Platform-Windows-0078D6?style=flat-square&logo=windows&logoColor=white" alt="Platform">
</p>

---

## ✨ 功能特性

### 核心功能

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

### 画质增强

- **Anime4K 动画增强** — 移植自 Anime4K v4.0，支持 S/M/L 三档模型的 WebGL2 CNN 实时 2× 超分辨率，为动画/动漫视频提供高质量画面增强
- **Film 影视增强** — WebGL2 上的 FSRCNNX 卷积超分 + CAS 自适应锐化，这两步总是执行。着色器里另外写了去隔行、降噪、时间混合、HDR 色调映射四段，但播放器用 `FILM_PRESET`（这四项默认关闭、且运行期没有任何入口能改）实例化，所以 Film 模式现在只做「超分 + 锐化」，那四段是待接线的半成品
- **换集自动重建** — 切集/切线路时增强管线会被销毁并重启，同时显式归还 WebGL 上下文（每个页面的上下文数量有上限，不还就会静默拿不到新上下文）
- 两个增强器都实现了 `updateOptions`，`FilmUpscaler` 还有按帧率降级的 `adaptQuality`；三者在当前 UI 里都没有调用点，`autoQuality` 恒为 false

### 扩展包

规范在 [`docs/plugins.md`](docs/plugins.md)，理由记录在 ADR 0007（声明式那一层）与 ADR 0008（放开脚本这一步）。
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
  包 JS 跑在应用自己的权限里，`XMLHttpRequest` / `WebSocket` 那类旁路没拦（播放器正在用它拉分片），细节见 [`docs/plugins.md`](docs/plugins.md) §2.1。
- **设置页的「动画」「日志」「诊断」本身就是三个随应用自带的包**（`app/plugin/builtin/`）。它们只在第一次启动时落进
  `plugins\` 一次，之后应用不再碰：删掉那几个文件夹，设置页就真的少了那几项。「动画」那一份还带着全应用
  的动效时长与缓动（`motion.css`），改它就能把动画调快调慢；设置 → 动画里另有「开启动画 / 关闭动画」总开关，
  关掉之后所有过渡退化成直接切换。
- **卸载有两条路**：卡片上的「卸载」按钮直接把那个包目录删掉（不进回收站、不留备份），或者自己删文件夹。
  上面那三个内置包不给这个按钮，Go 侧也拒绝删它们。
- 校验失败整包判 `invalid`，设置页直接展开机器可读的原因码与人话说明；单个坏包不影响其他包，也不影响内置功能。
  脚本包跑挂了更彻底：它这一轮注册的东西全部撤销，本机记下原因并停用，改好脚本点「重试」再注入一次。

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
| 扩展包 | 列出 `%APPDATA%\CCZJ Video\plugins` 下扫到的包、把包文件夹拖到卡片上即装（先校验后落位，逐个给回执）、按包启停、「卸载」直接删掉那个包目录（应用自带的三个包不给卸载按钮）、显示校验失败的原因码与说明、包声明的 `write`/`network` 权限与授权状态（可撤销，撤销后下次用到再问）、脚本包跑挂时的隔离原因与「重试」、图标与「几个文件 · 多大 · 最后改动」、复制/打开目录、手动重扫 |
| 日志 | 实时跟随、级别切换与过滤、关键字搜索、历史文件分页查看、导出与复制 |
| 诊断 | 两层读数。**实时**（每 2 秒读一次内存计数器）：本次会话的播放效果（首播等待、卡顿时长、丢帧率、当前线路规格、实际观看时长、播放错误）、按用途拆分的出网吞吐（请求数、失败、字节、均耗时、峰值速率）、Go 与前端各缓存的命中率（详情线路、豆瓣热榜、豆瓣评论、热榜匹配、TS 分片、海报、图片代理）。**快照**（点「刷新」才跑）：版本与安装标记、运行时长、Go/Wails 运行时与平台、协程数/GC/堆内存、数据目录与可执行文件路径、后台任务计数、数据库体积与日志占用、各表行数；采集调度状态与最近启停；豆瓣补全进度、反爬静默与 302 验证题求解结果；数据健康度（缺评分、缺条目 ID、冷却中、重复分组）；每个源的健康度点阵与「失效」提示。唯一的写动作是「疑似重复身份」的合并，动手前弹确认；探测采集源在「采集源」页 |
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
│  │  hls.js       │    │  Download Engine      │  │
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
| hls.js | HLS 流媒体播放（挂在原生 `<video>` 上，播放器 UI 自研） |
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

---

## 📁 项目结构

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
│   │   ├── download.go             # 下载任务生命周期：启动/暂停/续传/取消/进度与持久化
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

1. 首页浏览或搜索页搜关键词
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

### 跨源同片

侧栏的「合并影片库」页面已下线，跨源同片现在只在详情页体现：一次按 `global_id` 的本地查询就能给出「换源看」入口，不碰网络。

- **一个身份 = 豆瓣 ID**，没有豆瓣 ID 时是「归一化标题 + 年份 + 类型」；同一身份在 N 个源里的 N 行目录算作同一部片
- **详情页换源**：`/detail/:sourceKey/:globalId`，同一身份有多个源时详情页出现「其它源也有这部」列表，
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

### 装一个扩展包

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
[`docs/plugins.md`](docs/plugins.md)；`docs/examples/` 下有六份可直接拷走的样例，六份都带着自己的图标：
`script-ui-tweaks` 是一个「扩展包自检台」页面，把注入型包的能力各用了一遍；`ai-assistant` 是那套能力的
上限样本——一整页 AI 助手，把既有后端绑定当成模型的工具表来调，应用侧不改一行；`icon-gallery` 那一份专门为
看图标而写——卡片图标 + 包目录里 26 枚 PNG，整个文件夹拖进去就能一次验完「拖拽安装 → 校验 → 图标显示」。

写错了不会静默失效：整包判「校验失败」，卡片上直接展开原因码和人话说明，改好再点重扫即可。一个坏包不影响其他包，
也不影响内置功能。

### 看日志与诊断

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

1. 在 `app/service/` 里为 `App` 结构体加方法：绑定方法按域写进对应的 `facade_<域>.go`（video / source /
   douban / plugin …），业务实现放子包，`facade.go` 只留分区索引和跨域共用的 JSON 参数助手。
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

### 前端依赖

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

## 📄 更新日志

详细更新历史请查看 [CHANGELOG.md](CHANGELOG.md)

---

## 📄 开发规范

架构边界与开发决策都在 [`docs/adr/`](docs/adr)：0001~0006 是数据与服务层的既有决定（0001 / 0002 / 0005 / 0006
在 2026-10-01 按当前实现修订过），0007 / 0008 解释扩展包为什么分成「声明式」与「注入 JS」两层，
0009 是错误码的唯一口径——界面上要分支的错误一律走 `app/apperror` 的码，不许按 `err.Error()` 的文本猜。

- [ADR 0005](docs/adr/0005-architecture-boundaries.md) — 前端绑定出口、存储归属与 `app/service` 分层
- [ADR 0009](docs/adr/0009-coded-errors.md) — 错误码边界：什么错误带码、什么错误只串链
- [`docs/plugins.md`](docs/plugins.md) — 扩展包规范与原因码表
- [`docs/pending-decisions.md`](docs/pending-decisions.md) — 待拍板事项：已经查清、但方向要人定的记录。
  与 ADR 相反，这里只放没决定的；拍板之后挪进 `docs/adr/` 或直接改代码，再从这里删掉。

---

## 📄 使用许可

本项目**不是**可自由商用的开源软件，许可条款见根目录 [`LICENSE`](LICENSE)（当前条款版本 1.0）：

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

## 🙏 致谢

- [Wails](https://wails.io/) — 优秀的 Go + Web 桌面应用框架
- [Vue.js](https://vuejs.org/) — 渐进式 JavaScript 框架
- [hls.js](https://github.com/video-dev/hls.js) — HLS 流媒体播放
- [Anime4K](https://github.com/bloc97/Anime4K) — 动画超分辨率算法
- [FSRCNNX](https://github.com/igv/FSRCNN-TensorFlow) — 快速超分辨率卷积神经网络
