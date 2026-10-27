# 发布与自动更新

构建版本来源为仓库根目录 `version.json`。一键发布默认采用 CHANGELOG 最上方的版本段落，并同步版本文件；也可用 `-Version` 明确选择。例如 2.4.2 对应 Windows 文件版本 2.4.2.0、Android versionCode 20402。发布脚本读取同版本说明，统一生成应用内更新文案和 GitHub Release 正文。

## 一键发布

首次安装、登录官方 [GitHub CLI](https://cli.github.com/)：

```powershell
winget install --id GitHub.cli --exact
# 安装后重新打开终端
gh auth login --hostname github.com
```

GitHub CLI 是仅在发布电脑使用的官方 MIT 工具，不进入 EXE／APK；沿用本机 Git、Go、Node、Wails、Android SDK／NDK／JDK 和已有签名密钥。登录的账号需要目标仓库的代码推送和 Release 写入权限。Git 推送继续使用本机 Git 凭据，不把令牌写入项目文件。

平时在 CHANGELOG 中写好目标版本的一段说明，然后双击 `build/publish.cmd`，或运行 `task publish`。脚本会将**所有未被 Git 忽略的改动**提交为 `Release v版本号`，因此先把当前工作区整理为要发布的内容。也可以指定下一版本：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File build/release.ps1 -Publish -Commit -Version 2.4.2
```

流程会同步 `version.json`、README、更新下载地址和历史说明，执行编译与静态检查，然后提交、构建并校验 Windows EXE／便携 ZIP／ARM64 APK。发布调用 `scripts/verify.ps1 -StaticOnly`，不运行自动化测试；界面与行为由用户验收。标签必须指向实际构建的干净提交；不允许把旧产物配上新 HEAD，也不移动既有版本标签。

随后先推送标签和对应源码，创建 GitHub 草稿并上传八个发布文件，逐个检查附件名称、大小和 SHA256，再正式发布并设置为最新版本。全部文件齐全之后才推送当前分支，避免 `main/version.json` 提前向更新客户端公布尚不可下载的版本。GitHub 的草稿和发布行为见[官方 CLI 文档](https://cli.github.com/manual/gh_release_create)。

### 预览与重试

```powershell
# 只显示计划：不改文件、不提交、不上传，也不要求已登录 GitHub
powershell -NoProfile -ExecutionPolicy Bypass -File build/release.ps1 -Publish -Commit -Plan

# 源码已经自行提交：不自动收集工作区改动
powershell -NoProfile -ExecutionPolicy Bypass -File build/release.ps1 -Publish
```

构建、校验或上传失败时不会发布不完整的草稿。重试同一个命令可继续使用与当前提交完全匹配的已校验产物；草稿可重传附件，公开版本只核对、不覆盖。GitHub 按标签查询草稿可能返回 404，脚本会继续分页查询当前账号可见的发布列表，恢复原草稿；列表查询失败时停止，不重复创建。草稿可见性见 [GitHub Releases API](https://docs.github.com/en/rest/releases/releases#list-releases)。源码推送允许最长 30 分钟，以容纳首次上传较大的源库；实际速度仍取决于当前网络。

发布后若分支因网络问题推送失败，排除网络问题后重试；若远端在构建期间前进，需自行合并发布提交后推送分支，不移动已有标签、不强制推送。Release 源码始终由标签保留，已经公开的版本需要修改代码时应提升版本号。发布工具自身的修复可以单独提交到主分支，既有应用版本标签和产物仍指向其实际构建提交。

`-SkipBuild` 用于明确复用现成产物，仍要求清单记录的源码提交和当前干净 HEAD 一致；发布模式不能使用 `-SkipVerify`。

## 只在本地打包

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File build/release.ps1 -NoOpen
```

也可运行 `task release` 或双击 `build/release.cmd`。这个入口保留本地准备功能，不创建标签、不提交、不上传。发布目录是 `dist/releases/v版本号/`；`-SkipBuild` 仅重做清单，不能把修改后的源码编进旧文件。旧入口 `build/windows/release.ps1` 继续转接统一构建流程。

默认 `-AndroidSigning ExistingPreview` 使用本机已有预览版密钥；不存在时拒绝生成新身份。APK 包名仍为 `com.cczj.video.preview`，沿用既有预览版证书以支持覆盖安装，Release APK 不可调试。请保存原密钥，不能把它当作应用商店正式签名方案。

使用自管密钥时，设置 `ANDROID_KEYSTORE_FILE`、`ANDROID_KEYSTORE_PASSWORD`、`ANDROID_KEY_ALIAS`、`ANDROID_KEY_PASSWORD`，并传 `-AndroidSigning Environment`。更换证书通常无法覆盖已有预览版，规则见 [Android 应用签名文档](https://developer.android.com/studio/publish/app-signing)。

## 发布附件与手动上传

每个版本发布八个文件，名称大小写必须原样保留。以下以 2.4.2 为例：

- `cczjVideo-windows-amd64-2.4.2.exe`
- `cczjVideo-windows-amd64-2.4.2-portable.zip`
- `cczjVideo-android-arm64-2.4.2.apk`
- `checksums.txt`
- `version.json`
- `artifact-manifest.json`
- `android-metadata.json`
- `release-notes.md`

仍可手动创建普通公开 Release，标签指向实际源码提交，正文复制 `release-notes.md` 并上传全部附件。只传 EXE 或 APK 会缺少自动更新的校验依据。独立检查命令：

```powershell
node scripts/check-release-artifacts.mjs dist/releases/v2.4.2
```

## 自动更新行为

Windows 保留下载、SHA256 校验、立即安装／退出时安装和替换回执。附件选择同时核对系统、架构和扩展名。

下载保留 GitHub 直链与代理前缀并发探测、测速后选择可用候选的流程。代理前缀是远端下载入口，与用户电脑的 Clash 等本机代理分开；验收需要记录实际选中的地址及设备代理状态，不能把本机代理成功当作无代理网络通过。

后台下载和下载完成使用右侧滑入的共享 Tips，提供关闭按钮，停留五秒自动消失；关闭提示不取消下载或删除安装包。提示的“详细信息”可打开更新窗口，消失后仍可从设置的检查更新入口查看状态和安装。采集状态同样使用五秒 Tips，点击“查看该源采集”进入并展开对应来源，详细进度留在采集源页面。

更新说明和历史版本说明使用同一 `MarkdownContent` 渲染标题、列表、强调、链接、代码和表格。现有依赖没有 Markdown 解析器，因此引入维护中的 [markdown-it](https://github.com/markdown-it/markdown-it) 15.0.3（MIT，包含 TypeScript 类型）；未增加 UI 框架。解析器随更新内容组件按需加载，本地构建约增加 100 kB JavaScript／42 kB gzip；不依赖 Node 原生扩展或远端 CDN，Windows WebView2 与 Android WebView 使用相同构建。禁用原始 HTML，保留链接协议校验，外部链接带 `noopener noreferrer`。更新回归脚本同时检查窄屏排版及 HTML 不执行。

Android 下载匹配架构的 APK 到应用私有更新目录，校验后交给系统安装器。原生宿主还会核对路径、校验凭据、包名、版本号和签名。未授权“安装未知应用”时打开系统设置，授权后返回并再次点击安装；系统安装界面由用户确认，取消不算安装成功。权限接口依据 [Android PackageManager 文档](https://developer.android.com/reference/android/content/pm/PackageManager#canRequestPackageInstalls())。

2.4.0 之前的 Android 宿主缺少新安装桥接，首次需从 GitHub 下载 APK 后由系统覆盖安装，保留应用数据。安装 2.4.0 或更新宿主后，后续版本可使用应用内更新。ARM64 手机包不能安装到仅支持 x86_64 的模拟器。

## 验证范围

发布默认执行 `scripts/verify.ps1 -StaticOnly`：检查文档、版本元数据、前端约定与依赖边界、生成数据、样式复用，完成前端类型检查与构建、Go 静态检查与编译。后续产物校验核对版本、架构、签名、源码提交及远端附件摘要。已有发布流程测试仍保留在不带 `-StaticOnly` 的历史完整验证入口中，只有用户明确要求时运行。

2.4.2 发布收尾的原始日志位于 `dist/audit/release-2.4.2`。完整仓库验证通过，前端为 74 文件／350 项测试；架构画布的 31 条功能链覆盖全部 216 个公开绑定。Android amd64 隔离验收的 16 项检查通过，覆盖正文／漫画图片、共享取消、分页、SQLite 旧值导入与重启恢复、写失败重试、备份身份还原，以及下载暂停、重启续传和 4 MiB 逐字节校验。拥塞检查实测六个下载名额占用、一条等待时，播放仍取得独立额度。

Android 系统文件选择返回真实文件内容；安装交接拒绝错误包名与摘要不符，权限缺失转入系统设置，取消安装保持原版本。这些结果来自独立 `acceptance` 包，不代替 `preview` ARM64 在实体手机的升级验收。发布 APK 仍核对包名、versionCode 20402、ARM64、非调试与既有签名。

补充的播放验收使用当前 2.4.2 amd64 APK：详情自动探测声明的 1080P／720P、保留原线路身份、实际播放切线、加载与续播布局、原生全屏中的线路抽屉通过，桌面面板排序及长名称布局通过。模拟器中的旧临时来源在测试期间停用，结束后恢复，避免已关闭的历史服务器抢占网络预算。日志为 `android-playback-routes.log` 和 `browser-playback-routes.log`；声明的分辨率不等同于合成短片的实际编码尺寸。

Windows production 隔离实例已验证窗口关闭／主动退出、启动失败、目录与备份绑定；替换／回滚的真实批处理成功、锁占用失败与退出安装由 Go 测试覆盖。正式 EXE 的 WebView2 初始化会清除外部浏览器参数，外部设置的 CDP 端口无法连接；按用户要求不使用 computer-use，未取得真实正式 EXE 升级／回滚的界面验收结论。实体电视、ARM64 手机／鸿蒙 4 与长期设备资源趋势仍需现场验证。

2026-10-23 已通过 GitHub CLI 实际发布 [v2.4.0](https://github.com/ws-cczj/CCZJ-Video/releases/tag/v2.4.0) 和 [v2.4.1](https://github.com/ws-cczj/CCZJ-Video/releases/tag/v2.4.1)：各上传八个文件，远端附件大小和 SHA256 与本地清单一致，2.4.1 设为最新版本。对应源码提交分别为 `e140d1888c87c72d5237aa3c9131654d99a85bd2` 与 `7e867886a80ac945030631e9a4514c178f5c26d4`，主分支包含两个提交；2.4.0 从独立历史检出重建，未用 2.4.1 产物替代。

2.4.0 的安装与更新验收保留在[发布验收记录](https://github.com/ws-cczj/CCZJ-Video/blob/v2.4.5/reports/release-2.4.0/README.md)，其中测试项和限制属于当时版本。移动设置检查见[模拟器记录](https://github.com/ws-cczj/CCZJ-Video/blob/v2.4.5/reports/mobile-settings-2026-10-22/README.md)。鸿蒙 4 真机及真实旧 EXE 到新 EXE 的完整替换仍需单独验收。
