package plugin

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// iconScriptManifest 比 scriptManifest 多一个包级 icon 键——图标不属于任何 kind 段，
// 四种 kind 都能声明它，所以测试里单独造一份。
func iconScriptManifest(id, icon string) string {
	return fmt.Sprintf(`{
		"manifest_version": 1,
		"id": %q,
		"version": "1.0.0",
		"name": {"en": "Icon Pack"},
		"kind": "script",
		"icon": %q,
		"script": {"entry": "main.js"}
	}`, id, icon)
}

func TestScanReportsIconAndPackStats(t *testing.T) {
	service, pluginsDir, _ := testService(t)
	packDir := writePack(t, pluginsDir, "stats_pack", iconScriptManifest("stats_pack", "art/icon.png"))
	writeFile(t, filepath.Join(packDir, "main.js"), validScriptSource)
	writeFile(t, filepath.Join(packDir, "notes/readme.md"), "# note")
	// 杂物不计入统计：`.git` 里那几千个文件不是「这个扩展包的内容」。
	writeFile(t, filepath.Join(packDir, ".git", "config"), "[core]\n")
	writeFile(t, filepath.Join(packDir, "Thumbs.db"), "junk")

	png := tinyPNG(t)
	if err := os.MkdirAll(filepath.Join(packDir, "art"), 0o755); err != nil {
		t.Fatalf("mkdir art: %v", err)
	}
	if err := os.WriteFile(filepath.Join(packDir, "art", "icon.png"), png, 0o644); err != nil {
		t.Fatalf("write icon: %v", err)
	}

	infos, err := service.Scan()
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(infos) != 1 {
		t.Fatalf("注册表 = %d 条，想要 1 条", len(infos))
	}
	info := infos[0]
	if info.Status != StatusReady {
		t.Fatalf("包没通过校验: %+v %s", info, info.Reason)
	}
	if info.Icon != "art/icon.png" {
		t.Fatalf("Icon = %q，想要 art/icon.png", info.Icon)
	}
	// plugin.json + main.js + notes/readme.md + art/icon.png
	if info.Files != 4 {
		t.Fatalf("Files = %d，想要 4（杂物不该计数）", info.Files)
	}
	if info.Bytes <= int64(len(png)) {
		t.Fatalf("Bytes = %d，至少要比图标本身大", info.Bytes)
	}
	if info.Updated == "" {
		t.Fatal("Updated 为空")
	}

	// 图标要能被 ReadAsset 读成 data URL，界面才能直接当 <img src>。
	url, err := service.ReadAsset(info.ID, info.Icon)
	if err != nil {
		t.Fatalf("ReadAsset(icon): %v", err)
	}
	if !strings.HasPrefix(url, "data:image/png;base64,") {
		t.Fatalf("data URL 前缀不对: %.30s", url)
	}
}

func TestPackStatsOnInvalidPack(t *testing.T) {
	service, pluginsDir, _ := testService(t)
	// manifest 写坏了也照样数：那一行数字是用户核对「文件夹对不对」的依据。
	packDir := writePack(t, pluginsDir, "broken_stats", `{
		"manifest_version": 1,
		"id": "broken_stats",
		"version": "1.0.0",
		"name": {"en": "Broken"},
		"kind": "script",
		"script": {"entry": "nope.js"}
	}`)
	writeFile(t, filepath.Join(packDir, "a.js"), "x")
	writeFile(t, filepath.Join(packDir, "b.js"), "yy")

	infos, err := service.Scan()
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(infos) != 1 || infos[0].Status != StatusInvalid {
		t.Fatalf("想要一条 invalid 记录，得到 %+v", infos)
	}
	if infos[0].Files != 3 || infos[0].Bytes <= 3 {
		t.Fatalf("统计不对: files=%d bytes=%d", infos[0].Files, infos[0].Bytes)
	}
}

