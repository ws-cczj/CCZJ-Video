package plugin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// repoDir 从测试的工作目录往上找到仓库根（以 go.mod 为记号），
// 这样 docs/examples 的绝对位置不用写死在测试里。
func repoDir(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for current := dir; ; {
		if info, statErr := os.Stat(filepath.Join(current, "go.mod")); statErr == nil && !info.IsDir() {
			return current
		}
		parent := filepath.Dir(current)
		if parent == current {
			t.Fatalf("没找到仓库根（从 %s 往上都没有 go.mod）", dir)
		}
		current = parent
	}
}

// 六个样例包是 docs/plugins.md §9 对作者的承诺：它们必须过得了同一套严格校验，
// 否则文档就是在教人写一个会被应用拒掉的包。
func TestShippedExamplePacksValidate(t *testing.T) {
	root := repoDir(t)
	examplesDir := filepath.Join(root, "docs", "examples")
	if _, err := os.Stat(examplesDir); err != nil {
		t.Fatalf("样例目录缺失（docs/plugins.md §8 承诺过它）: %v", err)
	}

	// 把样例拷进临时 plugins 根，走一遍真实的扫描路径。
	service, pluginsDir, _ := testService(t)
	entries, err := os.ReadDir(examplesDir)
	if err != nil {
		t.Fatalf("read examples: %v", err)
	}
	if len(entries) != 6 {
		t.Fatalf("样例应是 6 个包，实际 %d: %+v", len(entries), entries)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		copyTree(t, filepath.Join(examplesDir, entry.Name()), filepath.Join(pluginsDir, entry.Name()))
	}

	infos := mustScan(t, service)
	if len(infos) != 6 {
		t.Fatalf("6 个样例都该被识别，实际 %+v", infos)
	}
	for _, info := range infos {
		if info.Status != StatusReady {
			t.Fatalf("样例包 %s 校验未通过: code=%q reason=%q", info.ID, info.ReasonCode, info.Reason)
		}
	}

	// 顺带验证样例的内容确实是文档说的那样。
	if got := len(readyAdapters(t, service)); got != 1 {
		t.Fatalf("source 样例应有 1 个适配器，实际 %d", got)
	}
	if got := len(readyShaders(t, service)); got != 1 {
		t.Fatalf("shader 样例应有 1 个档位，实际 %d", got)
	}
	presets := readyPresets(t, service)
	if len(presets) != 1 || presets[0].ID != "dusk" {
		t.Fatalf("theme 样例应有 dusk 预设，实际 %+v", presets)
	}

	// 背景图能通过 ReadAsset 真读出来（文档里那句「包内图片」得站得住）。
	asset, err := service.ReadAsset(presets[0].Pack, presets[0].BGImage)
	if err != nil {
		t.Fatalf("读样例背景图失败: %v", err)
	}
	if len(asset) < len("data:image/png;base64,") {
		t.Fatalf("data URL 太短: %d", len(asset))
	}

	shader := readyShaders(t, service)[0]
	if _, err := service.ReadFile("shader-soft-sharpen", shader.Entry); err != nil {
		t.Fatalf("读样例着色器失败: %v", err)
	}

	// script 样例：文档里那句「入口脚本 + 包内样式」得能被前端真读出来。
	var scriptInfo *Info
	for i := range infos {
		if infos[i].ID == "script-ui-tweaks" {
			scriptInfo = &infos[i]
		}
	}
	if scriptInfo == nil {
		t.Fatalf("样例里应有 script-ui-tweaks，实际 %+v", infos)
	}
	if scriptInfo.Script == nil {
		t.Fatalf("script-ui-tweaks 应带 script 段")
	}
	if scriptInfo.Script.Entry != "main.js" || len(scriptInfo.Script.Styles) != 1 || scriptInfo.Script.Styles[0] != "inspector.css" {
		t.Fatalf("script 段与 manifest 写的不一致: %+v", scriptInfo.Script)
	}
	for _, path := range append([]string{scriptInfo.Script.Entry}, scriptInfo.Script.Styles...) {
		body, err := service.ReadFile(scriptInfo.ID, path)
		if err != nil {
			t.Fatalf("读样例脚本文件 %s 失败: %v", path, err)
		}
		if strings.TrimSpace(body) == "" {
			t.Fatalf("样例脚本文件 %s 是空的", path)
		}
	}

	// 图标：文档承诺「manifest.icon 指向的包内图片会显示在卡片上」，样例就得真带一张。
	if scriptInfo.Icon != "icon.png" {
		t.Fatalf("script-ui-tweaks 的 icon = %q，期望 icon.png", scriptInfo.Icon)
	}
	icon, err := service.ReadAsset(scriptInfo.ID, scriptInfo.Icon)
	if err != nil {
		t.Fatalf("读样例图标失败: %v", err)
	}
	if !strings.HasPrefix(icon, "data:image/png;base64,") {
		t.Fatalf("样例图标不是 PNG data URL: %q", icon[:min(len(icon), 40)])
	}
}

