package plugin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// builtinPackIDs 返回内嵌的那批内置包目录名：断言要跟着 builtin/ 的内容走，
// 免得测试写死两个名字、将来加第三个内置包时只能靠失败才发现漏了。
func builtinPackIDs(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(".", builtinSourceRoot))
	if err != nil {
		t.Fatalf("读取内嵌源目录失败: %v", err)
	}
	var ids []string
	for _, entry := range entries {
		if entry.IsDir() {
			ids = append(ids, entry.Name())
		}
	}
	if len(ids) == 0 {
		t.Fatal("builtin/ 下没有任何内置包")
	}
	return ids
}

func TestSeedBuiltinLandsAndScansReady(t *testing.T) {
	service, pluginsDir, _ := testService(t)
	if err := service.SeedBuiltin(); err != nil {
		t.Fatalf("SeedBuiltin: %v", err)
	}

	want := builtinPackIDs(t)
	for _, id := range want {
		if _, err := os.Stat(filepath.Join(pluginsDir, id, manifestFileName)); err != nil {
			t.Fatalf("内置包 %q 没有落盘: %v", id, err)
		}
	}

	infos, err := service.Scan()
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(infos) != len(want) {
		t.Fatalf("注册表数量 = %d，期望 %d（%+v）", len(infos), len(want), infos)
	}
	for _, id := range want {
		info := findInfo(t, infos, id)
		if info.Status != StatusReady {
			t.Fatalf("内置包 %q 校验失败: %s / %s", id, info.ReasonCode, info.Reason)
		}
		if info.Kind != "script" {
			t.Fatalf("内置包 %q kind = %q，期望 script", id, info.Kind)
		}
		if info.Script == nil {
			t.Fatalf("内置包 %q 没有 script 段", id)
		}
		// 统计来自磁盘：内置包同样列得出文件数，面板上那一行不是空字符串。
		if info.Files == 0 {
			t.Fatalf("内置包 %q 文件统计为 0", id)
		}
	}

	if _, err := os.Stat(filepath.Join(pluginsDir, builtinMarkerName)); err != nil {
		t.Fatalf("marker 没写: %v", err)
	}
}

// 卸载的语义就是「删文件夹」，所以种过一次之后应用不能替用户留着。
// 这条断言盯着 marker：marker 在，就一个字节都不动。
func TestSeedBuiltinDoesNotResurrectDeletedPack(t *testing.T) {
	service, pluginsDir, _ := testService(t)
	if err := service.SeedBuiltin(); err != nil {
		t.Fatalf("第一次 SeedBuiltin: %v", err)
	}

	deleted := filepath.Join(pluginsDir, "logs-panel")
	if err := os.RemoveAll(deleted); err != nil {
		t.Fatalf("删除内置包目录: %v", err)
	}
	if err := service.SeedBuiltin(); err != nil {
		t.Fatalf("第二次 SeedBuiltin: %v", err)
	}
	if _, err := os.Stat(deleted); err == nil {
		t.Fatal("用户删掉的内置包被复活了")
	}

	infos, err := service.Scan()
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	for _, info := range infos {
		if info.ID == "logs-panel" {
			t.Fatal("注册表里还有已删除的 logs-panel")
		}
	}
}

// 改包是这条功能的卖点，不是副作用：即使内置清单换了版本（marker 没了），
// 已经存在的包目录也必须原样留着。
func TestSeedBuiltinNeverOverwritesUserEdits(t *testing.T) {
	service, pluginsDir, _ := testService(t)
	if err := service.SeedBuiltin(); err != nil {
		t.Fatalf("SeedBuiltin: %v", err)
	}

	entry := filepath.Join(pluginsDir, "logs-panel", "main.js")
	if err := os.WriteFile(entry, []byte("// 我自己改过的\n"), 0o644); err != nil {
		t.Fatalf("改写入口脚本: %v", err)
	}
	if err := os.Remove(filepath.Join(pluginsDir, builtinMarkerName)); err != nil {
		t.Fatalf("移除 marker: %v", err)
	}
	if err := service.SeedBuiltin(); err != nil {
		t.Fatalf("重种 SeedBuiltin: %v", err)
	}

	data, err := os.ReadFile(entry)
	if err != nil {
		t.Fatalf("读取入口脚本: %v", err)
	}
	if string(data) != "// 我自己改过的\n" {
		t.Fatalf("用户改动被覆盖了:\n%s", data)
	}
}

