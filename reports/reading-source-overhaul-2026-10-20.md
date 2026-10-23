# 小说、漫画源管理整改与真实数据验收

> 2026-10-20 复核说明：本文为历史实施与样本验证记录。用户后续实际使用仍报告返回恢复、源兼容与页面可用性问题；本文的“通过/完成”不得作为整体整改完成的证明。后续工作见 [小说与漫画整改任务书](history/reading.md#origin-reading-redesign)，实际应用修复尚待实施和验证。

日期：2026-10-20（北京时间）。本轮处理源管理复杂、只能看到三个可导入源、导入后首页仍显示无源，以及小说翻页后返回按钮不可点击的问题。此前阅读器修复记录保留在 [小说问题检查](novel-bug-audit-2026-10-19.md)。

## 已实现

- 源管理分为「源库」「已添加」，单个源可直接添加，也可多选导入。链接、文件导入与手工配置保留在次要入口。默认列表隐藏不必要的搜索和高级筛选；完整库可搜索名称/域名、按项目和检测状态筛选、排序、加载更多。筛选或翻页不清空已经选择的源。
- 默认只展示已经适配的源。完整源库使用随版本携带的快照，离线即可打开；联网更新为主动操作。没有三源安装上限，后端单批 100 个，前端自动分批。
- 可见状态区分完整链路通过、部分通过、尚未检测、检测过期、失败及待适配。排序优先采用 24 小时内的完整链路结果；详情展示分数、耗时、阶段、样本数、时间和原始来源。本机检测覆盖版本携带记录；外部 JSON 不能自报高分。
- 小说、漫画首页以及「我的」页返回时重读源状态，旧的在途请求也会被替代。已添加但全部停用时明确提示启用源，不显示为没有添加。
- 小说和漫画在工具栏隐藏时保留 44px 返回入口；翻页后仍可点击。MuMu WebView 110 的颜色、边框兼容样式也已验证。
- Legado 静态转换增加相对选择器链、节点索引、正则替换、全部正文段落拼接以及明确的漫画图片规则。CSS 在导入时校验；不支持的脚本、XPath、登录流程和扩展二进制显示原因。
- 适配 Mihon 新版 JSON 索引结构，新增崩坏3官方漫画和泰拉记事社专用请求流程。泰拉图片链接按当前接口生成，页表保持顺序，并通过现有图片代理保留请求上下文。

## 源数量与可用情况

生产后端对完整快照解析、去重后的数量（[机器记录](reading-usability-2026-10-19/source-library-stats.json)）：

| 模式 | 完整库候选 | 已适配、可导入 | 完整链路样本通过 | 部分通过 | 失败 | 待适配 |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 小说 | 2,801 | 43 | 7 | 4 | 32 | 2,758 |
| 漫画 | 1,506 | 4 | 4 | 0 | 0 | 1,502 |

候选数量不代表可读数量。40 个新增小说规则逐一进行了真实网络检测，结果为 4 个完整通过、4 个部分通过、32 个失败；另外三个原有已适配小说源也完成复测。快照保留失败原因，用户可以在自己的网络环境重新检测。两个英文小说入口和一个专书入口属于完整库中的通过样本；英文入口复测耗时约 54 秒，未列为默认推荐。

默认的八个源均通过真实搜索、详情、目录和正文／图片验证，并经过实际导入后的存储及阅读调用：

| 模式 | 默认源 |
| --- | --- |
| 小说 | 酷我小说、阅友小说、番茄小说、平凡文学 |
| 漫画 | 瓜子漫画、咚漫漫画、崩坏3官方漫画、泰拉记事社 |

单次样本通过不等于长期稳定；检测记录超过 24 小时会显示过期。当前仍未实现任意 Legado 脚本或 Mihon APK 的通用执行能力，这些候选会显示待适配，不能作为可用源安装。

## 验证证据

| 环境 | 结果与边界 |
| --- | --- |
| 完整仓库检查 | `scripts/verify.ps1` 退出 0；53 个前端测试文件、238 项测试通过，类型检查、构建、架构检查及全部 Go 包检查通过。[日志](reading-usability-2026-10-19/verify.log) |
| 真实网络后端 | 8 个推荐源完整通过，含实际导入后搜索、存储目录、正文和页表读取；漫画首图经过实际内容校验。[样本](reading-usability-2026-10-19/live-recommended.json)、[日志](reading-usability-2026-10-19/live-recommended.log) |
| 新增静态规则网络检测 | 40 个候选逐个检测，失败也保留。[记录](reading-usability-2026-10-19/static-source-live.json)、[英文复测](reading-usability-2026-10-19/english-recheck.json) |
| 浏览器、真实 Go 服务 | 源排序、完整库筛选、选择导入、检测、真实小说正文、漫画图片解码、320/360/412/768px 布局通过；仅替换 Wails 传输，使用临时 SQLite。[结果](reading-source-browser-2026-10-19/result.json) |
| 浏览器回归 | 缓存首页导入后刷新、翻页后实际点击返回、单个源添加、已添加页隐藏高级操作，共 4 组通过。[结果](reading-usability-2026-10-19/result.json)；[修复前失败记录](reading-usability-2026-10-19/baseline.json) |
| MuMu Android 15 | 安装独立 `com.cczj.video.acceptance`，WebView 110，真实 Go、SQLite、网络及触摸事件；超过三个源的列表、添加后返回缓存首页、离线完整库、真实番茄正文及分页后点击退出、官方漫画图片解码及退出，共 5 组通过。[结果](reading-usability-2026-10-19/android/result.json) |
| Windows | 可执行文件构建及版本资源检查通过；本轮没有启动新的 Windows 原生窗口做交互验收。[构建日志](reading-usability-2026-10-19/windows-build.log) |

Android 使用独立验收包，没有清理或覆盖用户预览包的数据；验收包中的测试内容保留。没有安装本次生成的预览 APK 到用户预览应用，也没有发布、推送或提交仓库改动。

## 快照来源与更新

小说：[aoaostar/legado](https://github.com/aoaostar/legado)，输入 [b778fe6b.json](https://raw.githubusercontent.com/aoaostar/legado/master/sources/b778fe6b.json)，20,987,130 字节，SHA-256 `a50c80e81e45a6ad77f6cdb94ab420e74de29691f8924692415a3e9f0e022747`。

漫画：[Keiyoushi](https://keiyoushi.github.io/docs/guides/getting-started)，输入 [index.json](https://raw.githubusercontent.com/keiyoushi/extensions/repo/index.json)，1,499,683 字节，SHA-256 `3951cdb38bdbec4b78a52d4747684523872de92100c1ad06dfbe7b15d6201ff2`。旧 `index.min.json` 已变成升级提示占位，不能再作为完整索引使用。站点请求流程参考对应官方扩展源码，来源链接保存在每个候选中。

原始小说集合含 3,907 项。快照生成器只保留元数据和可转换的静态文档，不包含源中的脚本内容、APK 或原始正文。相同来源身份的重复项在预览时合并，因此原始集合、快照行数与最终界面候选数不同。

维护者设置 `CCZJ_CATALOG_INPUT`、`CCZJ_CATALOG_OUTPUT`、`CCZJ_CATALOG_ORIGIN` 后，可运行 `go test ./app/library -run TestBuildSourceLibrarySnapshot -count=1 -v` 重建快照；质量记录必须来自真实检测，不能从远程自报分数生成。

## 本次构建

- [Windows 可执行文件](../bin/cczjVideo-reading-fix.exe)
- [MuMu / Android x86_64 预览 APK](../bin/CCZJ-Video-android-amd64-preview.apk)
- [独立 Android 验收 APK](../bin/CCZJ-Video-android-amd64-acceptance.apk)
- [Windows 便携包目录](../dist/reading-fix-20261020)
- [文件哈希](reading-usability-2026-10-19/artifacts.json)

截图：[漫画源简化界面](reading-source-browser-2026-10-19/comic-sources-mobile.png)、[Android 完整源库](reading-usability-2026-10-19/android/novel-source-library.png)、[Android 小说分页返回](reading-usability-2026-10-19/android/novel-paged-exit.png)、[Android 官方漫画正文](reading-usability-2026-10-19/android/comic-real-reader.png)。
