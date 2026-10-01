package plugin

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// dropTree 在临时目录里造一个「用户从资源管理器拖进来的文件夹」，返回它的路径。
// 键是包内相对路径（正斜杠），值是其文本内容。
func dropTree(t *testing.T, name string, entries map[string]string) string {
	t.Helper()
	files := make(map[string][]byte, len(entries))
	for path, content := range entries {
		files[path] = []byte(content)
	}
	return dropBinaryTree(t, name, files)
}

func dropBinaryTree(t *testing.T, name string, entries map[string][]byte) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	for path, content := range entries {
		target := filepath.Join(dir, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(target), err)
		}
		if err := os.WriteFile(target, content, 0o644); err != nil {
			t.Fatalf("write %s: %v", target, err)
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	return dir
}

// assertStagingClean 确认临时目录没有残留：安装失败也一样的道理——不该在
// 用户的数据目录里留下半个包。
func assertStagingClean(t *testing.T, service *Service) {
	t.Helper()
	entries, err := os.ReadDir(service.stagingRoot())
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		t.Fatalf("读临时目录: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("临时目录里有残留: %v", entries)
	}
}

func TestInstallScriptPack(t *testing.T) {
	service, pluginsDir, _ := testService(t)

	info, err := service.InstallFromPath(dropTree(t, "随便什么文件夹名", map[string]string{
		"plugin.json": scriptManifest("drop_script", `{"entry": "main.js", "styles": ["look.css"]}`),
		"main.js":     validScriptSource,
		"look.css":    validCSS,
	}))
	if err != nil {
		t.Fatalf("InstallFromPath: %v", err)
	}
	if info.ID != "drop_script" || info.Kind != "script" || info.Status != StatusReady {
		t.Fatalf("返回的 Info 不对: %+v", info)
	}
	if info.Script == nil || info.Script.Entry != "main.js" || len(info.Script.Styles) != 1 {
		t.Fatalf("script 段没解析出来: %+v", info.Script)
	}

	// 目录名来自 manifest 的 id，不是拖进来的文件夹名。
	want := filepath.Join(pluginsDir, "drop_script")
	if info.Dir != want {
		t.Fatalf("包目录 = %q，想要 %q", info.Dir, want)
	}
	for _, name := range []string{manifestFileName, "main.js", "look.css"} {
		if _, err := os.Stat(filepath.Join(want, name)); err != nil {
			t.Fatalf("缺少 %s: %v", name, err)
		}
	}

	// 安装完的注册表里就有它，界面不需要再手动重扫。
	infos, err := service.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(infos) != 1 || infos[0].ID != "drop_script" {
		t.Fatalf("注册表 = %+v", infos)
	}
	assertStagingClean(t, service)
}

func TestInstallUnwrapsSingleWrapperDirectory(t *testing.T) {
	service, pluginsDir, _ := testService(t)

	// 下载解压最常见的形状：拖进来的是外层目录，包在里层同名目录里。
	_, err := service.InstallFromPath(dropTree(t, "my-pack-1.0", map[string]string{
		"my-pack-1.0/plugin.json": scriptManifest("nested_pack", `{"entry": "main.js"}`),
		"my-pack-1.0/main.js":     validScriptSource,
	}))
	if err != nil {
		t.Fatalf("InstallFromPath: %v", err)
	}
	if _, err := os.Stat(filepath.Join(pluginsDir, "nested_pack", "main.js")); err != nil {
		t.Fatalf("脱层失败: %v", err)
	}
	// 外层目录不该被保留成一层子目录。
	if _, err := os.Stat(filepath.Join(pluginsDir, "nested_pack", "my-pack-1.0")); !os.IsNotExist(err) {
		t.Fatalf("外层目录还留着: %v", err)
	}
}

