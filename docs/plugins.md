# CCZJ Video 扩展包规范（plugin.json / manifest_version 1）

扩展包是数据目录下的一个文件夹：一份 `plugin.json` 加若干包内文件。应用扫它、严格校验它，
再把里面声明的东西接进对应的引擎。四类包里 `source` / `shader` / `theme` 只提供配置与素材，
由内置引擎解释执行；`script` 这一类提供**前端 JS/CSS，由应用亲自注入界面**——这是个个人使用的
软件，允许用户按自己的意思改界面，边界是「不动内核」（§6.5）。

这样做的理由与取舍记录在
[`docs/adr/0007-declarative-extension-packs.md`](adr/0007-declarative-extension-packs.md)（声明式那一层）与
[`docs/adr/0008-injected-frontend-packs.md`](adr/0008-injected-frontend-packs.md)（放开脚本这一步）。

---

## 1. 目录与加载

```
%APPDATA%\CCZJ Video\
  plugins\
    my-pack\
      plugin.json      ← 必需，文件名与位置固定
      shader.glsl       ← 包内数据文件，由 manifest 引用
      bg.jpg
```

- 扫描深度只有一层：只有 `plugins/<目录>/plugin.json` 会被当作扩展包。
- 目录名以 `.` 或 `_` 开头的直接跳过（留给临时目录、备份目录）。
- 目录里没有 `plugin.json` 就当作「这不是扩展包」跳过：不报错、不占数量上限，
  免得一个备份文件夹凭空多出一张错误卡片。
- 注册表在第一次被取用时扫一次盘；设置页「扩展」分组还能手动重扫，不需要重启。
- 卸载有两种：卡片上的「卸载」按钮，或直接删掉那个目录。按钮就是 `os.RemoveAll` 那一步，
  不放进任何回收站，所以删了只能重新拖一次；停用而不删除请点同一行的「停用」按钮。
  应用自带的两个包（§1.2）不给按钮，Go 侧也会拒绝删它们。

### 1.1 拖进去也算安装

把包文件夹从资源管理器拖进**应用窗口的任意位置**即可（不必先跑去设置页），
不必自己找到 `plugins` 目录：

1. 原生拖放交给应用的只有被拖条目的**绝对路径**（WebView2 的 `File` 对象读不出目录
   内容），所以读目录、丢杂物、复算上限全在 Go 侧；一次拖多个文件夹会逐个装，
   一个坏掉不影响其余。
2. Go 侧先在 `<dataDir>/plugin-staging/install-<随机名>/` 里按那份路径复制出来，再按 §2
   校验；**校验通过才会出现在 `plugins/` 里**。失败时整个临时目录删掉，等于什么都没装。
   拖进来的原始文件夹自始至终只读，不会被移动或改写。
3. 同名目录已存在时：旧包先挪进同一临时区、新包挪进来，中间任何一步失败都会把旧的
   放回原位——不会因为一次失败的安装同时失去新旧两份。用户的停用/启用状态跟着 `id`
   保留，重装一次不会把他关掉的包又打开。
4. 装完立刻重扫，卡片上直接显示这一包的图标与统计；每张卡片的成功/失败都有回执。

拖进来仍要满足全部规则（`id` 与目录名一致、体积上限、符号链接一律不跟），拖拽不是后门。
`.git`、`Thumbs.db`、`desktop.ini`、`__MACOSX` 这类随行文件会被安装与统计一起忽略。

### 1.2 内置扩展包

`plugins/motion-effects`（设置页的「动画」分组）、`plugins/logs-panel`（「日志」）与
`plugins/diagnostics-panel`（「诊断」）由应用自带，**第一次启动落盘一次**，之后只做
「补缺失的文件」这一件事：

- 删掉这几个文件夹，设置页就真的少了那几项。它们在界面上**没有「卸载」按钮**（删应用
  自己的东西不叫卸载），Go 侧 `Uninstall` 也按目录名拒绝；想让它不出现，点「停用」或者
  自己删那个目录。
- 已经种过的包只往里补**磁盘上没有**的文件（应用新版本给内置包加了图标就走这条路）：
  改过的 `main.js` 不会被覆盖，删掉的包也不会在下次启动复活。
