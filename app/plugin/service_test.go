package plugin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 服务端不做「只认 ready」的聚合：包里的适配/着色器/主题段是前端按 status 自己筛的
// （frontend/src/stores/plugins.ts 的 readyPacks）。下面三个替身只给测试用，让
// 「停用即不生效」和「无效包不拖累邻居」这两条仍然被钉住。
func readyInfos(t *testing.T, service *Service) []Info {
	t.Helper()
	infos, err := service.List()
	if err != nil {
		t.Fatalf("List() = %v", err)
	}
	out := make([]Info, 0, len(infos))
	for _, info := range infos {
		if info.Status == StatusReady {
			out = append(out, info)
		}
	}
	return out
}

func readyAdapters(t *testing.T, service *Service) []Adapter {
	t.Helper()
	out := make([]Adapter, 0)
	for _, info := range readyInfos(t, service) {
		if info.Source != nil {
			out = append(out, info.Source.Adapters...)
		}
	}
	return out
}

func readyShaders(t *testing.T, service *Service) []ShaderEntry {
	t.Helper()
	out := make([]ShaderEntry, 0)
	for _, info := range readyInfos(t, service) {
		if info.Shader != nil {
			out = append(out, info.Shader.Shaders...)
		}
	}
	return out
}

func readyPresets(t *testing.T, service *Service) []ThemePreset {
	t.Helper()
	out := make([]ThemePreset, 0)
	for _, info := range readyInfos(t, service) {
		if info.Theme != nil {
			out = append(out, info.Theme.Presets...)
		}
	}
	return out
}

func TestScanValidPackOfEachKind(t *testing.T) {
	service, pluginsDir, _ := testService(t)

	writePack(t, pluginsDir, "src-demo", sourceManifest("src-demo"))
	shaderDir := writePack(t, pluginsDir, "shd-demo", shaderManifestSingle("shd-demo"))
	writeFile(t, filepath.Join(shaderDir, "shader.glsl"), validShaderSource)
	themeDir := writePack(t, pluginsDir, "thm-demo", themeManifest("thm-demo"))
	writeFile(t, filepath.Join(themeDir, "dusk.png"), string(tinyPNG(t)))

	infos := mustScan(t, service)
	if len(infos) != 3 {
		t.Fatalf("应扫到 3 个包，实际 %d: %+v", len(infos), infos)
	}
	for _, info := range infos {
		if info.Status != StatusReady {
			t.Fatalf("%s 应为 ready，实际 %+v", info.ID, info)
		}
		if info.ReasonCode != "" || info.Reason != "" {
			t.Fatalf("ready 包不该带原因，实际 %+v", info)
		}
	}

	source := findInfo(t, infos, "src-demo")
	if source.Kind != "source" || source.Source == nil || source.Shader != nil || source.Theme != nil {
		t.Fatalf("source 包的结构不对: %+v", source)
	}
	if len(source.Source.Adapters) != 1 {
		t.Fatalf("应有 1 个适配器，实际 %+v", source.Source.Adapters)
	}
	adapter := source.Source.Adapters[0]
	if adapter.ID != "demo_cms" || adapter.Name["en"] != "Demo CMS" {
		t.Fatalf("适配器内容不对: %+v", adapter)
	}
	var strategy map[string]any
	if err := json.Unmarshal(adapter.Strategy, &strategy); err != nil {
		t.Fatalf("strategy 原样转发应是合法 JSON: %v", err)
	}
	if fmt.Sprint(strategy["version"]) != "2" || strategy["strategy"] != "declarative" {
		t.Fatalf("strategy 内容被改动了: %+v", strategy)
	}
	if _, ok := strategy["field_mapping"]; !ok {
		t.Fatalf("strategy 应保留 field_mapping: %+v", strategy)
	}

	shader := findInfo(t, infos, "shd-demo")
	if shader.Shader == nil || len(shader.Shader.Shaders) != 1 {
		t.Fatalf("shader 包的结构不对: %+v", shader)
	}
	entry := shader.Shader.Shaders[0]
	if entry.ID != "soft_sharpen" || entry.Entry != "shader.glsl" || entry.Scale != 2 || entry.Category != "film" {
		t.Fatalf("着色器条目不对: %+v", entry)
	}
	if entry.Name["zh-CN"] != "柔和锐化" {
		t.Fatalf("名称应原样转发: %+v", entry.Name)
	}

	theme := findInfo(t, infos, "thm-demo")
	if theme.Theme == nil || len(theme.Theme.Presets) != 1 {
		t.Fatalf("theme 包的结构不对: %+v", theme)
	}
	preset := theme.Theme.Presets[0]
	if preset.ID != "dusk" || preset.Primary != "#6b4fbb" || preset.Mode != "dark" || preset.Pack != "thm-demo" {
		t.Fatalf("主题预设不对: %+v", preset)
	}
	if preset.Tint["ink"] != "#0d0b14" {
		t.Fatalf("tint 不对: %+v", preset.Tint)
	}
	if preset.BGImage != "dusk.png" {
		t.Fatalf("bg_image 应是包内相对路径，实际 %q", preset.BGImage)
	}

	// 引擎取数入口只给 ready 包的内容。
	if adapters := readyAdapters(t, service); len(adapters) != 1 || adapters[0].ID != "demo_cms" {
		t.Fatalf("Adapters() = %+v", adapters)
	}
	if shaders := readyShaders(t, service); len(shaders) != 1 || shaders[0].ID != "soft_sharpen" {
		t.Fatalf("Shaders() = %+v", shaders)
	}
	if presets := readyPresets(t, service); len(presets) != 1 || presets[0].ID != "dusk" {
		t.Fatalf("ThemePresets() = %+v", presets)
	}
}