// 拖进来的外层若并列摆着两个包，往下猜哪一个是错的：宁可拒绝，让用户自己选那个文件夹。
func TestInstallRefusesAmbiguousWrapperDirectory(t *testing.T) {
	service, pluginsDir, _ := testService(t)

	_, err := service.InstallFromPath(dropTree(t, "downloads", map[string]string{
		"pack-a/plugin.json": scriptManifest("pack_a", `{"entry": "main.js"}`),
		"pack-a/main.js":     validScriptSource,
		"pack-b/plugin.json": scriptManifest("pack_b", `{"entry": "main.js"}`),
		"pack-b/main.js":     validScriptSource,
	}))
	if err == nil || !strings.Contains(err.Error(), manifestFileName) {
		t.Fatalf("两个包的外层目录应当被拒: %v", err)
	}
	entries, readErr := os.ReadDir(pluginsDir)
	if readErr != nil {
		t.Fatalf("读 plugins: %v", readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("被拒之后 plugins 里留下了东西: %v", entries)
	}
}

func TestInstallSkipsSystemJunk(t *testing.T) {
	service, pluginsDir, _ := testService(t)

	info, err := service.InstallFromPath(dropTree(t, "pack", map[string]string{
		"plugin.json":        scriptManifest("junk_pack", `{"entry": "main.js"}`),
		"main.js":            validScriptSource,
		"desktop.ini":        "[.ShellClassInfo]",
		".DS_Store":          "junk",
		"__MACOSX/._main.js": "junk",
		".git/config":        "[core]",
		"sub/Thumbs.db":      "junk",
		"sub/notes/说明.md":    "# 说明",
	}))
	if err != nil {
		t.Fatalf("InstallFromPath: %v", err)
	}
	if info.Status != StatusReady {
		t.Fatalf("状态 = %+v", info)
	}
	for _, gone := range []string{"desktop.ini", ".DS_Store", "sub/Thumbs.db"} {
		if _, err := os.Stat(filepath.Join(info.Dir, filepath.FromSlash(gone))); !os.IsNotExist(err) {
			t.Fatalf("%s 应该被跳过，却在包里: %v", gone, err)
		}
	}
	if _, err := os.Stat(filepath.Join(info.Dir, "sub", "notes", "说明.md")); err != nil {
		t.Fatalf("正常子目录文件被误杀: %v", err)
	}
	// .git / __MACOSX 整棵子树都不该进来。
	for _, gone := range []string{".git", "__MACOSX"} {
		if _, err := os.Stat(filepath.Join(info.Dir, gone)); !os.IsNotExist(err) {
			t.Fatalf("%s 目录应该整个跳过: %v", gone, err)
		}
	}
	if _, err := os.Stat(filepath.Join(pluginsDir, "junk_pack", "main.js")); err != nil {
		t.Fatalf("包没落到位: %v", err)
	}
}

// TestInstallRejectsInvalidPackIsNoOp 是这条路径最重要的保证：拖进来一个写坏的
// 包，除了错误信息之外什么都不该发生。
func TestInstallRejectsInvalidPackIsNoOp(t *testing.T) {
	cases := []struct {
		name       string
		entries    map[string]string
		wantCode   string
		wantInWant string
	}{
		{
			name: "未知字段",
			entries: map[string]string{
				"plugin.json": `{"manifest_version":1,"id":"bad_pack","version":"1.0.0",
					"name":{"en":"Bad"},"kind":"script","script":{"entry":"main.js"},"typo":true}`,
				"main.js": validScriptSource,
			},
			wantCode: reasonUnknownField,
		},
		{
			name: "manifest 缺 id 格式",
			entries: map[string]string{
				"plugin.json": `{"manifest_version":1,"id":"Bad ID","version":"1.0.0",
					"name":{"en":"Bad"},"kind":"script","script":{"entry":"main.js"}}`,
				"main.js": validScriptSource,
			},
			wantCode: reasonIDInvalid,
		},
		{
			name: "入口文件不在包里",
			entries: map[string]string{
				"plugin.json": scriptManifest("missing_pack", `{"entry": "absent.js"}`),
				"main.js":     validScriptSource,
			},
			wantCode: reasonPathMissing,
		},
		{
			name: "入口路径越界",
			entries: map[string]string{
				"plugin.json": scriptManifest("escape_pack", `{"entry": "../outside.js"}`),
				"main.js":     validScriptSource,
			},
			wantCode: reasonPathInvalid,
		},
		{
			name: "不是扩展包文件夹",
			entries: map[string]string{
				"README.md": "just notes",
				"main.js":   validScriptSource,
			},
			wantInWant: manifestFileName,
		},
		{
			name:       "空文件夹",
			entries:    map[string]string{},
			wantInWant: "没有任何文件",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			service, pluginsDir, _ := testService(t)

			info, err := service.InstallFromPath(dropTree(t, "broken-pack", tc.entries))
			if err == nil {
				t.Fatalf("坏包居然装上了: %+v", info)
			}
			if info != nil {
				t.Fatalf("失败时不该返回 Info: %+v", info)
			}
			if tc.wantCode != "" {
				var pe *packError
				if !asPackError(err, &pe) {
					t.Fatalf("错误没带原因码: %v", err)
				}
				if string(pe.Code) != tc.wantCode {
					t.Fatalf("原因码 = %q，想要 %q（消息 %q）", pe.Code, tc.wantCode, pe.Message)
				}
			}
			if tc.wantInWant != "" && !strings.Contains(err.Error(), tc.wantInWant) {
				t.Fatalf("错误信息 %q 里没有 %q", err.Error(), tc.wantInWant)
			}
			// 一个字节都不该进 plugins。
			entries, readErr := os.ReadDir(pluginsDir)
			if readErr != nil {
				t.Fatalf("读 plugins: %v", readErr)
			}
			if len(entries) != 0 {
				t.Fatalf("plugins 里留下了东西: %v", entries)
			}
			assertStagingClean(t, service)
		})
	}
}