- 面板本体（`MotionPanel.vue` / `LogPanel.vue` / `DiagnosticsPanel.vue`）仍编译在应用里，
  包只负责把它挂上分组条（§6.2 的 `settingsTab` + `components`）；因此这些包坏了不会拖垮
  设置页其余部分，最坏是那一项不出现。
- `motion-effects` 比另外两个多带一份 `motion.css`：那是全应用动效的**参数层**（时长与
  缓动的 CSS 变量），注入在 `<head>` 末尾，所以包在用期间由它覆盖 `animations.css` 里的
  同名基线。改那个文件就能把动画调快调慢，不必重新编译应用；停用它只是退回基线数字，
  「关闭动画」的退化规则写在应用里，不受包在不在影响。
- 想加回：从应用源码 `app/plugin/builtin/` 复制那个文件夹进 `plugins`，或者自己写一个
  同样调 `cczj.settingsTab` 的包。


## 2. manifest 通用字段

```json
{
  "manifest_version": 1,
  "id": "my-pack",
  "version": "1.0.0",
  "name": { "zh-CN": "我的扩展包", "en": "My Pack" },
  "description": { "zh-CN": "一句话说明", "en": "One line." },
  "author": "your-name",
  "kind": "shader",
  "icon": "art/icon.png",
  "permissions": ["write", "network"],
  "shader": { ... }
}
```

| 字段 | 必填 | 规则 |
| --- | --- | --- |
| `manifest_version` | 是 | 只能是 `1`。 |
| `id` | 是 | `^[a-z0-9][a-z0-9_-]{0,63}$`，**必须与所在目录名相同**；不同包之间不得重复。 |
| `version` | 是 | `^\d+\.\d+\.\d+$`。 |
| `name` | 是 | 对象，`zh-CN` 与 `en` 至少给一个（不能是空串）；每个语言的取值最长 64 字符；当前界面语言缺失时回落到另一个。 |
| `description` | 否 | 同 `name` 规则，最长 200 字符。 |
| `author` | 否 | 最长 64 字符，仅作展示。 |
| `kind` | 是 | `source` \| `shader` \| `theme` \| `script`。 |
| `icon` | 否 | 包内相对路径，指向一张不超过 **256 KiB** 的 `.png/.jpg/.jpeg/.webp/.gif/.bmp/.avif`（就是 §7 那份图片白名单，`svg` 不在里面）；设置页的扩展包卡片会显示它。路径规则与 §2 末尾的 `path_invalid` 完全一致，文件不存在或过大都整包判 `invalid`。**不写这一行也有图标**：包根目录里的 `icon.png`/`icon.jpg`/`icon.jpeg`/`icon.webp`/`icon.gif` 会按这个顺序自动认（太大或不是文件则当作没有，不影响校验）。两者都没有时卡片退化成按 kind 画的形状。 |
| `source` / `shader` / `theme` / `script` | 见下 | **只允许出现与 `kind` 匹配的那一节**，其他节存在即报错。`script` 是唯一的例外：它是横切段，任何 kind 都可以额外带一份（§6）。 |
| `permissions` | 否 | 数组，元素只能是 `write` \| `network`（§2.1）。名字写错、重复出现都整包判 `invalid`——拼错的那条会静默失效，而这条链上静默失效的意思是「包以为自己有权，结果一动就崩」。 |

校验是严格的：出现表里没有的字段就整包判 `invalid`，不做「未知字段忽略」。
宁可让你立刻看到写错了，也不要一个拼错的键静默不生效。

### 2.1 `permissions` — 写操作与出网的授权闸门

只有带 `script` 段的包会真的被拦：声明是给闸门看的，闸门只在注入运行时里生效。
两条能力：

| 能力 | 拦住的是什么 | 放行的是什么 |
| --- | --- | --- |
| `write` | `cczj.bindings` 里所有**不是只读**的绑定——`SetPluginEnabled`、`StartCollect`、`DeleteVideo`、`SetSetting`、`UninstallPlugin` 这一类会改动应用数据或状态的调用。 | 名字以 `Get` / `List` / `Find` / `Read` / `Search` / `Check` / `Is` / `Has` / `Count` / `Query` 开头的，外加 `ProxyImage`、`DoubanSearch`、`WindowGetSize` 等一份显式只读名单。判不准时一律算写：宁可让你多点一次「允许」，也不放过一次悄悄改了库的调用。 |
| `network` | 包代码里的 `fetch(...)` 往**应用外面**发请求。 | 应用自己的请求（播放器取分片、探测、图片代理）完全不经过闸门。递给应用自己主机的也一律直通：Wails 调后端绑定本身就是一次打到 `wails.localhost` 的 `fetch`，包只要调一个绑定就会撞进闸门——那拦的不是「出网」，是应用自己在说话。 |

