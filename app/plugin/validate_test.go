package plugin

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 每条校验规则单独测一遍：原因码是界面和文档里公开的契约（docs/plugins.md §2），
// 改了规则却没改这里，界面就会显示一堆没人看得懂的卡片。
func TestValidateManifestRules(t *testing.T) {
	cases := []struct {
		name        string
		manifest    string
		wantCode    string
		wantContain string
	}{
		{
			name:        "未知字段整包无效，并且消息里要点出键名",
			manifest:    `{"manifest_version":1,"id":"pack","version":"1.0.0","name":{"en":"P"},"kind":"shader","shader":{},"author":"me","homepage":"nope"}`,
			wantCode:    reasonUnknownField,
			wantContain: "homepage",
		},
		{
			name:     "manifest_version 只能是 1",
			manifest: `{"manifest_version":2,"id":"pack","version":"1.0.0","name":{"en":"P"},"kind":"shader","shader":{}}`,
			wantCode: reasonManifestVersion,
		},
		{
			name:     "id 字符合法性",
			manifest: `{"manifest_version":1,"id":"Pack_One","version":"1.0.0","name":{"en":"P"},"kind":"shader","shader":{}}`,
			wantCode: reasonIDInvalid,
		},
		{
			name:     "id 必须等于目录名",
			manifest: `{"manifest_version":1,"id":"other","version":"1.0.0","name":{"en":"P"},"kind":"shader","shader":{}}`,
			wantCode: reasonIDDirMismatch,
		},
		{
			name:     "version 必须三段",
			manifest: `{"manifest_version":1,"id":"pack","version":"1.0","name":{"en":"P"},"kind":"shader","shader":{}}`,
			wantCode: reasonVersionInvalid,
		},
		{
			name:     "name 缺 zh-CN 与 en",
			manifest: `{"manifest_version":1,"id":"pack","version":"1.0.0","name":{"ja":"こんにちは"},"kind":"shader","shader":{}}`,
			wantCode: reasonNameMissing,
		},
		{
			name:     "name 超长",
			manifest: fmt.Sprintf(`{"manifest_version":1,"id":"pack","version":"1.0.0","name":{"en":%q},"kind":"shader","shader":{}}`, strings.Repeat("x", 65)),
			wantCode: reasonLimitExceeded,
		},
		{
			name:     "description 超长",
			manifest: fmt.Sprintf(`{"manifest_version":1,"id":"pack","version":"1.0.0","name":{"en":"P"},"description":{"en":%q},"kind":"shader","shader":{}}`, strings.Repeat("y", 201)),
			wantCode: reasonLimitExceeded,
		},
		{
			name:     "author 超长",
			manifest: fmt.Sprintf(`{"manifest_version":1,"id":"pack","version":"1.0.0","name":{"en":"P"},"author":%q,"kind":"shader","shader":{}}`, strings.Repeat("z", 65)),
			wantCode: reasonLimitExceeded,
		},
		{
			name:     "kind 取值受限",
			manifest: `{"manifest_version":1,"id":"pack","version":"1.0.0","name":{"en":"P"},"kind":"shaderz","shader":{}}`,
			wantCode: reasonKindInvalid,
		},
		{
			name:     "kind 对应的段必须存在",
			manifest: `{"manifest_version":1,"id":"pack","version":"1.0.0","name":{"en":"P"},"kind":"shader"}`,
			wantCode: reasonSectionMissing,
		},
		{
			name:     "显式写成 null 的 shader 段不算提供",
			manifest: `{"manifest_version":1,"id":"pack","version":"1.0.0","name":{"en":"P"},"kind":"shader","shader":null}`,
			wantCode: reasonSectionMissing,
		},
		{
			name:     "只能出现与 kind 匹配的那一节",
			manifest: `{"manifest_version":1,"id":"pack","version":"1.0.0","name":{"en":"P"},"kind":"shader","shader":{"id":"a","name":{"en":"A"},"entry":"shader.glsl"},"theme":{"presets":[{"id":"p","name":{"en":"P"},"primary":"#fff","mode":"dark"}]}}`,
			wantCode: reasonKindSection,
		},
		{
			name:        "permissions 只认声明过的那几条能力",
			manifest:    `{"manifest_version":1,"id":"pack","version":"1.0.0","name":{"en":"P"},"kind":"shader","shader":{},"permissions":["write","shell"]}`,
			wantCode:    reasonPermissionInvalid,
			wantContain: "shell",
		},
		{
			name:     "permissions 重复声明算写坏了",
			manifest: `{"manifest_version":1,"id":"pack","version":"1.0.0","name":{"en":"P"},"kind":"shader","shader":{},"permissions":["write","write"]}`,
			wantCode: reasonPermissionInvalid,
		},
		{
			name:     "JSON 语法错误",
			manifest: `{"manifest_version":1,`,
			wantCode: reasonJSONInvalid,
		},
		{
			name:     "尾随内容也算语法错误",
			manifest: `{"manifest_version":1,"id":"pack","version":"1.0.0","name":{"en":"P"},"kind":"shader","shader":{}}{"x":1}`,
			wantCode: reasonJSONInvalid,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			service, pluginsDir, _ := testService(t)
			writePack(t, pluginsDir, "pack", testCase.manifest)
			infos, err := service.Scan()
			if err != nil {
				t.Fatalf("scan: %v", err)
			}
			if len(infos) != 1 {
				t.Fatalf("注册表应有 1 个包，实际 %d", len(infos))
			}
			info := infos[0]
			if info.Status != StatusInvalid {
				t.Fatalf("状态应是 invalid，实际 %+v", info)
			}
			if info.ReasonCode != testCase.wantCode {
				t.Fatalf("原因码 = %q，想要 %q（reason=%q）", info.ReasonCode, testCase.wantCode, info.Reason)
			}
			if info.Reason == "" {
				t.Fatal("invalid 包必须带人话说明")
			}
			if testCase.wantContain != "" && !strings.Contains(info.Reason, testCase.wantContain) {
				t.Fatalf("reason %q 应包含 %q", info.Reason, testCase.wantContain)
			}
			// 无效的包四个段都得是 nil，界面才不会去渲染半个对象。
			if info.Source != nil || info.Shader != nil || info.Theme != nil || info.Script != nil {
				t.Fatalf("invalid 包不应带任何段: %+v", info)
			}
		})
	}
}

