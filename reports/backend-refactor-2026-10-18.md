# Go 后端基础架构重构记录

日期：2026-10-18。工作基于原工作区的未提交改动，与 Qorder 的小说、漫画开发并行进行；没有提交 Git、打包发布、启动桌面应用或操作用户数据库。

## 调研依据与取舍

参考 Prometheus v3.5.0 的消费侧小接口和请求 context、Grafana v12.0.0 的新旧错误共存与公开响应映射、etcd v3.6.0 的领域错误映射和初始化失败资源回收。固定版本源码、原理与适用范围见 [研究记录](go-backend-reference-study-2026-10-18.md)。

本轮保留 Go + Wails v3 + Vue 的单体结构。先修复职责、协议和生命周期，不引入额外 DI 框架、微服务或通用 Repository。前端工具链升级仍需要独立评估兼容组合，本轮没有修改依赖版本。

## 已落实的改动

### 错误从文本协议转为结构化出口

- `application.Options.MarshalError` 接入 `apperror.Marshal`，输出 `code/message`，需要时附带 `retryAfterSeconds`。
- 内部继续允许普通 Go error、sentinel、自定义领域错误。保留 `errors.Is/As` 的原因链；可通过 `ErrorCode()` 或 `PublicError()` 接入公共映射。
- 前端优先读取 Wails 的结构化 cause，旧前缀解析仅作兼容。公开消息不自动拼接底层路径、SQL 和原始响应。
- Wrap(nil) 返回 nil；包装保留具体错误码；typed nil 不再使 CodeOf 崩溃。
- 区分 SQL 缺记录和存储故障，修正源读取、阅读资源和视频身份查询的错误归类。收藏身份查询不再把查询故障静默当作 false。
- provider 的 429 输出限流与等待信息，401 和 403 分别表达鉴权与访问拒绝。聚合搜索的错误消息不再重复嵌入错误码。

### 收敛模块职责与原生能力

- 删除 media.Service 中 15 个没有独立行为的透传方法，绑定适配直接调用实际 handler 或 DB 操作。保留推荐默认值、身份协调、收藏和观看历史逻辑。
- 视频身份查询由装配处显式注入，测试能替换缺记录、校验失败和存储故障。
- 新增独立资源中间件承接 HLS、阅读图片及 Android 媒体端点，移除 3 个误暴露为 Wails RPC 的 HTTP 方法。现有 URL 和资源回退行为有回归测试。
- 生成绑定保持 `.js`，使用 `wails3 generate bindings -clean=false`；末次生成 179 个绑定方法，没有原先的 ResponseWriter/函数/通道序列化警告。保留其他开发者新增的阅读、投屏、诊断等方法。

### 数据库与请求生命周期

- 新增 `db.Open(dir)`，初始化、建表和迁移使用显式连接；失败关闭部分资源，成功才向默认入口发布实例。
- 包级 InitDB 作为兼容入口，替换 sync.Once：连续失败不会返回假成功，修复目录后可以重试；关闭后可重新打开，活动实例不会静默切到其他目录。
- 测试覆盖独立连接不替换默认连接、初始化连续失败、恢复和重开，以及迁移失败后的 Windows 文件句柄释放。
- 阅读网络绑定、视频详情与线路测速使用 Wails 请求 context，并通过 `lifecycle.WithShutdown` 连接应用退出。完成后解除关闭 hook。
- 阅读 API 的 DTO 归一化保留 CancellablePromise 的取消链；共享 AsyncResource 在句柄支持时取消旧请求，并继续以 generation 拒绝旧结果。实际 HTTP 请求取消已用本地源站测试验证。

取消能力的边界：普通 Promise 没有 cancel 属性；业务仓库若再用 async 包装接口，会丢掉该句柄。共享层不再宣称“框架无法取消”，但本轮没有把 Qorder 正在编辑的每一个读者页面都改成 AbortSignal 驱动，不能据此宣称所有页面都已停止后台请求。