func TestShaderSectionAcceptsArrayForm(t *testing.T) {
	service, pluginsDir, _ := testService(t)
	packDir := writePack(t, pluginsDir, "pack", `{"manifest_version":1,"id":"pack","version":"1.0.0","name":{"en":"P"},"kind":"shader","shader":{"shaders":[
		{"id":"one","name":{"en":"One"},"entry":"one.glsl","scale":1,"category":"anime"},
		{"id":"two","name":{"en":"Two"},"entry":"two.glsl"}
	]}}`)
	writeFile(t, filepath.Join(packDir, "one.glsl"), validShaderSource)
	writeFile(t, filepath.Join(packDir, "two.glsl"), validShaderSource)

	info := findInfo(t, mustScan(t, service), "pack")
	if info.Status != StatusReady {
		t.Fatalf("数组写法应通过，实际 %+v", info)
	}
	if len(info.Shader.Shaders) != 2 {
		t.Fatalf("应归一化成 2 个档位，实际 %+v", info.Shader.Shaders)
	}
	first, second := info.Shader.Shaders[0], info.Shader.Shaders[1]
	if first.Scale != 1 || first.Category != "anime" {
		t.Fatalf("显式写的 scale/category 不该被覆盖: %+v", first)
	}
	// 缺省值按 docs/plugins.md §4：scale=2、category=film。
	if second.Scale != 2 || second.Category != "film" {
		t.Fatalf("缺省值不对: %+v", second)
	}
}