行为是这样的：

1. **没声明就连问都不问**，调用当场失败并写一条 WARN 日志。声明是前提，不是提示。
2. **声明了就在第一次真用到的那一刻弹一次确认框**，标题写清楚是哪个包、要做什么、这一次具体是哪次调用。
3. 答复记在 settings 表的 `plugin_permissions` 里，**重启还在**：`已允许`就一路放行，`已拒绝`就一路拦着，两种都不再骚扰你。
4. 设置页 → 扩展包的卡片上能看到「声明的能力」和当前答复，点 **撤销** 就回到「还没问过」——下次它真动手时会重新问你。拒绝不是黑名单，改主意随时可以。
5. 每一次允许 / 拒绝 / 因未声明被拦下都会往日志时间线写一条，来源标成包 id。

说清楚它不是什么：**这不是沙箱**。包代码跑在应用自己的 realm 里，`XMLHttpRequest`
和 `WebSocket` 就明晃晃地没被拦——前者是播放内核在用的东西，动它等于把播放器拖下水。
另一条现成的旁路是
`cczj.stores`：包去调应用自己的 action 时，那条调用用的是应用 import 进去的绑定，不经过
`cczj.bindings` 这层代理，因此也不拦。这一层的价值是「你点过一次才算数」，加上把坏代码的
破坏停在第一次调用上，而不是提供隔离。

### 卡片上那行数字来自磁盘

每张扩展包卡片副标题下的 `N 个文件 · X KB · 最后改动 时间` 不是 manifest 里写的，
而是扫描时真的走进那个目录数出来的（`WalkDir` + `Stat`）：

- **`invalid` 的包同样统计**——这正是你需要它的时候：对着「有几个文件、什么时候改的」
  才能判断拖进来的到底是不是你改过的那一份。
- `.git`、`Thumbs.db`、`desktop.ini`、`__MACOSX` 这类随行文件不计入，也不在安装时落盘。
- `icon` 走 `ReadAsset(id, path)` 读，与包内其他图片同一套路径与体积限制（§7）。

### 校验失败会看到什么

注册表给每个包三种状态之一：

- `ready` — 通过校验且已启用。
- `disabled` — 通过校验，但用户在设置页关掉了。
- `invalid` — 未通过校验，附带机器可读的原因码 + 人话说明，设置页直接展开显示。

原因码：`manifest_too_large`、`manifest_unreadable`、`json_invalid`、`unknown_field`、
`manifest_version`、`id_invalid`、`id_dir_mismatch`、`id_duplicate`、`version_invalid`、
`name_missing`、`kind_invalid`、`kind_section`、`permission_invalid`、`section_missing`、`path_invalid`、
`path_missing`、`path_too_large`、`entry_empty`、`scale_invalid`、`resolution_changing_pass`、
`limit_exceeded`，以及四段各自的 `strategy_invalid` / `shader_invalid` / `theme_invalid` / `script_invalid`。

`path_invalid` 覆盖所有「想越出包目录」的写法：绝对路径、盘符、反斜杠、任何 `..` 片段，
以及符号链接的真身在包外（链接指向包外一律拒绝）。

单个包的任何错误都不影响其他包，也不影响内置功能。

上面这套 `reason_*` 只描述「这个文件夹不是一个合法扩展包」。装包与卸包时撞到的盘上的事
走另一套通用码（ADR 0009：`app/apperror`）——临时目录建不出来、旧包挪不走、删除失败是
`STORAGE`，拖进来的路径已经不存在是 `NOT_FOUND`，想卸应用自带的那三个包是 `CONFLICT`，
读不出非文本资源是 `UNSUPPORTED`。界面 `normalizeApiError` 把 `CODE: 说明` 拆开后只显示说明，
所以这些失败在卡片上是一句人话，而原因码仍然可以按 `code` 分支。

## 3. `kind: source` — 采集源适配包

提供**采集策略**，不是新驱动。内置的 MAC CMS 策略始终是参考实现；适配包只能声明
「怎么拼 URL、怎么读返回信封、字段叫什么」，这些描述会被写进采集源已有的
`sources.strategy_config` 列（见 ADR 0001：包不能建表、不能自己造 `source_key`）。