数据库的边界：大量既有查询仍使用包级默认连接，尚未全部改成实例方法。Open 提供独立装配入口，兼容查询可以逐步迁移；不是一次改名就宣称已消除全部全局状态。

### 一份有效规范

- 根目录 [AGENTS.md](../AGENTS.md) 是统一开发规范。
- 删除 `docs/adr/0005-architecture-boundaries.md`、`docs/adr/0009-coded-errors.md` 和本地重复技能索引 `.agents/SKILL-INDEX.md`。
- 其余 ADR 标为历史设计记录，待决事项和术语表标明用途；修复 README、实现说明和代码注释中的旧规则引用。历史数据决策、插件外部格式与调查证据保留。
- 删除“全应用只有一种错误类型”“乱序无法测试”“绑定无法取消”等错误断言；Wails 技能改为本项目实际 v3 工具参考。
- 移除文件行数、根目录文件名、下载所在文件名和 scoped style 行数硬闸。未引用 locale 键降为清理提示，缺失引用、语言键位不一致与不存在的工具类仍报错。
- 绑定依赖和浏览器存储检查改用 TypeScript/Vue 解析器，避免把漫画领域文件注释里的绑定路径误判成非法 import。检查器有 3 项独立回归测试，并接入 verify.ps1。

## 验证结果

| 检查 | 结果 |
|---|---|
| `go vet ./...` | 通过 |
| `go test -count=1 ./...` | 通过，30 个 app 包及其他可编译入口 |
| 后续受影响的 service / apperror / plugin 定向测试 | 通过 |
| `npm run test -- --reporter=dot` | 28 个文件、144 项测试通过 |
| `npm run build`（frontend 目录） | 通过，包含 vue-tsc 和 Vite 生产构建 |
| AST 架构检查及检查器 3 项测试 | 通过 |
| 相关源码、文档、生成绑定的 diff 空白检查 | 通过 |
| 完整 `scripts/verify.ps1` | 通过，退出码 0；包含版本信息、AST 检查、前端约定与测试、生产构建、Go vet 与全量测试 |
| Windows `go test -race`（apperror/media/lifecycle/provider） | 未通过：测试进程以 `0xc0000139` 退出，未取得 race 检测结果；相关包已加入现有 Linux CI race 清单，未声称 CI 已运行 |

原先的三个前端失败没有靠修改产品行为来消除：诊断测试补齐了运行时环境替身，并校验新增的架构上下文字段；缩略图测试在移动过程中检查监听器最多一个，再等待当前请求和排队请求都超时后检查归零。生产超时时间未改。

Qorder 在最终验证前补齐了阅读文案；最终中英各 1792 个键，所有字面量引用有效，38 个未引用键作为清理提示。完整输出保存为 [verify.log](backend-refactor-2026-10-18/verify.log)。

另一次从仓库根目录直接调用旧版 vue-tsc 的检查混入了用户目录中的 Vue 3.4 类型，与项目 Vue 3.5 冲突；在 frontend 目录运行正式构建通过。因此已撤回针对该误报尝试的三个组件修改，未用 unknown 或强制转换掩盖环境问题。

## 与并行开发的交接

保留 `sources.kind`、v11 迁移、视频源过滤、共享 SafeDialContext、splitBlob/缓存子目录、CategoryReading、builtinPanels 的 PanelModule 修复以及新增绑定方法。未抢改小说、漫画页面、分类、书架 store 或 locale 文案；需要交叉的改动集中于错误映射、请求 context、API Promise 归一化与共享异步资源。

书架/阅读进度仍存在 localStorage 持久化实现。统一规范记录 SQLite 迁移方向及旧值导入要求，本轮没有变更它们的数据格式；没有假称已经解决清理 WebView 后业务数据丢失的问题。

依赖维护、剩余全局查询迁移、逐页面取消贯通及书架持久化属于仍有明确边界的后续工作，不作为新的“禁止修改”清单。
