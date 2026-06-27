# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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