```json
{
  "kind": "source",
  "source": {
    "adapters": [
      {
        "id": "demo_cms",
        "name": { "zh-CN": "示例 CMS 适配", "en": "Demo CMS" },
        "strategy": {
          "version": 2,
          "strategy": "declarative",
          "list":   { "action": "videolist", "page_param": "pg", "limit_param": "limit", "extra": { "ac": "videolist" } },
          "search": { "action": "videolist", "keyword_param": "wd", "page_param": "pg" },
          "detail": { "action": "detail", "id_param": "ids" },
          "response": { "list_path": "list", "code_path": "code", "ok_codes": ["1"], "pagecount_path": "pagecount" },
          "field_mapping": { "vod_name": "name", "vod_pic": "cover" }
        }
      }
    ]
  }
}
```

`strategy` 一节必须能通过 [`docs/source-strategy-v2.schema.json`](source-strategy-v2.schema.json)。
逐字段语义以那份 schema 为准，这里是要点：

- `list` / `search` / `detail` 三个操作各自描述：动作值、动作参数名（`action_param`）、
  分页参数名、条数参数名、类型参数名、关键词参数名、时间参数名、ID 参数名，
  以及 `extra` 里的固定附加参数。
- `response` 描述返回信封：列表在哪个路径、总页数/总数在哪、成功码是什么。路径是点号
  分隔的 JSON 路径（`data.list`、`result.0.list`），不写就走 MAC CMS 的默认信封
  （`list` / `page` / `pagecount` / `total` / `code == 1`）。写了 `ok_codes` 就必须
  至少有一项（数组元素是字符串）。
- `field_mapping` 把源侧字段名映射到内置视频字段；不写则用内置别名表。键值都必须是字符串。
- 适配包的 `id`（`demo_cms`）字符集同包 `id`（`^[a-z0-9][a-z0-9_-]{0,63}$`），
  在同一包内唯一，跨包不要求唯一。

**怎么用**：采集源管理页 → 编辑某个源 → 展开「高级选项」→「采集适配」里选中这个适配，
保存时它的 `strategy` 就被写进该源的 `strategy_config`。之后采集、搜索、详情都按这份声明走，
包被删掉也不影响已经配置好的源（界面上那一行会显示「保留现有配置」，不会替你改回内置）。

安全边界：URL 由 `sources.api_url`（用户在采集源页填的）和声明的参数名拼成，适配包**不能**
指定请求打到哪台主机，也不能注入 header。

## 4. `kind: shader` — 播放器着色器包

给播放器的「画质增强」下拉框增加一个自定义档位。着色器用 **mpv hook 语法**书写，
这正是内置影视增强引擎解析 FSRCNNX 模型用的语法，所以网上大量现成的 mpv 锐化/去噪/调色
着色器可以直接放进来。

```json
{
  "kind": "shader",
  "shader": {
    "id": "soft_sharpen",
    "name": { "zh-CN": "柔和锐化", "en": "Soft Sharpen" },
    "entry": "shader.glsl",
    "scale": 2,
    "category": "film"
  }
}
```

| 字段 | 必填 | 规则 |
| --- | --- | --- |
| `id` | 是 | 包内唯一，`^[a-z][a-z0-9_-]{0,31}$`；画质偏好里存的是 `ai_custom_<包 id>/<这个 id>`。 |
| `name` | 是 | 下拉框显示的文字，规则同 manifest `name`。 |
| `entry` | 是 | 包内相对路径，指向 `.glsl` 文件，≤ 2 MiB，至少含一个 `//!HOOK`。 |
| `scale` | 否 | `1` 或 `2`，默认 `2`。输出分辨率 = 源分辨率 × scale。 |
| `category` | 否 | `anime` \| `film`，默认 `film`，只影响分组显示。 |

一个包可以带多个档位：把 `shader` 写成 `{ "shaders": [ {…}, {…} ] }` 即可，数组里每一项
的字段与上表完全一样，两种写法都合法（但只能选一种：数组写法里再出现 `id` / `entry`
就是未知字段）。注册表把它们统一归一化成列表返回给界面。

引擎的约束（写包前必须知道）：

