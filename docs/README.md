# 文档索引

开发规范统一在 [AGENTS.md](../AGENTS.md)。先读当前主题文档，历史方案不作为当前功能或约束的依据。

## 当前维护入口

| 主题 | 文档 |
| --- | --- |
| 目录职责、复用入口与开发检查 | [开发与复用指南](development.md) |
| 公共 UI、主题、工具类与 CSS 组合 | [CCZJ 样式复用](cczj-styles.md) · [生成速查表](cczj-utilities-cheatsheet.md) |
| Android 构建、热更新与原生验收 | [Android 开发](android.md) |
| 请求合并、写入策略、取消与事件身份 | [请求通道](request-pipelines.md) |
| 扩展包格式、信任与能力 | [扩展包规范](plugins.md) · [采集策略 Schema](source-strategy-v2.schema.json) |
| 阅读源排名与章节／话次匹配 | [策略设计](reading-source-routing-design.md) · [实现入口](reading-source-routing-implementation.md) |
| 阅读源质量检查与新源复测 | [质量基准](reading-source-quality-benchmark.md) |
| 构建与 GitHub 发布 | [发布与自动更新](releases.md) |
| 功能与接口全景图 | [生成与操作](development.md#开发与检查)，输出同一可缩放画布，公开绑定全部归入功能链 |
| 全项目待核查范围 | [整改任务书](project-remediation.md)，其中待办需对照当前实现确认 |
| 领域用语与架构选择 | [术语](../CONTEXT.md) · [ADR 目录](adr/) |

## 验收与调研

- [移动设置检查](../reports/mobile-settings-2026-10-22/README.md)：实际文件选择、分享、更新入口与窄屏交互。
- [2.4.0 发布验收](../reports/release-2.4.0/README.md)：产物身份、更新交接和验证限制。
- [阅读性能验收](../reports/reading-performance-2026-10-22/README.md)：模拟器搜索、阅读与连续翻页。
- [阅读源批量检查](../reports/reading-quality-bulk-run-2026-10-22.md)：数据与检测范围。
- [全部报告](../reports/)：历史观测与原始交接资料，报告中的路径、数量和成功率适用于记录时的环境。

## 合并归档

32 份旧文档已按主题合并为以下 6 份，原文件名保留为章节，原有引用更新到相应位置。保留原始判断和待办，不声称它们已全部解决。

| 主题 | 历史记录 |
| --- | --- |
| Android、移动布局与导航 | [移动历史](../reports/history/mobile.md) |
| 播放、内存、资源与诊断 | [播放历史](../reports/history/playback.md) |
| 设置与扩展包排查 | [设置与扩展历史](../reports/history/settings-plugins.md) |
| 小说与漫画早期方案 | [阅读历史](../reports/history/reading.md) |
| 旧实现说明、讨论与更新排查 | [实现与讨论历史](../reports/history/architecture.md) |
| 早期移动、小说、漫画视觉参考 | [设计历史](../reports/history/design.md) |

后续优先更新上面的主题文档；一次性运行结果放 `dist/audit`，有长期价值的验收说明归入已有报告目录。
