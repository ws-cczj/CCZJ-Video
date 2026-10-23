# CCZJ Video 架构与约束调查

> 重构前的调查快照，不代表当前实现或开发规范。后续改动与最终验证见 [重构记录](backend-refactor-2026-10-18.md)，有效规范见 [AGENTS.md](../AGENTS.md)。原始源码位置与行号按调查时记录。

调查日期：2026-10-18。调查对象：当前工作区，Git HEAD 为 `c900bb1`；包含大量未提交修改与未跟踪文件。本文记录的是调查时的源码与测试结果，不代表已发布程序的全部行为。

本轮只新增本报告，没有修改业务实现、既有规则、依赖、生成绑定或 Qorder 正在编辑的文件；没有构建或启动应用，没有操作用户数据库。数据库探针运行在独立进程，使用临时目录，结束后删除。

## 结论与优先级

用户对“不恰当的约束和注释”的担忧有实证支持。问题不只是错误码不够多：项目有把历史实现选择写成永久规则的倾向，而且部分选择依据已经与当前依赖能力不符。另一些拆分只改变文件位置，并未改变依赖关系。

但目前的证据不足以支持整体更换 Go、Vue 或 Wails，也不足以评价 Qorder 在其他会话里拒绝修改的具体原因。需要区分仓库既有设计、开发中的小说漫画模块、应用程序框架和前端工具链。

| 编号 | 优先级 | 发现 | 证据状态 / 归属 |
|---|---|---|---|
| A1 | 高 | 错误传输依赖文本协议，设计前提与当前 Wails 能力不符 | 源码确认；既有债务 |
| A2 | 高 | 错误分类过粗且部分分类错误，机器可用信息仍被塞回文案 | 源码确认；既有与新增代码均有 |
| A3 | 高 | 阅读模块把“没有接通取消”写成“框架无法取消”，把可测试竞态写成不可测试 | 框架源码确认；开发中代码 |
| A4 | 中 | 数据库全局初始化在失败后第二次调用错误地返回成功 | 独立探针复现；HEAD 已存在 |
| A5 | 中 | 文件拆分掩盖聚合类与透传层，职责和依赖仍集中 | 源码抽查确认；HEAD 已存在 |
| A6 | 中 | 行数、目录、字符串扫描等实现规则被当成架构约束 | 守卫脚本与 ADR 确认 |
| A7 | 高 | 前端工具链存在已停止维护版本，与运行时版本代际不协调 | 锁文件与官方维护政策确认 |
| A8 | 中 | 存储抽象只有类型断言，缺少统一运行时解码与失败语义 | 源码确认；部分调用点已有补救 |
| A9 | 高（作为开工基线） | 当前测试和类型检查不全绿，不能据此宣布架构改造完成 | 实测；尚未逐项归因 |
| A10 | 中 | 本地 Wails 技能仍描述 v2，可能继续产生错误建议 | 文档与实际依赖比对确认 |

“高”表示应优先处理或在继续扩展前确定方案，不表示已经发生用户数据损坏。开发中的发现需要在 Qorder 完成当前轮次后复核。

## A1：AppError 把展示文本变成了传输协议

证据：

- [错误包开头与实现](<C:/Users/18446/Desktop/CCZJ Video/app/apperror/error.go:1>)：称其为“全应用唯一的错误类型层”，要求跨包错误采用这里的类型；`Error()` 把 Code、Message、Cause 拼成一条字符串。
- 原 ADR 0009：理由是 Wails 把错误转成字符串，前端只有这一条输入。该文件已在后续重构删除，历史版本可用 `git show c900bb1:docs/adr/0009-coded-errors.md` 查看。
- [前端归一化](<C:/Users/18446/Desktop/CCZJ Video/frontend/src/api/app.ts:28>)：用正则拆 `CODE: message`，并剥离 Wails 的英文包装文本；不读取 `Error.cause`。

