# Android 播放器：刘海白边、倍速侧栏与画中画

2026-10-21。用户报告视频全屏左侧白边、画中画提示不支持、倍速未从侧边展开，补充设备使用鸿蒙 4。未提供具体机型，也未连接华为实机。

## 2026-10-22：长按临时倍速及屏幕提示

反馈为按住画面不加速、没有“正在 X 倍速播放”提示。修复前单元测试按下 700ms 后速率仍为 1.25，能够稳定复现。原移动手势只有滑动进度／音量／亮度，未实现静止长按；桌面 OSD 也不会在手机显示。

沿用 `useMobilePlayerGestures` 的单指所有权：播放中在非控件区域按住 450ms 后临时使用至少 2 倍速，原设定高于 2 倍时不降低；整个长按期间由现有移动反馈层显示实际倍速和“松手恢复”。松手恢复原选定速率，不切换暂停，也不把临时值写成偏好。位移达到滑动阈值、锁屏、暂停或未就绪不会启动；取消触摸、失去指针捕获、页面隐藏／失焦、暂停／结束／换片、全屏变化与销毁均清理计时和临时速率。

真实触摸下还检查了异步事件与加载行为：计时器保留根元素而不是稍后已清空的 `event.currentTarget`；子视频隐式捕获转交父层时的 `lostpointercapture` 不误取消父层手势。普通选定倍速同时同步 `defaultPlaybackRate`，防止切集的 `load()` 把已选 1.5 倍重置为 1 倍；临时长按不修改该默认值。提示复用移动反馈过渡、Icon、CCZJ 工具类及中英文文案。

验收复用真实 VideoPlayer 与本地循环 WebM，使用 CDP 真实触摸，不用直接调用组件手势来代替输入：

```powershell
python scripts/test-player-hold.py --url http://127.0.0.1:19555
python scripts/test-player-hold.py --android --device emulator-5554
```

Android 使用 `build/android/dev.ps1 -Device emulator-5554 -Port 19555` 将当前前端装入已有 Debug 宿主。浏览器与 Android 15 / MuMu WebView 均验证短按、持续长按、提示保持、松手恢复且不暂停、偏好不变、触摸取消、暂停取消、横屏全屏长按、退出全屏及换集。测试保存并恢复预览包的播放器偏好和原页面，不改来源或书架。原始结果和截图在 `dist/audit/player-hold/browser`、`dist/audit/player-hold/android`；12 项手势单元回归覆盖其他取消和释放路径。本次未重建正式 APK／EXE，未声称鸿蒙实机通过。

本轮完整 `scripts/verify.ps1` 通过：66 个前端测试文件、319 项用例、类型检查、生产构建、全仓 Go 测试／vet、样式复用与文档链接检查。原 `scripts/test-player-browser.py --channel chrome --port 9247` 的播放、切线／换集、全屏、资源释放和 Film WebGL 等回归也通过。输出分别为本轮证据目录下的 `verify.log` 和 `player-browser.log`。

## 复现与修复

回归命令：`python -u scripts/test-mobile-player-regressions.py --output reports/mobile-player-fixes-2026-10-21/after-cutout`。使用独立验收包、已有验收源、本地 H.264/AAC 视频、真实 Go/Java/WebView 与可信触摸。通过 Android 模拟刘海覆盖配置复现，结束后恢复模拟刘海设置；不操作用户预览包数据库。

| 问题 | 修复前证据 | 处理与修复后证据 |
| --- | --- | --- |
| 全屏左侧白边 | 模拟 tall 刘海时，1920px 截图左侧有 144px 浅色条；网页只有 592×360 CSS px | 全屏窗口允许覆盖短边刘海，移除浏览容器的系统区填充，设置黑色背景并在退出时恢复。viewport-fit=cover；原生安全区同步到 CSS，控件避开刘海。回归窗口为 640×360，白条消失 |
| 横屏倍速底部弹窗 | 对话框为 x=16/40、y=270、底部短面板 | 全屏走右侧抽屉、竖屏保留底部面板；选项进入抽屉正常文档流。右侧范围 x=426.67～640、y=0～360，实际选到 1.5x，媒体元素未重建 |
| 画中画误判与失败反馈 | 平台构造时缓存一次 native PiP 结果；首次桥接未就绪会一直不支持。原生权限拒绝此前只有失败状态，没有明确展示 | 每次重新探测；音频/亮度/厂商 AppOps 探测失败不吞掉独立 PiP 能力；权限关闭与硬件不支持分开，只有 MODE_ALLOWED 视为已授权。原生拒绝事件有展示并清理 PiP 页面状态，监听由播放器释放 |