1. 支持的指令：`//!HOOK`、`//!BIND`、`//!SAVE`、`//!DESC`、`//!COMPONENTS`。
2. **每个 pass 都在源分辨率上运行**。`//!WIDTH` / `//!HEIGHT` 里出现 `2 *`
   这类改变分辨率的声明会被拒绝（`resolution_changing_pass`，原因里会把那一行原样引出来）
   ——放大由引擎负责，包只做同分辨率处理（锐化、去噪、去色带、调色、反交错）。
   这条判定与 WebGL 执行器 `frontend/src/utils/filmUpscaler.ts` 的 `parseMpvShader`
   用的是同一个字面规则：先按行 trim，再看 `//!WIDTH ` / `//!HEIGHT ` 前缀里有没有 `2 *`。
3. `LUMA` 绑定按 Rec.709 亮度取值；`ORIGINAL` / `SOURCE` 指向上一个有效输入。
4. pass 之间用 `//!SAVE` 的名字串联，只能引用前面 pass 存下的纹理。
5. body 结尾必须给 `res` 赋值（引擎生成 `out = res`）。
6. 无 MRT、无 compute、无外部纹理输入。

编译失败（GL 报错）时：这个档位在本机标记为「不可用」并附错误摘要，自动回落原高清；
其他档位与内置引擎不受影响。标记持久化在本地，重启后依然可见。

## 5. `kind: theme` — 主题扩展包

追加主题预设。字段与内置预设一致：给 `primary` + `mode`，其余颜色由内置配色算法派生，
`tint` 只覆盖你明确想改的通道。

```json
{
  "kind": "theme",
  "theme": {
    "presets": [
      {
        "id": "dusk",
        "name": { "zh-CN": "暮色", "en": "Dusk" },
        "primary": "#6b4fbb",
        "mode": "dark",
        "tint": { "bgApp": "#141018", "bgSidebar": "#191325" },
        "bg_image": "dusk.jpg"
      }
    ]
  }
}
```

| 字段 | 必填 | 规则 |
| --- | --- | --- |
| `presets[].id` | 是 | 包内唯一，`^[a-z][a-z0-9_-]{0,31}$`。 |
| `presets[].name` | 是 | 规则同 manifest `name`。 |
| `presets[].primary` | 是 | `#rgb` 或 `#rrggbb`，大小写都行；注册表里统一成小写。 |
| `presets[].mode` | 是 | `light` \| `dark`。 |
| `presets[].tint` | 否 | 键必须是内置配色通道名（`bgApp`/`bgCard`/`bgSidebar`/`border`/`textSecondary`/`danger`… 全集见 `frontend/src/stores/theme.ts` 的 `ColorPalette`），值只接受 `#rgb` 或 `#rrggbb`。认不出的通道名会被界面直接忽略，内置预设里那层 `rgba(...)` 半透明表面暂时写不进包里。 |
| `presets[].bg_image` | 否 | 包内相对路径指向图片，≤ 8 MiB；扩展名限 `png` / `jpg` / `jpeg` / `webp` / `gif` / `bmp` / `avif`。 |

只有一个预设时可以把预设字段直接写在 `theme` 里（不套 `presets` 数组），两种写法都合法，
注册表统一按数组返回。

背景图从包的磁盘路径解析，不走内置预设的「打包资源指纹」那条路，因此升级版本不会让
扩展主题的黑底图失效。界面读它的方式是「按包 id + 包内相对路径取 data URL」，
所以 `bg_image` 必须写在包里，不能引用外部 URL。

扩展包主题和用户自建主题一样可以进主题编辑器微调；微调结果存进用户自己的
`theme_customs`，不回写包文件。

## 6. `kind: script` — 前端注入包

给界面加自己的 JS 与 CSS。`entry` 指向的脚本在界面挂载之后当成 ES 模块跑一遍，拿到一份 `cczj`
接口（§6.2）：可以加页面、加侧边栏入口、注入全局样式、包住应用自己的方法、订阅后端事件、
调用应用的任何后端能力。

```json
{
  "kind": "script",
  "script": {
    "entry": "main.js",
    "styles": ["inspector.css"]
  }
}
```

| 字段 | 必填 | 规则 |
| --- | --- | --- |
| `entry` | 是 | 包内相对路径，扩展名 `.js` / `.mjs`，≤ 2 MiB，必须存在且不为空（全是空白也算空）。 |
| `styles` | 否 | 包内相对路径数组，只收 `.css`，其余规则同 `entry`；注入顺序即数组顺序，且整批都在 `entry` 之前。 |