func TestManifestIconRules(t *testing.T) {
	for _, tc := range []struct {
		name string
		icon string
		want string
	}{
		{"文件不存在", "missing.png", reasonPathMissing},
		{"扩展名不是图片", "main.js", reasonPathInvalid},
		{".. 越界", "../outside.png", reasonPathInvalid},
		{"绝对路径", "C:/Windows/notepad.png", reasonPathInvalid},
		{"反斜杠", "art\\icon.png", reasonPathInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service, pluginsDir, _ := testService(t)
			packDir := writePack(t, pluginsDir, "icon_pack", iconScriptManifest("icon_pack", tc.icon))
			writeFile(t, filepath.Join(packDir, "main.js"), validScriptSource)
			writeFile(t, filepath.Join(pluginsDir, "outside.png"), "x")

			infos, err := service.Scan()
			if err != nil {
				t.Fatalf("Scan: %v", err)
			}
			if len(infos) != 1 {
				t.Fatalf("注册表 = %d 条", len(infos))
			}
			if infos[0].Status != StatusInvalid || infos[0].ReasonCode != tc.want {
				t.Fatalf("状态/原因码 = %s/%s，想要 invalid/%s（%q）",
					infos[0].Status, infos[0].ReasonCode, tc.want, infos[0].Reason)
			}
		})
	}
}

func TestManifestIconTooLarge(t *testing.T) {
	service, pluginsDir, _ := testService(t)
	packDir := writePack(t, pluginsDir, "fat_icon", iconScriptManifest("fat_icon", "icon.png"))
	writeFile(t, filepath.Join(packDir, "main.js"), validScriptSource)
	if err := os.WriteFile(filepath.Join(packDir, "icon.png"),
		make([]byte, maxIconBytes+1), 0o644); err != nil {
		t.Fatalf("write icon: %v", err)
	}

	infos, err := service.Scan()
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(infos) != 1 || infos[0].ReasonCode != reasonPathTooLarge {
		t.Fatalf("想要 path_too_large，得到 %+v", infos)
	}
}

func TestManifestWithoutIconStaysValid(t *testing.T) {
	service, pluginsDir, _ := testService(t)
	packDir := writePack(t, pluginsDir, "no_icon", scriptManifest("no_icon", `{"entry": "main.js"}`))
	writeFile(t, filepath.Join(packDir, "main.js"), validScriptSource)

	infos, err := service.Scan()
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(infos) != 1 || infos[0].Status != StatusReady || infos[0].Icon != "" {
		t.Fatalf("没有 icon 的包应当照常可用: %+v", infos)
	}
}

// 包根目录里那张约定名的图不用写进 manifest 也算图标：手工做的包十个有八九就这么放，
// 而「图标明明在文件夹里却不显示」是最难自己查出来的一类问题。
func TestScanPicksUpConventionalIconWithoutManifestKey(t *testing.T) {
	service, pluginsDir, _ := testService(t)
	packDir := writePack(t, pluginsDir, "plain_pack", scriptManifest("plain_pack", `{"entry": "main.js"}`))
	writeFile(t, filepath.Join(packDir, "main.js"), validScriptSource)
	if err := os.WriteFile(filepath.Join(packDir, "icon.png"), tinyPNG(t), 0o644); err != nil {
		t.Fatalf("write icon: %v", err)
	}

	infos, err := service.Scan()
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(infos) != 1 || infos[0].Status != StatusReady {
		t.Fatalf("注册表 = %+v", infos)
	}
	if infos[0].Icon != "icon.png" {
		t.Fatalf("约定名的 icon.png 没被认成图标: %+v", infos[0])
	}
	url, err := service.ReadAsset(infos[0].ID, infos[0].Icon)
	if err != nil {
		t.Fatalf("ReadAsset(icon): %v", err)
	}
	if !strings.HasPrefix(url, "data:image/png;base64,") {
		t.Fatalf("图标不是 PNG data URL: %q", url[:min(len(url), 40)])
	}
}

func TestInstallReportsIconAndStats(t *testing.T) {
	service, _, _ := testService(t)
	png := tinyPNG(t)

	info, err := service.InstallFromPath(dropBinaryTree(t, "icon_pack", map[string][]byte{
		"plugin.json":  []byte(iconScriptManifest("icon_pack", "art/icon.png")),
		"main.js":      []byte(validScriptSource),
		"art/icon.png": png,
	}))
	if err != nil {
		t.Fatalf("InstallFromPath: %v", err)
	}
	if info.Icon != "art/icon.png" {
		t.Fatalf("Icon = %q", info.Icon)
	}
	// 安装结尾走一次 Scan，所以返回的那一行也带着目录统计。
	if info.Files != 3 || info.Updated == "" {
		t.Fatalf("统计没跟上: files=%d updated=%q", info.Files, info.Updated)
	}
}
