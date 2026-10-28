# CCZJ Video

Current release: **v2.4.9**

视频、小说与漫画多源聚合应用，使用 Wails v3、Vue 3、TypeScript 和 Go。Windows 10/11 版需要 WebView2 运行时，Android 版目前为预览版。

iOS 正在适配，提供 [本地构建、云端打包与个人自签流程](docs/ios.md)；尚未完成 iOS 编译与真机验收，不作为已支持的发布平台。

## 能做什么

- 管理多个采集源，采集视频目录、搜索、筛选和查看详情。
- 在线播放、切换线路、断点续播，手动加载 SRT/VTT 字幕。
- 短剧海报剧场与竖屏播放，支持推荐刷剧、单剧连播、长按倍速和抽屉选集。
- 离线下载，支持暂停、恢复和取消。
- 收藏、观看历史、回收站、数据备份和换机恢复。
- 使用 Anime4K 动画增强，或 FSRCNNX 2× 超分与 CAS 锐化的影视增强。
- 安装采集适配、主题、着色器和前端脚本扩展包。
- 小说与漫画分别提供内置源、浏览、多源搜索和阅读；书架、追漫、阅读进度和偏好保存在本机 SQLite，可随完整备份恢复。

“相似推荐”按演员、导演、类型、年份与地区匹配，不读取观看历史或收藏。画质增强效果与流畅度取决于素材和 GPU。

## 开始使用

1. 在“采集源”添加你自己的视频 API 地址并执行采集。
2. 从首页或搜索打开影片详情，选择集数和播放线路。
3. 在“设置 → 高级”导出数据备份；恢复时只补充或合并数据，不用旧库覆盖当前库。

应用数据默认保存在 `%APPDATA%\CCZJ Video`。升级后找不到旧数据时，先查看“设置 → 高级 → 旧数据找回”；更新失败的结果可在下次启动和日志中查看。

## 扩展包与信任

Windows 可在“设置 → 扩展包”拖入扩展包文件夹；Android 在该页管理内置及已安装扩展。格式和样例见[扩展包规范](docs/plugins.md)。

带脚本的包在首次明确启用时询问是否信任，授权按本机包身份保留，更新内容后不会自动重新询问。原封不动的内置包按程序内嵌内容校验后运行。清除信任会停用该包，重新启用时再确认；内容备份不会携带本机授权。

脚本与应用共享权限，写入和网络授权提示只约束配合规则的代码。**这不是沙箱，请只运行可信来源的脚本。** 信任与权限记录属于当前安装，不随备份导入导出。

## 开发与验证

开发规范见 [AGENTS.md](AGENTS.md)，代码和公共能力入口见 [开发与复用指南](docs/development.md)。完整文档与历史资料从 [文档索引](docs/README.md) 查找。

工具链版本以 `go.mod`、`frontend/package.json` 和锁文件为准：当前 Go 1.26、推荐 Node 24，Wails Go／JS／CLI 使用 beta.30。

```powershell
npm --prefix frontend ci
wails3 dev -config ./build/config.yml -port 9245
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/verify.ps1
```

完整验证包含文档链接、版本同步、架构边界、CCZJ 样式复用、前端测试与构建、Go vet 和 Go 测试。公共控件先使用 `components/ui`；基础布局先查 `cczj-utilities.css`，不要在新页面另建一套。

Windows 单独构建使用 `task build`。Android 构建、模拟器与热更新见 [Android 开发](docs/android.md)。同时准备 EXE、便携 ZIP、ARM64 APK 和校验清单，运行：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File build/release.ps1 -NoOpen
```

版本读取 `version.json`，发布文件位于 `dist/releases/v版本号/`。一键提交、构建并发布 GitHub Release，双击 `build/publish.cmd` 或运行 `task publish`；更新说明读取 CHANGELOG。首次需要安装 GitHub CLI 并登录，详见[发布与自动更新](docs/releases.md)。

## 更多资料

- [文档索引](docs/README.md)：当前维护文档、验收结果和合并归档。
- [扩展包规范](docs/plugins.md)：格式、权限和信任。
- [更新日志](CHANGELOG.md)。
- [使用许可](LICENSE)：分发与修改以原文条款为准。