// 内置包后来加一张图标（比如包图标从首字占位换成真图）时，已经装好的数据目录也要拿到
// 那张图：重种只往里补「磁盘上没有」的文件，用户改过的、删掉的都原样留着。
func TestSeedBuiltinFillsMissingFilesIntoExistingPack(t *testing.T) {
	service, pluginsDir, _ := testService(t)
	if err := service.SeedBuiltin(); err != nil {
		t.Fatalf("SeedBuiltin: %v", err)
	}

	packDir := filepath.Join(pluginsDir, "logs-panel")
	icon := filepath.Join(packDir, "icon.png")
	if _, err := os.Stat(icon); err != nil {
		t.Fatalf("首种就该带上 icon.png: %v", err)
	}
	if err := os.Remove(icon); err != nil {
		t.Fatalf("删掉图标: %v", err)
	}
	entry := filepath.Join(packDir, "main.js")
	if err := os.WriteFile(entry, []byte("// 我自己改过的\n"), 0o644); err != nil {
		t.Fatalf("改写入口脚本: %v", err)
	}
	// 版本没变、包名也都在标记里，正常路径是整轮直接跳过；删掉 marker 模拟「清单版本变了」。
	if err := os.Remove(filepath.Join(pluginsDir, builtinMarkerName)); err != nil {
		t.Fatalf("移除 marker: %v", err)
	}
	if err := service.SeedBuiltin(); err != nil {
		t.Fatalf("重种 SeedBuiltin: %v", err)
	}

	if _, err := os.Stat(icon); err != nil {
		t.Fatalf("缺失的图标没补回来: %v", err)
	}
	data, err := os.ReadFile(entry)
	if err != nil {
		t.Fatalf("读取入口脚本: %v", err)
	}
	if string(data) != "// 我自己改过的\n" {
		t.Fatalf("补文件时覆盖了用户改动:\n%s", data)
	}
}

// 旧格式 marker 只写了版本号，没记包名。这种情况必须按「当时在场的那些都已经种过」
// 处理，否则升级那一次会把用户早就删掉的内置包又装回来。
func TestSeedBuiltinLegacyMarkerDoesNotResurrect(t *testing.T) {
	service, pluginsDir, _ := testService(t)
	if err := service.SeedBuiltin(); err != nil {
		t.Fatalf("SeedBuiltin: %v", err)
	}
	if err := os.RemoveAll(filepath.Join(pluginsDir, "logs-panel")); err != nil {
		t.Fatalf("删除内置包目录: %v", err)
	}
	diagIcon := filepath.Join(pluginsDir, "diagnostics-panel", "icon.png")
	if err := os.Remove(diagIcon); err != nil {
		t.Fatalf("删掉图标: %v", err)
	}
	marker := filepath.Join(pluginsDir, builtinMarkerName)
	// 旧格式：只有版本号一行，没有包名。写死 "1" 而不是当前版本，模拟真实的那一次升级。
	if err := os.WriteFile(marker, []byte("1"), 0o644); err != nil {
		t.Fatalf("写旧格式 marker: %v", err)
	}

	if err := service.SeedBuiltin(); err != nil {
		t.Fatalf("按旧 marker 重种: %v", err)
	}
	if _, err := os.Stat(filepath.Join(pluginsDir, "logs-panel")); err == nil {
		t.Fatal("旧 marker 下用户删掉的内置包被复活了")
	}
	// 另一侧的兼容：在场的包该补的文件照样补得到。
	if _, err := os.Stat(diagIcon); err != nil {
		t.Fatalf("旧 marker 下没有补齐内置包文件: %v", err)
	}
}

// marker 是文件而不是目录，Scan 那一层跳过非目录条目，所以它不会变成一张校验失败的卡片。
func TestSeedBuiltinMarkerIsNotAPack(t *testing.T) {
	service, _, _ := testService(t)
	if err := service.SeedBuiltin(); err != nil {
		t.Fatalf("SeedBuiltin: %v", err)
	}
	infos, err := service.Scan()
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	for _, info := range infos {
		if strings.Contains(info.ID, "cczj-builtin-seed") {
			t.Fatalf("marker 被当成扩展包列进注册表: %+v", info)
		}
	}
}

// 数据目录还没解析出来时（首次运行的极早期、或测试里故意传空）静默返回：
// 内置包晚一点种上不影响任何功能，报错反而会让启动看起来坏了。
func TestSeedBuiltinWithoutDataDirIsNoop(t *testing.T) {
	service := NewService(func() string { return "" })
	if err := service.SeedBuiltin(); err != nil {
		t.Fatalf("空数据目录应当静默返回，实际: %v", err)
	}
}
