package plugin

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// testService 造一个只在临时目录里活动的注册表：启用状态用内存 map 代替
// settings KV 表（app/db 要库打开才可用，测试不去碰它，纯编解码另测）。
func testService(t *testing.T) (*Service, string, map[string]bool) {
	t.Helper()
	root := t.TempDir()
	pluginsDir := filepath.Join(root, "plugins")
	if err := os.MkdirAll(pluginsDir, 0o755); err != nil {
		t.Fatalf("mkdir plugins: %v", err)
	}
	states := map[string]bool{}
	service := NewService(func() string { return root })
	service.loadStates = func() (map[string]bool, error) {
		out := make(map[string]bool, len(states))
		for id, enabled := range states {
			out[id] = enabled
		}
		return out, nil
	}
	service.saveStates = func(next map[string]bool) error {
		clear(states)
		for id, enabled := range next {
			states[id] = enabled
		}
		return nil
	}
	return service, pluginsDir, states
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// writePack 建一个目录并写入 plugin.json；目录名即包 id，所以两处要一起给。
func writePack(t *testing.T, pluginsDir, dirName, manifest string) string {
	t.Helper()
	packDir := filepath.Join(pluginsDir, dirName)
	writeFile(t, filepath.Join(packDir, manifestFileName), manifest)
	return packDir
}

const validShaderSource = `//!HOOK ORIGINAL
//!BIND luma
//!SAVE sharpened
//!DESC test unsharp
vec4 hook() {
    vec4 centre = luma_tex(v_uv);
    vec4 blur = luma_tex(v_uv + vec2(0.001, 0.0));
    vec4 res = clamp(centre + (centre - blur) * 0.5, 0.0, 1.0);
    return res;
}
`

func sourceManifest(id string) string {
	return fmt.Sprintf(`{
		"manifest_version": 1,
		"id": %q,
		"version": "1.0.0",
		"name": {"zh-CN": "采集适配", "en": "Collection Adapter"},
		"description": {"en": "one line"},
		"author": "tester",
		"kind": "source",
		"source": {
			"adapters": [
				{
					"id": "demo_cms",
					"name": {"en": "Demo CMS"},
					"strategy": {
						"version": 2,
						"strategy": "declarative",
						"list": {"action": "videolist", "page_param": "pg", "extra": {"ac": "videolist"}},
						"search": {"action": "videolist", "keyword_param": "wd"},
						"detail": {"action": "detail", "id_param": "ids"},
						"response": {"list_path": "list", "ok_codes": ["1"]},
						"field_mapping": {"vod_name": "name"}
					}
				}
			]
		}
	}`, id)
}

func shaderManifestSingle(id string) string {
	return fmt.Sprintf(`{
		"manifest_version": 1,
		"id": %q,
		"version": "0.1.0",
		"name": {"en": "Soft Sharpen"},
		"kind": "shader",
		"shader": {
			"id": "soft_sharpen",
			"name": {"zh-CN": "柔和锐化"},
			"entry": "shader.glsl",
			"scale": 2,
			"category": "film"
		}
	}`, id)
}

func themeManifest(id string) string {
	return fmt.Sprintf(`{
		"manifest_version": 1,
		"id": %q,
		"version": "1.2.3",
		"name": {"zh-CN": "暮色", "en": "Dusk"},
		"kind": "theme",
		"theme": {
			"presets": [
				{
					"id": "dusk",
					"name": {"en": "Dusk"},
					"primary": "#6B4FBB",
					"mode": "dark",
					"tint": {"ink": "#0d0b14"},
					"bg_image": "dusk.png"
				}
			]
		}
	}`, id)
}

// scriptManifest 造一个 kind=script 包；entry 与 styles 由调用方给相对路径。
func scriptManifest(id, section string) string {
	return fmt.Sprintf(`{
		"manifest_version": 1,
		"id": %q,
		"version": "1.0.0",
		"name": {"zh-CN": "侧栏改造", "en": "Sidebar Mod"},
		"kind": "script",
		"script": %s
	}`, id, section)
}

const validScriptSource = `export function setup(cczj) {
    cczj.nav({ path: '/demo', label: 'Demo', icon: 'star' })
}
`

const validCSS = `.demo { color: rebeccapurple; }`

// tinyPNG 用标准库编一张 4x4 的实色 PNG：主题包要引用一张真实存在的图片，
// 手搓字节容易写出解码器不认的假 PNG，那测的就不是校验器而是运气。
func tinyPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: 45, G: 30, B: 90, A: 255})
		}
	}
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buffer.Bytes()
}

func infoByStatus(t *testing.T, infos []Info, status Status) []Info {
	t.Helper()
	var out []Info
	for _, info := range infos {
		if info.Status == status {
			out = append(out, info)
		}
	}
	return out
}

func findInfo(t *testing.T, infos []Info, id string) Info {
	t.Helper()
	for _, info := range infos {
		if info.ID == id {
			return info
		}
	}
	t.Fatalf("注册表里没有 %q，实际是 %+v", id, infos)
	return Info{}
}