// 权限声明的读侧契约：声明了什么就原样传出去（顺序固定），没声明就是空。
// 授权状态不在这里——那本账记在前端注入运行时，Go 只负责「这个包说了什么」。
func TestValidateManifestPermissions(t *testing.T) {
	cases := []struct {
		name     string
		extra    string
		wantJoin string
	}{
		{name: "不声明就是空", extra: "", wantJoin: ""},
		{name: "声明了两条就都传出去", extra: `,"permissions":["network","write"]`, wantJoin: "write,network"},
		{name: "只声明一条", extra: `,"permissions":["network"]`, wantJoin: "network"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			service, pluginsDir, _ := testService(t)
			packDir := writePack(t, pluginsDir, "pack",
				`{"manifest_version":1,"id":"pack","version":"1.0.0","name":{"en":"P"},"kind":"script","script":{"entry":"main.js"}`+testCase.extra+`}`)
			writeFile(t, filepath.Join(packDir, "main.js"), validScriptSource)
			infos, err := service.Scan()
			if err != nil {
				t.Fatalf("scan: %v", err)
			}
			info := infos[0]
			if info.Status != StatusReady {
				t.Fatalf("状态应是 ready，实际 %+v", info)
			}
			if got := strings.Join(info.Permissions, ","); got != testCase.wantJoin {
				t.Fatalf("permissions = %q，想要 %q", got, testCase.wantJoin)
			}
		})
	}
}

