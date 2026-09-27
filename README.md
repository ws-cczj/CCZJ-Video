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
- **在线播放** — 内置 HLS 播放器（基于 xgplayer），支持多集连播、播放进度记忆
- **智能搜索** — 全局关键词搜索 + 源内模糊搜索 + 分类/年份/地区多维筛选
- **离线下载** — 多线程分片下载引擎，支持暂停/恢复/取消，下载进度实时展示
- **收藏 & 历史** — 视频收藏同步、观看历史自动记录，支持断点续看

### 画质增强

- **Anime4K 动画增强** — 移植自 Anime4K v4.0，支持 S/M/L 三档模型的 WebGL2 CNN 实时 2× 超分辨率，为动画/动漫视频提供高质量画面增强
- **FilmUpscaler 影视增强** — 基于 FSRCNNX 的全链路视频增强管线，包含去隔行、降噪、时间混合、HDR 色调映射、CAS 锐化
- **智能画质模式** — 根据 GPU 性能自动调整增强质量，帧率过低时自动降级

### 自动化引擎

- **采集调度器** — 后台定时自动采集，支持全量/增量模式、可配置循环间隔
- **豆瓣数据补全** — 自动爬取豆瓣评分、封面、简介等信息，丰富视频元数据
- **启动补采** — 应用启动时自动补采离线期间遗漏的数据

### 智能推荐

- **相似视频推荐** — 详情页展示同类型视频作为兜底推荐
- **推荐页面** — 基于用户观看历史和偏好生成个性化推荐列表

### 版本更新

- **自动检查更新** — 每天首次启动自动检查 GitHub 版本更新
- **多渠道版本获取** — 支持 GitHub API、GitHub Raw、jsdelivr CDN、Gitee 等多个版本信息源
- **更新日志展示** — 版本升级后自动展示更新内容，支持历史版本查看

### 设置与诊断

原先独立的后台管理面板（`/dev-admin`）已整体移除——它的每一项能力都已在侧栏页面中存在。运维能力改为集中在「设置」页的分组里：

| 分组 | 功能 |
|------|------|
| 基本设置 | 窗口尺寸、字体与语言、主题外观、关闭行为、数据入库策略；豆瓣补全轮询间隔；日志保留天数 |
| 主题外观 | 深浅色跟随、预设与自定义主题、主色与派生色 |
| 缓存管理 | 图片/接口缓存占用、一键清理 |
| 日志 | 实时跟随、级别切换与过滤、关键字搜索、历史文件分页查看、导出与复制 |
| 诊断 | 只读运行状态：版本与安装标记、运行时长、Go/Wails 运行时与平台、协程数/GC/堆内存、数据目录与可执行文件路径、后台任务计数；数据库与磁盘缓存体积、各表行数；采集调度状态与最近启停；豆瓣补全进度、反爬静默与 302 验证题求解结果；数据健康度（缺评分、缺条目 ID、冷却中、重复分组明细）；采集源逐个连通性探测 |
| 高级 | 豆瓣补全队列统计、手动触发一轮补全 |

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
| Pinia | 状态管理（12 个 store） |
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
│   │   ├── facade.go               # 85 个转发方法：只做参数校验与转调子包
│   │   ├── diagnostics.go          # 诊断页只读快照、采集源探测
│   │   ├── logs.go                 # 日志查询/导出/级别切换
│   │   ├── download.go             # 下载引擎（直连多线程 + m3u8）
│   │   └── direct_resume.go        # 断点续传清单
│   ├── apperror/                   # 统一错误码（前端 normalizeApiError 依赖它）
│   ├── applog/                     # 日志系统：环形缓冲 + 按天滚动 + 订阅
│   ├── cache/                      # 缓存占用统计与清理
│   ├── collect/                    # 采集引擎（fetcher → processor → strategy）
│   ├── collection/                 # 采集/豆瓣调度器
│   ├── db/                         # SQLite 数据层（source / video / douban / catalog / diagnostics）
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
├── frontend/
│   ├── bindings/                   # Wails 自动生成的 JS 绑定（勿手改）
│   ├── src/
│   │   ├── api/                    # 前端访问绑定/运行时/事件的唯一出口
│   │   ├── components/             # 公共组件（16 个）+ ui/ 基础组件（11 个）
│   │   ├── locales/                # zh-CN.ts / en.ts，键必须严格对齐
│   │   ├── platform/               # localStorage 等平台能力封装
│   │   ├── player/                 # 播放器控制逻辑
│   │   ├── stores/                 # Pinia（12 个 store）
│   │   ├── styles/                 # 主题变量与 cczj-* 工具类
│   │   ├── utils/                  # Anime4K / FSRCNNX 着色器与权重、推荐算法
│   │   └── views/                  # 12 个页面：Home / Search / Detail / Player /
│   │                               #   Sources / VideoTypes / Recent / History /
│   │                               #   Favorites / Downloads / Recommendations / Settings
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

1. 首页浏览推荐 或 搜索页搜索关键词
2. 点击进入详情页查看简介、选集
3. 选择集数进入播放器

### 下载视频

1. 在详情页点击「下载」按钮选择集数
2. 侧边栏「下载管理」查看进度
3. 下载完成后点击「打开」定位文件

### 启用画质增强

在播放器页面点击画质增强按钮，选择 Anime4K（动画）或 Film（影视）模式。

### 看日志与诊断

- 应用内：「设置 → 日志」实时跟随、按级别过滤、搜关键字、翻页看历史文件、导出/复制。
- 应用外：日志按天写在 `%APPDATA%\CCZJ Video\applog\cczj-YYYY-MM-DD.log`，保留天数在「设置 → 基本设置」里改。
- 「设置 → 诊断」是只读快照：运行时环境、内存与协程、数据库与各表行数、采集与豆瓣调度状态、
  反爬静默与 302 验证题求解结果、数据健康度（缺评分 / 缺条目 ID / 冷却中 / 同豆瓣 ID 重复分组）。
  唯一的主动动作是底部的采集源逐个连通性探测。

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
- Go 绑定调用统一在 store 内封装，组件不直接调用绑定

### 国际化

`frontend/src/locales/zh-CN.ts` 与 `en.ts` 的键必须一一对应，缺一项 `vue-tsc` 阶段就会失败。
组件内用 `t()`，模块级常量里用 `tr()`——模块作用域直接调用 `t()` 会在切换语言时冻结成旧文案。

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
