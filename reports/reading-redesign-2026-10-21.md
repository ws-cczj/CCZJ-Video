# 小说与漫画前端整改记录

2026-10-21。已修改源码并验证。浏览器截图使用隔离的测试书籍与封面，不是用户收藏，也不是内置推荐内容。Windows Wails 窗口与 Android 原生安装包本轮没有重新安装或验收。

## 本轮交付

- 从项目技能目录移出 `frontend-design`、`ui-ux-pro-max`、`web-design-guidelines`，清理相应锁文件项。备份保存在 `C:/Users/18446/.codex/disabled-project-skills/CCZJ-Video-20261021/`；没有采用这些技能生成设计。
- 小说默认进入书架：小封面书籍行、书名与作者、章节和章内进度、直接续读、收藏与历史切换、书架搜索、排序及显式管理入口。已入库作品仍可从找书页进入浏览。
- 漫画默认进入书库：封面网格、话与页进度、追漫和历史切换、筛选排序。续读与作品详情各有入口。缺失封面使用标题占位，图片仍走现有代理、可见性加载和资源释放链路。
- 两种模式均收敛为三个主入口，并同步移动端滑动导航、页面缓存与设置子页身份。旧书架/书库路由保留，旧“我的”入口转到设置。
- 搜索补齐可见的提交按钮、内容源入口、已入库浏览入口和无源状态；不再在漫画初始搜索页显示无意义的筛选器。
- 调整详情排版、简介、目录与底部阅读动作。漫画补上目录搜索和定位当前话；小说阅读偏好增加实时排版预览。设置分组按需打开，源管理先显示“我的源”，点“添加源”才加载候选目录，导入成功后回到我的源。
- 书架/书库先显示保存快照，校验失败保留已有条目并提示重试；不把传输失败当作书籍不存在。小说续读使用按来源身份解析后的本地作品 ID，旧章节 ID 由现有稳定章节身份恢复。
- 暖白/绿色用于小说，浅灰/蓝色用于漫画。提供深色主题、键盘焦点和窄屏布局。清除 125 条旧首页横幅、旧导航等样式。

## 实际参考

阅读了以下讨论和实现，借鉴任务顺序、信息密度及封面/文字分工，未复制整套产品页面或引入外部 UI 依赖：

