# 2.4.0 发布验收记录

日期：2026-10-22。输出目录：`dist/releases/v2.4.0`，尚未上传 GitHub。

## 已验证

- `scripts/verify.ps1`：版本说明、架构规则、i18n、298 项前端测试、类型检查、前端生产构建、Go vet 与全仓库 Go 测试。
- `go test -race ./app/updater ./app/update`：Windows 上使用本机 GCC 运行通过。
- `scripts/test-update-browser.py`：更新提示、旧缓存、已知／未知总长进度、完整性失败、完成、后台下载、键盘操作、退出时安装、安装失败恢复、下载失败。
- `scripts/check-release-artifacts.test.mjs`：完整清单通过、损坏 APK 拒绝、缺失 APK 拒绝。
- `build/release.ps1`：Windows 版本资源、Android Release 构建及 lintVitalRelease、APK 包名／版本／ABI／不可调试标记／证书、全部发布文件 SHA256。
- `scripts/test-update-installer-android.py`：MuMu Android 15、x86_64、独立包 `com.cczj.video.acceptance`，六项原生安装交接检查通过。原始结果见 [result.json](android-installer/result.json)，截图见 [权限页](android-installer/unknown-source-permission.png) 和 [系统安装器](android-installer/system-installer.png)。

安装交接测试仅复制本地 APK 样本到验收应用的私有目录；错误应用包、错误校验、目录外路径被拒绝；合法同包同签名 APK 打开系统安装器后取消。测试恢复了原安装权限状态并删除指定样本，未清除预览应用数据，未确认安装、未测试在线下载到覆盖安装的完整链路。

## 产物身份

- Windows：amd64，版本 2.4.0.0／2.4.0。
- Android：ARM64，包名 `com.cczj.video.preview`，versionName 2.4.0，versionCode 20400，不可调试。
- Android 证书 SHA256：`d1d10119ad1cfa1abf97f4695bef02df94cdc3bd8a6c6f7a9d53bd10f7c945ff`，与旧预览版一致，仍是已有预览版 Android Debug 证书。
- 产物哈希、字节数与构建时源码状态以发布目录的 `artifact-manifest.json` 为准；源码仍含未提交改动。

## 待验证与已知限制

没有连接鸿蒙 4 真机，不能声称华为兼容性或真实手机覆盖安装通过。Windows 未执行旧 EXE 到新 EXE 的完整替换。GitHub 尚未发布 2.4.0，在线发现及发布后真实网络下载需在发布后验收。

当前更新包仍沿用预览版签名身份；旧 Android 宿主需手动覆盖安装一次 2.4.0 才具备新桥接能力。下载完整性校验用于识别损坏，不代替代码签名身份验证。

对更新包运行完整默认 golangci-lint 时仍有既存的未检查 Close／Remove 返回值及未使用兼容函数告警；未将其隐藏或声称全量 lint 通过。Go vet 与仓库要求检查通过。
