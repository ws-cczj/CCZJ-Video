# CCZJ Video 架构健壮性与可扩展性优化计划

> 状态：执行中  
> 审计日期：2026-07-14  
> 适用范围：Go/Wails 后端、Vue 前端、SQLite 数据层、构建与交付流程

## 1. 文档目的

本计划用于指导 CCZJ Video 在不改变现有产品语义、Wails 前端绑定名称和用户数据兼容性的前提下，继续提升以下能力：

- 故障可控：启动、迁移、采集、下载和退出过程能够失败可见、恢复可预测。
- 变更可验证：关键业务具备自动化测试和持续集成门禁。
- 边界清晰：UI、应用服务、领域逻辑、基础设施不再互相持有隐式全局状态。
- 易于扩展：新增数据源、下载策略、媒体能力和设置项时，不需要修改多个超大文件。
- 易于维护：大文件拆成围绕业务能力组织的模块，公共契约集中管理。

本计划推荐继续采用模块化单体架构，不引入微服务，不更换 Wails、Vue 或 SQLite。

## 1.1 方案比较与结论

| 方案 | 优点 | 主要问题 | 结论 |
|---|---|---|---|
| A. 仅修补高风险问题 | 见效快、变更少 | 大文件、全局状态和无测试问题会继续累积 | 只适合作为第一个迭代 |
| B. 渐进式模块化单体 | 保持技术栈和兼容性，可按阶段交付 | 需要先建设测试与迁移基础，短期功能产出会放慢 | 推荐 |
| C. 全量重写 | 可一次设计理想边界 | 难以验证行为兼容，数据库和播放器回归风险最高 | 不采用 |

推荐选择 B，并将 A 中的数据安全修补作为 B 的阶段 0–1。这样既能优先关闭真实风险，又不会把后续工作限制在持续打补丁。

## 2. 审计基线

当前项目约包含：

- Go：39 个文件，约 15,078 行。
- Vue/TypeScript：77 个文件，约 33,291 行。
- 自动化测试：0 个 Go/TypeScript 测试文件。
- 后端主要超大文件：`app.go` 约 3,080 行，`app/db/video.go` 约 1,356 行，`app/db/douban.go` 约 1,200 行，`app/updater/updater.go` 约 980 行。
- 前端主要超大文件：`VideoPlayer.vue` 约 4,263 行，`Settings.vue` 约 2,842 行，`Player.vue` 约 1,845 行，`tsCache.ts` 约 1,773 行，`Search.vue` 约 1,733 行。
- 仓库中有 42 个已跟踪的 `bin`、运行日志、数据库、WAL/SHM 或生成绑定相关文件。

现有重构已经建立了设置、窗口、更新、缓存、媒体、采集调度和生命周期服务，这是继续演进的良好基础。下一阶段的重点不再是简单移动代码，而是建立契约、测试和可恢复的数据边界。

## 3. 主要问题与优先级

### 3.1 P0：数据与安全边界

#### 3.1.1 动态表名缺少统一验证

项目按照数据源创建 `v_{source_key}` 和 `e_{source_key}` 动态表。部分 SQL 使用 `safeIdent`，部分路径直接拼接 `VideoTableName(sourceKey)`。普通新增来源会派生安全 key，但导入来源只检查 key 非空，因此导入文件可能绕过命名约束。

影响：非法标识符、表名碰撞、导入失败；在最坏情况下形成 SQL 注入或破坏其他来源表的风险。

建议：

- 定义唯一的 `SourceKey` 值对象或验证函数，只允许 `^[a-z0-9][a-z0-9_]{0,63}$`。
- 新增、更新、导入、重建、删除、采集前全部调用同一验证逻辑。
- 数据库层禁止接收未验证字符串作为标识符；统一使用一个返回 `(string, error)` 的标识符构造函数。
- 为非法 key、规范化碰撞、旧数据兼容增加测试和迁移检查。

#### 3.1.2 数据库迁移不可原子恢复

当前迁移由启动代码直接执行，多处忽略 `ALTER/DROP/UPDATE` 错误，重建 `global_video` 的步骤没有放入单一事务，也没有显式 schema 版本和迁移日志。

影响：应用在迁移中断电或某一步失败时，可能留下半迁移状态；后续启动无法判断已经执行到哪一步。

建议：

- 引入 `schema_migrations(version, applied_at, checksum)`。
- 每个迁移使用独立版本文件和事务；SQLite 不支持事务内完成的操作需采用临时表、校验、原子重命名方案。
- 破坏性迁移前创建经过校验的数据库备份，并保留最近 2 份。
- 迁移完成后执行表结构、行数和关键外键校验。
- 启动失败时向 UI 返回可诊断错误，不直接 `panic` 后丢失上下文。