func TestValidateSourceSection(t *testing.T) {
	cases := []struct {
		name     string
		source   string
		wantCode string
	}{
		{
			name:     "strategy.version 只能是 2",
			source:   `{"adapters":[{"id":"a","name":{"en":"A"},"strategy":{"version":3,"strategy":"declarative"}}]}`,
			wantCode: reasonStrategyInvalid,
		},
		{
			name:     "strategy 名必须在枚举里",
			source:   `{"adapters":[{"id":"a","name":{"en":"A"},"strategy":{"version":2,"strategy":"magic"}}]}`,
			wantCode: reasonStrategyInvalid,
		},
		{
			name:     "list 段里的未知键",
			source:   `{"adapters":[{"id":"a","name":{"en":"A"},"strategy":{"version":2,"strategy":"declarative","list":{"action":"x","page_par":"pg"}}}]}`,
			wantCode: reasonUnknownField,
		},
		{
			name:     "顶层未知键",
			source:   `{"adapters":[{"id":"a","name":{"en":"A"},"strategy":{"version":2,"strategy":"declarative","max_pages":3}}]}`,
			wantCode: reasonUnknownField,
		},
		{
			name:     "response 未知键",
			source:   `{"adapters":[{"id":"a","name":{"en":"A"},"strategy":{"version":2,"strategy":"declarative","response":{"list_path":"list","magic":1}}}]}`,
			wantCode: reasonUnknownField,
		},
		{
			name:     "ok_codes 写了就要至少一项",
			source:   `{"adapters":[{"id":"a","name":{"en":"A"},"strategy":{"version":2,"strategy":"declarative","response":{"ok_codes":[]}}}]}`,
			wantCode: reasonStrategyInvalid,
		},
		{
			name:     "field_mapping 的值必须是字符串",
			source:   `{"adapters":[{"id":"a","name":{"en":"A"},"strategy":{"version":2,"strategy":"declarative","field_mapping":{"vod_name":1}}}]}`,
			wantCode: reasonStrategyInvalid,
		},
		{
			name:     "适配器 id 重复",
			source:   `{"adapters":[{"id":"a","name":{"en":"A"},"strategy":{"version":2,"strategy":"custom"}},{"id":"a","name":{"en":"A2"},"strategy":{"version":2,"strategy":"custom"}}]}`,
			wantCode: reasonStrategyInvalid,
		},
		{
			name:     "adapters 不能是空数组",
			source:   `{"adapters":[]}`,
			wantCode: reasonStrategyInvalid,
		},
		{
			name:     "缺 strategy",
			source:   `{"adapters":[{"id":"a","name":{"en":"A"}}]}`,
			wantCode: reasonStrategyInvalid,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			service, pluginsDir, _ := testService(t)
			manifest := fmt.Sprintf(`{"manifest_version":1,"id":"pack","version":"1.0.0","name":{"en":"P"},"kind":"source","source":%s}`, testCase.source)
			writePack(t, pluginsDir, "pack", manifest)
			info := findInfo(t, mustScan(t, service), "pack")
			if info.Status != StatusInvalid {
				t.Fatalf("应判 invalid，实际 %+v", info)
			}
			if info.ReasonCode != testCase.wantCode {
				t.Fatalf("原因码 = %q，想要 %q（reason=%q）", info.ReasonCode, testCase.wantCode, info.Reason)
			}
		})
	}
}

