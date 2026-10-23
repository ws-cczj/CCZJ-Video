# 小说模式缺陷检查与修复（2026-10-19）

本轮从真实操作检查小说模式，修复下列七类可复现问题。旧的 234 个前端测试、原阅读浏览器测试及阅读 Go 包在修改前通过，但原浏览器用例只有一章短正文，不能证明多章长文阅读正常。

## 缺陷与根因

| 问题 | 复现与影响 | 修复 |
| --- | --- | --- |
| 横竖屏、窗口缩放后分页错位 | 412px 屏幕中栏宽 364px，切到 860px 后视口为 720px，正文栏宽仍为 364px | 尺寸监听跟随异步创建、切章替换的视口，重新测量分页；滚动模式不执行分栏测量 |
| 滚动模式章末翻页无效 | 正文已到底部，翻页键仍只调用 scrollBy，不能进入下一章 | 在顶部、底部处理章节边界，其余位置按屏滚动 |
| 上一章的章末定位长期覆盖进度 | 从上一章章末读到 30%，刷新仍回到 100% | 消费一次后清除 position=end 导航参数 |
| 本地章节 ID 变化后无法续读 | 恢复记录中的 chapterId=0、remoteId=chapter/2，却回到第一章或提示缺失 | 用来源内章节身份定位，同时修正阅读 URL；再次刷新仍可续读 |
| 原进度章节缺失时悄悄从头读 | 保存章节已不存在但本地 ID 与其他章节重合，可能读错章节并覆盖原记录 | 区分稳定身份与本地 ID，明确显示章节缺失，保留原进度；不可用章也不退回第一章 |
| 搜索框修改后“加载更多”报错 | 第一页搜索“山川”，只改输入框为“河流”，续页却混用旧游标和新关键词 | 保存已提交关键词，续页始终使用同一查询；新查询取消旧查询并防止迟到回写，重复结果按作品身份去重 |
| 下架标记在重启后丢失 | 书架 available=false，但旧作品快照 available=true，加载后被误判可用 | 同时保留书架可用标记和作品快照状态 |

进度恢复使用稳定章节身份，不按书名合并作品；缺失章节的错误状态不覆盖原进度。未执行用户数据清理、数据库重建或源批量删除。

## 验证入口与证据

最终结果：

| 验证 | 结果 |
| --- | --- |
| 完整 verify.ps1 | 退出码 0；前端 53 文件 / 238 测试通过，类型检查、生产构建、Go vet 和 Go 测试通过 |
| 新增长文交互回归 | 13 项通过，无未处理页面异常 |
| 原阅读浏览器回归 | 通过，涵盖书架、分页列表、持久化失败重试及模式隔离 |
| 真实书源浏览器回归 | 通过，包含源选择导入、质量信息、酷我与番茄正文及漫画图片解码 |
| MuMu Android 15 / WebView 110 | 6 组原生验收通过；实际设备视口 360×616，滚动正文宽 312px 与视口宽一致 |

Android 验收还检查了旋转时栏宽从 364px 更新到 720px，重新加载后恢复约 55% 的章内进度；跨章后的 SQLite 章节 ID 与阅读路由一致。

新增 `scripts/test-novel-audit.py`：实际 Vue 页面、隔离的确定性传输和长正文，覆盖多章、缩放、字号、分页/滚动切换、续读、缺失章节、旧 ID 恢复及再次刷新、关键词变化、慢请求取消和未处理异常。测试替身与应用共享同一份 Wails runtime，避免两份 CancelError 构造器造成虚假取消异常。

```powershell
python scripts/test-novel-audit.py --channel msedge
python scripts/test-reading-browser.py --channel msedge
python scripts/test-reading-source-browser.py --channel msedge
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/verify.ps1
python scripts/test-novel-android.py --device emulator-5554
```

本机浏览器测试使用隔离 Python 环境：`C:/Users/18446/AppData/Local/Temp/cczj-upgrade-browser-20261018/Scripts/python.exe`。复跑时安装 `scripts/browser-test-requirements.txt`；Android CDP 工具另需 `scripts/native-profile-requirements.txt`。原生测试须使用与 frontend/dist 匹配的验收 APK。

证据文件：

- `reports/novel-audit-2026-10-19/baseline.json`：修改前的失败场景，包含五类首批复现。
- `reports/novel-audit-2026-10-19/result.json`：最终交互回归。
- `reports/novel-audit-2026-10-19/verify-final.log`：完整验证。
- `reports/reading-source-browser-2026-10-19/result.json`：真实 Go 服务、临时 SQLite、真实网络书源的浏览器复测。
- `reports/novel-audit-2026-10-19/android/result.json`：MuMu 原生 Android WebView 验收结果。
- `reports/novel-audit-2026-10-19/android/reader.png`：实际设备视口的阅读截图。

## 验证范围

真实网络浏览器验证包含用户选择导入、源质量信息、酷我小说和番茄正文；Android 验收使用独立包 `com.cczj.video.acceptance`，覆盖番茄源、搜索、目录、字体解码、横竖屏、SQLite 进度重载、跨章一致性和 Android 返回关闭设置。原生脚本恢复验收包原有模式、路由与阅读偏好。

用户的 `com.cczj.video.preview` 数据未被清理，未向它安装验收包。Windows 前端构建与 Go 验证不等同 Windows 原生窗口验收；本轮没有运行 Windows 原生阅读交互，也没有运行真实手机或 iOS。以上结果不表示所有站点、所有设备或所有小说模式缺陷均已穷尽。

## 可运行产物

- Windows：`bin/cczjVideo-novel-fix.exe`。使用现有打包脚本构建并回读版本资源，另存以保留正在运行的原程序；打包日志为 `novel-audit-2026-10-19/windows-build.log`。
- MuMu / Android x86_64 预览包：`bin/CCZJ-Video-android-amd64-preview.apk`。已构建，未替换用户设备上的预览包。
- 原生测试包：`bin/CCZJ-Video-android-amd64-acceptance.apk`。已安装到 MuMu 独立验收包并运行上述用例。
- 文件大小与 SHA-256：`reports/novel-audit-2026-10-19/artifacts.json`。