#### 3.1.3 本地高权限能力缺少输入边界

Wails 暴露了打开目录、定位文件、图片代理、数据源导入和更新安装等本地或网络能力。图片代理当前可请求任意 HTTP(S) URL，导入内容缺少显式大小限制。

建议：

- 图片代理拒绝环回、链路本地和私网地址，限制响应类型和最大字节数。
- 导入文件、Base64、压缩解包设置上限并防止压缩炸弹。
- 打开文件/目录仅允许用户数据目录、下载目录和显式选择的路径。
- 更新包增加 SHA-256 校验；若发布渠道可支持，再引入签名校验。

### 3.2 P1：缺少安全网

项目已有约 4.8 万行业务代码，但没有自动化测试和 CI 门禁。现有 `go test` 实际只做编译检查，无法验证调度、迁移、收藏聚合、下载恢复和播放器状态。

建议建立三层测试：

1. 纯逻辑单元测试：URL 模板、来源 key、剧集解析、推荐去重、事件映射、下载状态转换。
2. SQLite 集成测试：每个测试使用临时数据库，覆盖迁移、来源 CRUD、收藏/历史 global_id 语义、导入回滚。
3. 关键流程测试：采集取消、调度停止、下载暂停/恢复、前端 store 的事件订阅与清理。

首批门禁：

- Go：`gofmt`、`go vet ./...`、`go test ./...`。
- 并发包：`go test -race` 覆盖 lifecycle、collection、download、douban。
- 前端：`npm ci`、`vue-tsc --noEmit`、生产构建、Vitest。
- 后续加入 ESLint、格式检查和 Playwright 最小冒烟测试。

不以全仓覆盖率数字作为第一目标；先要求所有 P0/P1 行为拥有回归测试，再逐步提高核心包覆盖率。

### 3.3 P1：后端所有权仍不完整

#### 3.3.1 `app.go` 仍承担下载引擎和基础设施细节

`app.go` 已有服务组合根，但仍包含约 1,500 行下载协议、持久化和状态逻辑，以及来源导入导出、代理请求和文件系统操作。

建议目标：

- `app.go` 只保留 Wails DTO、兼容方法和服务委托，目标控制在 600 行以内。
- `app/download` 拥有 `Service`、`Task`、`Store`、`DirectTransport`、`HLS/PlaylistTransport`、持久化和事件发布。
- `app/source` 拥有来源导入、导出、管理操作和安全校验。
- `app/proxy` 拥有受限 HTTP 代理。
- `app/files` 封装文件选择、目录打开和路径授权。

#### 3.3.2 包级全局状态阻碍测试与多实例

数据库实例、调度器、采集引擎表、豆瓣缓存和更新配置仍有包级可变状态。它们使测试难以隔离，也让生命周期所有权不明确。

建议：

- 数据库改为 `Store`/Repository 实例，由组合根注入。
- 调度器和采集注册表由服务实例持有，不再通过 `GetScheduler` 全局单例获取。
- HTTP Client、Clock、Filesystem、EventSink 只在真正的外部边界定义小接口。
- 正则表达式、常量配置等不可变全局值可以保留。

#### 3.3.3 生命周期组还需要状态机保护

当前 `lifecycle.Group` 能 cancel 和 wait，但没有防止 Stop 后继续 Add，也没有明确处理重复 Stop；部分采集和代理请求仍使用 `context.Background()` 或裸 goroutine。

建议：

- `Group.Go` 返回错误，Stop 后拒绝新任务。
- Stop 使用 `sync.Once`，并记录超时未退出任务名称。
- 所有长任务从 Wails 生命周期根 context 派生；取消以 `context.Canceled` 结束，不记为业务错误。
- 禁止使用 `time.Sleep` 等待状态同步，改为 done channel、condition 或明确状态转换。

### 3.4 P1：前端模块边界过大

`VideoPlayer.vue` 同时负责媒体装载、HLS、缓存、画质增强、快捷键、全屏、OSD、进度保存、缩略图和大量 UI；`Settings.vue`、`Player.vue`、`Search.vue`、`Sources.vue` 也同时承担页面、业务和持久化。

风险：重复事件监听、定时器泄漏、状态互相覆盖、修改一个功能引发播放器其他能力回归。

建议按能力拆分：