最初 PiP 脚本误命中同一区域的弹幕开关，已更正为直接子级的画中画按钮。此前相关失败不作为产品 PiP 故障证据。正确入口在 MuMu 的修复前、修复后都能进入系统 pinned task。

中途带刘海复测出现原生入口返回 false，曾怀疑旋转时序。继续排查确认验收包 AppOps 为显式 `default`：系统只接受 MODE_ALLOWED，早期补丁错误地把 MODE_DEFAULT 也算作允许。这是本次权限补丁及测试环境的问题，不能据此认定华为用户的原始故障根因。新增 `default` 原生回归先失败再修复；撤掉无证据的旋转延时。最终测试在独立验收包临时设置 allow、ignore、default，结束恢复原模式。

## 验证

- 前端 62 文件 / 288 项测试通过；生产构建、类型检查及前端约定检查通过。
- 新增单元回归先复现“启动时读不到原生能力、随后能力就绪仍不支持”，修复后通过；权限拒绝不调用 native、旧桥接兼容分别验证。
- Android 15 / MuMu amd64 验收包在普通屏幕、tall 模拟刘海两轮实际运行均通过：全屏无白条、安全区控件边界、右侧倍速选择、系统 pinned task、画中画持续播放、保留同一 video 元素。每轮五项检查全绿；[普通屏幕结果](mobile-player-fixes-2026-10-21/after-normal/checks.json)。
- 使用 Android AppOps 临时设置 ignore/default，原生能力仍报告支持、权限报告未允许；真实触摸后正确显示“画中画权限未开启”，播放器恢复；测试结束恢复原权限、模式、路由和倍速偏好，不清库。
- 修复前：[刘海截图](mobile-player-fixes-2026-10-21/before-cutout/fullscreen.png)、[倍速截图](mobile-player-fixes-2026-10-21/before-cutout/speed.png)。修复后：[全屏](mobile-player-fixes-2026-10-21/after-cutout/fullscreen.png)、[倍速侧栏](mobile-player-fixes-2026-10-21/after-cutout/speed.png)、[PiP](mobile-player-fixes-2026-10-21/after-cutout/pip.png)、[检查结果](mobile-player-fixes-2026-10-21/after-cutout/checks.json)。
- amd64 验收 APK、ARM64 手机 APK 构建成功；手机包通过 apksigner v2 验签、aapt 确认 arm64-v8a。交付文件：`dist/mobile-player-fixes-20261021/CCZJ-Video-android-arm64-2.3.2-preview.apk`，SHA256 `cc5ebf856f46dd38053cb64bc27d7af205c903c477d0c01722ad7927a7c10287`。Windows 原生 UI 未在本轮运行。

## 鸿蒙范围

鸿蒙 4 按当前 Android APK 路径处理，没有擅自切换成 HarmonyOS NEXT 的 ArkTS/HAP 接口，也未增加悬浮窗权限或隐藏厂商 API。当前可证明的是检测时序和权限处理的修复，以及 Android 模拟器行为；不能声称华为实机已通过，或用户实机根因一定就是启动探测失败。需要具体机型或实机诊断才能进一步确认标准 Android PiP 是否开放。

调研仅采用官方依据：[Android PiP](https://developer.android.com/develop/ui/views/picture-in-picture)、[AOSP 设备 PiP 能力](https://source.android.google.cn/docs/core/display/pip?hl=en)、[华为关于悬浮窗与画中画的说明](https://consumer.huawei.com/cn/support/content/zh-cn01040466/)。华为说明不能据此推断所有鸿蒙 4 机型对标准 Android PiP API 的支持。

AppOps 判据核对：[AOSP ActivityRecord.checkEnterPictureInPictureAppOpsState](https://android.googlesource.com/platform/frameworks/base/+/master/services/core/java/com/android/server/wm/ActivityRecord.java) 只接受 MODE_ALLOWED；能力和权限探测保持独立。
