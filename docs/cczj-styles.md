# CCZJ 样式复用

`frontend/src/styles/cczj-utilities.css` 是公共工具类的定义来源，由 `main.ts` 全局加载。视频、小说、漫画及移动页面都可以引用它；颜色类读取现有主题变量。公共交互控件从 `frontend/src/components/ui` 引用，页面不再复制控件内部的按钮、加载和禁用样式。

## 模板与 CSS 中的用法

简单、固定的结构直接写类：

```vue
<div class="toolbar cczj-flex cczj-items-center cczj-gap-4">
  <Button variant="primary">保存</Button>
</div>
```

有作用域、状态、响应式或既有选择器的规则，可以在当前规则内组合相同的类：

```vue
<style scoped>
.toolbar {
  @apply cczj-flex cczj-items-center cczj-gap-4;
  padding: 10px 18px;
}
@media (max-width: 640px) {
  .toolbar { @apply cczj-flex-col cczj-items-stretch; }
}
.toolbar.is-busy { @apply cczj-pointer-events-none; }
</style>
```

Vite 的本地 PostCSS 插件 `frontend/build/cczjUtilities.ts` 从同一份公共 CSS 读取定义，把 `@apply` 在原位置展开。选择器、scoped 作用域、声明顺序、媒体条件和覆盖关系都保留。公共定义变动会触发消费者重新处理；未知类、条件类和放在选择器外的 `@apply` 会使构建失败。产物是普通 CSS，Android WebView 不需要理解新的语法。

伪类应保留为选择器。例如写 `.button:hover { @apply cczj-text-accent; }`，不能把 `cczj-hover-underline` 展开为无条件声明。`cczj-truncate` 等多声明工具可以整体组合。

插件通过构建期 CSS 组合处理既有复杂规则。分页计算、播放器控制、特定布局尺寸和动画关键帧继续由对应模块维护；相同声明适合复用，不代表所有不同业务布局都需要合并成一个组件。

## 兼容边界

