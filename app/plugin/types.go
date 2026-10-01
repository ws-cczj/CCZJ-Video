// Package plugin 实现「扩展包」：扫描 <dataDir>/plugins、校验每个包的
// plugin.json、维护启用/禁用状态，并把结果交给界面。
//
// 包只提供数据（plugin.json + JSON/GLSL/图片/前端脚本），本包从不执行任何东西；
// 真正的执行发生在前端（script 段由 frontend/src/plugins 注入 webview），
// 所以这里的边界是「读得到什么」而不是「跑得了什么」：越出包目录的路径、
// 超大文件、未知字段一律整包判 invalid，绝不做「忽略未知字段」式的宽容。
package plugin

import (
	"encoding/json"
	"regexp"
)

// Status 是注册表里每个包的三种状态之一（docs/plugins.md §2）。
type Status string

const (
	StatusReady    Status = "ready"
	StatusDisabled Status = "disabled"
	StatusInvalid  Status = "invalid"
)

// Info 是扫描结果，也是 Wails 绑定直接交给前端的结构，字段名即 JSON 契约。
type Info struct {
	ID          string            `json:"id"`
	Kind        string            `json:"kind"`
	Version     string            `json:"version"`
	Name        map[string]string `json:"name"`
	Description map[string]string `json:"description"`
	// Author 来自 manifest，只用于展示：扩展包是第三方内容，界面要能看出是谁写的。
	Author     string `json:"author,omitempty"`
	Dir        string `json:"dir"`
	Status     Status `json:"status"`
	ReasonCode string `json:"reason_code"`
	Reason     string `json:"reason"`
	// Source / Shader / Theme 恰好一个非 nil，与 Kind 对应；Status=="invalid" 时三个都是 nil。
	// Script 是横切段：任何 kind 都可以带，kind=script 时它是唯一的那一节。
	Source *SourceSection `json:"source,omitempty"`
	Shader *ShaderSection `json:"shader,omitempty"`
	Theme  *ThemeSection  `json:"theme,omitempty"`
	Script *ScriptSection `json:"script,omitempty"`
	// Icon 是 manifest 声明的包内相对图标路径，已校验存在、扩展名合法、够小。
	// 界面用 ReadAsset(id, icon) 取它；空串表示这个包没提供图标。
	Icon string `json:"icon,omitempty"`
	// Permissions 是 manifest 声明要用哪几条能力（见 packPermissions）。声明只说明
	// 「这个包会用到」；用户给没给记在注入运行时那一本账里，两者分得很开。
	Permissions []string `json:"permissions,omitempty"`
	// Files / Bytes / Updated 是扫描时从包目录实际数出来的，不来自 manifest：
	// 包就是用户自己放进去的一个文件夹，界面得能说出里面有多少东西、最后一次
	// 改动是什么时候——那是判断「我刚改的文件生效了没有」唯一诚实的依据。
	Files   int    `json:"files"`
	Bytes   int64  `json:"bytes"`
	Updated string `json:"updated,omitempty"`
	// Builtin 标记「这个包是应用自带的」（app/plugin/builtin/ 里那批）。界面据此
	// 决定给不给卸载按钮：删掉别人的包是卸载，删应用自己的东西不是。
	Builtin bool `json:"builtin"`
}

// Adapter 是 kind=source 包里的一个采集适配条目（docs/plugins.md §3）。
type Adapter struct {
	ID   string            `json:"id"`
	Name map[string]string `json:"name"`
	// Strategy 是包里的 strategy 文档，已按 source-strategy-v2 schema 校验过形状，
	// 原样转给界面写进 sources.strategy_config。
	Strategy json.RawMessage `json:"strategy"`
}

// SourceSection 对应 manifest 的 source 段。
type SourceSection struct {
	Adapters []Adapter `json:"adapters"`
}

// ShaderEntry 是 kind=shader 包里的一个自定义档位（docs/plugins.md §4）。
type ShaderEntry struct {
	ID       string            `json:"id"`
	Name     map[string]string `json:"name"`
	Entry    string            `json:"entry"` // 包内相对路径，已校验确实存在
	Scale    int               `json:"scale"` // 1 或 2
	Category string            `json:"category"`
}

// ShaderSection 对应 manifest 的 shader 段：单个对象与 shaders 数组都归一化到这里。
type ShaderSection struct {
	Shaders []ShaderEntry `json:"shaders"`
}

// ThemePreset 是校验通过的主题预设（docs/plugins.md §5）。
type ThemePreset struct {
	ID      string            `json:"id"`
	Name    map[string]string `json:"name"`
	Primary string            `json:"primary"`
	Mode    string            `json:"mode"`
	Tint    map[string]string `json:"tint,omitempty"`
	// BGImage 是包内相对路径（已校验存在且没越界）；界面用 Service.ReadAsset(Info.ID, BGImage) 取图。
	BGImage string `json:"bg_image,omitempty"`
	// Pack 是归属的扩展包 id，读包内文件时要用它定位目录。
	Pack string `json:"pack"`
}

// ThemeSection 对应 manifest 的 theme 段。
type ThemeSection struct {
	Presets []ThemePreset `json:"presets"`
}