`script` 是横切段：`kind` 不是 `script` 的包也可以额外带一份（主题包捎一段自己的样式是合法用法），
带了就按同样规则校验、同样注入。路径校验与别处一致：绝对路径、盘符、反斜杠、任何 `..` 片段、
以及真身在包外的符号链接都直接判 `invalid`。

### 6.1 入口约定

三种写法认一种，都不满足就判这个包没有入口：

```js
export function setup(cczj) { ... }      // 具名 setup
export default function (cczj) { ... }   // default 就是函数
export default { setup(cczj) { ... } }   // default 是带 setup 的对象
```

`setup` 可以是 `async`。它同步抛出的异常、以及它 `await` 的东西 reject 出来的异常，都算这个包注入失败。

### 6.2 `cczj` 接口

| 成员 | 给什么 |
| --- | --- |
| `pack` | 这个包自己的 manifest 摘要：`id` / `kind` / `version` / `name` / `dir`（磁盘目录）/ `icon`（§2 那张卡片图标的包内路径，没写就是空串），只读。 |
| `bindings` | 应用全部 Wails 绑定（`frontend/src/api/app` 那个 ES module 命名空间）加 `normalizeApiError`。**只读**：命名空间对象改不动，所以 `intercept` 包住它上面的函数既不影响应用自己的调用点、也不会生效。要改后端能力的行为，去包 store 的 action，或者直接自己调绑定。另外它外面套了一层授权闸门（§2.1）：只读那批直通，其余调用要先有 `write` 授权才会真的发出去，返回值照旧是 Promise，包的写法不用改。 |
| `router` | vue-router 实例：`push` / `replace` / `currentRoute`。 |
| `vue` | `h` `ref` `shallowRef` `reactive` `computed` `watch` `watchEffect` `nextTick` `markRaw` `toRaw` `defineComponent` `onMounted` `onBeforeUnmount`。包代码没有 import 能力，组件全靠这些。 |
| `stores` | 九个 pinia store：`video` `source` `theme` `plugins` `logs` `download` `collect` `confirm` `error`。取用时才实例化，拿到的是应用那一份单例。 |
| `i18n` | `t(key, named?)`；`merge(messages)` 往应用语言包里并键，建议挂在 `pluginPacks.<命名空间>.*` 下别撞内置键——命名空间请用不带连字符的标识符，包 id 里的连字符会让 vue-i18n 的点号路径解析失败（得改写成 `pluginPacks['my-pack'].x`）；`locale` 读当前语言（读了就建立响应式依赖，切换语言时包里的 `computed` 会重算）。 |
| `events.on(name, handler)` | 订阅后端事件（`collect:done`、`app:log`…），返回退订函数，包卸载时自动退订。 |
| `storage` | `get(key, fallback)` / `set(key, value)` / `remove(key)`，落在 localStorage 的 `plugin:<包 id>:` 命名空间下。**删包或停用都不清它**——用户的偏好不该跟着包一起删。 |
| `log.info/warn/error(message, detail?)` | 写进应用的日志时间线（设置页 → 日志），来源标成包 id。生产构建把 `console.log` 整条 drop 了，这里是包唯一能被看见的出口。 |
| `nav({ path, label, icon })` | 在侧边栏「管理」区加一项。`label` 要写成函数才会跟随语言切换；`icon` 是内置图标名（全集见 `frontend/src/components/Icon.vue` 的分支，认不出来时回落到通用形状），不填用 `code`。返回撤销函数。 |
| `route(path, component)` | 追加一条路由：`path` 以 `/` 开头、不含 `..`，且不能与内置页面或其他包登记过的路径撞车（撞了直接抛，本包进隔离）。组件必须是渲染函数写法。返回撤销函数。 |
| `css(text)` | 注入一段全局 `<style>`。返回撤销函数。 |
| `settingsTab({ id, label, component, icon?, order? })` | 在设置页顶部的分组条上加一项，面板内容就是 `component`。`label` 写成函数才会跟随语言切换；`icon` 是内置图标名；`order` 与内置分组共用一把刻度（基础 10、主题 20、内置动画 25、扩展包 30、内置日志 40、内置诊断 50、高级 60、关于 70），不填是 50。`id` 撞上内置分组（`basic`/`theme`/`extensions`/`advanced`/`about`）直接抛——想占应用自己的分组得改应用。返回撤销函数。 |
| `components` | 应用现成的三个面板，省得包作者重抄：`MotionPanel`、`LogPanel`、`DiagnosticsPanel`（就是 §1.2 那三个内置包挂上去的东西）。已 `markRaw`，直接交给 `settingsTab` / `route` 即可。 |
| `intercept(target, method, wrapper)` | 包住一个已有对象上的方法（store 的 action、普通对象上的函数）：`wrapper(next, ...args)` 里调 `next()` 走原实现，也可以完全不调它——这就是改行为的入口。返回撤销函数，卸载时自动还原。 |
| `onDispose(cleanup)` | 登记卸载时要跑的东西：定时器、外部连接、自己插进 DOM 的节点。 |