- 选集名称由 `player/views/EpisodeLabel.vue` 在详情页、播放面板和手机选集抽屉统一展示。参考[芒果 TV 综艺选集](https://www.mgtv.com/b/859271/24322442.html)的内容标题与日期信息：有效日期前缀在有剩余标题时作为副行，标题允许完整换行；日期或长名称列表在手机使用两列并让按钮自然增高。纯日期及无法确认的日期格式保留原样，原始 `ep_name`、播放坐标、历史和下载名称不改写。
- 移动端触摸反馈在 `styles/mobile/frame.css` 的平台根作用域统一处理，包含 Teleport 弹窗及各应用模式。参考 [web.dev 触摸样式](https://web.dev/articles/add-touch-to-your-site)：关闭 WebView 原生点击高亮，按钮、导航和选项禁止长按选中文字；输入框、可编辑内容及普通正文仍允许选择。触摸焦点仅取消 `:focus:not(:focus-visible)` 的默认轮廓，键盘焦点沿用控件的 `:focus-visible` 样式，参见 [MDN](https://developer.mozilla.org/en-US/docs/Web/CSS/Reference/Selectors/:focus-visible)。不要通过全局 `outline: none` 或按下即 blur 取消键盘和编辑操作。
- 间距数字沿用 2px 单位，例如 `cczj-gap-4` 为 8px。历史尺寸类有两套命名，例如 `cczj-w-8` 为 32px、`cczj-w-16` 为 16px。保持旧值兼容，使用前查[生成手册](cczj-utilities-cheatsheet.md)，不要推算尺寸或引入另一套隐含单位。
- 模板类与组合类遵循正常 CSS 层叠。带条件或特定选择器的样式保留在原规则内组合，避免把移动覆盖、交互状态或作用域样式随意移动到全局模板类后改变优先级。
- 扩展包运行时注入的 CSS 不经过 Vite。扩展可以在 DOM 上使用公共类，自己的注入样式仍使用普通 CSS。
- 组件库不持有用户主题数据；样式重构不改写已保存的主题、背景、源和阅读/观看记录。

## 维护与验证

公共库不是封闭清单。缺少能被多个页面／模式使用的布局、文本或交互样式时，在 `cczj-utilities.css` 添加职责明确的工具类，并直接用于真实消费者；不要另建工具文件或在页面反复复制。先查等价类，保持原有单位和兼容性。单个页面独有的书页几何、模式配色、关键帧不需要抽成通用类。

修改公共 CSS 后执行：

```powershell
npm --prefix frontend run gen:css-data
npm --prefix frontend run check:css-data
npm --prefix frontend run audit:css
npm --prefix frontend run check:css-reuse
```

生成器同步 VS Code 的 HTML 类名补全、CSS 指令说明及速查手册，包含转义类名和条件类。`--check` 只校验、不改文件。完整验证 `scripts/verify.ps1` 包含文档同步校验、组合插件测试和前端构建，并检查生产 CSS 中不存在未展开的 `@apply`。

统计脚本分别报告模板引用、CSS 组合引用和仍然存在的相同字面声明。它不把业务类名中的 `cczj-` 前缀算成工具库使用，也不把动画关键帧中的相同透明度值当作必须删除的问题。

`check:css-reuse` 是完整验证的一部分：当公共库存在精确等价类时，拒绝静态 CSS／Vue 样式块中重复声明显示、对齐、方向、换行、收缩、溢出、盒模型和零最小尺寸。模板使用工具类或原规则内 `@apply` 均可；`cczj-mobile-*` 业务类前缀不能替代实际复用。关键帧、业务宽高、模式配色和带优先级的声明不自动转换，避免破坏领域含义；有例外时说明实际原因，不以 `!important` 或内联写法绕过检查。

2026-10-22 已将 9 个文件中的 278 处基础布局改为公共组合，并逐个核对展开后的 CSS AST，选择器、条件、声明顺序和值一致。早期迁移记录见[项目样式整改报告](https://github.com/ws-cczj/CCZJ-Video/blob/v2.4.5/reports/css-reuse-2026-10-20.md)。

移动按钮密度由 Button 的 sm／md／lg 档位选择 `--cczj-mobile-control-sm`、`--cczj-mobile-control-md`、`--cczj-mobile-control-lg`，全局移动样式读取 `--cczj-control-height`。这些值使用 clamp 随屏宽变化，并只限定最小高度，长文案或大字号仍可撑开控件；桌面尺寸保持现有规则。移动页面间距优先使用 `--cczj-mobile-section-padding`，不再用统一 44px 覆盖所有尺寸档位。

## 字体与大字号布局

界面字号由 `platform/typography.ts` 管理，启动挂载前恢复，基础设置使用同一组 12／13／14／16／18／20px 档位。根字号默认 14px；公共文字工具类、主题字号变量和页面文字使用相对单位，例如 `calc(12rem / 14)` 在默认档位仍为 12px，调大后按比例增长。表单控件继承界面字体，避免浏览器默认字体与正文混杂。图标、装饰画和阅读正文保留各自的几何或字号偏好，不用界面字号改写阅读配置。

大字号不能靠截断按钮名称适配：采集源操作按字号与可用宽度换列，必要时换行；诊断分类使用 Segment 的 `wrap` 选项。视频网格复用 layout store，在空间足够时保持设置列数，窄容器按最小卡片宽度减少列数。共享 Empty 省略与标题完全相同的说明，保留不同的恢复提示。

逐页检查脚本 `scripts/audit-page-layouts.py` 在 Chrome 中打开真实路由，通过独立 Android acceptance 包执行后端调用；仅替换传输和桌面窗口控制。它读取视频目录快照、创建本地阅读验收源，覆盖三种模式及设置子页的桌面／手机、默认／最大字号，输出截图和溢出、按钮裁切、重叠、重复标题清单；结束后删除自己的源并恢复设置。正常阅读内容和阅读字号需要单独确认，错误空态不能算正文通过。

排查移动端时运行 `python scripts/audit-page-layouts.py --mobile-only`，只检查 390px 默认字号和 320px 最大字号，独立截图集输出至 `dist/audit/page-polish/mobile/`。