// icon-gallery 这个样例包专门负责「图标能不能显示」这条链路：卡片图标、包内清单、
// 清单里每一张 PNG 都得真从文件夹读出来。前端只是把它们画上去，读的动作全在 Go 侧，
// 所以这一节用 Go 测试守住就够了。
func TestIconGalleryExamplePackReadsEveryIcon(t *testing.T) {
	root := repoDir(t)
	packDir := filepath.Join(root, "docs", "examples", "icon-gallery")

	service, pluginsDir, _ := testService(t)
	copyTree(t, packDir, filepath.Join(pluginsDir, "icon-gallery"))

	infos := mustScan(t, service)
	if len(infos) != 1 {
		t.Fatalf("只该扫到 icon-gallery 一个包，实际 %+v", infos)
	}
	info := infos[0]
	if info.Status != StatusReady {
		t.Fatalf("icon-gallery 校验未通过: code=%q reason=%q", info.ReasonCode, info.Reason)
	}
	if info.Icon != "icon.png" {
		t.Fatalf("icon = %q，期望 icon.png", info.Icon)
	}
	card, err := service.ReadAsset(info.ID, info.Icon)
	if err != nil {
		t.Fatalf("读卡片图标失败: %v", err)
	}
	if !strings.HasPrefix(card, "data:image/png;base64,") {
		t.Fatalf("卡片图标不是 PNG data URL: %q", card[:min(len(card), 40)])
	}
	// plugin.json + main.js + gallery.css + icon.png + icons.json + icons/*.png
	if info.Files != 31 {
		t.Fatalf("扫描数出的文件数 = %d，期望 31（子目录 icons/ 也得数进来）", info.Files)
	}

	list, err := service.ReadFile(info.ID, "icons.json")
	if err != nil {
		t.Fatalf("读 icons.json 失败: %v", err)
	}
	var entries []struct {
		File string `json:"file"`
	}
	if err := json.Unmarshal([]byte(list), &entries); err != nil {
		t.Fatalf("icons.json 不是合法 JSON: %v", err)
	}
	if len(entries) != 26 {
		t.Fatalf("清单应有 26 枚图标，实际 %d", len(entries))
	}
	for _, entry := range entries {
		rel := "icons/" + entry.File
		asset, readErr := service.ReadAsset(info.ID, rel)
		if readErr != nil {
			t.Fatalf("读 %s 失败: %v", rel, readErr)
		}
		if !strings.HasPrefix(asset, "data:image/png;base64,") {
			t.Fatalf("%s 不是 PNG data URL", rel)
		}
	}
}

// 用户接下来要做的动作是把 docs/examples/icon-gallery 整个文件夹拖进窗口。这条路的
// 单测用的都是小合成包，这里换成真文件夹过一遍：31 个文件、嵌套的 icons/、一张卡片图
// 加 26 张 PNG，都在 §6 的个数与体积上限之内。装不上就是交付了一个拖不动的样例。
func TestIconGalleryExamplePackInstallsByDrag(t *testing.T) {
	service, pluginsDir, _ := testService(t)
	packDir := filepath.Join(repoDir(t), "docs", "examples", "icon-gallery")
	info, err := service.InstallFromPath(packDir)
	if err != nil {
		t.Fatalf("拖放安装 icon-gallery 失败: %v", err)
	}
	if info.ID != "icon-gallery" || info.Status != StatusReady {
		t.Fatalf("装完的 Info 不对: %+v", info)
	}
	if info.Dir != filepath.Join(pluginsDir, "icon-gallery") {
		t.Fatalf("包目录 = %q", info.Dir)
	}
	if info.Files != 31 {
		t.Fatalf("安装后注册表数出的文件数 = %d，期望 31", info.Files)
	}
	assertStagingClean(t, service)
	if _, statErr := os.Stat(filepath.Join(info.Dir, "icons", "home.png")); statErr != nil {
		t.Fatalf("嵌套目录里的 PNG 没落盘: %v", statErr)
	}
}

