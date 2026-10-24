# 共享网络请求约束收尾

2026-10-23 更新：按用户当前要求，梯度冷却已从仅弹幕扩展到全部查询出口，原文关于旧查询保持原重试频率的描述仅记录当时实现。当前边界与缓存策略见 [请求通道与取消](../docs/request-pipelines.md)。

同日后续实现已将公共代理池装配到 Go 后端 HTTP 出口，当前实现及验收见文末“公共代理池装配与验收”；下述单代理探测和 2026-10-19 表格保留当时结果。

同日外网验收：`CCZJ_DOUBAN_CHART_LIVE=1 go test ./app/douban -run '^TestLiveChartWithSharedCooldown$' -count=1 -v` 使用临时 SQLite 与标准档中间件，最多允许三次出站。实际仅两次：热榜 HTTP 302，验证页 HTTP 403，解析条目为 0，实时可用性断言失败；再次提交相同请求返回剩余约 331 秒，未新增出站。原始输出位于 `dist/audit/douban/live-chart.log`。此结果证明当前环境的冷却生效，不证明豆瓣可访问或不会触发反爬，也不等同于桌面／Android 实机验收。

随后按用户要求进行代理对照：Windows 配置的本地 HTTP 代理 `127.0.0.1:7890` 仍得到热榜 302／验证页 403。通过 [ProxyScrape 公开列表](https://github.com/proxyscrape/free-proxy-list) 选取三条不同国家、声明支持 CONNECT 的代理，以 HTTPS 出口查询检查连通性，分别得到连接超时、隧道 502、成功。仅对通过检查的一条代理运行一次真实 Go 热榜链路：返回 HTTP 200、解析 10 条，重复请求复用响应，真实出站计数仍为 1；日志在 `dist/audit/douban/live-chart-public-proxy.log`，代理连通性记录在 `dist/audit/douban/public-proxy-results.json`。这个结果仅证明该次代理访问成功，不证明长期可用或所有接口均可用；应用未接入自动代理池，也未写入用户数据库或系统代理设置。测试入口通过 `CCZJ_DOUBAN_LIVE_PROXY` 显式选择 HTTP／HTTPS／SOCKS5 代理，未设置时仍直连，保留三次出站上限。

2026-10-19，Windows 本地源码验证。用户最终约束：原有请求频率没有问题，统一增加整体约束；新弹幕接口需要随机的长冷却。本轮不打包、不进行模拟器/真机操作测试，保留其他会话改动。

## 实现结果

- `app/netpolicy` 接入现有 Go HTTP 出口，包括采集、豆瓣、阅读、弹幕、图片/播放代理、下载、更新与投屏 HTTP 控制。原有 Cookie、来源身份、SSRF、超时与并发通道保留。
- 原有查询完成后照常再次出站；普通连接失败不添加五分钟等待，不替换各模块现有重试策略。同时发生的相同安全读取可以复用完整小响应。no-store、大响应、未读完的响应不作为成功缓存，等待者可正常获取自己的响应。
- 默认标准档位只用较高的突发上限约束异常请求；稳健/低频档位降低突发上限。HTTP 429 / 带 Retry-After 的 503 按主机共享源站等待。统一层没有后台重试队列。
- 仅新增弹幕相同请求采用随机冷却：标准 5–6 分钟、稳健 10–12 分钟、低频 30–35 分钟。重试或切档不重置已有截止时间，失败及已准入后取消占用间隔；不同身份的视频/片段不共用一个长锁。不进行多平台自动补位。
- SQLite 有序迁移 v16 增加哈希冷却账本，保留 v15 弹幕数据；原有至少一天的弹幕内容缓存及 30 天淘汰继续有效。源站等待与弹幕冷却可跨重启恢复。
- 播放、图片、下载、测速和设备控制保持连续传输，不捕获大响应、不套长冷却。安装包下载显式标记连续传输；弹幕查询记录达到容量上限时仍允许播放传输。
- 视频高级设置、小说和漫画设置显示同一个全局策略面板，保存到 `network_request_policy_v1` 后立即生效。弹幕源管理中没有频率设置。
- 诊断 `runtime.network_policy` 记录出站、复用、拦截次数，只返回当前模式和共享更新服务；Wails 绑定已重新生成。复用和拦截不重复计入 netstats 字节，诊断导出/记录沿用 runtime 快照自动携带计数。

详细职责、档位数值、容量与覆盖边界见 `docs/adr/0018-shared-network-guardrails.md`。

## 实际验证

| 命令/检查 | 结果与范围 |
| --- | --- |
| `go test ./app/netpolicy ./app/netstats ./app/danmaku ./app/collect ./app/provider ./app/db ./app/lifecycle` | 7 个包通过。netpolicy 共 20 个顶层测试，另有部分子用例；测试使用内存 SQLite 和替身 RoundTripper，无真实弹幕源请求。 |
| 原有频率与重试回归 | collect/douban/novel/comic/update 同一成功查询连续两次实际出站；普通连接失败连续两次仍返回原错误且无附加冷却；不产生持久查询冷却记录。 |
| 弹幕与身份 | 签名变化不绕过相同查询冷却；失败后重试/切档不重置截止时间；不同分段、来源、请求体、鉴权头不复用同一响应；重启从 SQLite 恢复等待；404 缺段在冷却期间仍保持 NotFound 语义。 |
| 并发、缓存与释放 | 并发相同查询合并，等待者取消不取消所有者；no-store/大响应/普通 HTTP 错误不错误拒绝后续旧查询；部分响应不缓存；退出关闭未消费响应体；连续流不保留响应字节；账本满不阻断播放。 |
| 源站限流与计数 | Retry-After 支持秒数/HTTP 日期；主机等待跨查询/分类和重启生效；同一 3 字节响应复用两次只累计 3 字节真实流量。 |
| `go test ./... -run '^$'` | 全包编译通过，**没有执行全套 Go 测试**。cast 没有运行监听/发现测试。 |
| `go vet ./...` | 通过。 |
| `powershell -NoProfile -ExecutionPolicy Bypass -File scripts/generate-bindings.ps1` | 通过；先编译 service，再生成绑定，无 WARNING。333 packages、190 methods、142 models。 |
| `npm run test -- tests/network-policy.test.ts tests/danmaku-lifecycle.test.ts tests/danmaku-sources.test.ts tests/danmaku-timeline.test.ts` | 4 文件、15 测试通过。覆盖档位解码、随机后备延迟、后端剩余等待，以及已有弹幕生命周期/来源/时间线。 |
| `frontend/node_modules/.bin/vue-tsc.cmd --noEmit`（frontend 目录） | 绑定更新后通过。 |
| `node scripts/check-frontend-conventions.mjs` | 通过，中英各 1938 键；9 个未引用键只作为清理提示。 |
| `node scripts/check-architecture.mjs` | 通过。 |

## 未验证的环境与结论限制

没有生成 EXE、APK 或前端 dist；没有启动桌面程序、浏览器替身、Android 模拟器或真机；没有运行 race、完整 Go/前端测试或完整 verify.ps1。本轮是请求行为与集成编译检查，不能据此宣称设备性能提升、所有源站可用或桌面/Android 验收完成。已安装程序仍需后续构建才能包含这些 Go 变更。

后续用户反馈时可在当前模式诊断的网络请求策略中观察：正常使用的拦截数是否异常增长；弹幕重复操作是否主要变成复用；真实出站请求/流量是否同步下降。没有运行记录前不报告耗时或内存改善百分比。浏览器自行发起的 fetch/原生媒体及系统网络不自动受这套 Go 出站中间件约束。

## 公共代理源调研

2026-10-23 按用户要求核对发布方仓库、接口文档和实际下载结果。以下是候选地址数据源；获取列表本身不会改变后续请求的出口，也不是具有可用性承诺的托管代理服务。

| 来源 | 自动更新与数据格式 | 接入判断与一手依据 |
| --- | --- | --- |
| ProxyScrape | 发布方声明 API 每分钟更新、仓库每五分钟更新；[CONNECT 候选 JSON](https://raw.githubusercontent.com/proxyscrape/free-proxy-list/main/proxies/protocols/https/data.json) 为对象数组，字段有 `protocol/ip/port/ssl/last_checked/latency_ms/uptime_percent`；同路径 `data.txt` 为带协议的 URL 行 | `https` 目录实际是 `protocol=http && ssl=true`，应组装 `http://IP:PORT`，不能误写成 TLS 到代理。提供检查时间和延迟，可作为优先候选源；[README](https://github.com/proxyscrape/free-proxy-list#readme)、[MIT 许可](https://github.com/proxyscrape/free-proxy-list/blob/main/LICENSE)。 |
| Databay | 声明每五分钟发布；匿名可读的 [HTTP TXT](https://raw.githubusercontent.com/databay-labs/free-proxy-list/master/http.txt) 每行 `IP:PORT`，没有逐条检查时间；GitHub 列表要求曾通过目标 HTTPS 证书验证，但成功检查最多可距今六小时 | `http.txt` 是 HTTP CONNECT；`https.txt` 才是 TLS 到代理，且可能包含自签代理证书。优先接入前者，保留正常 TLS 验证。网站 JSON API 需要登录，本次匿名请求为 403，不作为无凭据默认源；[README](https://github.com/databay-labs/free-proxy-list#readme)、[API 说明](https://github.com/databay-labs/free-proxy-list/blob/master/docs/api.md)、[MIT 许可](https://github.com/databay-labs/free-proxy-list/blob/master/LICENSE)。 |
| Proxifly | 声明每五分钟收集及验证；[HTTP JSON](https://raw.githubusercontent.com/proxifly/free-proxy-list/main/proxies/protocols/http/data.json) 为对象数组，含 `proxy/protocol/ip/port/https/anonymity/score/geolocation`；同路径 `data.txt` 为 URL 行 | `https` 能力标记与 `https://` 代理连接方式不同，应使用原始 `proxy` 的协议；缺少逐条检查时间。作为可选补充源；仓库是 GPL-3.0，分发数据或复制实现时应保留来源并评估许可边界，不把它写成 MIT；[README](https://github.com/proxifly/free-proxy-list#readme)、[许可](https://github.com/proxifly/free-proxy-list/blob/main/LICENSE)。 |

本次从这三个 raw URL 下载均返回 HTTP 200，分别解码到 261、6903、5617 条候选；数量只描述这一时点，不作为最小容量或可用数量。本轮仅验证列表下载和格式，没有逐条探测，也没有重新请求豆瓣。Databay 的浏览索引曾显示旧版“无需登录”文字，直接读取当前 raw README/API 文档及匿名请求均不支持该说法，以当前文件和实测为准。

需要稳定服务时，可另行配置托管网关。例如 [Webshare 官方连接文档](https://apidocs.webshare.io/proxy-connection) 提供 `p.webshare.io`、账号密码/IP 授权、固定会话和轮换参数。它负责提供账号内的代理出口，和下载公共列表不同；需用户自己的服务配置，本次未注册、购买或实测，不能据文档推断豆瓣可用。

建议默认候选使用 ProxyScrape CONNECT JSON 与 Databay HTTP TXT，并在本地完成有限、可取消的健康检查、去重、失效退避与快照保存。列表的“已验证”“匿名”“延迟”是发布者检查点的观测，不代表本机到豆瓣、弹幕或视频站的表现；CDN 也可能滞后。代理只改变传输出口，不应清空现有查询冷却或七天榜单缓存。后端 HTTP 统一装配仍须逐一确认自定义 Transport、重定向、流式传输及退出释放；局域网投屏控制、回环地址、代理列表首次引导和浏览器/原生播放器自行出站需要单独定义边界，不能仅修改默认客户端就声称全部覆盖。HTTP 明文请求可能被公共代理读取或改写，HTTPS 保留目标证书验证；“能 CONNECT”不能证明代理运营者可信，也不能保证不返回 403。

## 公共代理池装配与验收

按用户明确边界实现：局域网直接访问；所有 Go 后端公网 HTTP 请求先使用自动更新且通过验证的公共代理节点，池内可用节点都失败后才回退本机代理；没有公网直连兜底。统一路由在 `netstats.WrapTransport` 装配，原 `netpolicy` 梯度、业务缓存、Cookie、SSRF 与网络预算继续生效。新增 `app/outbound` 拥有更新、健康检查、短期快照、节点／站点失败冷却与有界 Transport 缓存；`app/netaddress` 共享已有地址分类判据。应用生命周期拥有更新任务，SQLite 关闭前停止与等待；配置和快照复用 settings，不加新数据库或依赖。当前参数及配置见[公网代理池](../docs/request-pipelines.md#公网代理池)。

首次完整外网验收暴露 HTTP/2 ALPN 问题：`http.Transport.Clone()` 初始化原始协议配置后，再增加代理拨号方法会改变克隆的默认协议选择，造成协商 h2 却按 HTTP/1 读取。先运行 `TestProxyCloneKeepsNegotiatedHTTP2Support` 复现失败，再保留原协议集合修复，重跑完整链路通过。

| 验收 | 结果与实际范围 |
| --- | --- |
| 自动更新与真实豆瓣请求 | `CCZJ_OUTBOUND_POOL_LIVE=1 go test ./app/outbound -run '^TestLiveAutomaticPoolAndChart$' -count=1 -v -timeout 115s` 通过；临时 SQLite，两个默认源，最多十二候选、三个健康节点；实际得到一个健康节点、公共代理尝试计数 2、本机代理回退 0、豆瓣 HTTP 200／10 条榜单。重复请求命中中间件响应，没有新增代理尝试。耗时约 25.27 秒；输出 `dist/audit/douban/live-automatic-pool.log`。这是当次可用性，不承诺免费代理长期可用。 |
| 确定性本地链路 | 真 HTTP 代理与 TLS CONNECT 测试覆盖：全部节点失败后才访问本机代理、没有公网直连、局域网绕过配置代理、混合 DNS 拒绝、十二个后端分类路由、站点并发稳定选路、TLS 证书拒绝、Range／206 流式读取、取消、不重放已发非幂等写入、更新失败保留旧节点、快照恢复不续期、容量与退出清理。 |
| 完整源码验证 | `scripts/verify.ps1` 通过：全项目 Go 测试、前端 71 文件／333 测试、类型检查与生产构建、请求策略／locale／CSS／文档检查。绑定已通过 `scripts/generate-bindings.ps1` 重新生成，无生成器警告。 |
| 静态分析 | `go vet ./app/outbound ./app/netaddress ./app/netstats ./app/netpolicy ./app/service ./app/proxy` 通过。当前环境未安装 `golangci-lint`，未完成该工具检查。 |
| 浏览器布局 | 复用 `scripts/test-diagnostics-browser.py --proxy-pool-only`，真实 Vue 诊断组件与绑定替身；1280px 桌面、390px 窄屏、已有指标与更新失败状态通过；截图 `dist/audit/outbound-proxy-1280.png`、`dist/audit/outbound-proxy-390.png`。属于浏览器替身，不是 Wails／Android 原生运行验收。 |
| race 检测限制 | `go test -race ./app/outbound ./app/netstats ./app/netpolicy ./app/proxy` 在测试进程启动时均以 `0xc0000139` 失败；单独不引用项目的空 `TestSmoke` 也返回同一状态码。未获得 race 检测结果，不能宣称通过或据此排除竞态。 |

未替换用户 EXE，未运行 Android 原生、真实视频 CDN 持续吞吐或所有内容站点实测。WebView／原生播放器自行发送的请求不经过 Go HTTP 中间件；无可用快照、种子或本机代理的首次启动无法下载候选列表，会明确报错。Windows 手动代理和环境变量已接入；PAC 不执行。配置修改通过后端设置保存并在重启时装配。

### 用户补充来源核实

2026-10-23 经本机入口 `127.0.0.1:7890` 做有限 HTTP 页面下载，单请求超时 15～20 秒、响应上限 2 MiB；保留目标 TLS 校验。仅检查页面结构，没有连接页面列出的任何代理，没有请求豆瓣，也没有执行或绕过验证脚本。HTML 与检查摘要保存在 `dist/audit/outbound-sources/`，本表的条数是可解析候选行数，不是可用节点数。

| 用户补充来源 | 本次地址、HTTP 结果与解析观察 | 更新与协议边界 |
| --- | --- | --- |
| 快代理 | [高匿开放代理](https://free.kuaidaili.com/free/inha/) 为 200，原始 HTML 已有 12 行；定位 `#table__free-proxy table tbody tr`，七列表头是 IP、PORT、类型、位置、响应速度（秒）、最后验证时间、付费方式。分页链接位于 `/free/inha/数字`；不需要执行 JavaScript 才能拿到这次的行。 | 页面描述称每日更新，本页记录验证时间按小时排列；未核实公开 JSON/API。此页实际类型均为 `HTTP`，不能凭页面通过 HTTPS 提供就推断代理支持 CONNECT。根页面的“优质私密代理”需要鉴权，不能与这个开放列表混用。 |
| 齐云代理 | 从[首页](https://www.qiyunip.com/)链接确认 [免费列表](https://www.qiyunip.com/freeProxy/)，200；`table#proxyTable tbody tr` 有 15 行，字段 IP、PORT、匿名度、类型、属地位置、响应速度、录取时间；分页 `/freeProxy/数字.html`。列表行在原始 HTML 中，无需登录或运行脚本。 | 这次包含 `http`／`https` 标签，页面未解释 `https` 是目标 HTTPS 能力还是 TLS 到代理，不能直接生成 `https://IP:PORT`。未找到这个免费列表的固定更新周期承诺；录取时间不能当成逐条最近健康检查。页面说明它是第三方资源，不保证有效性。 |
| 站大爷 | [实时免费列表](https://www.zdaye.com/free/)与[每日发布目录](https://www.zdaye.com/everyday_proxy/) 都返回 200，但响应是含 `meta[name=aliyun_waf]`、隐藏 `textarea#renderData` 和混淆 JS 的验证页，没有代理表格。另一个已发现的[筛选分页](https://www.zdaye.com/free/16/?https=1)返回普通页面，但筛选共 248 条、每页 20 条，第 16 页超出范围，零行并不表示该源没有代理。 | 普通页面称持续更新、每 60 分钟循环验证；提供 http／https／socks4／socks5 筛选，但本次未拿到有效数据行或免费 API 结构。不能将“HTTP 200”记作抓取成功，也不能假定旧版 `#ipc` 选择器仍可使用。暂不作为已验证的自动源。 |
| 方法 SEO／seofangfa | [代理页](https://seofangfa.com/proxy/)及 `https://www.seofangfa.com/` 本次均为 HTTP 530。 | 未获得本站有效页面，不能确认当前表格、协议、更新周期或可用性；没有据旧教程猜测 JSON/API 地址。 |
| France VPN GitHub Pages | [首页](https://francevpn.github.io/)及[全部代理页](https://francevpn.github.io/free-proxy/)都为 200，各有 30 条静态 HTML 行。定位 `table.table tbody tr`；十二列依次是 IP、端口、用户名、密码、国家、协议、匿名度、速度、运行时间、回复、延迟、更新。凭据显示为 `****`，本次对应 `img.js_openeyes` 的 `data-text` 全是 `no need`，不能把星号当密码。 | 首页这次只有 socks4／socks5，全部代理页有 http／socks4／socks5；保留逐行协议，不能把整个站当 HTTP 源。首页宣称每 180 秒更新，行内却只有相对时间，未取得绝对检查时间、公开机器 API 或明确数据许可证；该频率只记录为发布者声明，不保证列表新鲜或节点可信。 |

快代理与齐云的 HTML 可作为可选候选源，建议使用表头校验与 IP／端口／协议字段解析，拒绝登录页、挑战页和结构变化，不从整页文本正则收集任意 `IP:PORT`。HTML 源没有给出的 CONNECT 能力应由有限健康检查确认；列表故障应保留其他源和有效旧节点，不触发无限重抓，也不改变已有站点冷却。

### 新来源适配与独立更新

用户补充来源后，已实现统一 HTML／JSON／文本适配、协议提示、JSON envelope、字符串端口与协议数组；每源独立周期和失败退避、轮换取样、跨源去重及有界并发。HTML 按表头解析，拒绝验证页；修复 SOCKS5 裸地址误用 HTTP，以及 HTTPS 能力标签误作 TLS 到代理的风险。旧显式 `sources` 配置保留原选择，新增 `feeds` 不自动覆盖自定义来源，完整配置与迁移方法见[公网代理池](../docs/request-pipelines.md#公网代理池)。本轮没有新增第三方依赖。

#### Geonode

- 官方入口：[Free proxy list](https://geonode.com/free-proxy-list)。下载的页面直接包含 `proxylist.geonode.com/api/proxy-list` JSON 分发地址，确认是第一方接口。
- 本次验证 URL：<https://proxylist.geonode.com/api/proxy-list?limit=20&page=1&sort_by=lastChecked&sort_type=desc&country=CN&protocols=https%2Csocks5>。
- HTTP 200，9,450 字节；顶层 `{data:[],total:64,page:1,limit:20}`，20 条 `country` 全为 `CN`；`protocols` 中 15 条 `socks5`、5 条 `https`。
- 每条字段：`ip` 字符串，`port` **字符串**（例 `"59999"`），`protocols` 字符串数组，`country` 两字母代码；另有 Unix 秒 `lastChecked` 和 ISO 时间 `updated_at`。应显式解码和校验端口，按每个受支持协议展开候选。
- 响应头明确 `RateLimit-Policy: 100;w=3600`、`RateLimit-Limit: 100`、`Cache-Control: public,max-age=60`、ETag。建议单页有界下载、至少 15 分钟刷新；不要无界翻页或探测时再取源。
- 现场证据：`geonode-cn.json`、`geonode.headers`、`geonode-page.html`。未核实独立再分发许可证；官网所称分钟更新不等同于每条候选的实际验证时间。

#### proxy.scdn.io

- 官方[API 文档](https://proxy.scdn.io/api_docs.php)：`protocol=http|https|socks4|socks5|all`，`count` 上限 20，国家参数是 **`country_code`**，不是 `country`。
- 实测两个精确 URL：<https://proxy.scdn.io/api/get_proxy.php?protocol=https&count=3&country_code=CN>；<https://proxy.scdn.io/api/get_proxy.php?protocol=socks5&count=3&country_code=CN>。
- 都是 HTTP 200，分别 124／129 字节。实际结构与文档一致：`{code:200,message:"success",data:{proxies:["ip:port"],count:3}}`。没有行级国家、协议、更新时间；协议与 CN 条件只能来自本次请求配置，不能当作实测地理位置。
- 建议独立 `scdn` envelope 适配器：必须校验业务 `code==200`，`data.proxies` 为字符串数组，空结果保持空；每个源配置固定协议，不能用正则扫描整个 JSON 或从 `message` 提取地址。
- 现场证据：`scdn-cn-https.json`、`scdn-cn-socks5.json` 及对应 headers。未发现明确更新 SLA、请求配额或再分发许可证，建议保守 15 分钟刷新与失败退避。

#### TheSpeedX

- 第一方仓库：[TheSpeedX/PROXY-List](https://github.com/TheSpeedX/PROXY-List)。验证 URL：<https://raw.githubusercontent.com/TheSpeedX/PROXY-List/master/socks5.txt>。
- 经本机代理 HTTP 200、76,558 字节、4,235 行，格式为每行 `ip:port`，协议由文件明确为 SOCKS5，**不携带国家**。
- 官方 README 仍引用旧仓库名 `TheSpeedX/SOCKS-List`；运行配置优先使用已实测的当前 `PROXY-List` 路径。[README](https://raw.githubusercontent.com/TheSpeedX/PROXY-List/master/README.md) 有 Last Updated 字段，仓库描述每天更新，正文只承诺不定期更新；没有稳定更新 SLA，也未看到独立 LICENSE。
- 使用已有文本适配器，默认协议 `socks5`，严格逐行解析与有界去重；不要给这些节点猜测 CN。响应有 ETag、`max-age=300`，可条件更新。
- 现场证据：`speedx-socks5.txt`、`speedx.headers`。

#### monosans

- 第一方仓库：[monosans/proxy-list](https://github.com/monosans/proxy-list)。[MIT LICENSE](https://raw.githubusercontent.com/monosans/proxy-list/main/LICENSE) 已下载核实，README 说明每小时重新检查，地理数据另注明 MaxMind GeoLite2。
- SOCKS5 文本：<https://raw.githubusercontent.com/monosans/proxy-list/main/proxies/socks5.txt>，经本机代理 HTTP 200、14,122 字节。每行 `host:port`，README 还允许 `username:password@host:port`；`all.txt` 则含协议前缀。
- 元数据 JSON：<https://raw.githubusercontent.com/monosans/proxy-list/main/proxies.json>，HTTP 200、967,274 字节，实际为**顶层数组**，共 1,378 条（459 http、123 socks4、796 socks5）。不要套旧版 `{data:...}` 结构。
- 元素字段：`protocol` 字符串、`host` 字符串、`port` **整数**、可空 `username/password`、`timeout` 秒数、`exit_ip` 字符串、`geolocation.country.iso_code` 可选字符串。没有逐条更新时间；文件 ETag 可辅助更新。没有国家要求时优先复用文本解析器；需要 CN 时可用独立 JSON 适配器读取真实行级 `iso_code`，缺失不能默认 CN。
- 现场证据：`monosans-proxies.json`、`monosans-socks5.txt`、`monosans-license.txt` 及 headers。


#### Proxora 与 Rola

[Proxora 官方 API 文档](https://proxora.io/zh/free-proxy-list/api) 提供 `https://api.proxora.io/api/tools/free-proxies`；实际用 `https=true&clean=true&per_page=50` 得到 HTTP 200、`items` 数组，字段为 `host/port/protocol/country_code/latency_ms/uptime/https`。因此默认选官方 JSON API，避免重复抓 HTML；通用 HTML 适配器也覆盖代理单元格嵌套文本结构。API 的 `https=true` 是目标 HTTPS 能力筛选，不能把所有节点拼成 `https://`。

[Rola 免费列表](https://rola-ip.co/zh/tools/free-proxy-list/)本次 HTTP 403。与站大爷、seofangfa 一同登记为停用，保留 URL 与格式配置，恢复后可显式启用；没有执行验证脚本。

#### 本轮实际验收

- 回归先复现：HTTP 200 的 `aliyun_waf` 页面曾被 `decodeCandidates` 接受为空列表；`TestSourceChallengeIsNotSuccessfulEmptyList` 先失败，修复后通过。补充真实结构缩减用例覆盖 Geonode、scdn、monosans、Proxora、齐云 `th` 数据行、France 隐藏凭据、SOCKS5 文本、国家筛选、业务 code、失配结构、退避、独立截止时间、去重和轮换。
- 来源专项联网检查 `CCZJ_OUTBOUND_SOURCES_LIVE=1` **失败并如实保留结果**：11 个启用源中 10 个下载并解析成功，齐云本次 HTTP 403，状态为 `error/http_403`，下次尝试约 30 分钟后；早期页面调查曾为 200，说明当前可用性存在变化。快代理 12、France 22、Geonode CN 64、scdn 两源各 20、SpeedX 4233、monosans 795、Proxora 89、ProxyScrape 361、Databay 6903 个候选。条数含公共地址校验与去重，不表示可用代理数量。本检查没有连接候选代理；原始结果 `dist/audit/outbound-sources/live-sources.log`。
- 默认启用多源的真实代理／豆瓣验收 **通过**：24 候选上限、三路健康检查、临时 SQLite，约 43.30 秒后得到 1 个健康节点；公共尝试 1、本机回退 0，豆瓣 HTTP 200／10 条榜单，重复请求没有新增代理尝试。齐云失败没有阻断其他源。日志 `dist/audit/outbound-sources/live-pool-chart.log`。这只是当次链路结果，不保证之后没有 403，也没有对所有弹幕站点或视频 CDN 做实测。
- 完整 `scripts/verify.ps1` 再次通过：全项目 Go 测试、前端 71 文件／333 测试、类型检查与生产构建、locale／CSS／文档检查。绑定重新生成为 214 methods／164 models，无警告；`go vet` 相关包通过。末次增加小候选预算的跨源轮换后，outbound／service／netstats／netpolicy 再次通过。
- 浏览器复用原验收脚本，以本机 Edge 执行 `--proxy-pool-only --channel msedge`，桌面 1280px 与窄屏 390px 的来源表、403 退避、停用状态、更新失败提示及外层无横向溢出均通过。截图 `dist/audit/outbound-proxy-sources-1280.png`、`dist/audit/outbound-proxy-sources-390.png`。默认 Playwright Chromium 未安装，改用已有 Edge，未另装浏览器；此结果仍属于绑定替身，不是 Wails／Android 原生运行验收。race 仍受上述进程启动错误限制，本轮未宣称通过。未替换用户 EXE 或改写用户现有代理设置。
