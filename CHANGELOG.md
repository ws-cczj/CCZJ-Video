# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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

## [1.1.1] - 2026-06-27

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

- 更新版本号至 1.1.1

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

[1.1.1]: https://github.com/ws-cczj/CCZJ-Video/compare/v1.1.0...v1.1.1
[1.1.0]: https://github.com/ws-cczj/CCZJ-Video/releases/tag/v1.1.0
