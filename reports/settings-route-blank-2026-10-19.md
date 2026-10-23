# 桌面设置入口空白诊断与修复

## 原因与证据

首页模板在主容器外新增了 `CczjMobileEditActions`。即使桌面端的 `v-if` 为假，它仍使首页成为 Fragment 根节点。桌面壳直接对该路由组件执行 `Transition mode="out-in"`；离开时 Vue 无法为 Fragment 安装正常的元素退出动画，路由出口停留在注释占位，后续页面也不再显示。这是 Vue 警告而非抛出的异常，因此窗口的 error/unhandledrejection 监听没有报告。

隔离复现使用真实 DesktopVideoShell、Settings 和双根节点首页替身，替换 Wails 绑定以避免访问用户数据与网络。修复前：相同 `non-element root node` 警告，`settings: false`，主内容为 `<!---->`，脚本退出 1。单根首页替身则正常，排除了设置首次初始化本身导致这次故障。

## 修改

- 桌面路由使用按组件类型复用的 scene 组件，提供单个元素根节点承接动画。保留页面名称以兼容 KeepAlive include，保留路由与播放器 key；没有在每次导航时重建 KeepAlive。
- 在应用初次导航前注册 Vue errorHandler 与 router.onError，接入现有错误展示、去重和后端日志。没有把普通 Vue 警告升级成错误弹窗。
- Wails 开发监听器从 debug 改为 info，停止输出被忽略的 `.git/objects` 变动；需重启开发监听进程后生效。

`/wails/custom.js` 的 404 与本次空白无关。本地使用的 Wails v3 beta.30 在 `pkg/application/application.go` 中明确为桌面模式返回 404，runtime.debug.js 使用 HEAD 探测可选脚本并跳过非成功响应。未修改第三方 runtime 或伪造脚本响应。浏览器图片懒加载 Intervention 也是浏览器提示。

## 验证结果

运行隔离复现：安装有 Playwright 的环境可直接运行 `node scripts/probe-settings-route.mjs`；使用外部安装时，设置 `CCZJ_PLAYWRIGHT_NODE_MODULES` 为包含 playwright 的 node_modules 绝对目录。需要已安装 Edge，不自动下载浏览器。

修复后结果：

| 路径顺序 | 内容可见 |
| --- | --- |
| 首页 → 设置 | 是 |
| 设置 → 首页 | 是 |
| 首页 → 设置 | 是 |
| 设置 → 播放器替身第 1 集 | 是 |
| 播放器替身第 1 集 → 第 2 集 | 是 |
| 播放器替身 → 设置 | 是 |

- Vue 根节点警告 0，页面异常 0。
- 首页挂载 1 次、激活 2 次、停用 2 次；缓存保留。
- 播放器替身挂载 1 次；同一视频切集未重建实例。
- `npx vitest run tests/settings-route-render.test.ts`：1 项通过。
- `npx vue-tsc --noEmit`：通过。

这是路由机制的浏览器隔离回归，播放器替身仅验证组件身份，不代表真实播放验收。未启动原生 EXE、未操作模拟器、未打包；其他会话修改保留。