// 单文件拖进来没有意义：包是一个目录。这句话得由 Go 说，因为路径就是它读的。
func TestInstallRejectsDroppedFile(t *testing.T) {
	service, pluginsDir, _ := testService(t)

	file := filepath.Join(t.TempDir(), "main.js")
	if err := os.WriteFile(file, []byte(validScriptSource), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := service.InstallFromPath(file)
	if err == nil || !strings.Contains(err.Error(), "请拖整个扩展包文件夹") {
		t.Fatalf("拖单个文件应当被拒: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(pluginsDir)); statErr != nil {
		t.Fatalf("读 plugins: %v", statErr)
	}
}

// 包目录里指向外面的符号链接不该被装进来：WalkDir 不跟链接，这里确认它也没把
// 链接本身当成一个普通文件写进包里。
func TestInstallIgnoresSymlinks(t *testing.T) {
	service, _, _ := testService(t)

	dir := dropTree(t, "pack", map[string]string{
		"plugin.json": scriptManifest("link_pack", `{"entry": "main.js"}`),
		"main.js":     validScriptSource,
	})
	if err := os.Symlink(filepath.Join(dir, "main.js"), filepath.Join(dir, "alias.js")); err != nil {
		t.Skipf("这台机器建不了符号链接: %v", err)
	}
	info, err := service.InstallFromPath(dir)
	if err != nil {
		t.Fatalf("InstallFromPath: %v", err)
	}
	if _, err := os.Stat(filepath.Join(info.Dir, "alias.js")); !os.IsNotExist(err) {
		t.Fatalf("符号链接被装进了包: %v", err)
	}
}

func TestInstallReplacesSameIDPack(t *testing.T) {
	service, pluginsDir, _ := testService(t)

	first, err := service.InstallFromPath(dropTree(t, "pack", map[string]string{
		"plugin.json": scriptManifest("v2_pack", `{"entry": "main.js"}`),
		"main.js":     "export function setup() {}\n",
	}))
	if err != nil {
		t.Fatalf("首次安装: %v", err)
	}
	if first.Version != "1.0.0" {
		t.Fatalf("首装版本 = %q", first.Version)
	}

	// 再拖一次同一个文件夹（改了内容）就是更新：目录名不变、内容换新。
	second, err := service.InstallFromPath(dropTree(t, "pack", map[string]string{
		"plugin.json": `{"manifest_version":1,"id":"v2_pack","version":"1.1.0",
			"name":{"en":"Tweaks"},"kind":"script","script":{"entry":"main.js"}}`,
		"main.js": validScriptSource,
	}))
	if err != nil {
		t.Fatalf("覆盖安装: %v", err)
	}
	if second.Version != "1.1.0" || second.Dir != first.Dir {
		t.Fatalf("覆盖结果不对: %+v", second)
	}
	data, err := os.ReadFile(filepath.Join(pluginsDir, "v2_pack", "main.js"))
	if err != nil {
		t.Fatalf("读 main.js: %v", err)
	}
	if !strings.Contains(string(data), "cczj.nav") {
		t.Fatalf("内容还是旧的: %q", data)
	}
	infos, err := service.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(infos) != 1 {
		t.Fatalf("覆盖安装多出了包: %+v", infos)
	}
	assertStagingClean(t, service)
}

func TestInstallBinaryContentRoundTrips(t *testing.T) {
	service, pluginsDir, _ := testService(t)
	png := tinyPNG(t)

	info, err := service.InstallFromPath(dropBinaryTree(t, "theme_pack", map[string][]byte{
		"plugin.json": []byte(themeManifest("image_pack")),
		"dusk.png":    png,
	}))
	if err != nil {
		t.Fatalf("InstallFromPath: %v", err)
	}
	if info.Status != StatusReady || info.Theme == nil || len(info.Theme.Presets) != 1 {
		t.Fatalf("主题包没装好: %+v", info)
	}
	written, err := os.ReadFile(filepath.Join(pluginsDir, "image_pack", "dusk.png"))
	if err != nil {
		t.Fatalf("读 dusk.png: %v", err)
	}
	if len(written) != len(png) {
		t.Fatalf("字节数变了: %d vs %d", len(written), len(png))
	}
	for i := range written {
		if written[i] != png[i] {
			t.Fatalf("第 %d 字节变了", i)
		}
	}
}

func TestInstallFileCountCap(t *testing.T) {
	service, pluginsDir, _ := testService(t)

	entries := map[string]string{
		"plugin.json": scriptManifest("many_files", `{"entry": "main.js"}`),
		"main.js":     validScriptSource,
	}
	for i := 0; i < maxInstallFiles; i++ {
		entries[fmt.Sprintf("extra/notes%03d.md", i)] = "# note"
	}
	_, err := service.InstallFromPath(dropTree(t, "pack", entries))
	if err == nil || !strings.Contains(err.Error(), reasonLimitExceeded) {
		t.Fatalf("文件数上限没生效: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(pluginsDir, "many_files")); !os.IsNotExist(statErr) {
		t.Fatalf("超限仍装进了包: %v", statErr)
	}
	assertStagingClean(t, service)
}

func TestInstallTotalSizeCap(t *testing.T) {
	service, pluginsDir, _ := testService(t)

	_, err := service.InstallFromPath(dropBinaryTree(t, "pack", map[string][]byte{
		"plugin.json": []byte(scriptManifest("huge_pack", `{"entry": "main.js"}`)),
		"main.js":     make([]byte, maxInstallTotalBytes+1),
	}))
	if err == nil || !strings.Contains(err.Error(), reasonPathTooLarge) {
		t.Fatalf("体积上限没生效: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(pluginsDir, "huge_pack")); !os.IsNotExist(statErr) {
		t.Fatalf("超限仍装进了包: %v", statErr)
	}
	assertStagingClean(t, service)
}

// TestInstallRespectsPackCountCap 覆盖 §6 的数量上限：第 65 个包不进目录。
// 同名覆盖不受这条限制——它不会让包变多。
func TestInstallRespectsPackCountCap(t *testing.T) {
	service, pluginsDir, _ := testService(t)
	for i := 0; i < maxPacks; i++ {
		id := fmt.Sprintf("fill_%02d", i)
		writePack(t, pluginsDir, id, scriptManifest(id, `{"entry": "main.js"}`))
		writeFile(t, filepath.Join(pluginsDir, id, "main.js"), validScriptSource)
	}

	_, err := service.InstallFromPath(dropTree(t, "pack", map[string]string{
		"plugin.json": scriptManifest("overflow_pack", `{"entry": "main.js"}`),
		"main.js":     validScriptSource,
	}))
	if err == nil || !strings.Contains(err.Error(), reasonLimitExceeded) {
		t.Fatalf("数量上限没生效: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(pluginsDir, "overflow_pack")); !os.IsNotExist(statErr) {
		t.Fatalf("超限仍装进了包: %v", statErr)
	}

	replaced, err := service.InstallFromPath(dropTree(t, "pack", map[string]string{
		"plugin.json": scriptManifest("fill_00", `{"entry": "main.js"}`),
		"main.js":     validScriptSource,
	}))
	if err != nil {
		t.Fatalf("满额时覆盖同名包应当允许: %v", err)
	}
	if replaced.ID != "fill_00" || replaced.Status != StatusReady {
		t.Fatalf("覆盖结果不对: %+v", replaced)
	}
}

func TestInstallKeepsDisabledState(t *testing.T) {
	service, _, states := testService(t)

	fixture := map[string]string{
		"plugin.json": scriptManifest("kept_state", `{"entry": "main.js"}`),
		"main.js":     validScriptSource,
	}
	if _, err := service.InstallFromPath(dropTree(t, "pack", fixture)); err != nil {
		t.Fatalf("InstallFromPath: %v", err)
	}
	if err := service.SetEnabled("kept_state", false); err != nil {
		t.Fatalf("SetEnabled: %v", err)
	}
	info, err := service.InstallFromPath(dropTree(t, "pack", fixture))
	if err != nil {
		t.Fatalf("覆盖安装: %v", err)
	}
	// 用户关掉的包，重装一次不该自己又开起来。
	if info.Status != StatusDisabled {
		t.Fatalf("状态 = %q，想要 disabled（states=%v）", info.Status, states)
	}
}

func TestInstallNeedsDataDir(t *testing.T) {
	service := NewService(func() string { return "" })
	_, err := service.InstallFromPath(dropTree(t, "pack", map[string]string{
		"plugin.json": scriptManifest("no_dir", `{"entry": "main.js"}`),
	}))
	if err == nil || !strings.Contains(err.Error(), "数据目录") {
		t.Fatalf("没有数据目录时应当拒绝: %v", err)
	}
}
