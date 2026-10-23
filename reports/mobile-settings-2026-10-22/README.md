# 移动端视频设置检查与修复

检查环境：MuMu Android 15、x86_64、独立验收应用 `com.cczj.video.acceptance`。没有清除用户预览应用的数据，没有发送导出文件或确认安装。鸿蒙 4 真机未连接。

## 复现与原因

原生检查确认扩展包页展示“打开目录”、私有文件夹路径和从资源管理器拖放安装的说明；备份导出后出现“打开所在文件夹”。前端渲染回归先出现失败，记录见 `unit-before.log`。原生基线与页面文本分别保存在 `before-native/`、`before.json`。

桌面窗口设置已有平台判断，但扩展和备份复用了桌面操作。Android `WebChromeClient` 没有处理 HTML 文件选择回调，因此导入备份、背景图片选择无法正常打开系统选择器。关于页原先隐藏检查更新，全局更新组件也只在桌面挂载；接通按钮后又复现了弱网连续重试期间长时间等待。

## 修复

- 扩展包：移动端保留启停、卸载和刷新，移除打开目录、复制私有路径、文件夹拖放入口；内置扩展使用适合手机的功能说明。
- 备份：导出后提供系统分享；导入使用系统文件选择器；恢复设置后提示关闭重开应用，移除桌面重启按钮。
- Android 文件输入：独立管理 HTML 选择回调，与 Wails 原生选择器区分请求码；取消、重复打开和 Activity 销毁会释放回调，仅接受外部 content URI。
- 文件分享：仅在既有日志／诊断导出之外，增加应用导出的指定备份文件；使用只读 URI。系统分享窗口启动失败会反馈给前端。
- 主题：手机文案改为点击选图和次级背景，不再提示鼠标拖放或侧边栏。
- 主题卡片的编辑／删除按钮在手机常显，修复原先必须悬停才出现的问题。日志导出按钮组在窄屏换行，避免分享按钮出现后溢出。
- 下载设置：移动端由应用管理存储，不再显示手填桌面文件路径的控件。
- 更新：手机挂载全局更新组件，显示检查按钮和加载状态；30 秒触发取消，失败后可重试。保留忽略版本恢复，隐藏桌面退出安装、重启及回滚操作。

## 验证

`scripts/test-mobile-settings-capabilities.py` 在 320、360、412 CSS 像素宽度遍历 36 个设置页面，共 113 项检查通过，核对布局边界、桌面操作缺席、诊断横向导航可达，以及更新响应、超时反馈、导入选择器重复打开／取消、备份／日志／诊断分享、主题选图与下载设置。最终结果见 [after-native/result.json](after-native/result.json)。诊断横向导航有意滚动，测试逐项滚入视口确认可达，没有通过放宽布局阈值忽略问题。

`scripts/test-android-file-input.py` 通过真实系统文档界面选择唯一 JSON 样本，并用 FileReader 核对名称和完整正文；随后取消应用的导入确认，没有导入该样本。结果见 [file-input/result.json](file-input/result.json)。共享存储样本按确切文件名删除。MuMu 的 uiautomator 工具有时在写出 XML 后退出 139，脚本记录此情况，只接受本次新建且能解析的界面快照，不读取旧快照；这不是验收应用的崩溃。

`scripts/test-update-installer-android.py` 重新覆盖错误路径、错误包、校验不一致、未知来源权限、系统安装器交接与取消，未确认安装。更新浏览器回归通过，记录见 `update-browser.log`。

完整仓库验证通过，前端共 298 项测试通过；最后界面调整后重复通过前端测试、类型检查、生产构建与约定检查。记录见 `verify.log`、`tests.log`、`frontend-build.log`、`conventions.log`。APK 构建与安装日志见 `android-build.log`。

公开发布文件仍为 2.4.0，在 `dist/releases/v2.4.0` 重新生成 APK、EXE、便携 ZIP 和完整校验清单。模拟器测试不等于 ARM64 鸿蒙真机兼容性验收，系统分享窗口打开也不代表已经向其他应用发送文件。

Android 接口依据：[WebChromeClient.onShowFileChooser](https://developer.android.com/reference/android/webkit/WebChromeClient#onShowFileChooser(android.webkit.WebView,%20android.webkit.ValueCallback%3Candroid.net.Uri[]%3E,%20android.webkit.WebChromeClient.FileChooserParams))、[FileChooserParams](https://developer.android.com/reference/android/webkit/WebChromeClient.FileChooserParams)。
