# Go 后端设计参考研究

调查日期：2026-10-18。本文是研究证据与本项目的适用性分析，不是新增的一套开发规则。后续有效约束应集中在根目录统一文档，研究报告仅保留证据、理由与验收建议。

## 结论

建议保留 Go + Wails 桌面单体，以明确的模块责任、显式资源生命周期、普通 Go 错误链和传输边界映射进行渐进重构。大型项目中可以直接借鉴的是这些机制；没有证据要求本项目引入微服务、全面 DDD、通用 Repository、依赖注入框架，或把所有内部错误改成唯一类型。

这是一项根据下列源码和本地调查作出的工程判断，不是声称这些上游项目发布过同一份架构规范。

## 样本和复现范围

| 样本 | 固定版本 | 选择理由 |
|---|---|---|
| Prometheus | `v3.5.0` | 同一进程内协调存储、查询、采集与 API，适合观察模块协作和取消 |
| Grafana | `v12.0.0` | 大型 Go 后端包含渐进错误类型迁移和后台服务生命周期 |
| etcd | `v3.6.0` | 展示领域错误到传输错误的集中映射，以及启动失败清理 |
| Wails | `v3.0.0-beta.30` | 本项目已锁定版本，用于确认实际可采用的框架入口 |

以上是分析用固定 tag，不代表调查时最新版本，也不是建议将本项目升级到这些软件。所有外部结论均来自官方仓库实际源码，已通过网络打开验证；没有使用第三方架构文章。没有下载、构建或运行上游工程，也没有对其作全面质量评级。

## Prometheus：按消费需要定义接口，内部错误和 API 错误分开