func TestValidateShaderSection(t *testing.T) {
	cases := []struct {
		name     string
		shader   string
		files    map[string]string
		wantCode string
	}{
		{
			name:     "scale 只能 1 或 2",
			shader:   `{"id":"a","name":{"en":"A"},"entry":"shader.glsl","scale":4}`,
			files:    map[string]string{"shader.glsl": validShaderSource},
			wantCode: reasonScaleInvalid,
		},
		{
			name:     "category 只能 anime|film",
			shader:   `{"id":"a","name":{"en":"A"},"entry":"shader.glsl","category":"cartoon"}`,
			files:    map[string]string{"shader.glsl": validShaderSource},
			wantCode: reasonShaderInvalid,
		},
		{
			name:     "entry 必须是 .glsl",
			shader:   `{"id":"a","name":{"en":"A"},"entry":"shader.txt"}`,
			files:    map[string]string{"shader.txt": validShaderSource},
			wantCode: reasonPathInvalid,
		},
		{
			name:     "entry 指向的文件不存在",
			shader:   `{"id":"a","name":{"en":"A"},"entry":"missing.glsl"}`,
			wantCode: reasonPathMissing,
		},
		{
			name:     "没有任何 //!HOOK 的着色器",
			shader:   `{"id":"a","name":{"en":"A"},"entry":"shader.glsl"}`,
			files:    map[string]string{"shader.glsl": "// 只是一段普通 GLSL\nvec4 hook() { return vec4(1.0); }\n"},
			wantCode: reasonEntryEmpty,
		},
		{
			name:     "shader id 重复",
			shader:   `{"shaders":[{"id":"a","name":{"en":"A"},"entry":"one.glsl"},{"id":"a","name":{"en":"B"},"entry":"two.glsl"}]}`,
			files:    map[string]string{"one.glsl": validShaderSource, "two.glsl": validShaderSource},
			wantCode: reasonShaderInvalid,
		},
		{
			name:     "shaders 空数组",
			shader:   `{"shaders":[]}`,
			wantCode: reasonShaderInvalid,
		},
		{
			name:     "单个对象写法里出现未知键",
			shader:   `{"id":"a","name":{"en":"A"},"entry":"shader.glsl","intensity":0.5}`,
			files:    map[string]string{"shader.glsl": validShaderSource},
			wantCode: reasonUnknownField,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			service, pluginsDir, _ := testService(t)
			packDir := writePack(t, pluginsDir, "pack", fmt.Sprintf(
				`{"manifest_version":1,"id":"pack","version":"1.0.0","name":{"en":"P"},"kind":"shader","shader":%s}`, testCase.shader))
			for name, content := range testCase.files {
				writeFile(t, filepath.Join(packDir, name), content)
			}
			info := findInfo(t, mustScan(t, service), "pack")
			if info.Status != StatusInvalid {
				t.Fatalf("应判 invalid，实际 %+v", info)
			}
			if info.ReasonCode != testCase.wantCode {
				t.Fatalf("原因码 = %q，想要 %q（reason=%q）", info.ReasonCode, testCase.wantCode, info.Reason)
			}
		})
	}
}

func TestShaderRejectsResolutionChangingPass(t *testing.T) {
	// mpv 的放大 pass 用栈式表达式声明输出尺寸（`//!WIDTH hook.w 2 *`），
	// 执行器把带 "2 *" 的行当聚合 pass；放大倍率由 scale 管，包里不许自己改分辨率。
	service, pluginsDir, _ := testService(t)
	packDir := writePack(t, pluginsDir, "pack", shaderManifestSingle("pack"))
	writeFile(t, filepath.Join(packDir, "shader.glsl"), `//!HOOK MAIN
//!BIND sharpen
//!WIDTH hook.w 2 *
//!HEIGHT hook.h 2 *
//!DESC FSRCNNX - upscale
vec4 hook() { return sharpen_tex(v_uv); }
`)

	info := findInfo(t, mustScan(t, service), "pack")
	if info.Status != StatusInvalid || info.ReasonCode != reasonResolutionChanging {
		t.Fatalf("应判 resolution_changing_pass，实际 %+v", info)
	}
	if !strings.Contains(info.Reason, "//!WIDTH hook.w 2 *") {
		t.Fatalf("reason 要引用出错的那一行，实际 %q", info.Reason)
	}
}

func TestShaderAllowsSameResolutionDirectives(t *testing.T) {
	service, pluginsDir, _ := testService(t)
	packDir := writePack(t, pluginsDir, "pack", shaderManifestSingle("pack"))
	writeFile(t, filepath.Join(packDir, "shader.glsl"), `//!HOOK ORIGINAL
//!BIND luma
//!WIDTH hook.w
//!HEIGHT hook.h
//!COMPONENTS 3
//!DESC same-resolution denoise
vec4 hook() {
    vec4 res = luma_tex(v_uv);
    return res;
}
`)

	infos, err := service.Scan()
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if info := findInfo(t, infos, "pack"); info.Status != StatusReady {
		t.Fatalf("WIDTH/HEIGHT 引用 hook.w 不该被判放大，实际 %+v", info)
	}
}