撤销顺序是后登记的先撤销：同一个方法上叠了两层补丁时，这样还原才不会留下半截包装。

### 6.3 注入时机与生命周期

- 顺序是「先 `app.mount('#app')`，再注入」。注入型包要往 router 上加页面、往 head 里塞样式，
  有的还会立刻 `push` 路由——太早跑的话 `<router-view>` 还没就位，导航会被静默吞掉。
- 启动流程不 `await` 注入：某个包慢或者炸，都不该挡住应用起来。
- 包源码走 blob URL 的动态 `import()`。应用没有 CSP（也不打算加），这比 `eval` 干净；
  每轮都是新 URL，所以改完包点「重新扫描」一定拿到新代码。
- 一轮只跑一个 pass：启动、重扫、开关某个包都排进同一条 Promise 链，不会交错登记。
- 每轮先整表收回再重建，所以「已经不在注册表里的包」不会继续在界面上生效。

### 6.4 坏包只隔离自己

某个包注入失败时，应用做的是：

1. 撤销它这一轮已经做过的每一件事——界面上不会留下半个页面、一个没有后端的按钮或一段孤儿样式；
2. 把错误摘要记进 localStorage 的 `plugin_script_broken`，在设置页那一行展开显示，并给一个「重试」；
3. 往日志时间线写一条 `ERROR`：`<包 id> :: 扩展包注入失败，已隔离：<原因>`。

记下来的包下一轮直接跳过：坏代码每次都炸一遍的话，用户只会看到应用一直卡。改好脚本后点「重试」
会清掉记录并再注入一次。其他包与内置功能全程不受影响。

### 6.5 边界：能动什么，不能动什么

- **没有模板编译器**。应用打包的是 runtime-only Vue，`<template>` 与 `.vue` 单文件组件都用不了，
  组件写 `h()` 渲染函数。
- **没有 import**。blob URL 后面没有模块解析器，相对路径和裸模块名都解析不了；需要的一切从 `cczj` 拿。
- **没有 Node、没有文件系统、碰不到 Go 进程内**。包能触及的只有界面运行时，外加 `bindings` 那批已导出的能力。
- **改数据与出网要授权**。`bindings` 里不是只读的那批、以及往应用外面发的 `fetch`，都要先在 manifest
  声明、再由你在第一次调用时点一次「允许」（§2.1）。这一层拦的是坏代码和没打招呼的代码，
  不是恶意代码：`XMLHttpRequest` 与 `WebSocket` 没拦（前者播放内核在用），递给应用自己主机（`wails.localhost`）的请求一律直通（调后端绑定就走那条路），
  包自己再造一个 blob 模块去发请求也不在认领范围内。
- **样式是全局的**。注入的 CSS 影响整个应用，每条选择器请自己加包名前缀，否则会在别人身上生效。
- **「不动内核」的落地**：不改 Go 侧、不建表、不换 `source_key`（ADR 0001）、不覆盖内置页面。
  最后一条由 `route()` 撞路名即报错来保证——想换掉应用自己的页面，得改应用，不是改包。

## 7. 数量与体积上限

| 项 | 上限 |
| --- | --- |
| 扩展包目录数 | 64 |
| `plugin.json` | 256 KiB |
| 单个着色器文件 | 2 MiB |
| 单个脚本入口 / 样式文件 | 2 MiB |
| 单张背景图 | 8 MiB |
| `manifest.icon` 那张图 | 256 KiB |
| 界面读出来的包内文本文件 | 2 MiB |
| 一次拖放的文件数 / 总字节 | 256 个 / 16 MiB |
| 一次拖放里的单个包内路径 | 240 字符 |
| manifest 字符串字段 | 见各表 |