- `player/core/useMediaSession.ts`：播放源、生命周期、基础事件。
- `player/hls/useHlsEngine.ts`：HLS 创建、恢复、错误策略。
- `player/cache/useMediaCache.ts`：TS 缓存和预取。
- `player/enhance/useVideoEnhancer.ts`：Anime4K/FSRCNNX/WebGL 生命周期。
- `player/input/usePlayerShortcuts.ts`：键盘、鼠标、手势。
- `player/progress/usePlaybackProgress.ts`：进度读写和节流。
- `player/ui/`：控制条、OSD、统计面板、设置面板。

每个 composable 必须返回 `dispose()` 或在自己的 `onScopeDispose` 中清理事件、定时器、AbortController 和 WebGL 资源。禁止在重复初始化函数中注册不可移除的匿名监听器。

页面拆分采用“页面容器 + feature composables + 小组件”，单个手写 Vue/TS 文件建议控制在 800 行内；WebGL shader/模型权重和生成文件例外。

### 3.5 P1：跨端契约与持久化存在双轨

前端下载 store 通过动态 `window.go.main.App` 和 `any` 调用后端，同时又包含生成 bindings；多个功能同时把设置写入 SQLite 和 localStorage，并通过兼容字段名读取不同结构。

影响：类型检查无法发现绑定变化；SQLite 与 localStorage 可能互相覆盖；错误依赖字符串前缀，例如 `duplicate_url:`。

建议：

- 建立 `frontend/src/api/`，只在这里包装 Wails 生成 bindings 和事件。
- 页面与 store 禁止直接访问 `window.go` 或 Wails `Events`。
- 定义稳定的 DTO 和结构化错误码，如 `DOWNLOAD_DUPLICATE`，不再解析错误文本。
- 明确每类数据唯一来源：业务数据和跨会话设置以 SQLite 为准；仅 UI 临时偏好可使用 localStorage；大媒体缓存使用 IndexedDB。
- 所有 localStorage 数据带版本号和迁移函数。

### 3.6 P2：仓库与交付卫生

仓库仍跟踪二进制、数据库、运行日志和 WAL/SHM 文件。虽然 `.gitignore` 已配置忽略，已跟踪文件不会自动退出版本控制。

建议：

- 在确认发布资产已有独立存储后，从 Git 索引移除 `bin/data`、日志、SQLite 运行文件和构建产物。
- 发布包由 CI 构建并上传，不提交可执行文件到源码分支。
- 自动生成 bindings 明确采用一种策略：要么由 CI 生成并验证无差异，要么提交且在 PR 中检查同步；不要两种方式混用。
- 删除 Vite timestamp 临时文件并加入忽略规则。

### 3.7 P2：可观测性与错误模型

当前错误处理同时存在直接忽略、日志后继续、字符串判断、panic 和返回 error。日志缺少统一任务 ID 和来源 ID。

建议：

- 定义错误分类：Validation、NotFound、Conflict、Transient、Canceled、Internal。
- Wails 层将内部错误转换成稳定错误码和用户消息，内部日志保留原始 cause。
- 采集、下载、更新使用 `operation_id/task_id/source_key` 结构化字段。
- 增加启动诊断：应用版本、schema 版本、数据库路径、迁移结果、后台任务数。
- 对重复失败和超时增加有界重试、指数退避和随机抖动。

## 4. 推荐目标架构

```text
main.go
  └─ bootstrap / composition root
      ├─ Wails App facade（只做 DTO 转换与委托）
      ├─ application services
      │   ├─ media
      │   ├─ source
      │   ├─ collection
      │   ├─ download
      │   ├─ update
      │   └─ settings/window/cache
      ├─ domain/core
      │   ├─ source key 与规则
      │   ├─ media/global_id 语义
      │   ├─ task state machine
      │   └─ typed errors
      └─ infrastructure
          ├─ sqlite repositories + versioned migrations
          ├─ HTTP clients
          ├─ filesystem
          ├─ Wails event adapter
          └─ lifecycle/task group

frontend/src
  ├─ api/              Wails bindings 与事件适配
  ├─ features/         player、download、collection、source 等能力
  ├─ stores/           页面共享状态，不直接访问 Wails
  ├─ components/ui/    纯 UI 组件
  ├─ views/            路由容器
  └─ platform/         storage、WebGL、IndexedDB、日志适配
```

依赖方向必须保持单向：UI/Wails → application service → domain → repository interface；基础设施实现接口，但领域层不反向依赖 Wails、SQLite 或 Vue。

## 5. 分阶段实施计划

### 阶段 0：冻结基线与保护用户数据

目标：在继续重构前建立可回滚基线。

任务：

