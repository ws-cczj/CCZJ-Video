# 开发与复用指南

执行规范见 [AGENTS.md](../AGENTS.md)。本页负责给出真实代码入口和维护命令，避免在新页面重新实现已有能力。

## 目录职责

| 目录／文件 | 用途 |
| --- | --- |
| `main.go`、`app.go` | 程序入口、装配与应用生命周期 |
| `app/service` | Wails 绑定适配；业务规则进入所属领域模块 |
| `app/*` | 视频、阅读、下载、更新、存储和共享基础能力 |
| `frontend/src/modes` | 模式选择、切换和壳层装配 |
| `frontend/src/api` | 绑定、事件、请求策略与 DTO 解码 |
| `frontend/src/platform`、`frontend/src/mobile` | 宿主能力与移动呈现 |
| `frontend/src/novel`、`comic`、`reading`、`player` | 独立领域体验与适当共享的阅读／播放能力 |
| `frontend/src/components/ui`、`styles` | 公共交互控件、工具类与样式 |
| `frontend/build`、`build` | 前端构建插件源码、原生宿主与发布脚本 |
| `scripts`、`frontend/tests`、Go `*_test.go` | 验证入口与回归测试 |
| `docs`、`reports` | 长期维护说明、验收与历史依据 |
| `.cache`、`bin`、`dist` | 本地缓存、可重建产物、运行证据与发布输出，默认不提交 |

## 复用入口

| 要解决的问题 | 先查看 |
| --- | --- |
| 按钮、加载、禁用、弹窗、下拉、数字输入、空状态 | [`components/ui/index.ts`](../frontend/src/components/ui/index.ts) |
| 图标、带请求上下文的图片 | [`Icon.vue`](../frontend/src/components/Icon.vue)、[`RemoteImage.vue`](../frontend/src/components/RemoteImage.vue) |
| 通用布局、主题变量与动效 | [`cczj-utilities.css`](../frontend/src/styles/cczj-utilities.css)、[`stores/theme.ts`](../frontend/src/stores/theme.ts)、[`motion`](../frontend/src/motion/) |
| 通用业务绑定和请求策略 | [`api/app.ts`](../frontend/src/api/app.ts)、[`requestPolicies.json`](../frontend/src/api/requestPolicies.json)、[请求说明](request-pipelines.md) |
| 数据校验、结构化错误与取消传播 | [`api/decode.ts`](../frontend/src/api/decode.ts)、[`response.ts`](../frontend/src/api/response.ts)、[`errors.ts`](../frontend/src/api/errors.ts) |
| 页面取消、迟到结果和 KeepAlive 活跃状态 | [`requestScope.ts`](../frontend/src/api/requestScope.ts)、[`useGeneration.ts`](../frontend/src/composables/useGeneration.ts)、[`useViewActivity.ts`](../frontend/src/composables/useViewActivity.ts) |
| 后端事件订阅 | [`api/events.ts`](../frontend/src/api/events.ts)，返回的解绑函数由订阅者持有 |
| 设置写入与浏览器缓存 | [`settingsQueue.ts`](../frontend/src/api/settingsQueue.ts)、[`platform/storage.ts`](../frontend/src/platform/storage.ts)、[`debouncedWrite.ts`](../frontend/src/platform/debouncedWrite.ts) |
| 文件分享、安装器、平台选择 | [`platform`](../frontend/src/platform/)、[`build/android`](../build/android/) |
| 阅读目录工具、桌面输入设置 | [`ChapterTools.vue`](../frontend/src/reading/ChapterTools.vue)、[`ReaderInputPreferences.vue`](../frontend/src/reading/ReaderInputPreferences.vue)，通过模式和偏好显式传入 |
| 阅读热缓存、搜索返回快照 | [`contentCache.ts`](../frontend/src/reading/contentCache.ts)、[`useSearchReturn.ts`](../frontend/src/reading/useSearchReturn.ts)，按阅读器／页面拥有，失败、取消与卸载清理 |
| 阅读持久化、书目与来源 | [`app/readingstate`](../app/readingstate/)、[`app/library`](../app/library/)、[`app/provider`](../app/provider/)、[`app/source`](../app/source/) |
| 工作生命周期、固定批次、在途合并 | [`app/lifecycle`](../app/lifecycle/)、[`app/parallel`](../app/parallel/)、[`app/workflight`](../app/workflight/) |
| 全应用出站限流、预算与统计 | [`app/netpolicy`](../app/netpolicy/)、[`app/netbudget`](../app/netbudget/)、[`app/netstats`](../app/netstats/) |
| 阅读五路 HTTP 闸门与检测批次 | [`quality/gate.go`](../app/library/quality/gate.go)、[`quality/dispatch.go`](../app/library/quality/dispatch.go)，由 [`library/service.go`](../app/library/service.go) 共享装配 |
| 错误、日志、资源、文件边界 | [`app/apperror`](../app/apperror/)、[`app/applog`](../app/applog/)、[`app/resource`](../app/resource/)、[`app/files`](../app/files/) |
| 模拟器 WebView 控制 | [`scripts/android_webview.py`](../scripts/android_webview.py)，复用连接、进程就绪等待与端口释放 |