- [V2EX：Readest 发布与使用反馈](https://global.v2ex.com/t/1095654)：参考滚动阅读、正文排版及低干扰阅读的需求。
- [V2EX：Moeli 阅读展示](https://us.v2ex.com/t/1204221)：参考小说与漫画分别组织阅读控制的做法。
- [Readest 书籍行源码](https://raw.githubusercontent.com/readest/readest/main/apps/readest-app/src/app/reader/components/sidebar/BookCard.tsx)：参考小封面、标题作者与独立详情动作。
- [Komga 封面卡片源码](https://raw.githubusercontent.com/gotson/komga/master/komga-webui/src/components/ItemCard.vue)：参考固定封面比例、书籍元信息与进度分别呈现。
- [BookLab 设计案例](https://cantika.fun/work/booklab/)：辅助比较收藏到阅读的页面层次。

## 验证结果

| 验证 | 结果与证据 |
| --- | --- |
| 修改前基线 | 59 个前端测试文件，281 项测试通过 |
| 完整验证脚本 | `scripts/verify.ps1` 退出 0；包含前端约定、282 项前端测试、类型检查、生产构建、CSS 审核、`go vet ./...` 与 `go test -count=1 ./...`；[日志](reading-redesign-2026-10-21/verify.log) |
| 最终前端构建 | [日志](reading-redesign-2026-10-21/final-build.log)；存在 Vite 关于未来 native config loader 与无扩展名导入的提示，不影响本次构建 |
| 新界面浏览器检查 | 桌面 1440×1000、手机 412×860，以及 320px 窄屏；浅/深色、收藏和历史、过滤、续读退出、目录搜索、设置预览、离线快照、无源空态、本地 ID 变化后的续读；[结果](reading-redesign-2026-10-21/result.json) |
| 既有阅读回归 | 旧数据导入、目录分页、全库筛选、重载、存储失败与重试、模式隔离、漫画页进度恢复；[结果](reading-redesign-2026-10-21/regression/result.json) |
| 使用路径回归 | 添加源后首页刷新、翻页后退出、单击添加、普通与高级操作分离；[结果](reading-redesign-2026-10-21/usability/result.json) |
| 真实内容链路 | 生产 Go 服务 + 临时 SQLite + 浏览器替代 Wails RPC：候选与导入、酷我/番茄正文、瓜子漫画图片实际解码、进度入库；[结果](reading-redesign-2026-10-21/live-sources/result.json) |

测试修正保留了原断言强度：旧脚本没有等待重载后的路由初始化，并使用不同的 runtime 模块及取消后再次拒绝的替身，已修正替身与等待条件；源候选的内置检测证据为 10 月 19 日，原脚本始终断言 healthy 与 24 小时过期规则冲突，现按证据时间验证状态。没有放宽可读正文、图片解码、存储错误或数据保留断言。

## 页面预览

- [手机小说书架](reading-redesign-2026-10-21/mobile-novel-shelf.png)
- [手机漫画书库](reading-redesign-2026-10-21/mobile-comic-library.png)
- [桌面小说书架](reading-redesign-2026-10-21/desktop-novel-shelf.png)
- [桌面漫画书库](reading-redesign-2026-10-21/desktop-comic-library.png)
- [漫画详情](reading-redesign-2026-10-21/mobile-comic-detail.png)
- [小说排版设置](reading-redesign-2026-10-21/mobile-novel-preferences.png)
- [小说深色界面](reading-redesign-2026-10-21/mobile-novel-dark.png)
- [漫画深色界面](reading-redesign-2026-10-21/mobile-comic-dark.png)

## 验证边界

### 2026-10-22 小说交互及高级设置修复补充

- 高级设置改为独立卡片，共用 Select；移除原生下拉框和冗长默认展开说明。桌面及移动宿主呈现的浏览器截图均检查过，移动验收使用实际 CczjMobileShell，不把缩窄的桌面侧栏当作手机页面。
- 小说目录共用筛选／定位／排序工具栏，去重复章号，静默刷新、正倒序、保留卷标题与回顶；搜索返回保留结果与位置，输入框只有一个清除入口和外框焦点提示。
- 小说分页和同书切章不使用淡入淡出；桌面滚轮、固定快捷键、自定义前后键和 F／Esc 沉浸阅读均有实际交互回归。全屏实现提取到 platform 后仍通过既有播放器浏览器回归。
- 本机日志中的阅读写入错误实际为 SQLite WAL 写锁升级失败；新增测试复现旧代码立即报 SQLITE_BUSY，修复事务顺序后等待并成功保存。未隐藏真实存储失败。
- 全拼检索只转换唯一已知书名，复用分模式语料及本地书目；`xiyouji` 到“西游记”的真实 Go 搜索调用链已用本地 HTTP 源验证，浏览器验证中文结果不会被拼音相关性过滤。此项不是外部源永久可用性证明。

完整 `scripts/verify.ps1` 通过：67 个前端测试文件、323 项测试，前端生产构建、CSS／文档校验、全量 Go vet 和测试。`scripts/test-novel-interactions.py` 的 12 项浏览器交互检查通过；原始结果及截图在 `dist/audit/novel-usability/`，验证日志在 `dist/audit/novel-usability-*.log`。Windows EXE／便携 ZIP 和 Android ARM64 Release APK 已重新构建到 `dist/test-builds/20261022-novel-usability/v2.4.0/`，产物版本、ABI、签名身份及 SHA256 校验通过；未上传 GitHub。本轮没有运行新的 Windows 原生宿主操作或华为真机 APK 验收，不能把浏览器与构建结果当作这两项结论。

2026-10-22 滚动阅读补充：小说未设置阅读方式时默认上下滚动，已有横向偏好继续保留。滚轮和触屏均支持在章节边界继续翻章，章节末尾保留可见按钮。阅读入口返回当前章及前后章的小窗口，目录面板首次打开才取完整目录；正文沿用磁盘缓存并增加有容量、条数和有效期上限的实例内副本，回看减少正文 IPC。10,000 行目录测试中阅读窗口 JSON 为 827 字节；同时复现并修复目录批量写入的 SQLite 参数超限。新服务实例命中同一磁盘正文缓存的 Go 测试通过。

`scripts/test-novel-interactions.py` 增加默认滚动、前后章边界、回看正文请求计数和懒加载目录检查，15 项浏览器检查通过。MuMu Android 15 模拟器的独立 acceptance 包通过真实番茄源“部族荣光”的搜索、正文、横竖屏排版、可信触屏跨章及回到上一章末、SQLite 进度恢复、磁盘缓存与小窗口原生绑定；没有替换源站响应，没有清理用户 preview 包数据。证据在 `dist/audit/novel-scroll/`。模拟器不代表华为鸿蒙真机结论。

2026-10-22 设置内运行诊断布局补充：独立诊断页统一为居中内容区，移除与模式导航重复的返回按钮；标题、操作栏和共享 Segment 分区形成清晰层级。概览保留关键指标，把资源口径、采集范围和浏览器所有权说明折叠显示；共享 Button 继续承担刷新、记录和导出的真实操作。窄屏调整操作区与指标卡，配色跟随小说／漫画。修复共享 Segment 选中项在 hover 时被普通文字颜色覆盖的问题，并添加 aria-pressed；浏览器逐项验证两模式、1280／375／320px 与浅／深主题，选中项 hover 后对比度不低于 4.5。

重复验证用 `scripts/test-reading-diagnostics.py --port 19320`，Android 验证用同脚本 `--native --device emulator-5554`。后者在 MuMu Android 15 的独立 acceptance 包里使用实际 Go 服务和可信触屏输入检查分区、存储、说明展开及刷新，没有替换响应；该结论不代表华为鸿蒙真机。原始截图、结果及构建日志归入 `dist/audit/settings-layout/`，完整前端 68 文件／327 测试及生产构建通过；CSS 复用、编辑器数据和文档链接校验通过。

这轮交付前端结构、视觉和相关交互修复。真实源验证是指定作品在当前网络下的样本，不代表所有源、所有作品永远可读。完整任意源适配、小说按文本锚点重排恢复、漫画缩放/适屏能力，以及 Windows/Android 原生端验收仍需独立推进；本轮没有把这些未完成项标为完成。