当前锁定的 Wails `v3.0.0-beta.30` 已经提供应用级和服务级 `MarshalError`。本地该版本的 `bindings.go` 第 425–439 行把序列化结果放入 `CallError.Cause`，`services.go` 第 31–39 行定义服务级定制入口。当前 `@wailsio/runtime` 的 `types/calls.d.ts` 第 23–29 行也明确说明异常的 `cause` 可以携带序列化结果。[同版本官方源码](https://github.com/wailsapp/wails/blob/v3.0.0-beta.30/v3/pkg/application/services.go)

因此，现有文本协议并非当前框架强迫采用的唯一方案。它使日志格式、用户文案和机器协议相互牵连；加上几层包装后，前端还要识别框架的英文措辞。

“统一跨进程错误格式”是合理目标；“全应用只能有一个错误类型”并非达成它的必要条件。ADR 自己又在末尾保留 `collect.PathError` 和 sentinel 错误，说明该规则的适用范围已经自相矛盾。

调查中未找到明确的“禁止重构 AppError”规定。`Error()` 上方实际写的是改格式时同时修改前端；这是兼容提醒，不是禁止改进。无法仅凭仓库认定 Qorder 为什么拒绝。

建议方向：保留普通 Go `error`、`errors.Is/As` 和领域错误；在 Wails 出口映射为统一的 JSON 错误对象。前端先消费结构化字段，旧文本解析只作迁移期兼容。用户可见消息与诊断用 Cause 分开。稳定的是对外字段和码的语义，内部类型与实现应允许演进。

## A2：已有 Code，但语义和结构仍不足

具体例子：

1. [source.Export](<C:/Users/18446/Desktop/CCZJ Video/app/source/service.go:33>) 把 `db.GetSourceByKey` 的所有错误归为 `NOT_FOUND`。该数据库方法既可能返回 `sql.ErrNoRows`，也可能返回查询故障，甚至在访问数据库前返回 source key 校验错误。这些不该统一告诉用户“来源不存在”。新增的 `library/sources.go` 也有同样模式。
2. [阅读请求中的 429](<C:/Users/18446/Desktop/CCZJ Video/app/provider/fetch.go:310>) 把 `Retry-After` 放进自然语言消息里；401/403 被归为 `UNSUPPORTED`。前端无法只根据这些通用码区分限流等待、鉴权失败和真正不支持某项能力。
3. [splitError](<C:/Users/18446/Desktop/CCZJ Video/app/library/service.go:358>) 虽然输出分开的 `errorCode` 和 `errorMessage`，消息仍然是整条 `err.Error()`，其中又带错误码及 Cause。两套表示开始重叠。
4. ADR 说已有具体码不能被外层码遮蔽，但 [Wrap](<C:/Users/18446/Desktop/CCZJ Video/app/apperror/error.go:93>) 不执行这个约束，`CodeOf` 读取最外层值。调用者必须靠记忆维持不变量。

独立探针得到：

```text
coded wrap: inner=UNSUPPORTED outer=VALIDATION
typed nil CodeOf panics=true
Wrap nil is nonnil=true
```

第一项证实规则没有被接口保证；第二项是 `CodeOf` 对包含 typed nil 的 error 会解引用空指针。第三项只是 Wrap 的当前语义，需要明确约定，不能单凭这一点判为生产 BUG。未发现 typed nil 已在真实调用链触发，不能放大其影响。

建议先定义少量明确类别与可操作的领域原因，例如“资源不存在”“需要鉴权”“受到限流”，在需要的场景携带结构化 HTTP 状态与重试等待信息。不要单纯靠再加几十个通用常量解决问题，也不要规定所有超时都可以重试：是否重试还取决于操作本身。

## A3：取消和异步测试被错误注释限制

[asyncResource.ts](<C:/Users/18446/Desktop/CCZJ Video/frontend/src/reading/asyncResource.ts:48>) 写道：

- 乱序响应在前端“测不出来”，单元测试全绿也无法发现；
- Go 绑定调用“一旦发出就无法从前端取消”；
- generation 是“唯一要紧的机制”。

第一条在技术上不成立：用两个可手动完成的 Promise，先发 A、再发 B、先完成 B、后完成 A，就能确定性验证 A 不覆盖 B，不需要真实网络抖动。

第二条也不符合当前版本：生成绑定返回 `CancellablePromise`，其 `cancel()` 会经 runtime 请求后端取消；Wails 为调用创建可取消 context，并支持把 context 注入 Go 方法的第一个参数。证据在本地 runtime `dist/calls.js` 第 50–77 行、Wails `messageprocessor_call.go` 第 60 行和 `bindings.go` 第 270–271 行。

但“框架支持”不等于当前业务已经支持：[阅读 facade](<C:/Users/18446/Desktop/CCZJ Video/app/service/facade_library.go:79>) 使用的是应用级 `a.background.Context()`；前端资源抽象只保留普通 Promise，没有保存可取消句柄。因此现在确实只作废结果，不停止该请求。这是接线未完成，而非不可突破的框架限制。

generation 防止旧结果回写，取消减少无用工作，两者互补。建议请求 context 贯通到 HTTP 与数据库操作，保留应用退出取消，并保留 generation 校验。当前新增公开接口还把 `run` 参数写成 `never[]`，强转已被类型检查拒绝，不能靠注释证明接口合理。

## A4：全局数据库生命周期存在真实缺陷

[InitDB](<C:/Users/18446/Desktop/CCZJ Video/app/db/sqlite.go:76>) 组合了包级 `sync.Once` 和函数内局部 `initErr`。第一次初始化失败后 Once 已消耗，第二次不再执行初始化，新建的局部错误却是 nil。

独立进程中，先创建一个普通文件，把它当数据目录传入两次：

```text
db.InitDB: first_error=true second_error=false db_nil=true
```

第二次返回成功，但数据库指针仍为空。该实现已在 HEAD 中存在。当前应用正常启动路径遇到第一次错误会退出，因此本轮没有证明它已导致用户启动重试时崩溃；它确实破坏了函数的成功语义，也妨碍同进程恢复和独立测试。

建议由应用装配阶段显式创建、持有和关闭数据库实例，按需要向模块注入少量操作。短期至少应保存初始化错误、明确失败可否重试，并清理部分初始化资源。不要给所有模块机械生成一整套 Repository 接口。

## A5：物理拆文件与真正模块化混在一起

抽样时 `app/service` 有 30 个非测试 Go 文件，同一个 `App` receiver 上约有 210 个方法，其中 182 个导出。根目录 `app.go` 的行数被限制了，但 `app/service/app.go` 仍有约 825 行，下载、诊断、生命周期、更新、代理和领域入口共享同一个对象。

典型调用链是：

```text
Wails App.GetVideoList
  -> media.Service.List
  -> handler.GetVideoList
  -> 包级 db / 缓存 / 其他协作者
```

[media.Service](<C:/Users/18446/Desktop/CCZJ Video/app/media/service.go:15>) 没有字段，很多方法只是透传 handler。DTO 又来自 handler。这样的层不是全部无价值——部分方法确有应用逻辑——但抽查到的透传部分增加了导航步骤，没有隐藏更多复杂度，也没有提供可替换依赖。

建议以视频浏览、采集调度、阅读资源、下载任务等真实职责分配所有权。绑定适配保持薄且合理；中间层则应拥有业务行为或稳定的转换职责。没有独立职责的透传可以合并，拥有复杂策略的模块应集中策略。无需为了套某种架构名称而增加层数。

## A6：哪些约束要保留，哪些要改写

| 现有约束 | 评价 | 建议 |
|---|---|---|
| 错误分类不用文案匹配 | 合理 | 保留，优先使用类型和结构化协议 |
| 所有跨包错误都用唯一 AppError 类型 | 过度扩大范围 | 改成跨进程输出格式统一，允许领域错误 |
| 错误码语义保持兼容 | 合理 | 保留，但写清迁移和废弃方式；不限制人类文案修改 |
| 生成绑定集中在前端适配处 | 合理 | 保留，适配应能处理 null、错误和取消能力 |
| 数据迁移前备份；SQL 不由源任意建表 | 有明确数据依据 | 保留，不应因反感约束而一并删除 |
| `app.go` 600 行、其他 Go 文件 900 行硬上限 | 可作预警，不能证明职责正确 | 用复杂度、依赖和职责审查补充；避免为过检查机械拆文件 |
| 根目录只能有 main.go 和 app.go，下载必须在指定文件 | 与特定布局绑定 | 改成入口与传输职责约束，允许等价目录组织 |
| scoped style 超 260 行必须搬到 styles 目录 | 风格规则，不是架构性质 | 降为提示，按可维护性和复用性判断 |
| 所有未引用 locale 键都使检查失败 | 对动态扩展场景可能过严 | 按实际导出契约定义允许项；不要靠补无效引用骗过检查 |
| 源身份不可随便改变、图片响应有字节上限 | 有真实不变量 | 保留，身份变化需要显式迁移；容量上限应有场景依据 |

证据：[verify.ps1](<C:/Users/18446/Desktop/CCZJ Video/scripts/verify.ps1:29>)、[前端守卫](<C:/Users/18446/Desktop/CCZJ Video/scripts/check-frontend-conventions.mjs:250>)、ADR 0005 和 ADR 0009。

守卫还使用正则匹配具体变量名和文本形式。例如错误文案检查只命中 `strings.Contains` 加 `err/e/lastErr.Error()` 这几种拼法，不能证明没有其他文案分类。它适合作为轻量提醒，不应被当成完整的架构正确性检查。

本轮检查到项目路径及所查祖先路径没有 AGENTS.md，项目根目录没有 `.qoder` 或 `.cursor` 规则目录。这个结果不包含 Qorder 的全局设置、其他会话上下文或远端规则。

## A7：确实有需要升级的工具链，但不能混同整体换框架

锁文件实际版本：

| 项目 | 锁定版本 | 调查判断 |
|---|---|---|
| Go 模块声明 / 本机 Go | 1.26.0 / 1.26.2 | 本轮无证据要求更换语言或升级大版本 |
| Wails / JS runtime | v3.0.0-beta.30 / 3.0.0-beta.30 | 两端匹配；已具备结构化错误和取消能力，先正确使用 |
| Vue | 3.5.42 | 不能因为架构问题就推导出需要换 Vue |
| Vite / plugin-vue | 3.2.11 / 3.2.0 | Vite 3 已不在官方维护范围 |
| TypeScript / vue-tsc | 4.9.5 / 1.8.27 | 应与 Vue、Vite 和类型生态一起验证升级 |
| Vitest | 0.34.6 | 与上述工具链成组验证，不独立盲升 |
| vue-i18n | 9.14.5 | v9 已 EOL，应安排升级 |
| hls.js | 1.7.0-beta.1.0.canary.11847 | 实际锁的是预发布 canary；需要记录采用原因和回归依据 |

[Vite 官方支持政策](https://vite.dev/releases)明确旧支持范围之外不再维护；[Vue I18n 官方维护页](https://vue-i18n.intlify.dev/guide/maintenance)明确 v9/v10 已结束维护。本文不把“存在旧依赖”直接等同于“已证实可利用漏洞”。

建议把工具链升级作为独立批次：先恢复当前基线，再按官方迁移指南选择受维护且兼容的组合；检查 Vue SFC、插件动态导入、Wails 资源嵌入、桌面与 Android WebView。不在本轮安装或修改依赖，也不直接指定“所有包升到 latest”。

## A8：存储工具隐藏异常，但没有验证数据形状

[storage.ts](<C:/Users/18446/Desktop/CCZJ Video/frontend/src/platform/storage.ts:8>) 将 JSON 结果断言为泛型 T。版本 envelope 只能表示版本，不保证 value 符合 T。合法 JSON 的对象、数组、null 或不合法条目都可能进入调用方。

小说、漫画状态中已经补上 `unknown`、`Array.isArray` 和 normalize；其他调用点仍会直接读取 `Record<...>`。同类修补分散，说明通用存储接口提供的保证比类型签名暗示的少。`writeStorage` 的 boolean 也不能保证调用方处理写入失败，例如部分插件状态写入直接忽略结果。

建议按状态定义解码器、迁移与失败策略，存储工具接收 decoder，而不是把未经校验数据变成 T。缓存可丢弃，书架和进度需要用户可理解的失败反馈，不能所有持久化都采用相同静默降级。

## A9：本轮验证结果与调查边界

执行结果：

| 检查 | 结果 |
|---|---|
| `go test -count=1 ./app/apperror ./app/db ./app/proxy ./app/provider ./app/library` | 5 个包通过 |
| `npm run test -- --reporter=dot` | 27 个文件；133 项通过，3 项失败 |
| 单独重跑 recording 与 thumbnail-lifecycle | 同样 3 项失败，另 6 项通过 |
| `node scripts/check-frontend-conventions.mjs` | 通过：中英各 1707 键，报告无未引用键 |
| `npx vue-tsc --noEmit` | 失败 |
| 独立 Go 探针 | 复现 InitDB 状态丢失、外层码覆盖、typed nil panic |

失败测试：

- `tests/recording.test.ts:25`：预期的 playback_snapshot marker 调用未发生。
- `tests/recording.test.ts:40`：慢快照串行化测试中预期调用未发生。
- `tests/thumbnail-lifecycle.test.ts:124`：预期 seeked 监听为 0，实际为 1。

这些是失败基线，尚不能断言都是运行时产品 BUG，也可能包含过时 mock 或断言。没有在调查中顺带修复。

类型检查至少包含阅读 API 把生成绑定的可空 DTO 当作非空 DTO、部分 Vue 模块类型不兼容，以及 `asyncResource.run` 的 `never[]` 强制转换失败。新增阅读模块仍在开发，这些不直接计作已发布程序回归。

没有运行完整 `verify.ps1`、完整 Go 测试、桌面构建、Android 构建或真实网络站点端到端测试。已通过的 5 个包不能证明整个系统正确；通过字符串协议测试也不能证明协议设计值得保留。

调查时前端页面仍有演示仓库，新增后端阅读能力正在接入。因此“尚未全面接入真实源”在本报告中属于开发状态，不列为最终架构缺陷。

## A10：本地技能也要接受版本审查

[项目 Wails 技能](<C:/Users/18446/Desktop/CCZJ Video/.agents/skills/wails/SKILL.md:1>) 仍使用 `wails/v2`、`wails dev`、`window.runtime`、`wailsjs` 等说明；项目实际是 v3 和 `@wailsio/runtime`。照着它执行可能导入错误版本、使用错误命令，或误判框架能力。

本轮没有按该技能的 v2 操作指令更改工程。建议给项目技能和架构说明注明适用版本，以当前锁定版本源码为校验依据；过时文档应更新或标明 superseded。注释中的技术断言也必须允许被证据推翻。

## 与用户旧项目的对照

本轮重点读了 `cczjVideo`、`bluebell`、`douyin`、`SP-TJob` 的错误实现，并抽查 `shortener` 的依赖装配，没有逐仓库做全面质量排名。

- 旧 `cczjVideo/pkg/e/error.go` 配合 `handler/common/common.go` 的 `Response`，直接把 `code/msg/data` 输出为 JSON。这点比当前把 Code 编进字符串再解析清楚，用户提出的比较在“传输设计”这一维成立。
- `bluebell` 把状态码和消息映射分离，可以借鉴其机器标识与文案分离的思路。
- `shortener/internal/svc/servicecontext.go` 显式持有数据库模型与序列依赖，也比空 Service 加隐式全局依赖更容易看出模块需要什么。
- 但旧 AppErr 没有 Cause/Unwrap；共享可变错误指针不宜直接照搬。`douyin` 的 `Code.Err()` 返回普通 `errors.New(msg)`，又会丢掉错误码。因此不能得出“旧实现每一方面都更好”，也不应整套复制。

Go 官方展示了包装错误、`Is/As` 与多种领域错误共存的方式；统一 Error 结构并不是使用 Go 错误链的前提。[Go 错误处理说明](https://go.dev/blog/go1.13-errors)

## 后续改造顺序建议

1. **先清理错误的约束依据**：修订 ADR 0009 和相关注释，明确内部实现可变、兼容契约可迁移；清理错误的“不能取消、不能测试”论断。保留数据安全不变量。
2. **恢复可验证基线**：与正在开发的小说漫画分支对齐，处理类型错误和 3 项测试失败，明确哪些是实现问题、哪些是测试适配问题。
3. **统一错误出口**：接入结构化序列化，建立少量领域原因与错误映射，区分安全用户消息和诊断详情；保留旧客户端/旧文本兼容测试。
4. **贯通请求生命周期并修复 DB 初始化语义**：验证取消后 HTTP 停止、旧结果不写回、应用退出仍可收尾；验证初始化失败不会被下次调用当成功。
5. **做有实际收益的模块整合**：选择一条视频读取链或阅读资源链，消除无职责透传，隔离数据库和请求协作者。用一次真实需求变更检验修改是否集中，而不是用文件变短衡量成功。
6. **独立升级前端工具链**：维护状态驱动升级，版本兼容和桌面/移动回归驱动验收。

这些是调查建议，没有作为新硬性规则写回工程。当前可直接采用的原则是：对外保证清晰、对内实现可改、技术断言需要证据，约束服务于功能与维护。

## 调查指纹

便于其他开发者确认所读版本。SHA-256：

```text
app/apperror/error.go
54CEFF32BA6644560E80A33C4F44F5F1E8693206F767D95F62B68B538388D20A
docs/adr/0009-coded-errors.md
03971482E86D03518EBED1F1578DCB686B007E849D77425321BE56617696D523
app/db/sqlite.go
BC3AD60AF657A315DC2D4D7A651E531D44FB81B5CD70583FF3C73A6630D5228C
frontend/src/reading/asyncResource.ts
2B46B702B5D88D6CD0752035D1E300417837DFE658179942CFA1A07399D9207A
frontend/package-lock.json
C256CA3E3C4A06D18D2A12004896925B3C9826D7ED7CC580DFCF8EE540CF9985
```