`web/api/v1/api.go` 在 API 所在包定义 `ScrapePoolsRetriever` 等窄接口；其中前者仅有 `ScrapePools() []string`。`NewAPI` 显式接收所需查询、存储及获取器依赖。`returnAPIError` 用 `errors.As` 识别查询取消、超时和存储错误，再用 `errors.Is` 识别 `context.Canceled`；对外 `Response` 有独立的 `errorType`、`error` 字段。查询从 `r.Context()` 派生截止时间并将 context 交给查询引擎。[固定版本源码](https://github.com/prometheus/prometheus/blob/v3.5.0/web/api/v1/api.go)

入口实际创建 notifier、scrape manager、query engine、rule manager 和 web handler，并用 `run.Group` 配对运行与中断行为。模块可以在同一可执行程序里协作，不需要先变成独立服务。[装配与运行入口](https://github.com/prometheus/prometheus/blob/v3.5.0/cmd/prometheus/main.go)

测试既有 `rulesRetrieverMock` 这样的手写替身，也使用真实测试存储；`TestReturnAPIError` 验证被 `%w` 包装的取消错误仍能正确分类，`TestRespondError` 检查 HTTP 输出。[API 测试](https://github.com/prometheus/prometheus/blob/v3.5.0/web/api/v1/api_test.go)

对本项目的推论：读取目录、更新书架等消费者需要什么才抽取什么接口；错误在 Wails 出口转换；查询接收请求 context。不能照抄它巨大的构造参数表和长入口文件，这些同样有导航和维护成本。

## Grafana：错误迁移允许共存，公开信息与诊断信息分离

`response.ErrOrFallback` 用 `errors.As` 尝试统一错误类型，失败时仍接收普通 error；取消则通过 `errors.Is` 判断。`response.Err` 以 `grafanaErr.Public()` 生成 JSON，同时将完整错误保留在响应对象中供日志使用。这是具体的兼容迁移机制，不要求一轮替换全仓库。[响应边界实现](https://github.com/grafana/grafana/blob/v12.0.0/pkg/api/response/response.go)

`errutil.Error` 分开保存 `Reason`、`MessageID`、`LogMessage`、`Underlying`、`PublicMessage` 和 `PublicPayload`，实现 `Unwrap`、`Is`；原始 Error 的 JSON 序列化被禁止，公开数据需要显式调用 `Public()`。[错误结构与公开投影](https://github.com/grafana/grafana/blob/v12.0.0/pkg/apimachinery/errutil/errors.go)

后台服务契约仅为 `Run(ctx context.Context) error`。Server 持有取消函数和 `errgroup`，运行服务并等待，Shutdown 发出取消并在调用者期限内等待结束。[小型生命周期接口](https://github.com/grafana/grafana/blob/v12.0.0/pkg/registry/registry.go)、[Server 运行与关闭](https://github.com/grafana/grafana/blob/v12.0.0/pkg/server/server.go)

`TestErrors` 对原响应函数和 `ErrOrFallback` 执行相同预期，`TestRespond` 使用表驱动测试核对序列化结果。[响应测试](https://github.com/grafana/grafana/blob/v12.0.0/pkg/api/response/response_test.go)

对本项目的推论：保留 AppError 作为一种可用类型，让错误出口同时识别领域错误、标准库错误和既有 AppError；前端消费公开 payload，日志保留原因。当前规模无需复制 Grafana 的模板、状态体系、服务注册和生成装配基础设施。

## etcd：领域包拥有错误，传输层映射协议

`v3rpc/util.go` 把 membership、mvcc、lease、auth 等包各自的 sentinel 映射为 gRPC 错误；取消和截止时间通过 `errors.Is` 识别。这说明内部错误可按领域存在，协议统一放在出口。该版本对大部分 sentinel 使用直接 map 查找，不能直接照抄成支持任意 `%w` 包装的分类器。[gRPC 错误映射](https://github.com/etcd-io/etcd/blob/v3.6.0/server/etcdserver/api/v3rpc/util.go)

`kvServer` 持有 `etcdserver.RaftKV`，Range/Put 等方法接收 gRPC 传入的 context，调用内部能力后用 `togRPCError` 转换失败。适配层做校验、调用、协议映射，具有明确职责；不能因此把所有薄适配层都判为多余。[KV 传输适配](https://github.com/etcd-io/etcd/blob/v3.6.0/server/etcdserver/api/v3rpc/key.go)

`embed.StartEtcd` 在创建过程中发生错误时通过 defer 关闭部分创建的实例并返回 nil；`Etcd` 持有服务、监听器和关闭状态。`Close` 对资源和子任务负责。[显式创建与失败清理](https://github.com/etcd-io/etcd/blob/v3.6.0/server/embed/etcd.go)

传输层有请求校验和错误识别的独立测试。[请求校验测试](https://github.com/etcd-io/etcd/blob/v3.6.0/server/etcdserver/api/v3rpc/key_test.go)、[错误识别测试](https://github.com/etcd-io/etcd/blob/v3.6.0/server/etcdserver/api/v3rpc/util_test.go)

对本项目的推论：数据库对象应有显式打开、成功发布、失败回收和关闭的生命周期。保留包级兼容入口可以降低迁移风险，但不能把“新增一个实例壳、内部仍全局查询”称为完成依赖注入。etcd 的 Raft、集群和 RPC 复杂度解决其部署需求，本地 SQLite 桌面应用没有同样需要。

## Wails：当前锁定版本已经提供需要的边界能力

`ServiceOptions.MarshalError` 接收 Go error，输出合法 JSON，服务级回调可回退到全局回调；启动、关闭契约也明确支持资源初始化和逆序释放。[beta.30 服务定义](https://github.com/wailsapp/wails/blob/v3.0.0-beta.30/v3/pkg/application/services.go)

调用处理器为每个 call ID 保存 cancel 函数，前端取消请求会触发它。值得注意：创建调用 context 时使用了 `context.WithoutCancel`，因此业务层不能仅凭方法参数有 context 就断言请求自动继承应用关闭信号；需要同时连接应用生命周期并释放相关 hook。[beta.30 调用与取消](https://github.com/wailsapp/wails/blob/v3.0.0-beta.30/v3/pkg/application/messageprocessor_call.go)

Go 标准错误链允许多种类型通过 `%w`、`Unwrap`、`errors.Is/As` 协作。公开哪些底层错误属于 API 设计决策，不要求每层都重新生成统一错误。[Go 官方错误链说明](https://go.dev/blog/go1.13-errors)

## 针对 CCZJ Video 的实施决策

本地依据为[上一轮调查](architecture-investigation-2026-10-18.md)，并复核了 `app/apperror/error.go`、`app/db/sqlite.go`、`app/media/service.go`、`app/service/facade_library.go`。工作区有正在开发的小说漫画功能；这里的建议不构成覆盖其他开发者修改的授权。

| 问题 | 具体重构方向 | 有意义的验收 |
|---|---|---|
| AppError 的字符串同时承担协议、日志、文案 | 新增公开错误 DTO，在 Wails MarshalError 边界映射；普通 Go error、领域错误和现有 AppError 共存；前端优先读取结构化 cause | 包装后仍可 Is/As；取消、限流、未找到、存储故障各归其类；普通错误不会把原始诊断直接公开；旧格式仅保留必要迁移兼容 |
| 所有数据库失败被误判为未找到 | 用 `errors.Is(err, sql.ErrNoRows)` 精确识别，其他故障保留原因并按语义处理 | 实际缺记录与 SQL 查询故障产生不同结果；收藏查询不能把故障静默变成 false |
| InitDB 的 Once 消耗后局部错误丢失 | 明确拥有者和失败策略；初始化失败关闭已打开连接；成功后再发布可用状态；保留旧入口时写明迁移范围 | 同进程连续失败不返回假成功；修正目录后能否重试符合定义；关闭后状态一致 |
| App → media.Service → handler 纯透传 | 合并无独立行为的中间调用，把有默认值、身份协调和错误语义的行为留在明确模块；真实外部协作者由构造显式给出 | 一次业务修改集中在拥有该行为的模块；测试能替换故障协作者；不以文件数量下降作为唯一验收 |
| 阅读只丢弃旧结果，不停止旧工作 | Wails 绑定接受请求 context，贯通 provider HTTP 和 DB；保留应用关闭取消；前端保留可取消句柄及 generation 检查 | 取消后上游请求结束；A/B 响应倒序不能覆盖新数据；应用关闭仍能结束进行中的请求 |
| md 和注释将历史实现固化 | 统一根目录一份有效规则；删除或重写与运行时代码不符的规定，修复指向已删除文档的引用；研究报告保留为证据 | 搜索不再出现“无法取消”“乱序不可测”等错误断言；规则不再要求内部错误唯一类型或按硬行数机械拆文件 |

上述 DTO 字段的最终命名、模块归属与兼容期需要由实施时的真实调用点决定；不要把这份方案的临时命名再写成永久禁令。限流重试等待、鉴权状态等只有实际需要才加入公开数据，不能简单声称“超时均可重试”。

## 统一规则文档应包含什么

建议统一文档只记录可以解释依据并有验证方式的稳定边界：模块责任与依赖方向、请求与应用生命周期、错误公开格式、持久化与迁移安全、必要验证命令、生成绑定操作，以及如何修改这些约定。操作系统路径、文件行数、固定目录列表、只有一个错误类型等不应替代这些边界。

注释应说明当前非显然的行为和理由；与代码重复的教程、带情绪的命令、绝对化框架能力断言可以删。有关源身份、迁移备份、内容大小限制等真实数据和资源不变量仍要保留，必要时缩短并链接到测试。已有 ADR 若被统一规则替代，应删除重复约束或明确其仅为历史记录；文档清理必须同步修复引用。

这次重构成功的标准是对外行为可解释、错误可分类、取消真的传播、资源状态可验证、业务修改更集中。更换框架或添加架构名词本身不是完成标准。