func TestValidateThemeSection(t *testing.T) {
	cases := []struct {
		name     string
		theme    string
		wantCode string
	}{
		{
			name:     "primary 必须是 #rgb 或 #rrggbb",
			theme:    `{"presets":[{"id":"p","name":{"en":"P"},"primary":"6b4fbb","mode":"dark"}]}`,
			wantCode: reasonThemeInvalid,
		},
		{
			name:     "primary 四位不行",
			theme:    `{"presets":[{"id":"p","name":{"en":"P"},"primary":"#6b4fbb00","mode":"dark"}]}`,
			wantCode: reasonThemeInvalid,
		},
		{
			name:     "tint 的值同样要是十六进制颜色",
			theme:    `{"presets":[{"id":"p","name":{"en":"P"},"primary":"#fff","mode":"dark","tint":{"ink":"black"}}]}`,
			wantCode: reasonThemeInvalid,
		},
		{
			name:     "mode 只能 light|dark",
			theme:    `{"presets":[{"id":"p","name":{"en":"P"},"primary":"#fff","mode":"midnight"}]}`,
			wantCode: reasonThemeInvalid,
		},
		{
			name:     "预设 id 非法",
			theme:    `{"presets":[{"id":"Dusk","name":{"en":"P"},"primary":"#fff","mode":"dark"}]}`,
			wantCode: reasonThemeInvalid,
		},
		{
			name:     "presets 空数组",
			theme:    `{"presets":[]}`,
			wantCode: reasonThemeInvalid,
		},
		{
			name:     "tint 的通道名不能带空格",
			theme:    `{"presets":[{"id":"p","name":{"en":"P"},"primary":"#fff","mode":"dark","tint":{"bg app":"#0d0b14"}}]}`,
			wantCode: reasonThemeInvalid,
		},
		{
			name:     "bg_image 不存在",
			theme:    `{"presets":[{"id":"p","name":{"en":"P"},"primary":"#fff","mode":"dark","bg_image":"nope.jpg"}]}`,
			wantCode: reasonPathMissing,
		},
		{
			name:     "bg_image 扩展名不支持",
			theme:    `{"presets":[{"id":"p","name":{"en":"P"},"primary":"#fff","mode":"dark","bg_image":"bg.exe"}]}`,
			wantCode: reasonPathInvalid,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			service, pluginsDir, _ := testService(t)
			manifest := fmt.Sprintf(`{"manifest_version":1,"id":"pack","version":"1.0.0","name":{"en":"P"},"kind":"theme","theme":%s}`, testCase.theme)
			writePack(t, pluginsDir, "pack", manifest)
			info := findInfo(t, mustScan(t, service), "pack")
			if info.Status != StatusInvalid {
				t.Fatalf("应判 invalid，实际 %+v", info)
			}
			if info.ReasonCode != testCase.wantCode {
				t.Fatalf("原因码 = %q，想要 %q（reason=%q）", info.ReasonCode, testCase.wantCode, info.Reason)
			}
		})
	}
}

func TestThemeAcceptsSinglePresetObject(t *testing.T) {
	service, pluginsDir, _ := testService(t)
	writePack(t, pluginsDir, "pack", `{"manifest_version":1,"id":"pack","version":"1.0.0","name":{"en":"P"},"kind":"theme","theme":{"id":"solo","name":{"en":"Solo"},"primary":"#FFF","mode":"light"}}`)

	info := findInfo(t, mustScan(t, service), "pack")
	if info.Status != StatusReady {
		t.Fatalf("单个预设写法应通过校验，实际 %+v", info)
	}
	if info.Theme == nil || len(info.Theme.Presets) != 1 {
		t.Fatalf("应归一化成 1 个预设，实际 %+v", info.Theme)
	}
	if got := info.Theme.Presets[0].Primary; got != "#fff" {
		t.Fatalf("primary 应归一化成小写，实际 %q", got)
	}
	if info.Theme.Presets[0].Pack != "pack" {
		t.Fatalf("预设要带上归属包 id，实际 %q", info.Theme.Presets[0].Pack)
	}
}

