# 前后端请求通道与性能整改

2026-10-20。本轮保留工作区已有修改，在真实调用入口落实策略、身份、取消与有界执行；没有提交或发布版本。基线为 54 个前端测试文件、244 个测试通过，相关 Go 包通过。

## 查到并修复的问题

1. 前端直接重导出生成绑定，同时读取同一个设置会派发两次 IPC。已通过真实绑定适配层测试复现，再统一接入请求管道。
2. 重复读取缺少订阅者归属；简单共享一个可取消 Promise 会让一个页面取消另一个页面的工作。现每个订阅者独立取消，最后一个订阅者离开才取消实际绑定。
3. 写入前后的读取没有统一隔离。现写入派发与完成都推进代次，后续读取不会加入旧请求；共享结果保留模型原型并独立复制，避免页面之间互相修改 DTO。
4. 视频探测读取全部模式的源。回归测试复现小说、漫画混入视频探测时间线，现批量与单源入口都约束视频来源。
5. 取消前已给排队源登记冷却，实际没有探测也会等待五分钟。现只在工作协程领取任务时登记，已取消请求不再占用尚未执行任务的冷却机会。
6. 视频探测虽然限制同时 I/O 数量，却仍为每个源启动协程；线路测速则直接按线路数量启动并发。现都使用最多 4 个工作协程，并在取消后停止派发、等待已开始的任务结束。
7. 源页离开只停止计时器，既不取消请求，也会在旧请求的 finally 中再次读取时间线。现取消订阅、校验代次并阻止隐藏页面启动后续读取。
8. 视频源搜索进度没有查询身份，同源同词重试仍可能串入旧进度。新增 `SearchSourceRequest`，旧入口保留；进度匹配 requestId，离开取消，重新进入恢复未完成搜索。
9. 视频列表只丢弃旧响应，未取消实际绑定。现切换查询、休眠与销毁均释放旧请求，并保留迟到结果校验。
10. 视频搜索、豆瓣搜索/详情、更新检查和 DLNA 发现存在后台上下文，前端取消后仍可能继续 I/O。现传递请求 context 到 HTTP、限速等待、验证计算、UDP 与后备请求；用户取消不触发豆瓣反爬退避，也不继续遍历更新后备源。
11. 批量采集状态逐源发 IPC，且每次复制整份状态字典。新增最多 256 个源的批次接口，去重后顺序分批；前端按键更新，保留旧单源入口。
12. 小说目录每行通过 findIndex 遍历全部章节判定高亮。改为直接比较当前章节 ID，保留原有交互和样式。

## 通道约束

当前 199 个 App 绑定均在 `frontend/src/api/requestPolicies.json` 显式声明策略。调用携带固定 `rpc.方法名` 通道、独立 requestId 与可共享的 flightId。新绑定未分类或适配文件过期会使生成与验证失败；API 目录内也不能绕过入口导入原始绑定。

读取只合并在途工作，完成即移除，不增加结果缓存。最多容纳 128 个不同的读取工作；已有工作仍可加入。写入独立提交，不自动重试、合并或回滚。流式搜索保留独立事件归属，大型压缩参数不进入去重键。策略数量见 [policy-summary.json](request-pipelines-2026-10-20/policy-summary.json)。

Go 操作保留旧 operation 名称并添加相同的通道名与上下文身份。图片、HLS、Android 媒体 HTTP 返回通道和请求标识头。Wails 原生 call-id、前端订阅 ID、Go 操作 ID 各自保留职责；它们不是一个统一的跨进程 trace ID。没有额外增加逐请求的追踪 IPC。

详细约定及边界见 [请求通道与取消](../docs/request-pipelines.md)。前端导出的管道数值统计覆盖当前文档所有模式，Go 操作统计继续按模式筛选，不保存请求参数、正文或地址。

## 可复现的收益

| 场景 | 原实现 | 当前实现 | 验证性质 |
| --- | --- | --- | --- |
| 64 个相同并发读取 | 每调用发送一次 | 1 次派发、63 次加入、64 个独立订阅 ID | 浏览器真实适配器，替换 IPC |
| 600 个唯一源状态 | 逐源 600 次 IPC | 3 批：256、256、88 | store 行为测试；输入重复时也先去重 |
| 600 个源键重复输入两遍 | 测试观察到 1200 次调用 | 仍为 3 次 | 修复前失败、修复后通过 |
| 5000 章的目录高亮判断 | 12,502,500 次比较 | 5000 次比较 | 相同输出的表达式基准，O(n²) → O(n) |
| 10000 个探测调度任务 | 启动 10000 个协程 | 最多 4 个工作协程 | 调度基准与并发测试 |