入口表用于定位，不代表所有场景都要多包一层。新增职责放在所属模块，修改共享接口时同步所有消费者和验证。

## CCZJ 工具类与组件

先选公共组件，再组合工具类，最后补领域样式：

缺少可复用样式时扩展 `cczj-utilities.css`，在真实调用处使用并重新生成手册／编辑器数据；单个页面独有的排版、配色和动画留在领域样式。新增类先检查同义定义，不维护平行工具库。

```vue
<script setup lang="ts">
import { Button } from '../components/ui'
</script>

<template>
  <div class="toolbar cczj-flex cczj-items-center cczj-gap-4">
    <Button variant="primary">{{ $t('common.save') }}</Button>
  </div>
</template>

<style scoped>
@media (max-width: 640px) {
  .toolbar { @apply cczj-flex-col cczj-items-stretch; }
}
</style>
```

基础布局不再写第二份 `display:flex` 或 `align-items:center`。`@apply` 在构建时按原位置展开，保留 scoped、媒体条件和声明顺序。阅读分页尺寸、特定排版、模式主题和关键帧由对应领域保留，不能为了通过检查抹平设计。

样式检查会拒绝存在精确公共等价类的显示、对齐、方向、换行、收缩、溢出、盒模型和零最小尺寸声明。尺寸、颜色、关键帧与 `!important` 不作机械替换；这不代表允许用它们绕过复用。所有尺寸的实际值查[速查表](cczj-utilities-cheatsheet.md)。

## 开发与检查

Go 版本以 `go.mod` 为准（当前 1.26），Node 以 `frontend/package.json` 的 engines 为准（推荐 24）。Go 与 JS 的 Wails 版本保持一致，当前 beta.30。

```powershell
npm --prefix frontend ci
wails3 dev -config ./build/config.yml -port 9245

npm --prefix frontend run check:css-reuse
npm --prefix frontend run check:css-data
node scripts/check-document-links.mjs
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/verify.ps1
```

修改公共工具定义后运行 `npm --prefix frontend run gen:css-data`，随后校验生成文件。绑定签名变化后运行 `scripts/generate-bindings.ps1`，维护请求策略，前端调用继续经 `api`。

界面变更检查桌面和移动的实际控件、长文案、空状态、已有数据、重复进入以及操作后新增状态；涉及手机文件、权限、全屏、分享和安装器时运行原生验收。可先使用浏览器替身定位问题，但单独报告其验证范围。Android 命令见 [Android 开发](android.md)，打包见 [发布流程](releases.md)。

## 清理生成物

`scripts/clean-generated.ps1` 默认只预览已识别的旧构建、临时截图／日志、Android 中间产物与下载的临时编译工具；传 `-Apply` 才删除。它校验目标仍在工作区内，拒绝包含已跟踪文件的目标，跳过正在运行的程序。保留发布目录、最新测试包、当前验收证据、依赖环境、源配置、原始交接包及 `bin/data` 旧数据；不会把 `frontend/build` 插件源码当作构建产物。

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/clean-generated.ps1
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/clean-generated.ps1 -Apply
```

发布复用 `dist/releases/v2.4.0/`，避免为每次小修复再建一份日期目录。清理结果写入 `dist/audit/cleanup/result.json`；不能把预览或脚本生成当作删除完成。