// script 段只保证「前端要读的文件在包里、够小、扩展名对」。JS 语义合不合法
// 只有跑起来才知道，那一层的失败由前端的逐包隔离处理，不在扫描阶段判。
func TestValidateScriptSection(t *testing.T) {
	cases := []struct {
		name     string
		script   string
		files    map[string]string
		wantCode string
	}{
		{
			name:     "entry 必须是 .js/.mjs",
			script:   `{"entry":"main.txt"}`,
			files:    map[string]string{"main.txt": validScriptSource},
			wantCode: reasonScriptInvalid,
		},
		{
			name:     "entry 不能是可执行文件",
			script:   `{"entry":"main.exe"}`,
			files:    map[string]string{"main.exe": "MZ"},
			wantCode: reasonScriptInvalid,
		},
		{
			name:     "entry 指向的文件不存在",
			script:   `{"entry":"main.js"}`,
			wantCode: reasonPathMissing,
		},
		{
			name:     "entry 是空文件",
			script:   `{"entry":"main.js"}`,
			files:    map[string]string{"main.js": "  \n\t\n"},
			wantCode: reasonScriptInvalid,
		},
		{
			name:     "styles 只收 .css",
			script:   `{"entry":"main.js","styles":["x.js"]}`,
			files:    map[string]string{"main.js": validScriptSource, "x.js": validScriptSource},
			wantCode: reasonScriptInvalid,
		},
		{
			name:     "styles 里的文件不存在",
			script:   `{"entry":"main.js","styles":["nope.css"]}`,
			files:    map[string]string{"main.js": validScriptSource},
			wantCode: reasonPathMissing,
		},
		{
			name:     "未知键",
			script:   `{"entry":"main.js","defer":true}`,
			files:    map[string]string{"main.js": validScriptSource},
			wantCode: reasonUnknownField,
		},
		{
			name:     "缺 entry",
			script:   `{"styles":["a.css"]}`,
			files:    map[string]string{"a.css": validCSS},
			wantCode: reasonScriptInvalid,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			service, pluginsDir, _ := testService(t)
			packDir := writePack(t, pluginsDir, "pack", scriptManifest("pack", testCase.script))
			for name, content := range testCase.files {
				writeFile(t, filepath.Join(packDir, name), content)
			}
			info := findInfo(t, mustScan(t, service), "pack")
			if info.Status != StatusInvalid {
				t.Fatalf("应判 invalid，实际 %+v", info)
			}
			if info.ReasonCode != testCase.wantCode {
				t.Fatalf("原因码 = %q，想要 %q（reason=%q）", info.ReasonCode, testCase.wantCode, info.Reason)
			}
		})
	}
}

func TestScriptSectionNormalizesPaths(t *testing.T) {
	service, pluginsDir, _ := testService(t)
	packDir := writePack(t, pluginsDir, "pack", scriptManifest("pack",
		`{"entry":"src/main.js","styles":["style/a.css","style/b.css"]}`))
	writeFile(t, filepath.Join(packDir, "src", "main.js"), validScriptSource)
	writeFile(t, filepath.Join(packDir, "style", "a.css"), validCSS)
	writeFile(t, filepath.Join(packDir, "style", "b.css"), validCSS)

	info := findInfo(t, mustScan(t, service), "pack")
	if info.Status != StatusReady {
		t.Fatalf("应通过校验，实际 %+v", info)
	}
	if info.Script == nil || info.Script.Entry != "src/main.js" {
		t.Fatalf("入口路径应原样给出（正斜杠），实际 %+v", info.Script)
	}
	if len(info.Script.Styles) != 2 || info.Script.Styles[1] != "style/b.css" {
		t.Fatalf("样式列表应保持顺序，实际 %+v", info.Script.Styles)
	}
}

// script 是横切段：kind=theme 的包也能捎一段自己的入口，两节要同时在校。
func TestScriptSectionCoexistsWithAnyKind(t *testing.T) {
	service, pluginsDir, _ := testService(t)
	packDir := writePack(t, pluginsDir, "pack", `{
		"manifest_version":1,"id":"pack","version":"1.0.0","name":{"en":"P"},
		"kind":"theme",
		"theme":{"id":"solo","name":{"en":"Solo"},"primary":"#fff","mode":"dark"},
		"script":{"entry":"main.js","styles":["x.css"]}
	}`)
	writeFile(t, filepath.Join(packDir, "main.js"), validScriptSource)
	writeFile(t, filepath.Join(packDir, "x.css"), validCSS)

	info := findInfo(t, mustScan(t, service), "pack")
	if info.Status != StatusReady {
		t.Fatalf("theme + script 应同时通过，实际 %+v", info)
	}
	if info.Theme == nil || info.Script == nil {
		t.Fatalf("两节都该在校，实际 theme=%+v script=%+v", info.Theme, info.Script)
	}
}