Go 调度微基准连续 3 轮：10000 个空任务从约 6.04–6.12 ms 降至 0.173–0.174 ms，分配从约 264–301 KB、10215–10375 次降为约 456 B、12 次。源码来源是旧探测派发模式与新 Each。它衡量调度开销，不代表网络整体提速 35 倍。

目录表达式基准在 Node v24.18.0 下，5000 章中位计算时间约 8.22 ms → 0.042 ms；10000 章约 33.30 ms → 0.084 ms。排除了 DOM、布局与网络，不能当作完整页面渲染耗时。

复现与原始数据：

```powershell
go test ./app/parallel -run '^$' -bench . -benchmem -count=3
node scripts/benchmark-request-algorithms.mjs
```

[协程基准](request-pipelines-2026-10-20/worker-benchmarks.log)、[目录基准](request-pipelines-2026-10-20/directory-benchmarks.json)。

## 验证

- `scripts/verify.ps1` 成功：架构检查、199 个请求策略检查、前端 58 个文件 / 259 个测试、类型检查、生产构建、CSS 检查、Go vet 与全量 Go 测试通过。见 [verify.log](request-pipelines-2026-10-20/verify.log)。
- 新增 Go 取消测试确认实际 HTTP/UDP 收到取消、更新不继续后备请求、豆瓣不误记反爬；批次上限、去重、固定工作协程、视频源隔离及上下文身份测试通过。
- 浏览器 8 组请求管道检查通过：缓存页面取消、隐藏后没有时间线请求、重新激活、64:1 合并、替换搜索取消、进度身份过滤、源搜索恢复、视频搜索取消及恢复。使用真实组件与 API 适配器，仅替换 IPC。见 [browser-result.json](request-pipelines-2026-10-20/browser-result.json)。
- 设置页、阅读可用性和视频播放器浏览器回归通过。真实 Go 服务与临时 SQLite 的源选择、质量排序、导入、小说正文及漫画解码链路通过。见 [真实源结果](request-pipelines-2026-10-20/live-reading-result.json) 与同目录日志。
- MuMu Android 15 / WebView 110，独立 `com.cczj.video.acceptance` 安装本轮包，6 组原生链路通过：四个小说源选择导入、编辑器真实触摸、缓存首页刷新、2801 个源候选排序、番茄真实正文/翻页/退出、官方漫画图片解码及退出。见 [Android 结果](request-pipelines-2026-10-20/android/result.json)。未清理或更新用户的 preview 应用数据。
- Android 诊断工具已适配编译后的策略包装入口，避免验收时绕过管道；两个工具回归测试通过。

竞态检测未通过执行环境启动阶段：本机 `go test -race` 的二进制以 `0xc0000139` 退出。已检查导入表和本机 DLL 导出，缺少 `kernel32.dll` 的 `WaitOnAddress`、`WakeByAddressSingle`、`WakeByAddressAll` 入口。见 [race 日志](request-pipelines-2026-10-20/race-tests.log) 与 [加载检查](request-pipelines-2026-10-20/race-loader.json)。不能宣称 race 通过；常规并发/取消测试通过。

Windows 原生 UI 与实体 Android 手机未运行；Windows 已构建并检查发布资源，桌面交互由浏览器替身验证。DLNA 取消由本地 UDP 测试验证，没有声称与实际电视完成投屏。

## 安装产物

- [Windows EXE](../bin/cczjVideo-request-pipelines.exe)
- [Android preview APK](../bin/CCZJ-Video-android-amd64-request-pipelines-preview.apk)，已构建，未覆盖安装到用户 preview。
- [Android acceptance APK](../bin/CCZJ-Video-android-amd64-request-pipelines-acceptance.apk)，MuMu 本轮验收使用。

大小和 SHA-256 见 [artifacts.json](request-pipelines-2026-10-20/artifacts.json)。未上传或发布。
