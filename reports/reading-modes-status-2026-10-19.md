# 小说与漫画模式进度核对（2026-10-19）

> 2026-10-20 复核说明：本文为历史实施与样本验证记录。用户后续实际使用仍报告返回恢复、源兼容与页面可用性问题；本文的“通过/完成”不得作为整体整改完成的证明。后续工作见 [小说与漫画整改任务书](history/reading.md#origin-reading-redesign)，实际应用修复尚待实施和验证。

> 后续实施：本报告列出的搜索渐进展示、漫画签名刷新、正文预取、自动目录刷新和可选源导入已完成，并新增内置番茄小说适配；当前结果见 [源导入与真实数据验证](reading-source-completion-2026-10-19.md)。下文保留改动前的核对记录。

本文结合历史报告与当前代码核对；历史段落不代表当前代码事实。代码路径和本轮命令结果已经复核，文末单独列出静态发现及验证边界；未重新操作桌面或 Android 设备。当前判断：两个模式均已达到支持自定义声明式源的可用初版，真实源覆盖与稳定性收尾仍需继续。

## 历史目标与证据边界

- 目标是 Go 侧自有结构化声明式提取层，小说提供 search/detail/toc/content，漫画提供 search/detail/toc/pages/image，支持 JSON 路径和 CSS 选择器；不是照搬 Legado 规则语言。原需求看的是能力、真实响应和覆盖率，而不是通用书源兼容承诺。参见 [需求书第 43 行](<C:/Users/18446/Desktop/CCZJ Video/reports/05_调研需求_小说漫画提取层.md:43>)、[操作草案第 104 行](<C:/Users/18446/Desktop/CCZJ Video/reports/05_调研需求_小说漫画提取层.md:104>)。
- 不运行第三方源 JS、不为第三方源创建 WebView 是当时明确的范围；JS 渲染、任意签名/解密和图片字节解密不属于静态提取覆盖。参见 [需求书第 179 行](<C:/Users/18446/Desktop/CCZJ Video/reports/05_调研需求_小说漫画提取层.md:179>)、[能力清单第 72 行](<C:/Users/18446/Desktop/CCZJ Video/reports/06_提取层能力清单与实现参数.md:72>)。
- `05`、`06` 本身明确：公开书源能力出现频率、真实响应骨架、漫画扩展侧 Referer/header 实况、Go 统计编码探测方案仍缺证据，不能据其计算“已覆盖多少源”。参见 [收窄任务第 14 行](<C:/Users/18446/Desktop/CCZJ Video/reports/05_调研需求_小说漫画提取层.md:14>)、[证据缺口表第 338 行](<C:/Users/18446/Desktop/CCZJ Video/reports/06_提取层能力清单与实现参数.md:338>)。本文未进行新的公网取证。

## 历史实施阶段

| 阶段 | 报告记录的进展 | 应如何解读 |
| --- | --- | --- |
| 最初 UI 与阅读器 | 小说书城、分类、搜索、书架、详情、目录、阅读；漫画发现、筛选、详情、追漫、条漫/LTR/RTL；小说分页/滚动、字体主题、进度恢复 | 初始演示版本。两份文档开头已经声明模拟书籍/演示漫画下线，正文旧数据和旧 localStorage 描述不能作为现状。 |
| 内容源接入 | sources 按 kind 区分；作品与章节按来源保存；共享 app/library；结构化提取；正文与漫画页表独立缓存 | 从演示转入用户自加源，语义参照 Legado/Mihon，但使用自有格式。 |
| 升级第一阶段 | 来源稳定身份、图片缓存完整请求上下文与 Cookie 隔离、正文完整性状态、搜索不透明游标与取消 | 修补真实使用的正确性契约；报告没有原生设备验收。 |
| 升级第三阶段 | 书架/进度/偏好迁 SQLite、v2 浏览器值原子导入、备份 v2、组合请求取消、共享读取、核心预算、书库分页和批量读取 | 持久化已经越过“仅浏览器本地保存”阶段；历史文档该部分已过期。 |
| 10 月 19 日模式隔离与恢复 | 只挂载当前模式；切换先取消并排空；视频自动任务只在视频模式启动；保留轻量路由/滚动/表单状态，旧文档释放 | 没有三套完整页面树常驻；阅读长期进度在 SQLite，临时页面快照在 sessionStorage。 |

表格来源：[旧 UI 文档清理说明](history/reading.md#origin-reading-redesign--文档清理记录)、[内容领域 ADR](<C:/Users/18446/Desktop/CCZJ Video/docs/adr/0016-novel-comic-content-domain.md:17>)、[第一阶段](<C:/Users/18446/Desktop/CCZJ Video/reports/upgrade-phase1-2026-10-18.md:12>)、[第三阶段](<C:/Users/18446/Desktop/CCZJ Video/reports/upgrade-phase3-2026-10-18.md:11>)、[模式隔离](<C:/Users/18446/Desktop/CCZJ Video/reports/mode-isolation-2026-10-19.md:3>)、[模式状态恢复](<C:/Users/18446/Desktop/CCZJ Video/reports/mode-resume-2026-10-19.md:3>)。

第二阶段主要是视频下载可靠性；它将阅读 SQLite 迁移和统一资源预算明确留给后续阶段，不能单独据它判断阅读模式还没落地。[第二阶段边界](<C:/Users/18446/Desktop/CCZJ Video/reports/upgrade-phase2-2026-10-18.md:75>)。

## 历史验收到了哪里

- 第三阶段使用真实 SQLite 测迁移、回滚、身份重建；Chrome 使用实际 Vue 页面/store 和 Wails 传输替身，覆盖阅读导入、分页、筛选、清理 localStorage、失败重试。该报告明确 Chrome 不等于 Wails 桌面、Android 或真实站点。[验证范围](<C:/Users/18446/Desktop/CCZJ Video/reports/upgrade-phase3-2026-10-18.md:117>)。
- Android 后续验收修复过大目录临时路径错误、漫画图片原生桥丢请求上下文、漫画页码被空页表归零等真实链路问题。[修复记录](<C:/Users/18446/Desktop/CCZJ Video/reports/upgrade-acceptance-2026-10-19.md:69>)。
- 最终 Android `all-checks.json` 中，搜索/详情/目录、正文和带防盗链漫画图片、共享读取与上游取消、大目录分页、SQLite 导入、强停/清 WebView 后恢复、实际阅读页渲染、存储失败重试、旧值不复活、跨库备份身份等均记录为 passed。分别见 [源与目录](<C:/Users/18446/Desktop/CCZJ Video/reports/upgrade-acceptance-2026-10-19/android/all-checks.json:30>)、[正文和图片](<C:/Users/18446/Desktop/CCZJ Video/reports/upgrade-acceptance-2026-10-19/android/all-checks.json:2342>)、[取消](<C:/Users/18446/Desktop/CCZJ Video/reports/upgrade-acceptance-2026-10-19/android/all-checks.json:2422>)、[分页](<C:/Users/18446/Desktop/CCZJ Video/reports/upgrade-acceptance-2026-10-19/android/all-checks.json:2432>)、[迁移](<C:/Users/18446/Desktop/CCZJ Video/reports/upgrade-acceptance-2026-10-19/android/all-checks.json:2459>)、[恢复](<C:/Users/18446/Desktop/CCZJ Video/reports/upgrade-acceptance-2026-10-19/android/all-checks.json:2613>)、[渲染](<C:/Users/18446/Desktop/CCZJ Video/reports/upgrade-acceptance-2026-10-19/android/all-checks.json:2726>)、[失败与备份](<C:/Users/18446/Desktop/CCZJ Video/reports/upgrade-acceptance-2026-10-19/android/all-checks.json:2752>)。
- 这一批总计 15 过、1 失败，失败是下载压力下播放保留预算测量，报告指出夹具启动方式有问题但未重跑；不能说整批全过，也不能把旧 `reading-checks.json` 的较早失败当最终结果。[最终失败项](<C:/Users/18446/Desktop/CCZJ Video/reports/upgrade-acceptance-2026-10-19/android/all-checks.json:2970>)、[环境边界表](<C:/Users/18446/Desktop/CCZJ Video/reports/upgrade-acceptance-2026-10-19.md:86>)。
- 后来的模式隔离报告记录完整验证 176 项前端测试通过并在 MuMu 实际检查模式诊断；模式恢复报告记录 192 项前端测试及 Windows/Android 构建通过，但桌面 EXE 报错/空白仍待确认。这些是不同时间与范围的历史结论，不能叠加成当前全部环境通过。[隔离验证](<C:/Users/18446/Desktop/CCZJ Video/reports/mode-isolation-2026-10-19.md:28>)、[恢复验证与桌面限制](<C:/Users/18446/Desktop/CCZJ Video/reports/mode-resume-2026-10-19.md:26>)。

## 本轮主代理已核对的当前情况

以下为本轮代码检查及实际命令结果汇总，已由主代理复核。

- 当前达到“支持用户自定义声明式源的可用初版”：共享搜索/详情/目录，小说正文，漫画页表/图片；已有结构化 JSON v3、HTML/CSS、JSON dotted path、GET/POST、编码与分页，来源面板手工编辑 JSON 模板。没有 Legado/Mihon 格式适配，不能称通用兼容。
- `go test ./app/provider ./app/library ./app/readingstate ./app/db ./app/backup ./app/service` 通过（readingstate 包无独立测试文件）；`go test ./app/proxy -run 'Image|Img'` 通过；6 个前端阅读测试文件共 23 项通过；`npm run build` 的 vue-tsc、tsc、Vite 均通过。这些不替代本轮未重新执行的原生 UI/真实公网源验收。
- 静态发现一：前端仍等待 `LibrarySearchPage` 等聚合响应，没有消费逐源 `library:search:result`，慢源会拖延这一轮结果整体展示。参见 [当前 API 适配](<C:/Users/18446/Desktop/CCZJ Video/frontend/src/api/library.ts:113>)；这是体验差距，不表示搜索不存在。
- 静态发现二：漫画页失败重试只增加 nonce、重新创建同一 URL 的图片；后端 `ChapterPages` 优先读页表缓存，没有显式强刷/403 后重新解析签名 URL 的闭环。因此不能把重试按钮等同于“签名过期自动恢复”。参见 [阅读页重试](<C:/Users/18446/Desktop/CCZJ Video/frontend/src/comic/views/ComicReader.vue:184>)、[重试实现](<C:/Users/18446/Desktop/CCZJ Video/frontend/src/comic/reader/useComicPages.ts:92>)、[图片原 URL](<C:/Users/18446/Desktop/CCZJ Video/frontend/src/comic/components/ComicPanel.vue:50>)、[后端缓存优先](<C:/Users/18446/Desktop/CCZJ Video/app/library/content.go:371>)。该发现仅由代码路径判断，未做真实站点过期复现。
- 原设想尚未落地：`06` 设想的“正文前后各 1 章预取”和“剩余不足 3 章、距上次至少 10 分钟自动刷新目录”，主代理确认当前小说只加载当前章、目录仅首次空目录或按钮刷新。参见 [原参数第 374 行](<C:/Users/18446/Desktop/CCZJ Video/reports/06_提取层能力清单与实现参数.md:374>)、[目录刷新职责说明](<C:/Users/18446/Desktop/CCZJ Video/app/library/content.go:24>)。这是原设想与实现的差距，优先级低于实源验证和签名过期恢复。

## 仍需收尾

1. 源生态和兼容范围：补公开源/扩展的频率与响应形态证据，准备合法可控的代表性实际源样本；目前没有证据支持覆盖率数字。
2. 漫画签名过期恢复：实现用户重试能触发页表失效/重新解析的明确链路，并验证稳定身份、取消、Cookie/Referer 和缓存隔离。
3. 搜索渐进体验：确认是否要将已有逐源结果事件接入页面，避免聚合等待影响首批结果展示。
4. 验收边界：Windows 实际运行、真实手机/公网源与长时压力仍需分别记录；模式恢复报告中的桌面空白和 Android 压力夹具未完成项不能由构建成功替代。历史 Chrome 阅读恢复新增用例也曾失败、修正夹具后未重跑，本轮只重跑单元与构建，不能宣布浏览器阅读验收全绿。参见 [验收边界表](<C:/Users/18446/Desktop/CCZJ Video/reports/upgrade-acceptance-2026-10-19.md:86>)。

这些是进度核对结论与后续建议，本文未修改产品代码。
