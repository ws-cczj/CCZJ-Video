# CCZJ Video

<p align="center">
  <strong>多源视频资源聚合桌面应用</strong><br>
  基于 Wails v3 + Vue 3 + Go 构建的 Windows 桌面客户端
</p>

<p align="center">
  <img src="https://img.shields.io/badge/Go-1.25+-00ADD8?style=flat-square&logo=go&logoColor=white" alt="Go">
  <img src="https://img.shields.io/badge/Vue-3.x-4FC08D?style=flat-square&logo=vue.js&logoColor=white" alt="Vue">
  <img src="https://img.shields.io/badge/Wails-v3-alpha.98-EB2F2F?style=flat-square&logo=data:image/svg+xml;base64,&logoColor=white" alt="Wails">
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

### 自动化引擎

- **采集调度器** — 后台定时自动采集，支持全量/增量模式、可配置循环间隔
- **豆瓣数据补全** — 自动爬取豆瓣评分、封面、简介等信息，丰富视频元数据
- **启动补采** — 应用启动时自动补采离线期间遗漏的数据

### 后台管理系统

完整的 10 页面管理面板（`/dev-admin`），覆盖应用全生命周期：

| 模块 | 功能 |
|------|------|
| 仪表盘 | 统计总览、各源数据量、调度器状态、快捷采集 |
| 采集源管理 | CRUD、采集控制（全量/增量/停止）、导出/清空 |
| 视频数据 | 列表浏览、搜索筛选、批量删除、详情查看 |
| 分类管理 | 按源筛选、树形层级展示 |
| 采集调度器 | 全局配置编辑、各源独立调度、触发/停止操作 |
| 下载管理 | 任务列表、进度条、暂停/恢复/取消/打开文件 |
| 豆瓣数据 | 调度器状态、数据列表、单条手动补全 |
| 数据导入导出 | 文件路径导入、按源导出（支持 .json.br / .json.gz / .json） |
| 系统日志 | 文件选择、搜索高亮、级别过滤、一键清空 |
| 系统设置 | 调试模式、关闭行为、窗口参数、缓存管理、开发者模式、重启 |

### 用户体验

- **深色/浅色主题** — 跟随系统或手动切换，全局 CSS 变量自适应
- **滚动位置记忆** — 页面切换后自动恢复滚动位置
- **组件缓存** — KeepAlive 缓存列表页面，避免重复加载
- **自定义标题栏** — 原生无边框窗口 + 自定义标题栏（最小化/最大化/关闭）
- **图片代理** — Go 后端代理远程图片请求，绕过 CORS 限制

---

## 🏗️ 技术架构

```
┌─────────────────────────────────────────────┐
│                  Wails v3                    │
│  ┌───────────────┐    ┌──────────────────┐  │
│  │   Frontend     │    │    Backend       │  │
│  │   (WebView2)   │◄──►│    (Go)          │  │
│  │               │    │                  │  │
│  │  Vue 3 + TS   │    │  SQLite (modernc)│  │
│  │  Pinia        │    │  Collect Engine  │  │
│  │  Vue Router   │    │  Douban Crawler  │  │
│  │  UnoCSS       │    │  Scheduler       │  │
│  │  HLS.js       │    │  Download Engine │  │
│  │  xgplayer     │    │  Image Proxy     │  │
│  └───────────────┘    └──────────────────┘  │
└─────────────────────────────────────────────┘
```

### 前端