func TestKindScriptRequiresSection(t *testing.T) {
	service, pluginsDir, _ := testService(t)
	writePack(t, pluginsDir, "pack", scriptManifest("pack", "null"))

	info := findInfo(t, mustScan(t, service), "pack")
	if info.Status != StatusInvalid || info.ReasonCode != reasonSectionMissing {
		t.Fatalf("kind=script 缺 script 段应判 section_missing，实际 %+v", info)
	}
}

func TestScriptOverCapIsRejectedNotTruncated(t *testing.T) {
	service, pluginsDir, _ := testService(t)
	packDir := writePack(t, pluginsDir, "pack", scriptManifest("pack", `{"entry":"main.js"}`))
	writeFile(t, filepath.Join(packDir, "main.js"), validScriptSource+strings.Repeat("// pad\n", maxScriptBytes/7))

	info := findInfo(t, mustScan(t, service), "pack")
	if info.Status != StatusInvalid || info.ReasonCode != reasonPathTooLarge {
		t.Fatalf("应判 path_too_large，实际 %+v", info)
	}
}

// .js/.mjs/.css 进了 ReadFile 的白名单——注入运行时靠它取源码。
func TestReadFileServesScriptSources(t *testing.T) {
	service, pluginsDir, _ := testService(t)
	packDir := writePack(t, pluginsDir, "pack", scriptManifest("pack", `{"entry":"main.js","styles":["x.css"]}`))
	writeFile(t, filepath.Join(packDir, "main.js"), validScriptSource)
	writeFile(t, filepath.Join(packDir, "x.css"), validCSS)
	if _, err := service.Scan(); err != nil {
		t.Fatalf("scan: %v", err)
	}

	for _, path := range []string{"main.js", "x.css"} {
		got, err := service.ReadFile("pack", path)
		if err != nil {
			t.Fatalf("ReadFile(%q): %v", path, err)
		}
		if got == "" {
			t.Fatalf("ReadFile(%q) 返回空内容", path)
		}
	}
	if _, err := service.ReadFile("pack", "main.exe"); err == nil {
		t.Fatal("可执行扩展名仍要被 ReadFile 拒绝")
	}
}

func TestManifestOverCapIsRejectedNotTruncated(t *testing.T) {
	service, pluginsDir, _ := testService(t)
	packDir := writePack(t, pluginsDir, "pack", sourceManifest("pack"))
	// 把 name.en 填到超过 256 KiB，得到一个「合法但太大」的 manifest。
	padding := strings.Repeat("a", maxManifestBytes+1024)
	oversized := fmt.Sprintf(`{"manifest_version":1,"id":"pack","version":"1.0.0","name":{"en":%q},"kind":"source","source":{"adapters":[{"id":"a","name":{"en":"A"},"strategy":{"version":2,"strategy":"custom"}}]}}`, padding)
	writeFile(t, filepath.Join(packDir, manifestFileName), oversized)

	info := findInfo(t, mustScan(t, service), "pack")
	if info.Status != StatusInvalid || info.ReasonCode != reasonManifestTooLarge {
		t.Fatalf("应判 manifest_too_large，实际 %+v", info)
	}
}

func TestShaderOverCap(t *testing.T) {
	service, pluginsDir, _ := testService(t)
	packDir := writePack(t, pluginsDir, "pack", shaderManifestSingle("pack"))
	// 只多写一点点就够了：读取本身走 LimitReader，超大文件也不会被整份吸进内存。
	writeFile(t, filepath.Join(packDir, "shader.glsl"), validShaderSource+strings.Repeat("// pad\n", maxShaderBytes/7))

	info := findInfo(t, mustScan(t, service), "pack")
	if info.Status != StatusInvalid || info.ReasonCode != reasonPathTooLarge {
		t.Fatalf("应判 path_too_large，实际 %+v", info)
	}
}