// ai-assistant 是唯一一份「包自己往界面塞一整页、再把既有后端绑定当工具使」的样例，
// 也是文档 §6.5「能动界面、不能动内核」的活样本。这里守住三件事：入口读得出、
// 图标读得出、以及界面文案在两份语言里成对。最后那条不是洁癖——这份包的文案是包
// 自己 i18n.merge 进去的，少一边界面上看到的就是裸 key（写的时候就漏过两个）。
func TestAIAssistantExamplePack(t *testing.T) {
	service, pluginsDir, _ := testService(t)
	copyTree(t, filepath.Join(repoDir(t), "docs", "examples", "ai-assistant"), filepath.Join(pluginsDir, "ai-assistant"))

	infos := mustScan(t, service)
	if len(infos) != 1 {
		t.Fatalf("只该扫到 ai-assistant 一个包，实际 %+v", infos)
	}
	info := infos[0]
	if info.Status != StatusReady {
		t.Fatalf("ai-assistant 校验未通过: code=%q reason=%q", info.ReasonCode, info.Reason)
	}
	if info.Kind != "script" || info.Script == nil || info.Script.Entry != "main.js" {
		t.Fatalf("应是带 main.js 入口的 script 包: kind=%q script=%+v", info.Kind, info.Script)
	}
	if info.Icon != "icon.png" {
		t.Fatalf("icon = %q，期望 icon.png", info.Icon)
	}
	entry, err := service.ReadFile(info.ID, info.Script.Entry)
	if err != nil {
		t.Fatalf("读 main.js 失败: %v", err)
	}
	card, err := service.ReadAsset(info.ID, info.Icon)
	if err != nil {
		t.Fatalf("读卡片图标失败: %v", err)
	}
	if !strings.HasPrefix(card, "data:image/png;base64,") {
		t.Fatalf("卡片图标不是 PNG data URL: %q", card[:min(len(card), 40)])
	}

	used := map[string]bool{}
	for _, m := range regexp.MustCompile(`k\('([a-zA-Z]+)'\)`).FindAllStringSubmatch(entry, -1) {
		used[m[1]] = true
	}
	if len(used) == 0 {
		t.Fatalf("没从 main.js 里认出任何文案键，上面的正则该修了")
	}
	for name := range used {
		// 只认「12 空格缩进 + 值是字符串字面量」那一行：界面里 h('textarea', { placeholder: ... })
		// 也是 12 空格，不收紧就会把它当成文案声明数错。
		decls := regexp.MustCompile(`(?m)^ {12}`+name+`: '`).FindAllString(entry, -1)
		if len(decls) != 2 {
			t.Errorf("文案键 %s 在两份语言里出现 %d 次，应为 2 次", name, len(decls))
		}
	}
}

func copyTree(t *testing.T, from, to string) {
	t.Helper()
	entries, err := os.ReadDir(from)
	if err != nil {
		t.Fatalf("read %s: %v", from, err)
	}
	if err := os.MkdirAll(to, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", to, err)
	}
	for _, entry := range entries {
		source := filepath.Join(from, entry.Name())
		destination := filepath.Join(to, entry.Name())
		if entry.IsDir() {
			copyTree(t, source, destination)
			continue
		}
		data, err := os.ReadFile(source)
		if err != nil {
			t.Fatalf("read %s: %v", source, err)
		}
		if err := os.WriteFile(destination, data, 0o644); err != nil {
			t.Fatalf("write %s: %v", destination, err)
		}
	}
}