超限整包判 `invalid`（`limit_exceeded` / `path_too_large` / `manifest_too_large`），不做截断处理。
包数超上限时按目录名排序，前 64 个照常可用，之后带 `plugin.json` 的目录才判 `limit_exceeded`。
拖放那三条上限只在安装这一步生效（§1.1）：手工拷进 `plugins\` 的包不受文件数与总字节限制，
但 `plugin.json`、图标、各段引用的那些文件照旧逐条校验。

界面能读出来的文件类型是白名单：文本 `json` / `txt` / `md` / `glsl` / `vert` / `frag` /
`yaml` / `yml` / `csv` / `js` / `mjs` / `css`（必须是 UTF-8），图片 `png` / `jpg` / `jpeg` /
`webp` / `gif` / `bmp` / `avif`。白名单之外即使躺在包里也没有接口能把它读出来。

## 8. 打包与分发

- 装法有两种：把目录整个拷进 `%APPDATA%\CCZJ Video\plugins\`，或者直接从资源管理器
  拖进应用窗口（§1.1，整窗都是投放区，会先校验再落位）。无需安装包格式，也没有安装包。
- 卸载有两种：卡片上的「卸载」按钮，或直接删掉那个目录。按钮不做回收站、不备份，
  点下去就是 `os.RemoveAll` 那一步，删了只能重新拖一次。应用自带的两个包（§1.2）不给
  按钮。停用而不删除请点同一行的「停用」。
- 建议附一份 README 说明适配的源、依赖的接口风格；`script` 包还说清楚它加了哪些页面、
  包了哪些方法，方便用户以后想起来是谁干的。
- 前三类包（`source` / `shader` / `theme`）只有数据，应用读它们但不执行。
  `script` 包带的是前端 JS/CSS，应用会在界面挂载之后执行它——这是有意放开的（ADR 0008），
  所以判断标准从「包能不能执行代码」变成了「包只影响界面」：包碰不到 Go 侧，也换不掉内置页面。
- 包目录里出现 `.exe`、`.dll` 之类，应用没有读它们的接口，也不会加载；但它们仍在磁盘上。

## 9. 样例

`docs/examples/` 下有六份可直接使用的包，六份都带着自己的 `icon.png`（拖进去之后卡片
左上角就是那张图）：

- `docs/examples/source-adapter-demo/` — 采集源适配
- `docs/examples/shader-soft-sharpen/` — 播放器着色器
- `docs/examples/theme-dusk/` — 主题
- `docs/examples/script-ui-tweaks/` — 前端注入：一个「扩展包自检台」页面，把 §6 那套
  `cczj` 能力各用了一遍（注入样式、加侧边栏页面、包住 `loadSources` 计时、订阅 `collect:done`、
  读注册表、写自己的持久化）。拷进 `plugins\` 就能在侧边栏看到它，`icon.png` 会显示在卡片左上角。
- `docs/examples/ai-assistant/` — 前端注入的**上限样本**：一个完整的「AI 助手」页，把既有后端
  绑定当成模型的工具表来调（查片库、看采集与下载状态、开关扩展包、启停采集）。应用侧零改动，
  模型走任意 OpenAI 兼容接口，请求由包自己 `fetch` 发出。它同时把 §6.5 的两条硬边界摆在了界面上：
  出网要靠服务商放行 CORS，密钥只能明文留在本机 `localStorage`。会改动应用状态的工具一律先弹确认，
  删除、清空、还原备份、卸载包这类绑定根本不在表里。
- `docs/examples/icon-gallery/` — 专门看图标的那一份：卡片图标是包目录的 `icon.png`，
  页面里 26 枚图标先由 `icons.json` 列出清单、再逐张走 `ReadPluginAsset` 从 `icons/` 读出来。
  整个文件夹拖进设置页就能一次验完「拖拽安装 → 校验 → 图标显示」这条链路。

另有三份随应用自带的包在 `app/plugin/builtin/`（§1.2）：`motion-effects`、`logs-panel`、
`diagnostics-panel`。它们是 `settingsTab` 的参考实现——想给设置页加分组，照它们写就行。