- 记录当前 Wails 方法、事件名、SQLite schema 和关键用户流程。
- 为测试准备脱敏的小型数据库 fixture 和来源响应 fixture。
- 实现数据库启动备份和迁移失败恢复策略。
- 建立架构决策记录 `docs/adr/`，记录动态来源表、global_id、设置存储和下载恢复语义。

验收：现有数据库可完成备份、启动、关闭和恢复演练；关键契约形成机器可读清单。

### 阶段 1：P0 修复与 CI 安全网

目标：先消除可造成数据损坏或边界突破的问题。

任务：

- 统一 SourceKey 验证及 SQL 标识符构造。
- 为导入大小、代理 URL、文件路径和更新包增加限制与校验。
- 引入版本化、幂等、可恢复的数据库迁移器。
- 新增 CI，执行 Go、TypeScript、前端生产构建和首批测试。
- 建立测试工具：临时数据库、假 HTTP Server、fake clock、事件收集器。

验收：所有 P0 路径有自动化测试；迁移中断不会破坏原数据库；非法 source_key 无法进入数据库层。

### 阶段 2：后端实例化与错误契约

目标：消除影响隔离测试和生命周期的全局可变状态。

任务：

- 将 `db` 全局实例迁为 `Store`，按 media/source/settings/repository 拆小接口。
- 将 handler 全局调度器和采集引擎表迁入服务实例。
- 完善 lifecycle Group 状态机并贯穿 context。
- 建立 typed errors 和 Wails 错误 DTO。

验收：核心服务可在测试中创建多个互不影响的实例；Stop 后不能启动新任务；取消路径无 goroutine 泄漏。

### 阶段 3：完成后端业务模块化

目标：将 `app.go` 收敛为稳定门面。

任务：

- 完整迁移下载服务、来源导入导出、代理和文件系统能力。
- 拆分 `db/video.go`、`db/douban.go` 和 updater 大文件。
- 为下载状态机、断点续传、m3u8 合并、采集取消和导入事务建立集成测试。
- 保持现有 Wails 方法签名，通过别名/DTO 兼容旧前端。

验收：`app.go` 不超过约 600 行且无协议实现；业务包不直接持有 Wails App；关键服务测试通过 race detector。

### 阶段 4：前端契约层与单一数据源

目标：让 UI 不感知绑定细节和持久化兼容逻辑。

任务：

- 建立 typed API/event adapter，替换动态 `safeCall` 和直接 `Events.On`。
- 定义 SQLite、localStorage、IndexedDB 的所有权清单。
- 为设置、下载、收藏文件夹、观看进度增加版本化迁移。
- 为 store 增加单元测试，验证订阅只注册一次且 cleanup 可重复调用。

验收：views、components、stores 不直接引用 `window.go`；跨端 DTO 变化可由 TypeScript 编译发现；数据不再双向覆盖。

### 阶段 5：播放器与大页面拆分

目标：降低高频修改区域的回归面。

任务：

- 按媒体、HLS、缓存、增强、输入、进度和 UI 拆分播放器。
- 拆分 Settings、Player、Search、Sources 和 Detail。
- 建立 disposer/AbortController 统一清理模式。
- 为播放器状态机和缓存调度写 Vitest；用 Playwright 覆盖装载、切集、暂停、错误恢复和卸载。

验收：主要手写文件低于约 800 行；重复挂载不会增加监听器或定时器；WebGL context 和 HLS 实例可稳定释放。

### 阶段 6：仓库、发布与可观测性收尾

目标：形成长期可持续的开发与发布流程。

任务：

- 清理已跟踪的日志、数据库、WAL/SHM、构建二进制和临时文件。
- CI 生成可复现发布包，注入版本并输出校验和。
- 增加结构化操作日志、启动诊断和失败摘要。
- 更新 `PROJECT_CONVENTIONS.md`，修正过时架构和绑定示例。
- 建立依赖升级节奏，尤其关注 Wails v3 alpha 版本变更。

验收：全新检出可用一条命令完成测试和构建；源码分支不包含运行时用户数据；发布资产可追溯到提交和校验和。

## 6. 实施顺序与依赖

推荐严格按以下依赖推进：

```text
数据备份/契约清单
  → SourceKey 与迁移安全
  → 测试工具和 CI
  → 后端实例化/context/error contract
  → 后端大模块迁移
  → 前端 API 与存储契约
  → 播放器/页面拆分
  → 发布与仓库清理
```

前端大文件拆分不应先于 API 和测试安全网；数据库大改不应先于备份、版本化迁移和 fixture。

## 7. 里程碑门禁

