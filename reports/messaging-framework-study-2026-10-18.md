# 消息框架与本地任务系统调研（2026-10-18）

本报告只做技术研究，不新增开发规范，不修改产品代码或依赖。用户已明确项目继续作为本地单机应用，不需要服务器形式；因此不规划远程 worker、服务端接口或消息代理扩展层。结论依据实际打开的官方文档与固定版本源码，不以项目热度、框架数量或未经测试的性能数字做决策。

## 推荐决策

**当前不引入 RabbitMQ、NATS JetStream、Redis/Asynq 或 Temporal。需要升级的是本地任务的持久化、状态机、资源预算、失败恢复和可观测性。** 这属于我们的架构判断，不是这些项目的官方适用性结论。引入独立 broker 不能修复当前来源身份、缓存上下文、取消丢失、游标分页等问题。

River 是值得进一步验证的本地候选：官方已经支持 SQLite，不能再按“River 必须依赖 PostgreSQL”排除它；但其 SQLite 支持仍明确标为 experimental。当前建议先确定本项目的任务语义，再比较 River SQLite 与深化现有任务模块的复杂度。未安装、集成或验证 River，也未据此决定直接采用。[River SQLite 官方说明](https://riverqueue.com/docs/sqlite)

## 官方项目分别解决什么

| 项目 | 官方能力与运行前提 | 确认、失败和恢复语义 | 对本项目的判断 |
| --- | --- | --- | --- |
| RabbitMQ | 消息代理；Windows 安装需要受支持的 Erlang，并运行 RabbitMQ 节点/服务。[安装说明](https://www.rabbitmq.com/docs/install-windows) | publisher confirm 与 consumer ack 独立；手动确认模式下连接关闭会重新入队未确认消息；处理者需考虑幂等；prefetch 约束未确认在途数量。[确认机制](https://www.rabbitmq.com/docs/confirms) | 排除本轮引入：增加运行时、连接和数据目录管理，收益与单机需求不匹配。借鉴确认分界、在途预算与失败可见性。 |
| NATS JetStream | 在 NATS 中持久化消息，可回放；需要启用 JetStream 的 nats-server。[概览](https://docs.nats.io/learn/jetstream) | 显式 ack 后推进消费位置；未确认会超时重投；DoubleAck 等待服务端确认收到 ack。[投递与确认](https://docs.nats.io/learn/jetstream/delivery-and-acknowledgment) | 排除本轮引入：本地业务任务不需要一个独立消息流系统。借鉴有界消费与明确重试上限。 |
| Asynq | Redis 支持的 Go 任务库，提供调度、优先队列、重试和 worker 崩溃恢复，声明至少一次执行。[官方仓库](https://github.com/hibiken/asynq) | v0.25.1 processor 在出队前获取并发令牌；recoverer 检查租约过期任务并按尝试次数进入重试或归档。[processor 源码](https://github.com/hibiken/asynq/blob/v0.25.1/processor.go)、[recoverer 源码](https://github.com/hibiken/asynq/blob/v0.25.1/recoverer.go) | 排除本轮引入：为任务增加 Redis，与现有 SQLite 形成双存储。借鉴令牌取得时机、任务截止时间和恢复分支。 |
| River | 数据库任务队列，以 PostgreSQL 为主要目标；SQLite 自 v0.23.0 加入，当前仍为实验支持，官方使用 modernc.org/sqlite 测试。[SQLite 说明](https://riverqueue.com/docs/sqlite) | 支持同事务入队；任务至少一次执行，任务完成但完成标记未落库时可能重新执行。[事务入队](https://riverqueue.com/docs/transactional-enqueueing)、[可靠 worker](https://riverqueue.com/docs/reliable-workers) | 保留为本地技术验证候选；无需为了使用它换成 PostgreSQL。必须先验证现有 SQLite 连接、迁移、写争用与关闭顺序。 |
| Temporal | 通过事件历史和重放恢复工作流；Workflow 决策需可重放，外部 I/O 放在 Activity。[Workflow 说明](https://docs.temporal.io/workflows) | Activity 可能重试，官方建议幂等。[Activity 说明](https://docs.temporal.io/activities)；官方嵌入式 server 的定位为开发和测试。[自托管指南](https://docs.temporal.io/self-hosted-guide) | 排除本轮引入：项目尚无需要该编程模型的长周期工作流需求，不能把开发用嵌入 server 当桌面生产依赖。借鉴步骤边界和可恢复状态，暂不建设通用工作流引擎。 |

以上“排除/候选”均是针对当前单机产品的推论，不代表这些组件没有价值，也不构成永久禁用规则。Redis 或 PostgreSQL 的性能与部署条件没有在本项目中做对比实验。

## 源码里值得真正学的设计

### 1. River 把领取任务设计成数据库状态转换

固定源码：[River v0.23.0 SQLite river_job.sql](https://github.com/riverqueue/river/blob/v0.23.0/riverdriver/riversqlite/internal/dbsqlc/river_job.sql)。

该文件的 `JobGetAvailable` 按队列、调度时间、优先级和批量上限选择可运行项，以一个 `UPDATE ... RETURNING` 更新 running、attempt、attempted_at 并返回领取结果。schema 约束合法状态及最终完成时间；任务列表、清理和卡住任务查询都有批量上限。

**本项目借鉴**：任务领取与状态变更必须原子化；不是先查一组任务再无条件启动。网络、解析和文件下载在事务外进行，不能持有 SQLite 写事务等待上游。列表查询和历史清理同样应分页，不能只限制正在工作的 goroutine。

该版本 SQLite driver 说明连接并发与 SQLITE_BUSY 风险，且不支持 LISTEN/NOTIFY；因此不能把 PostgreSQL 路径的调度假设原样搬过来。[SQLite driver v0.23.0](https://github.com/riverqueue/river/blob/v0.23.0/riverdriver/riversqlite/river_sqlite_driver.go)

### 2. 幂等入队和幂等执行是两层保证

River 明确指出：unique job 约束插入次数，但不能保证只执行一次。已经完成副作用、尚未保存完成状态时崩溃，仍会重新执行。[Unique jobs](https://riverqueue.com/docs/unique-jobs)

**本项目借鉴**：业务去重键应包含来源持久身份、目标内容、操作类型和影响结果的配置版本。数据库结果采用唯一约束/条件更新；下载文件采用临时文件、完整性检查、最终落位和重启对账。文件落位和 SQLite 提交不是天然的跨介质原子事务，不能只写一句“exactly once”。

RabbitMQ confirm、JetStream DoubleAck 都不等于“本地文件或业务记录副作用永不重复”；若业务完成后确认前失败，仍需业务恢复策略。这是从上述确认机制推导的应用边界。

### 3. 崩溃恢复必须尊重用户取消和终止状态

River v0.23.0 `JobRescuer` 对超时 running 项分批处理，会检查取消标记、尝试次数和重试策略，而不是无条件全部重新执行。[恢复器源码](https://github.com/riverqueue/river/blob/v0.23.0/internal/maintenance/job_rescuer.go)

**本项目借鉴**：本地单实例可以结合运行实例身份与启动恢复，不必机械复制分布式心跳体系。恢复前确认旧执行者确已失效；用户取消、主动暂停、应用退出中断和实际失败分别持久化。对于未知执行结果先对账，再决定重新运行，避免把一次“停止下载”在重启后变成自动重新下载。

### 4. 重试间隔与在途预算要一起设计

JetStream 的 AckWait 超时与 nak 是不同路径；普通 nak 可立即重投，backoff 不会自动让它延迟；MaxDeliver 达到上限后也不自动提供死信队列，需要显式处理失败终态。[重投文档](https://docs.nats.io/learn/jetstream/acknowledgment)

**本项目借鉴**：重试应有错误分类、next_run_at、次数上限、退避与明确终态。429 尊重 Retry-After；不支持的配置、无效认证和用户取消不能一律快速重试。并发令牌、来源/主机预算及排队容量分别受控，避免无界任务和大量等待 goroutine。播放与当前章节优先级独立于后台采集，必要时使用加权公平调度，防止低优先级永久饥饿。

## 本地升级的建议链路

以下是建议设计，尚未实现；不要求统一所有既有业务状态名称。

```text
用户操作 / 本地计划
  → 领域用例校验与稳定业务身份
  → SQLite 事务保存业务意图与持久任务
  → 调度器领取有限批次
  → 应用预算 + 来源/主机预算
  → 具体业务步骤：下载 / 采集 / 索引 / 清理
  → 检查结果与保存检查点
  → 条件提交任务状态
  → 发布带任务 ID、attempt、状态版本的进度事件
```

持久任务至少表达：任务 ID、任务类型与参数版本、业务去重键、状态、attempt、next_run_at、结果/检查点、最后错误分类、取消或暂停意图、创建及更新时间。哪些字段独立成列，由查询与恢复规则决定，不把整套分布式系统字段无差别搬进来。

建议将三类执行分开：

- **请求工作**：搜索、详情、阅读当前页，遵循请求取消；通常不写持久任务表。
- **可恢复任务**：下载、采集、导入和索引，以 SQLite 为状态真相，允许退出后继续或恢复。
- **可丢弃维护**：缓存淘汰、统计刷新，受预算约束；是否持久化取决于是否有用户可见的业务承诺。

进程内进度通知只负责刷新 UI，不能当作任务完成的唯一真相。页面重新打开从 SQLite 查询快照，事件携带的状态版本用于拒绝迟到更新。调度、状态存储、业务执行和事件出口是明确职责，不必为了替换消息代理创建通用 MQ 接口。

## 采用 River SQLite 前的验证清单

当前仓库使用 `modernc.org/sqlite`（见 go.mod）与 SQLite，驱动方向与 River 文档相符，但这只是入口兼容，不能据此宣称已适配。

1. 固定候选版本，检查 SQLite 仍属实验支持的具体限制、schema 迁移与可用功能；区分 core 与 Pro，不把付费工作流/并发能力算作默认能力。
2. 验证同一业务事务入队是否能使用现有连接与迁移机制；若任务另放数据库文件，则不能声称与主库业务写入原子提交。
3. 在 Windows 和项目实际支持的平台验证启动、停止、强制退出、升级回滚与 SQLITE_BUSY；先测实际任务，不引用官方空任务吞吐代替产品测量。
4. 对比引入 River 后能删除多少现有重试、恢复、取消和保留期代码，以及还需维护多少额外适配；若收益不大，按相同语义深化现有本地任务模块。

## 必须能复跑的验收

- 任务持久化前崩溃、领取后崩溃、完成副作用后落状态前崩溃，重启后行为符合各自承诺。
- 同一用户动作重复入队，产生一个有效业务结果；重试不重复新增作品、不损坏最终文件。
- 取消/暂停与完成竞争时有确定结果；退出后不出现访问已关闭数据库的后台任务。
- 大量任务只领取有限批次，等待队列、在途网络和内存可测；前台阅读不会被后台采集长期阻塞。
- 磁盘满、SQLite 写入失败、配置版本改变、目标来源被移除等故障可见，不能误报成功。
- 队列等待、执行耗时、重试次数、运行中数量与恢复结果可从本地日志/诊断查看，无需部署监控服务器。

本轮只做官方资料和固定 tag 源码阅读，没有运行这些框架的性能或故障实验；上述验收是后续本地升级的交付条件。