func TestScanSkipsNonPackEntries(t *testing.T) {
	service, pluginsDir, _ := testService(t)

	// 不是目录的条目
	writeFile(t, filepath.Join(pluginsDir, "notes.txt"), "别把散落文件当包")
	// 没有 plugin.json 的目录：不是包，也不报 invalid
	if err := os.MkdirAll(filepath.Join(pluginsDir, "stray-folder"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(pluginsDir, "stray-folder", "README.md"), "我只是个备份文件夹")
	// 以 . 与 _ 开头的目录留给临时/备份用途
	writePack(t, pluginsDir, ".hidden-pack", shaderManifestSingle(".hidden-pack"))
	writePack(t, pluginsDir, "_backup-pack", shaderManifestSingle("_backup-pack"))
	// 深层目录不是包
	nested := filepath.Join(pluginsDir, "outer", "inner")
	writeFile(t, filepath.Join(nested, manifestFileName), shaderManifestSingle("inner"))

	packDir := writePack(t, pluginsDir, "real-pack", shaderManifestSingle("real-pack"))
	writeFile(t, filepath.Join(packDir, "shader.glsl"), validShaderSource)

	infos := mustScan(t, service)
	if len(infos) != 1 {
		t.Fatalf("只该扫到 1 个包，实际 %+v", infos)
	}
	if infos[0].ID != "real-pack" || infos[0].Status != StatusReady {
		t.Fatalf("扫到的不是预期的包: %+v", infos[0])
	}
}

func TestScanWithoutPluginsDirectory(t *testing.T) {
	root := t.TempDir()
	service := NewService(func() string { return root })
	service.loadStates = func() (map[string]bool, error) { return map[string]bool{}, nil }
	service.saveStates = func(map[string]bool) error { return nil }

	if got := service.Directory(); got != filepath.Join(root, "plugins") {
		t.Fatalf("Directory() = %q", got)
	}
	infos, err := service.List()
	if err != nil {
		t.Fatalf("没有 plugins 目录时不该报错: %v", err)
	}
	if len(infos) != 0 {
		t.Fatalf("应是空注册表，实际 %+v", infos)
	}
	if _, err := os.Stat(filepath.Join(root, "plugins")); !os.IsNotExist(err) {
		t.Fatalf("扫描不该创建目录: %v", err)
	}
	// 空注册表也不能挡住引擎取数。
	if len(readyAdapters(t, service)) != 0 || len(readyShaders(t, service)) != 0 || len(readyPresets(t, service)) != 0 {
		t.Fatal("空注册表应取不到任何条目")
	}
}

func TestInvalidPackNeverBreaksItsNeighbours(t *testing.T) {
	service, pluginsDir, _ := testService(t)

	// 三个邻居：坏 JSON、引用了不存在的文件、以及完全合法的那个。
	writePack(t, pluginsDir, "aaa-broken-json", `{"manifest_version":1,`)
	writePack(t, pluginsDir, "bbb-missing-file", shaderManifestSingle("bbb-missing-file"))
	writePack(t, pluginsDir, "ccc-good", sourceManifest("ccc-good"))
	// 一个不该被当包看的杂物目录，混在里面也不能影响别的包。
	if err := os.MkdirAll(filepath.Join(pluginsDir, "ddd-stray"), 0o755); err != nil {
		t.Fatal(err)
	}

	shaderDir := writePack(t, pluginsDir, "eee-shader", shaderManifestSingle("eee-shader"))
	writeFile(t, filepath.Join(shaderDir, "shader.glsl"), validShaderSource)

	infos := mustScan(t, service)
	if len(infos) != 4 {
		t.Fatalf("应扫到 4 个包（杂物目录跳过），实际 %+v", infos)
	}
	if got := findInfo(t, infos, "aaa-broken-json"); got.Status != StatusInvalid || got.ReasonCode != reasonJSONInvalid {
		t.Fatalf("坏 JSON 包: %+v", got)
	}
	if got := findInfo(t, infos, "bbb-missing-file"); got.Status != StatusInvalid || got.ReasonCode != reasonPathMissing {
		t.Fatalf("缺文件包: %+v", got)
	}
	if got := findInfo(t, infos, "ccc-good"); got.Status != StatusReady {
		t.Fatalf("邻居坏了不影响好包: %+v", got)
	}
	if got := findInfo(t, infos, "eee-shader"); got.Status != StatusReady {
		t.Fatalf("邻居坏了不影响好包: %+v", got)
	}
	// 无效包即使 id 撞上了也不参与去重，好包照常出现在引擎取数路径里。
	if adapters := readyAdapters(t, service); len(adapters) != 1 {
		t.Fatalf("Adapters() = %+v", adapters)
	}
	if shaders := readyShaders(t, service); len(shaders) != 1 {
		t.Fatalf("Shaders() = %+v", shaders)
	}

	// List 命中缓存：结果与 Scan 一致，且不再读盘。
	cached, err := service.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(cached) != len(infos) {
		t.Fatalf("缓存与扫描结果不一致: %d vs %d", len(cached), len(infos))
	}
}

func TestDuplicateIDMarksTheLaterPack(t *testing.T) {
	// 包 id 必须等于目录名，所以两个「合法」包天然不会撞 id；
	// 这条规则是防线（比如将来允许改名），直接对注册表做单元测试。
	infos := []Info{
		{ID: "dup", Dir: filepath.Join("plugins", "a"), Status: StatusReady},
		{ID: "dup", Dir: filepath.Join("plugins", "b"), Status: StatusReady},
	}
	markDuplicateIDs(infos)

	if infos[0].Status != StatusReady {
		t.Fatalf("先出现的那个应保持 ready，实际 %+v", infos[0])
	}
	if infos[1].Status != StatusInvalid || infos[1].ReasonCode != reasonIDDuplicate {
		t.Fatalf("后出现的那个应判 id_duplicate，实际 %+v", infos[1])
	}
	if infos[1].Source != nil || infos[1].Shader != nil || infos[1].Theme != nil {
		t.Fatalf("判 invalid 后要把段清空: %+v", infos[1])
	}
}

func TestPackCountOverCapMarksExtraPacksInvalid(t *testing.T) {
	service, pluginsDir, _ := testService(t)
	for index := 0; index <= maxPacks; index++ {
		id := fmt.Sprintf("pack%03d", index)
		packDir := writePack(t, pluginsDir, id, shaderManifestSingle(id))
		writeFile(t, filepath.Join(packDir, "shader.glsl"), validShaderSource)
		// 混一个杂物目录进去：它不该占配额，也不该被判 invalid。
		if index == 3 {
			if err := os.MkdirAll(filepath.Join(pluginsDir, "zzz-stray"), 0o755); err != nil {
				t.Fatal(err)
			}
		}
	}

	infos := mustScan(t, service)
	if len(infos) != maxPacks+1 {
		t.Fatalf("注册表应有 %d 个包，实际 %d", maxPacks+1, len(infos))
	}
	ready := infoByStatus(t, infos, StatusReady)
	invalid := infoByStatus(t, infos, StatusInvalid)
	if len(ready) != maxPacks {
		t.Fatalf("前 %d 个应可用，实际 %d", maxPacks, len(ready))
	}
	if len(invalid) != 1 || invalid[0].ReasonCode != reasonLimitExceeded {
		t.Fatalf("超上限的那 1 个应判 limit_exceeded，实际 %+v", invalid)
	}
	if got := len(readyShaders(t, service)); got != maxPacks {
		t.Fatalf("引擎取数应只拿到 %d 个档位，实际 %d", maxPacks, got)
	}
}

func TestDisabledRoundTrip(t *testing.T) {
	service, pluginsDir, states := testService(t)
	shaderDir := writePack(t, pluginsDir, "shd", shaderManifestSingle("shd"))
	writeFile(t, filepath.Join(shaderDir, "shader.glsl"), validShaderSource)
	writePack(t, pluginsDir, "src", sourceManifest("src"))

	if err := service.SetEnabled("shd", false); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if enabled, recorded := states["shd"]; !recorded || enabled {
		t.Fatalf("状态应落库成 false，实际 %v/%v", enabled, recorded)
	}

	infos, err := service.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if got := findInfo(t, infos, "shd"); got.Status != StatusDisabled {
		t.Fatalf("关闭后应是 disabled，实际 %+v", got)
	}
	// 未记录过的包默认可用，另一个包不受影响。
	if got := findInfo(t, infos, "src"); got.Status != StatusReady {
		t.Fatalf("别的包不该跟着被关: %+v", got)
	}
	if got := readyShaders(t, service); len(got) != 0 {
		t.Fatalf("disabled 包要退出引擎取数路径，实际 %+v", got)
	}
	if got := readyAdapters(t, service); len(got) != 1 {
		t.Fatalf("别的包仍要可取，实际 %+v", got)
	}

	// 重扫沿用持久化下来的状态（模拟重启）。
	infos = mustScan(t, service)
	if got := findInfo(t, infos, "shd"); got.Status != StatusDisabled {
		t.Fatalf("重扫后仍是 disabled，实际 %+v", got)
	}

	if err := service.SetEnabled("shd", true); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if _, recorded := states["shd"]; recorded {
		t.Fatalf("重新启用后应把这条记录删掉，实际 %v", states)
	}
	if got := findInfo(t, mustScan(t, service), "shd"); got.Status != StatusReady {
		t.Fatalf("重新启用后应是 ready，实际 %+v", got)
	}
	if got := readyShaders(t, service); len(got) != 1 {
		t.Fatalf("重新启用后档位要回来，实际 %+v", got)
	}
}

func TestSetEnabledRejectsUnknownID(t *testing.T) {
	service, pluginsDir, states := testService(t)
	writePack(t, pluginsDir, "src", sourceManifest("src"))

	if err := service.SetEnabled("ghost", false); err == nil {
		t.Fatal("注册表里没有的包 id 应报错")
	}
	if len(states) != 0 {
		t.Fatalf("报错时不该写状态: %v", states)
	}
	if err := service.SetEnabled("", false); err == nil {
		t.Fatal("空 id 应报错")
	}
}

func TestStatesCodec(t *testing.T) {
	states, err := decodeStates(`{"a":false,"b":true}`)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if states["a"] || states["b"] != true {
		t.Fatalf("解码结果不对: %v", states)
	}

	empty, err := decodeStates("")
	if err != nil {
		t.Fatalf("空值应可解: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("空值应是全启用: %v", empty)
	}

	if _, err := decodeStates("not json"); err == nil {
		t.Fatal("坏值要报错，让上层降级并记警告")
	}

	value, err := encodeStates(map[string]bool{"a": false, "b": true})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if value != `{"a":false}` {
		t.Fatalf("只该写下架的包，实际 %q", value)
	}
	if value, err := encodeStates(map[string]bool{"b": true}); err != nil || value != "" {
		t.Fatalf("全启用时应写空串，实际 %q/%v", value, err)
	}

	// 往返：编出来的值再解回去必须一致。
	roundTrip, err := decodeStates(value)
	if err != nil {
		t.Fatalf("round trip decode: %v", err)
	}
	if enabled, recorded := roundTrip["a"]; !recorded || enabled {
		t.Fatalf("round trip 后 a 仍应是禁用，实际 %v", roundTrip)
	}
	if _, recorded := roundTrip["b"]; recorded {
		t.Fatalf("round trip 后 b 不该有记录（缺省即可用）: %v", roundTrip)
	}
}

func TestReadFileAndReadAsset(t *testing.T) {
	service, pluginsDir, _ := testService(t)
	packDir := writePack(t, pluginsDir, "pack", themeManifest("pack"))
	writeFile(t, filepath.Join(packDir, "dusk.png"), string(tinyPNG(t)))
	writeFile(t, filepath.Join(packDir, "README.md"), "# 说明\n这是包内文档")
	writeFile(t, filepath.Join(packDir, "payload.exe"), "MZ\x90\x00")

	if err := os.WriteFile(filepath.Join(packDir, "big.png"), make([]byte, maxImageBytes+1), 0o644); err != nil {
		t.Fatal(err)
	}

	infos := mustScan(t, service)
	if got := findInfo(t, infos, "pack"); got.Status != StatusReady {
		t.Fatalf("主题包应可用，实际 %+v", got)
	}

	text, err := service.ReadFile("pack", "README.md")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(text, "包内文档") {
		t.Fatalf("内容不对: %q", text)
	}

	asset, err := service.ReadAsset("pack", "dusk.png")
	if err != nil {
		t.Fatalf("ReadAsset: %v", err)
	}
	if !strings.HasPrefix(asset, "data:image/png;base64,") {
		t.Fatalf("data URL 前缀不对: %.40s", asset)
	}

	// 越界与不该被读出来的东西。
	if _, err := service.ReadFile("pack", "../notes.txt"); err == nil {
		t.Fatal("包外路径必须拒绝")
	}
	if _, err := service.ReadFile("pack", "payload.exe"); err == nil {
		t.Fatal("非文本白名单必须拒绝")
	}
	if _, err := service.ReadAsset("pack", "payload.exe"); err == nil {
		t.Fatal("非图片白名单必须拒绝")
	}
	if _, err := service.ReadAsset("pack", "big.png"); err == nil {
		t.Fatal("超过 8 MiB 的图片必须拒绝而不是截断")
	}
	if _, err := service.ReadFile("pack", "nope.md"); err == nil {
		t.Fatal("不存在的文件要报错")
	}
	if _, err := service.ReadFile("ghost", "README.md"); err == nil {
		t.Fatal("不存在的包要报错")
	}
	// disabled 的包仍然可以读（设置页要看它的文件），只不进引擎。
	if err := service.SetEnabled("pack", false); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if _, err := service.ReadFile("pack", "README.md"); err != nil {
		t.Fatalf("disabled 包也应能读文件: %v", err)
	}
}

func TestInfoJSONContract(t *testing.T) {
	// 前端按这些键名写代码，改名等于改契约——这条测试把它钉住。
	service, pluginsDir, _ := testService(t)
	writePack(t, pluginsDir, "src", sourceManifest("src"))
	writePack(t, pluginsDir, "bad", `{"manifest_version":1,"id":"bad","version":"1.0.0","name":{"en":"B"},"kind":"shader"}`)

	infos := mustScan(t, service)
	encoded, err := json.Marshal(infos)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded []map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	wantKeys := []string{"id", "kind", "version", "name", "description", "dir", "status", "reason_code", "reason"}
	for _, item := range decoded {
		for _, key := range wantKeys {
			if _, ok := item[key]; !ok {
				t.Fatalf("Info 缺少 JSON 键 %q: %v", key, item)
			}
		}
	}

	var ready, invalid map[string]any
	for _, item := range decoded {
		if item["status"] == string(StatusReady) {
			ready = item
		} else {
			invalid = item
		}
	}
	if ready == nil || invalid == nil {
		t.Fatalf("用例没凑齐两种状态: %v", decoded)
	}
	if _, ok := ready["source"]; !ok {
		t.Fatalf("ready 的 source 包要带 source 段: %v", ready)
	}
	// author 只在 manifest 写了的时候出现：界面按「有就展示」处理。
	if got := ready["author"]; got != "tester" {
		t.Fatalf("ready 的 author = %v，想要 %q", got, "tester")
	}
	if _, ok := invalid["author"]; ok {
		t.Fatalf("manifest 没写 author 时不该出现这个键: %v", invalid)
	}
	for _, key := range []string{"shader", "theme"} {
		if _, ok := ready[key]; ok {
			t.Fatalf("source 包不该出现 %q 键", key)
		}
	}
	if _, ok := invalid["shader"]; ok {
		t.Fatalf("invalid 包不该带任何段: %v", invalid)
	}
	if invalid["reason_code"] != reasonSectionMissing {
		t.Fatalf("invalid 包要带原因码，实际 %v", invalid)
	}

	// Adapter 的 strategy 键名也是契约的一部分（界面直接写进 strategy_config）。
	adapterPayload, err := json.Marshal(readyAdapters(t, service)[0])
	if err != nil {
		t.Fatalf("marshal adapter: %v", err)
	}
	var adapter map[string]any
	if err := json.Unmarshal(adapterPayload, &adapter); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"id", "name", "strategy"} {
		if _, ok := adapter[key]; !ok {
			t.Fatalf("Adapter 缺少 JSON 键 %q: %v", key, adapter)
		}
	}
	// Strategy 是 json.RawMessage：过了 Wails 绑定后前端拿到的是「对象」而不是字符串，
	// 直接就能写进 sources.strategy_config，不需要再 JSON.parse 一次。
	if _, ok := adapter["strategy"].(map[string]any); !ok {
		t.Fatalf("strategy 要序列化成 JSON 对象，实际类型 %T: %v", adapter["strategy"], adapter["strategy"])
	}
}

// 生产路径在数据库没打开时必须照样能扫：NewService 只接 dataDir，
// 状态读写用的是 app/db；库为 nil 时降级成「全部启用」而不是 panic。
func TestScanWithoutOpenDatabase(t *testing.T) {
	root := t.TempDir()
	pluginsDir := filepath.Join(root, "plugins")
	service := NewService(func() string { return root })

	writePack(t, pluginsDir, "src", sourceManifest("src"))
	infos, err := service.Scan()
	if err != nil {
		t.Fatalf("数据库没打开时扫描不该报错: %v", err)
	}
	if len(infos) != 1 || infos[0].Status != StatusReady {
		t.Fatalf("应扫到 1 个 ready 包，实际 %+v", infos)
	}
	if err := service.SetEnabled("src", false); err == nil {
		t.Fatal("库没打开时保存状态要报错，不能静默丢失")
	}
}