每个阶段至少满足：

- `gofmt`、`go vet ./...`、`go test ./...` 通过。
- 涉及并发的包通过有针对性的 `go test -race`。
- `vue-tsc --noEmit` 和前端生产构建通过。
- 新增/修改的关键行为有对应测试。
- Wails 公共方法、事件名和 JSON 字段变化均有兼容说明。
- 数据库变更包含升级、重复执行、失败恢复和旧版本样本验证。
- 没有新增未登记的全局可变状态、裸 goroutine 或无法清理的监听器。

最终完成标准：

- 所有 P0/P1 风险关闭或有明确接受记录。
- `app.go` 成为纯门面；后端服务可独立测试。
- 前端高风险大组件完成能力拆分。
- 核心业务具备自动化回归和 CI 门禁。
- 用户数据库、设置、收藏、历史、下载记录保持兼容。

## 8. 明确不做的事项

- 不拆成微服务。
- 不更换 SQLite。
- 不一次性重写播放器或下载器。
- 不为了覆盖率数字给简单 getter/setter 堆测试。
- 不同时改变架构、数据库语义和 UI 交互。
- 不手工修改 Wails 生成 bindings。

## 9. 主要风险与缓解

| 风险 | 缓解措施 |
|---|---|
| 数据迁移破坏用户库 | 真实旧库样本、自动备份、事务迁移、失败恢复演练 |
| 大文件拆分引发行为漂移 | 先锁定契约和 fixture，再做小步委托迁移 |
| context 改造导致任务提前取消 | 明确应用、服务、单任务三层生命周期，增加取消测试 |
| 前端存储统一丢失旧偏好 | 每个 key 有版本化迁移和一次性兼容读取 |
| Wails alpha 升级造成绑定变化 | 固定版本、依赖升级 PR、生成绑定差异检查 |
| 重构周期过长影响功能开发 | 每阶段独立可合并；新功能必须落到目标模块而非旧大文件 |

## 10. 建议的首批工作包

首个迭代建议只包含以下内容：

1. 建立 `SourceKey` 验证与全路径测试。
2. 引入 `schema_migrations` 和数据库备份/恢复测试框架。
3. 建立 CI，锁定 Go 编译、vet、前端类型检查和生产构建。
4. 为 global_id 收藏/历史语义、生命周期 Stop、下载状态转换补首批测试。
5. 建立前后端契约清单和 ADR，不立即拆播放器。

完成这五项后，再进入后端实例化和大文件拆分。这样可以先把最危险的“数据损坏、无回归保护、隐式契约”问题变成可验证边界。

## 11. 执行记录

- 2026-07-14：新增 SourceKey 验证、动态标识符防御和回归测试。
- 2026-07-14：新增导入大小限制、图片代理私网拦截与响应上限。
- 2026-07-14：新增 `schema_migrations`、迁移校验、迁移前 SQLite 快照和迁移测试。
- 2026-07-14：增强应用任务组的停止状态机和生命周期测试。
- 2026-07-14：新增 GitHub Actions 验证工作流与本地 `scripts/verify.ps1` 门禁脚本；全仓 gofmt 将作为独立清理提交后再设为硬门禁。
- 2026-07-14：下载 store 改为直接使用 Wails 生成 bindings；图片代理开始迁入 `app/proxy` 服务。
- 2026-07-14：下载目录状态迁入 `app/download`；更新检查延迟和手动采集均接入可取消的应用任务组。
- 2026-07-14：新增受限本地文件访问策略和前端 Wails 事件订阅适配层；补充运行产物忽略规则。
- 2026-07-14：将本地验证脚本接入 Taskfile 的 `verify` 任务；运行时产物已从 Git 索引移除。
- 2026-07-15：完成前端 API/runtime 适配层迁移，页面与 Store 不再直接引用生成 bindings；新增结构化 API 错误模型与后端事件 payload 契约。
- 2026-07-15：新增版本化浏览器存储适配器，播放器设置、快捷键和观看进度统一经过适配层，并保留旧 key/旧 JSON 的兼容读取。
- 2026-07-15：采集事件与日志补充 `operation_id`、`source_key`、`mode`、`error` 关联字段；生命周期增加后台任务诊断快照，启动事件记录 schema 版本。
- 2026-07-15：验证脚本增加前端边界检查与 Vite 临时产物检查；Go 测试、vet、TypeScript 检查和生产构建全部通过。Windows 当前环境的 `go test -race` 因 race runtime 返回 `0xc0000139`，待具备 MinGW/CGO race 运行时的 CI 环境执行。