| 技术 | 用途 |
|------|------|
| Vue 3 Composition API | UI 框架 |
| TypeScript | 类型安全 |
| Pinia | 状态管理（10 个 store） |
| Vue Router | Hash 路由 + 滚动位置恢复 |
| UnoCSS (preset-wind) | 原子化 CSS |
| xgplayer + hls.js | 视频播放（HLS 流） |
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
├── app.go                          # 主入口，所有 Go 绑定方法
├── app/
│   ├── applog/                     # 日志系统（按月滚动）
│   ├── collect/                    # 采集引擎（fetcher → processor → strategy）
│   ├── db/                         # SQLite 数据层（source / video / douban）
│   ├── douban/                     # 豆瓣爬虫 + 调度器
│   ├── handler/                    # 请求处理器（collect / scheduler / source / video）
│   ├── model/                      # 数据模型
│   └── util/                       # 工具（压缩 / 加密 / ID 生成）
├── build/                          # 构建配置
├── frontend/
│   ├── bindings/                   # Wails 自动生成的 JS 绑定
│   ├── src/
│   │   ├── App.vue                 # 根组件（布局 / KeepAlive / 全局事件）
│   │   ├── components/             # 公共组件（22 个）
│   │   │   ├── TitleBar.vue        # 自定义标题栏
│   │   │   ├── Sidebar.vue         # 侧边导航
│   │   │   ├── VideoPlayer.vue     # HLS 播放器
│   │   │   ├── Carousel.vue        # 轮播图
│   │   │   ├── VideoCard.vue       # 视频卡片
│   │   │   ├── Icon.vue            # SVG 图标系统
│   │   │   └── ui/                 # UI 基础组件（Button / Modal / Input / ...）
│   │   ├── router/                 # Vue Router 配置
│   │   ├── stores/                 # Pinia 状态管理（10 个 store）
│   │   └── views/                  # 页面视图
│   │       ├── Home.vue            # 首页（推荐 + 轮播）
│   │       ├── Search.vue          # 搜索
│   │       ├── Detail.vue          # 视频详情
│   │       ├── Player.vue          # 播放器
│   │       ├── Sources.vue         # 数据源管理（用户侧）
│   │       ├── Settings.vue        # 设置（用户侧）
│   │       ├── Downloads.vue       # 下载列表
│   │       ├── Favorites.vue       # 收藏
│   │       ├── History.vue         # 历史
│   │       └── admin/              # 后台管理系统（12 个文件）
│   │           ├── Admin.vue       # 父容器（侧边导航 + router-view）
│   │           ├── AdminDashboard.vue
│   │           ├── AdminSources.vue
│   │           ├── AdminVideos.vue
│   │           ├── AdminCategories.vue
│   │           ├── AdminScheduler.vue
│   │           ├── AdminDownloads.vue
│   │           ├── AdminDouban.vue
│   │           ├── AdminDataOps.vue
│   │           ├── AdminLogs.vue
│   │           ├── AdminSettings.vue
│   │           └── composables/
│   │               └── useAdminData.ts  # 后台共享数据
│   └── package.json
├── go.mod
├── Taskfile.yml
└── wails.json
```

---

## 🚀 快速开始

### 环境要求

- **Go** 1.25+
- **Node.js** 18+
- **Wails v3 CLI** (`go install github.com/wailsapp/wails/v3@latest`)
- **Windows 10/11**（WebView2 运行时）

### 安装依赖

```bash
# Go 依赖
go mod tidy

# 前端依赖
cd frontend
npm install
```

### 开发模式

```bash
# 启动开发服务器（热重载）
wails dev
```

前端修改会即时热重载，Go 代码修改后自动重新编译。

### 构建生产版本

```bash
# 构建可执行文件
wails build

# 或仅构建前端
cd frontend
npm run build
```

构建产物位于 `bin/` 目录。

---

## 📖 使用说明

### 添加数据源

1. 点击侧边栏「资源管理」
2. 点击「添加源」，输入源名称、唯一 Key、API 地址
3. 保存后返回首页，数据将自动加载

### 采集数据

- **手动采集**：后台管理 → 采集源 → 点击「全量」或「增量」
- **自动采集**：后台管理 → 采集调度器 → 启用后台采集并配置间隔

### 播放视频

1. 首页浏览推荐 或 搜索页搜索关键词
2. 点击进入详情页查看简介、选集
3. 选择集数进入播放器

### 下载视频

1. 在详情页点击「下载」按钮选择集数
2. 侧边栏「下载管理」查看进度
3. 下载完成后点击「打开」定位文件

---

## ⌨️ 开发指南

### 添加新的 Go 绑定方法

在 `app.go` 中为 `App` 结构体添加方法，Wails 会自动生成前端 JS 绑定到 `frontend/bindings/`。

### 前端 Store 规范

- 使用 Pinia Composition API 风格（`defineStore('name', () => { ... })`）
- 导出函数和 ref，组件通过 `const store = useXxxStore()` 使用
- Go 绑定调用统一在 store 内封装，组件不直接调用绑定

### 共享 CSS 变量

全局 CSS 变量定义在 `App.vue` 中，支持深色/浅色主题自动切换：

```css
--bg-app, --bg-secondary, --bg-card, --bg-hover
--text-primary, --text-secondary, --text-muted
--accent, --accent-alpha-10, --accent-alpha-20, --accent-alpha-35
--border, --border-strong
--danger, --warning, --success, --info
```

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
| `unocss` | 原子化 CSS 引擎 |
| `vite` | 前端构建工具 |

---

## 📄 开源协议

本项目仅供个人学习研究使用。

---

## 🙏 致谢

- [Wails](https://wails.io/) — 优秀的 Go + Web 桌面应用框架
- [Vue.js](https://vuejs.org/) — 渐进式 JavaScript 框架
- [xgplayer](https://github.com/bytedance/xgplayer) — 西瓜播放器
- [hls.js](https://github.com/video-dev/hls.js) — HLS 流媒体播放