func TestPathReferencesStayInsidePack(t *testing.T) {
	cases := []struct {
		name     string
		entry    string
		wantCode string
	}{
		{"指向包外的 ..", "../outside.glsl", reasonPathInvalid},
		{"路径中间夹 ..", "sub/../outside.glsl", reasonPathInvalid},
		{"结尾的 ..", "sub/..", reasonPathInvalid},
		{"绝对路径", "/etc/outside.glsl", reasonPathInvalid},
		{"反斜杠逃逸", `..\outside.glsl`, reasonPathInvalid},
		{"盘符写法", `C:/Windows/outside.glsl`, reasonPathInvalid},
		{"空串", "", reasonPathInvalid},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			service, pluginsDir, _ := testService(t)
			root := filepath.Dir(pluginsDir)
			writeFile(t, filepath.Join(root, "outside.glsl"), validShaderSource)
			manifest := fmt.Sprintf(`{"manifest_version":1,"id":"pack","version":"1.0.0","name":{"en":"P"},"kind":"shader","shader":{"id":"a","name":{"en":"A"},"entry":%q}}`, testCase.entry)
			writePack(t, pluginsDir, "pack", manifest)

			info := findInfo(t, mustScan(t, service), "pack")
			if info.Status != StatusInvalid || info.ReasonCode != testCase.wantCode {
				t.Fatalf("应判 %q，实际 %+v", testCase.wantCode, info)
			}
		})
	}
}

func TestSymlinkOutsidePackIsRejected(t *testing.T) {
	service, pluginsDir, _ := testService(t)
	root := filepath.Dir(pluginsDir)
	outside := filepath.Join(root, "outside.glsl")
	writeFile(t, outside, validShaderSource)

	packDir := writePack(t, pluginsDir, "pack", shaderManifestSingle("pack"))
	link := filepath.Join(packDir, "shader.glsl")
	if err := os.Symlink(outside, link); err != nil {
		// Windows 上创建符号链接要权限（开发者模式或管理员），拿不到就跳过这条，
		// 同一件事已由上面的 .. 用例覆盖到词法层面。
		t.Skipf("无法创建符号链接（%v），跳过真机软链用例", err)
	}

	info := findInfo(t, mustScan(t, service), "pack")
	if info.Status != StatusInvalid {
		t.Fatalf("软链指向包外时应判 invalid，实际 %+v", info)
	}
	if info.ReasonCode != reasonPathInvalid && info.ReasonCode != reasonPathMissing {
		t.Fatalf("原因码应是路径类错误，实际 %q（reason=%q）", info.ReasonCode, info.Reason)
	}
	if !strings.Contains(info.Reason, "越出包目录") {
		t.Fatalf("reason 应说明越出包目录，实际 %q", info.Reason)
	}
}

func TestSymlinkInsidePackIsAllowed(t *testing.T) {
	service, pluginsDir, _ := testService(t)
	packDir := writePack(t, pluginsDir, "pack", shaderManifestSingle("pack"))
	writeFile(t, filepath.Join(packDir, "real.glsl"), validShaderSource)
	if err := os.Symlink(filepath.Join(packDir, "real.glsl"), filepath.Join(packDir, "shader.glsl")); err != nil {
		t.Skipf("无法创建符号链接（%v），跳过", err)
	}

	info := findInfo(t, mustScan(t, service), "pack")
	if info.Status != StatusReady {
		t.Fatalf("包内软链应可用，实际 %+v", info)
	}
}

func TestDirectorySymlinkToExternalFileIsRejected(t *testing.T) {
	// 包内放一个指向外部的目录软链，entry 写成 link/shader.glsl：
	// EvalSymlinks 之后真身在包外，同样要拒掉。
	service, pluginsDir, _ := testService(t)
	root := filepath.Dir(pluginsDir)
	external := filepath.Join(root, "external")
	writeFile(t, filepath.Join(external, "shader.glsl"), validShaderSource)

	packDir := writePack(t, pluginsDir, "pack", fmt.Sprintf(
		`{"manifest_version":1,"id":"pack","version":"1.0.0","name":{"en":"P"},"kind":"shader","shader":{"id":"a","name":{"en":"A"},"entry":"link/shader.glsl"}}`))
	if err := os.Symlink(external, filepath.Join(packDir, "link")); err != nil {
		t.Skipf("无法创建目录符号链接（%v），跳过", err)
	}

	info := findInfo(t, mustScan(t, service), "pack")
	if info.Status != StatusInvalid {
		t.Fatalf("目录软链指向包外时应判 invalid，实际 %+v", info)
	}
}

func mustScan(t *testing.T, service *Service) []Info {
	t.Helper()
	infos, err := service.Scan()
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	return infos
}
