package plugin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUninstallRemovesPack(t *testing.T) {
	service, pluginsDir, states := testService(t)
	packDir := writePack(t, pluginsDir, "demo-source", sourceManifest("demo-source"))
	if _, err := os.Stat(filepath.Join(packDir, manifestFileName)); err != nil {
		t.Fatalf("样例包没写上: %v", err)
	}
	// 先关掉再删：states 里留下一行 disabled 记录，卸载必须顺手把它清掉。
	if err := service.SetEnabled("demo-source", false); err != nil {
		t.Fatalf("SetEnabled: %v", err)
	}
	if _, recorded := states["demo-source"]; !recorded {
		t.Fatal("前置条件没建立：启用状态里没有这一行")
	}

	if err := service.Uninstall("demo-source"); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if _, err := os.Stat(packDir); !os.IsNotExist(err) {
		t.Fatalf("包目录还在: %v", err)
	}
	if _, recorded := states["demo-source"]; recorded {
		t.Fatal("卸载后启用状态里还留着这一行")
	}
	infos, err := service.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, info := range infos {
		if info.ID == "demo-source" {
			t.Fatalf("缓存注册表里还有已卸载的包: %+v", info)
		}
	}
}

// 内置包不给卸载：那一行在界面上压根没有按钮，走到这里说明是别处传来的调用。
func TestUninstallRefusesBuiltinPacks(t *testing.T) {
	service, pluginsDir, _ := testService(t)
	if err := service.SeedBuiltin(); err != nil {
		t.Fatalf("SeedBuiltin: %v", err)
	}

	for _, id := range []string{"logs-panel", "diagnostics-panel"} {
		err := service.Uninstall(id)
		if err == nil {
			t.Fatalf("卸载内置包 %q 竟然成功了", id)
		}
		if !strings.Contains(err.Error(), "应用自带") {
			t.Fatalf("错误应该说明是内置包，实际: %v", err)
		}
		if _, statErr := os.Stat(filepath.Join(pluginsDir, id, manifestFileName)); statErr != nil {
			t.Fatalf("内置包 %q 被删掉了: %v", id, statErr)
		}
	}
}

// 内置标记是界面判断「给不给卸载按钮」的唯一依据，所以扫描必须把它带上，
// 而且 invalid 的那一行也要有——内置包被改坏了的时候同样不该出现删除按钮。
func TestScanMarksBuiltinPacks(t *testing.T) {
	service, pluginsDir, _ := testService(t)
	if err := service.SeedBuiltin(); err != nil {
		t.Fatalf("SeedBuiltin: %v", err)
	}
	writePack(t, pluginsDir, "demo-source", sourceManifest("demo-source"))

	infos, err := service.Scan()
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	builtin := map[string]bool{}
	for _, info := range infos {
		builtin[info.ID] = info.Builtin
	}
	if !builtin["logs-panel"] || !builtin["diagnostics-panel"] {
		t.Fatalf("内置包没被标出来: %+v", builtin)
	}
	if builtin["demo-source"] {
		t.Fatal("第三方包被误标成内置")
	}
}

// 校验失败的包最需要「一键删掉重来」，所以卸载不能只认 ready 的包。
func TestUninstallWorksOnInvalidPack(t *testing.T) {
	service, pluginsDir, _ := testService(t)
	broken := writePack(t, pluginsDir, "broken-pack", `{
		"manifest_version": 1,
		"id": "broken-pack",
		"version": "nope",
		"name": {"en": "Broken"},
		"kind": "shader"
	}`)
	info := findInfo(t, mustScan(t, service), "broken-pack")
	if info.Status != StatusInvalid {
		t.Fatalf("前置条件没建立：这个包应该是 invalid，实际 %q", info.Status)
	}

	if err := service.Uninstall("broken-pack"); err != nil {
		t.Fatalf("卸载校验失败的包: %v", err)
	}
	if _, err := os.Stat(broken); !os.IsNotExist(err) {
		t.Fatal("invalid 包没被删掉")
	}
}

// 不在注册表里的 id 一律不碰磁盘：这条断言防的是「界面传来一个字符串，应用就照着删目录」。
func TestUninstallRejectsUnknownAndUnsafeIDs(t *testing.T) {
	service, pluginsDir, _ := testService(t)
	writePack(t, pluginsDir, "demo-source", sourceManifest("demo-source"))
	if _, err := service.Scan(); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	neighbor := filepath.Join(filepath.Dir(pluginsDir), "keepme")
	if err := os.MkdirAll(neighbor, 0o755); err != nil {
		t.Fatalf("mkdir 邻居目录: %v", err)
	}

	for _, id := range []string{"", " ", "not-installed", "../keepme", "..", "sub/dir"} {
		if err := service.Uninstall(id); err == nil {
			t.Fatalf("卸载 %q 竟然成功了", id)
		}
	}
	if _, err := os.Stat(neighbor); err != nil {
		t.Fatalf("邻居目录被删了: %v", err)
	}
	if _, err := os.Stat(filepath.Join(pluginsDir, "demo-source")); err != nil {
		t.Fatalf("注册表里的包被误删: %v", err)
	}
}

// 已经不在磁盘上的包（用户自己删了、或重复点了一次）不该让界面报一次「失败」。
func TestUninstallIsIdempotentWhenGone(t *testing.T) {
	service, pluginsDir, _ := testService(t)
	packDir := writePack(t, pluginsDir, "demo-source", sourceManifest("demo-source"))
	if _, err := service.Scan(); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if err := os.RemoveAll(packDir); err != nil {
		t.Fatalf("手工删目录: %v", err)
	}
	if err := service.Uninstall("demo-source"); err == nil {
		t.Fatal("缓存里的包目录已经没了，卸载应当报错而不是假装成功")
	}
}
