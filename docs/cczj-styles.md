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

图片主题裁剪沿用 [Cropper.js 的矩形选区模型](https://fengyuanchen.github.io/cropperjs/api/cropper-selection.html)，使用本项目 Button、NumberField 和主题变量表达选区、遮罩与数值调整，不新增 UI 或裁剪依赖。图片校验与无损裁剪使用 [createImageBitmap](https://developer.mozilla.org/en-US/docs/Web/API/Window/createImageBitmap) 和 worker 内 OffscreenCanvas，未裁剪原图不重编码；SQLite 的 GZIP 封装只改变存储表示，不改变图像像素或文件字节。启动壁纸通过 [HTMLImageElement.decode](https://developer.mozilla.org/en-US/docs/Web/API/HTMLImageElement/decode) 并行准备，选定开屏图后保持资源不变。

主题背景使用短 Blob URL 作为 CSS 变量值，原文件保留在主题数据中。[Chromium 的 CSSVariableData](https://chromium.googlesource.com/chromium/src/+/ab30ca95a699b434a885dd6b573a9fe4158caa76/third_party/blink/renderer/core/css/css_variable_data.h) 对单变量设有字节上限，不能将大图完整 base64 直接写入变量。裁剪交互采用独立 Modal、可移动选区、四边及角点缩放与像素字段；移动／缩放语义参考 [CropperSelection](https://fengyuanchen.github.io/cropperjs/api/cropper-selection.html)，使用项目原有控件与图标系统。界面保留动作、加载、错误和必要限制，技术依据集中在本文。


## 弹幕排版与设置

弹幕显示由 `player/danmaku/textStyle.ts` 统一构造字体和绘制文字，Canvas 测量、实际弹幕和 `DanmakuPreview.vue` 使用同一规则。默认常规字重、1.5px 描边；提供思源黑体、系统衬线／等宽回退，400／500／600／700 字重，以及描边／阴影／无效果。字号以24px名义字号为100%，界面支持50%–200%比例及1%数值步进；画面宽度480–640px连续缩放，不再跨500px突变，画面更窄保持75%下限。透明度10%–100%、显示区域25%–100%、密度25%–100%、行数2–24、描边0–4px（0.25px步进）独立设置。旧字号、透明度、速度及行数等已保存值保持，不因新增设置自动重置；“重置外观”保留弹幕总开关和缓存天数。

思源黑体来自 [Adobe 官方 Source Han Sans](https://github.com/adobe-fonts/source-han-sans)，使用官方CN可变WOFF2，原文件7995716字节（约7.63MiB），SHA256 `7087698d52240614659957608d8c4ad446759aee3208409dc9f566412e7af8f4`。原始下载路径为 `release/Variable/WOFF2/OTF/Subset/SourceHanSansCN-VF.otf.woff2`；文件未经裁剪或重新编码，字重由字体自身可变轴处理。字体只在弹幕启用或样式预览时按需从本地资源加载，不改变界面、阅读器或启动字体，不新增远程字体请求或运行依赖。遵循 [SIL OFL 1.1](../frontend/public/licenses/source-han-sans-OFL.txt)，完整版权与授权文本随前端public资源打包。字体由文档持有，HMR撤销注册；失败时使用系统字体并提供重试，加载完成清理测量基线重绘。字体资产会增加安装包约7.6MiB，低版本WebView不支持可变字体时采用明确的系统回退。

`timeline.ts` 保持播放时钟驱动，速度按画面宽度归一为8秒跨越视口的基准，再乘用户速度比例；同一视口的滚动弹幕保持等速，避免长文字追尾。区域限制同时作用于顶部、滚动与底部弹幕，候选轨道按实际纵坐标避让；密度调节同屏上限与间隔，同屏预算始终不超过120条。Canvas backing store最多约800万像素、最高3倍DPR；这只是分配上限，不代表已经完成设备内存或性能验收。

设置面板复用 Button／Select／NumberField，滑杆配合数字输入微调，预览不依赖当前是否匹配到弹幕。普通手机、桌面和原生全屏共用一套设置；短横屏保持固定标题及可滚动内容，移动全屏也使用44px高触控入口。范围与控件几何保留像素单位，界面文字继续消费界面字号。弹幕偏好由 `preferenceStore.ts` 共享状态及 settingsQueue 管理，播放器和弹幕源页只更新各自字段，失败保留脏值并允许重试；继续使用 `danmaku_preferences_v1`，SQLite及备份契约不变。

设计核对使用本机 B站9.17.0 APK静态REA，SHA256 `2b5aec7ea82223edd7bb0a1c7ccfae4aedc331b03cf2a090e032556156ce8a6f`，未启动APK或操作设备。`DanmakuSettingsDialog.j` 回退smali仍可确认字号比例读取上限2.0及独立透明度字段；`DMDisplayDensity.<clinit>` 明确NORMAL／PLENTY／STACKING三档；`DanmakuParams` 独立公开字号比例、文字样式和加粗参数；`DanmakuFontSwitchPreference.setChecked/b` 获取独立字体模块并查找 `SourceHanSansCN-Regular.otf`。这不证明本机已下载该模块，也不确定B站实际渲染器的全部描边像素、默认值或字形版本；本项目采用官方同字体家族的可变字体与自己的绘制规则，没有复制B站代码或动态字体资产。REA完整Evidence、DEX切片来源与未知项保存在本地 `dist/audit/danmaku-rea`，代表性ID见该目录 findings.md。最终字形、窄屏触控、字体回退、保存恢复和性能由用户设备验收。