// ScriptSection 对应 manifest 的 script 段：前端注入的入口脚本与样式表。
// 路径都是包内相对路径，校验时已确认存在、扩展名合法且没越出包目录；
// 内容不在这里读——前端拿 ReadPluginFile(id, entry) 取源码，好让重扫之后不缓存旧代码。
type ScriptSection struct {
	Entry  string   `json:"entry"`
	Styles []string `json:"styles,omitempty"`
}

// 数量与体积上限（docs/plugins.md §6）。超限整包判 invalid，绝不截断。
const (
	// maxPacks 是 plugins 目录下的扩展包数量上限。
	maxPacks = 64
	// maxManifestBytes: plugin.json 256 KiB。
	maxManifestBytes = 256 << 10
	// maxShaderBytes: 单个 .glsl 2 MiB。
	maxShaderBytes = 2 << 20
	// maxScriptBytes: 注入用的单个 .js / .css 2 MiB。
	maxScriptBytes = 2 << 20
	// maxImageBytes: 单张背景图 8 MiB。
	maxImageBytes = 8 << 20
	// maxIconBytes: manifest.icon 指向的那张小图 256 KiB。图标出现在每一行左上角，
	// 一张 HD 壁纸塞进来会把扩展包列表变成一次图片浏览器。
	maxIconBytes = 256 << 10
	// maxTextReadBytes: 界面经 ReadFile 读包内文本文件的上限。
	maxTextReadBytes = 2 << 20
	// maxInstallFiles / maxInstallTotalBytes: 一次拖放装一个包允许的文件数与总字节。
	// §6 里各段的单项上限照旧生效，这里限的是「整个文件夹」的总量。
	maxInstallFiles      = 256
	maxInstallTotalBytes = 16 << 20
	// maxInstallPathChars: 包内相对路径长度。Windows 的 MAX_PATH 是 260，而数据
	// 目录本身已经吃掉一截，所以深层子目录在落盘时就会撞墙——提前拒绝比让
	// CreateFile 报一句看不懂的对用户友好。
	maxInstallPathChars = 240

	maxNameChars        = 64
	maxDescriptionChars = 200
	maxAuthorChars      = 64

	manifestFileName = "plugin.json"
	// settingsKey 是启用/禁用状态在 settings KV 表里的键（docs/adr/0007）。
	settingsKey = "plugin_states"
)

var (
	// 包 id：docs/plugins.md §2，且必须与目录名相同。
	packIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)
	// 段内条目 id（shader / theme 预设）：§4 §5 的 ^[a-z][a-z0-9_-]{0,31}$。
	entryIDPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)
	// 适配条目 id：沿用包 id 的字符集，包内唯一。
	adapterIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)
	versionPattern   = regexp.MustCompile(`^\d+\.\d+\.\d+$`)
	hexColorPattern  = regexp.MustCompile(`^#[0-9a-fA-F]{3}$|^#[0-9a-fA-F]{6}$`)
	// tint 的键是内置配色通道名：那份通道表是 camelCase（bgApp / bgSidebar / textSecondary…），
	// 所以这里必须放行大写字母，否则真实通道名会被当成非法键名拒掉。
	tintKeyPattern = regexp.MustCompile(`^[a-z][A-Za-z0-9_]{0,31}$`)
)

// kinds 是 manifest.kind 的合法取值（docs/plugins.md §2）。
var kinds = map[string]bool{"source": true, "shader": true, "theme": true, "script": true}

// packPermissions 是 manifest.permissions 能声明的那几条（docs/plugins.md §2.1）。
// Go 只判「这条声明合不合法」；真正的授权在注入运行时里问用户要，账也记在那边
// （settings 表的 plugin_permissions），所以这里不存任何同意状态。
var packPermissions = map[string]bool{"write": true, "network": true}

// permissionNames 是给错误消息和界面用的固定顺序表，别用 map 迭代顺序（那是随机的）。
var permissionNames = []string{"write", "network"}

// strategyNames 是 docs/source-strategy-v2.schema.json 里 strategy 的枚举。
var strategyNames = map[string]bool{
	"standard_cms":  true,
	"cms_videolist": true,
	"declarative":   true,
	"custom":        true,
}

// textExtensions 是 ReadFile 允许当作文本返回的扩展名。script 包要的正是
// .js/.mjs/.css 的源码，所以这三种从「一律不给读」变成「和 glsl 同等待遇」：
// 读出来只是字符串，要不要执行由前端决定。
var textExtensions = map[string]bool{
	".json": true,
	".txt":  true,
	".md":   true,
	".glsl": true,
	".vert": true,
	".frag": true,
	".yaml": true,
	".yml":  true,
	".csv":  true,
	".js":   true,
	".mjs":  true,
	".css":  true,
}

// scriptExtensions 是 script.entry 认的扩展名；样式表另说，只收 .css。
var scriptExtensions = map[string]bool{".js": true, ".mjs": true}
var cssExtensions = map[string]bool{".css": true}

// imageMimes 是 ReadAsset 认的图片类型，键为小写扩展名。
var imageMimes = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".webp": "image/webp",
	".gif":  "image/gif",
	".bmp":  "image/bmp",
	".avif": "image/avif",
}

// shaderCategories 是 §4 的 category 取值。
var shaderCategories = map[string]bool{"anime": true, "film": true}

// themeModes 是 §5 的 mode 取值。
var themeModes = map[string]bool{"light": true, "dark": true}
