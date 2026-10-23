# 共享网络请求约束收尾

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
