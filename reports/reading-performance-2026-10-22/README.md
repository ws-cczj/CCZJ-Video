# 小说与漫画 Android 阅读验收

2026-10-22。已完成原生 MuMu 预检与问题修复；**实体手机未连接，鸿蒙 4 真机验收尚未完成**。ADB 最终只枚举到 `emulator-5554`。设备虽然返回 HONOR 型号指纹，但这是 MuMu，不能据此写成荣耀手机或鸿蒙测试。

## 测试方法与范围

在独立包 `com.cczj.video.acceptance` 中使用真实 Go 绑定、SQLite、源搜索、正文及图片代理。没有替换网络、来源或章节响应，没有清除预览包或用户数据库。交互使用 WebView 的可信触摸事件、输入和 Android Back；等待真实 click 送达后才操作下一个控件。

分别搜索小说《部族荣光》和漫画《逃离长空篇》（搜索词“崩坏”），进入作品详情和阅读器。最终回归每种模式包括 30 次页内翻阅、10 次滚动手势、4 次设置面板开关、下一章／话及返回上一章／话、4 轮重新进入及退出阅读器。翻页先前进一段再后退，不只在两张页之间切换。已有缓存保留，不能把这些数据当作首次安装或断网测试。

连续记录 WebView rAF 回调间隔、50ms 及以上长任务、页面布局移动、路由场景可见性、DOM 计数，并保存 `dumpsys meminfo`、进程身份、退出原因和本轮 crash buffer。`gfxinfo` 在此 MuMu 返回零帧及无效默认分位值，因此不据此声称硬件 60 FPS。rAF 也不是屏幕呈现帧率。

## 发现并修复的问题

两个阅读器新增换源入口后，底部四个按钮仍使用三列网格，“阅读设置”被挤到第二行。设备布局断言在修复前明确失败：前三个按钮 top=482，第四个 top=554；源码中两个阅读器的列数均为 3，与截图一致，因此直接修正对应布局，无需猜测渲染性能原因。

- 小说：`frontend/src/novel/styles/reader.css` 改为四列。
- 漫画：`frontend/src/comic/styles/comic.css` 改为四列。
- 回归检查四按钮同排、没有横向溢出，触控宽度至少 44 CSS px。

[修复前小说](toolbar-before/novel-toolbar.png) · [修复后小说](after-fix-verified/novel-toolbar.png) · [修复前漫画](toolbar-before/comic-toolbar.png) · [修复后漫画](after-fix-verified/comic-toolbar.png)

Android 构建还暴露 `golang.org/x/image` 已在 go.mod 声明、但 go.sum 缺少校验记录的问题。补齐锁定版本的校验值后，x86_64 和 ARM64 验收 APK 均成功构建。没有升级该依赖版本。

## 结果与实际限制

最终证据以 [after-fix-verified/result.json](after-fix-verified/result.json) 和 [汇总](summary.json) 为准：两个模式的功能与布局断言通过，应用进程身份未变，JavaScript 异常列表和本轮 crash buffer 为空。翻页阶段两种模式的 rAF 回调间隔 P95 均约 16.7ms，没有记录到长任务；翻页流程包含动画完成或当前漫画图片就绪检查。

这些结论限于本轮样本与模拟器。此前带录屏的扩展轮出现过 50ms、55ms 长任务；不录屏对照及最终回归未稳定复现，不能宣称所有操作永远零卡顿。初次重装后的详情加载还记录过一次较长任务，保留在 `after-fix/`，最终暖缓存回归不能替代冷启动、低端手机与不同 WebView 的测试。

路由采样未出现连续 100ms 没有可见场景的情况；这只是场景几何检查，不等于逐像素证明动画完美。录屏参考在 [扩展轮](mumu-final/session.mp4)，其中仍有修复前的三列底栏。当前未发现需要修改动画实现的稳定复现。

内存保存的是应用主进程 PSS，不能当成包含 WebView 隔离渲染进程的总内存。重复进入阅读器后 DOM 计数曾随自然 GC 回落；短时测试不能证明不存在长期泄漏。CPU 快照仅供观察，没有据此设定性能结论。

历史验收数据包含几个已失效的测试源，搜索界面如实显示失败；实际作品仍由真实可用来源返回。初始脚本在文档重载、输入焦点、跨章就绪、返回时仍存在的离场 DOM 及触摸事件送达判断上存在问题，相关失败轮保留，不计作通过，也没有据此修改应用业务行为。

## 可重复执行

先安装匹配的验收包，然后运行：

```powershell
python scripts/test-reading-performance-android.py --device emulator-5554 --environment emulator --output reports/reading-performance-new-run --turns 30 --reader-cycles 4
```

实体手机连接并授权 USB 调试后，使用实际序列号与 `--environment physical`。脚本会拒绝将明显的模拟器标记为实体手机。需要已安装验收包、可连接的调试 WebView、与已安装包一致的 frontend/dist，以及 `scripts/native-profile-requirements.txt` 中的依赖。

- [手机 ARM64 验收包](../../bin/CCZJ-Video-android-arm64-acceptance.apk)：已构建，尚未在实体手机安装或运行。
- [MuMu x86_64 验收包](../../bin/CCZJ-Video-android-amd64-acceptance.apk)：已安装并执行上述回归。

本次同时通过前端类型检查、生产构建、前端约定检查和 Python 语法检查。没有重新执行整个仓库的 Go／前端单元测试，也未测试 Windows EXE 运行。
