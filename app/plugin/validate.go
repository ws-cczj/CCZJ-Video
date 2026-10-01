package plugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// loadPack 校验一个候选目录。found=false 表示它不是扩展包（目录里没有
// plugin.json），调用方静默跳过；found=true 且 Status==invalid 才是真正
// 写坏了的包。单个包的失败绝不会传染给邻居（ADR 0007）。
func loadPack(packDir string, overCap bool) (Info, bool) {
	dirName := filepath.Base(packDir)
	if overCap {
		// 超出数量上限：只有真是扩展包的目录才值得报一张错误卡片，杂物目录照旧跳过。
		if _, err := os.Stat(filepath.Join(packDir, manifestFileName)); err != nil {
			return Info{}, false
		}
		return invalidInfo(dirName, packDir, newPackError(reasonLimitExceeded,
			"扩展包数量超过上限 %d，本目录被忽略；请先删掉不用的包", maxPacks)), true
	}

	doc, err := parseManifest(packDir, dirName)
	if err != nil {
		if errors.Is(err, errManifestAbsent) {
			return Info{}, false
		}
		return invalidInfo(dirName, packDir, err), true
	}

	basic := Info{
		ID:          doc.ID,
		Kind:        doc.Kind,
		Version:     doc.Version,
		Name:        doc.Name,
		Description: doc.Description,
		Author:      strings.TrimSpace(doc.Author),
		Dir:         packDir,
		// 声明归声明，授权是注入运行时那本账；这里只把「包自陈要用什么」原样传出去。
		Permissions: doc.Permissions,
	}

	source, shader, theme, script := sectionBody(doc.Source), sectionBody(doc.Shader), sectionBody(doc.Theme), sectionBody(doc.Script)
	for _, foreign := range []struct {
		name string
		body []byte
	}{
		{"source", source}, {"shader", shader}, {"theme", theme},
	} {
		// 这三节互斥且必须与 kind 同名；script 不在这里比——它是横切段，
		// 任何一种 kind 都可以捎带它。
		if foreign.body != nil && foreign.name != doc.Kind {
			return invalidInfo(dirName, packDir, newPackError(reasonKindSection,
				"kind=%s 只能带 %s 段，却出现了 %s 段", doc.Kind, doc.Kind, foreign.name)), true
		}
	}

	// own 是 kind 对应的那一节，缺失就说明包没提供它承诺的数据。
	own := map[string][]byte{"source": source, "shader": shader, "theme": theme, "script": script}[doc.Kind]
	if own == nil {
		return invalidInfo(doc.ID, packDir, newPackError(reasonSectionMissing,
			"kind=%s 必须提供 %s 段", doc.Kind, doc.Kind)), true
	}
	// 数据节各自校验；出错就整包判 invalid，不做「忽略坏段」式的宽容。
	var sectionErr error
	switch doc.Kind {
	case "source":
		basic.Source, sectionErr = validateSourceSection(own)
	case "shader":
		basic.Shader, sectionErr = validateShaderSection(packDir, own)
	case "theme":
		basic.Theme, sectionErr = validateThemeSection(packDir, doc.ID, own)
	case "script":
		basic.Script, sectionErr = validateScriptSection(packDir, own)
	}
	if sectionErr != nil {
		return invalidInfo(doc.ID, packDir, sectionErr), true
	}

	// 非 kind=script 的包也可以带 script 段：主题包捎一段自己的样式是合法用法。
	if doc.Kind != "script" && script != nil {
		section, err := validateScriptSection(packDir, script)
		if err != nil {
			return invalidInfo(doc.ID, packDir, err), true
		}
		basic.Script = section
	}

	icon, iconErr := resolveIcon(packDir, doc.Icon)
	if iconErr != nil {
		return invalidInfo(doc.ID, packDir, iconErr), true
	}
	basic.Icon = icon

	basic.Status = StatusReady
	return basic, true
}

// sectionBody 把「没有这个键」「显式 null」统一成 nil：写了 `"shader": null`
// 不算提供了一个 shader 段，不给它开后门。
func sectionBody(raw json.RawMessage) []byte {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return nil
	}
	return raw
}

func invalidInfo(id, dir string, err error) Info {
	info := Info{ID: id, Dir: dir, Status: StatusInvalid}
	var pe *packError
	if asPackError(err, &pe) {
		info.ReasonCode = string(pe.Code)
		info.Reason = pe.Message
		return info
	}
	info.ReasonCode = reasonJSONInvalid
	info.Reason = packErrorMessage(err)
	return info
}

// ---------- kind: source ----------

type sourceSectionDoc struct {
	Adapters []adapterDoc `json:"adapters"`
}

type adapterDoc struct {
	ID       string            `json:"id"`
	Name     map[string]string `json:"name"`
	Strategy json.RawMessage   `json:"strategy"`
}

func validateSourceSection(raw []byte) (*SourceSection, error) {
	var doc sourceSectionDoc
	if err := decodeStrict(raw, &doc, reasonStrategyInvalid); err != nil {
		return nil, inSection(err, "source")
	}
	if len(doc.Adapters) == 0 {
		return nil, newPackError(reasonStrategyInvalid, "source.adapters 至少要有 1 个适配器")
	}

	section := &SourceSection{Adapters: make([]Adapter, 0, len(doc.Adapters))}
	seen := make(map[string]bool, len(doc.Adapters))
	for index, item := range doc.Adapters {
		label := fmt.Sprintf("source.adapters[%d]", index)
		if !adapterIDPattern.MatchString(item.ID) {
			return nil, newPackError(reasonStrategyInvalid,
				"%s.id %q 不符合 ^[a-z0-9][a-z0-9_-]{0,63}$", label, item.ID)
		}
		if seen[item.ID] {
			return nil, newPackError(reasonStrategyInvalid, "适配器 id %q 在本包内重复", item.ID)
		}
		seen[item.ID] = true
		if err := validateLocalized(label+".name", item.Name, maxNameChars); err != nil {
			return nil, inSection(err, "source")
		}
		strategy, err := validateStrategy(item.Strategy)
		if err != nil {
			return nil, inSection(err, fmt.Sprintf("%s.strategy", label))
		}
		section.Adapters = append(section.Adapters, Adapter{ID: item.ID, Name: item.Name, Strategy: strategy})
	}
	return section, nil
}

// ---------- kind: shader ----------

type shaderArrayDoc struct {
	Shaders []shaderEntryDoc `json:"shaders"`
}

type shaderEntryDoc struct {
	ID       string            `json:"id"`
	Name     map[string]string `json:"name"`
	Entry    string            `json:"entry"`
	Scale    *int              `json:"scale"`
	Category string            `json:"category"`
}

// shaderEntries 同时接受 §4 的单个对象写法和 {"shaders": [...]} 数组写法，
// 归一化成列表；两种写法不能混（数组写法里再出现 id/entry 就是未知字段）。
func shaderEntries(raw []byte) ([]shaderEntryDoc, error) {
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(raw, &keys); err != nil {
		return nil, newPackError(reasonJSONInvalid, "shader 段不是 JSON 对象: %v", err)
	}
	if _, isArray := keys["shaders"]; isArray {
		var doc shaderArrayDoc
		if err := decodeStrict(raw, &doc, reasonShaderInvalid); err != nil {
			return nil, inSection(err, "shader")
		}
		return doc.Shaders, nil
	}
	var single shaderEntryDoc
	if err := decodeStrict(raw, &single, reasonShaderInvalid); err != nil {
		return nil, inSection(err, "shader")
	}
	return []shaderEntryDoc{single}, nil
}

func validateShaderSection(packDir string, raw []byte) (*ShaderSection, error) {
	entries, err := shaderEntries(raw)
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, newPackError(reasonShaderInvalid, "shader.shaders 至少要有 1 个档位")
	}

	section := &ShaderSection{Shaders: make([]ShaderEntry, 0, len(entries))}
	seen := make(map[string]bool, len(entries))
	for index, item := range entries {
		label := fmt.Sprintf("shader.shaders[%d]", index)
		if !entryIDPattern.MatchString(item.ID) {
			return nil, newPackError(reasonShaderInvalid,
				"%s.id %q 不符合 ^[a-z][a-z0-9_-]{0,31}$", label, item.ID)
		}
		if seen[item.ID] {
			return nil, newPackError(reasonShaderInvalid, "着色器 id %q 在本包内重复", item.ID)
		}
		seen[item.ID] = true
		if err := validateLocalized(label+".name", item.Name, maxNameChars); err != nil {
			return nil, inSection(err, "shader")
		}

		if !strings.EqualFold(filepath.Ext(item.Entry), ".glsl") {
			return nil, newPackError(reasonPathInvalid, "%s.entry %q 必须指向 .glsl 文件", label, item.Entry)
		}
		path, err := resolveInside(packDir, item.Entry)
		if err != nil {
			return nil, inSection(err, label+".entry")
		}
		if !fileWithinCap(path, maxShaderBytes) {
			return nil, newPackError(reasonPathTooLarge, "%s.entry 超过 %d MiB 上限", label, maxShaderBytes>>20)
		}
		source, err := readCapped(path, maxShaderBytes)
		if err != nil {
			return nil, inSection(err, label+".entry")
		}
		if err := checkShaderSource(source); err != nil {
			return nil, inSection(err, label+".entry")
		}

		scale := 2
		if item.Scale != nil {
			scale = *item.Scale
			if scale != 1 && scale != 2 {
				return nil, newPackError(reasonScaleInvalid, "%s.scale 只能是 1 或 2，当前是 %d", label, scale)
			}
		}
		category := "film"
		if strings.TrimSpace(item.Category) != "" {
			if !shaderCategories[item.Category] {
				return nil, newPackError(reasonShaderInvalid, "%s.category %q 不是 anime|film", label, item.Category)
			}
			category = item.Category
		}

		section.Shaders = append(section.Shaders, ShaderEntry{
			ID:       item.ID,
			Name:     item.Name,
			Entry:    filepath.ToSlash(item.Entry),
			Scale:    scale,
			Category: category,
		})
	}
	return section, nil
}

// checkShaderSource 对齐前端 WebGL 执行器 frontend/src/utils/filmUpscaler.ts 的
// parseMpvShader：它按行 trim 后认 `//!HOOK ` 起一个 pass，并把带 `2 *` 的
// //!WIDTH / //!HEIGHT 当成放大聚合 pass。包里的文件必须能被那条路径跑起来，
// 所以这里在扫描阶段就按同样的字面规则判掉。
func checkShaderSource(source []byte) error {
	hooks := 0
	for _, line := range strings.Split(string(source), "\n") {
		trimmed := strings.TrimSpace(line)
		fields := strings.Fields(trimmed)
		if len(fields) >= 2 && fields[0] == "//!HOOK" {
			hooks++
			continue
		}
		if strings.HasPrefix(trimmed, "//!WIDTH ") || strings.HasPrefix(trimmed, "//!HEIGHT ") {
			if strings.Contains(trimmed, "2 *") {
				return newPackError(reasonResolutionChanging,
					"pass 声明了改变分辨率的输出尺寸，引擎只跑同分辨率（放大由 scale 负责）: %q", trimmed)
			}
		}
	}
	if hooks == 0 {
		return newPackError(reasonEntryEmpty, "文件里没有任何 //!HOOK pass，执行器不会画出任何东西")
	}
	return nil
}

// ---------- kind: theme ----------

type themeArrayDoc struct {
	Presets []themePresetDoc `json:"presets"`
}

type themePresetDoc struct {
	ID      string            `json:"id"`
	Name    map[string]string `json:"name"`
	Primary string            `json:"primary"`
	Mode    string            `json:"mode"`
	Tint    map[string]string `json:"tint"`
	BGImage string            `json:"bg_image"`
}

// themePresets 与 shader 段一样接受 §5 的 presets 数组，也接受把单个预设
// 直接写在 theme 里。
func themePresets(raw []byte) ([]themePresetDoc, error) {
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(raw, &keys); err != nil {
		return nil, newPackError(reasonJSONInvalid, "theme 段不是 JSON 对象: %v", err)
	}
	if _, isArray := keys["presets"]; isArray {
		var doc themeArrayDoc
		if err := decodeStrict(raw, &doc, reasonThemeInvalid); err != nil {
			return nil, inSection(err, "theme")
		}
		return doc.Presets, nil
	}
	var single themePresetDoc
	if err := decodeStrict(raw, &single, reasonThemeInvalid); err != nil {
		return nil, inSection(err, "theme")
	}
	return []themePresetDoc{single}, nil
}

func validateThemeSection(packDir, packID string, raw []byte) (*ThemeSection, error) {
	entries, err := themePresets(raw)
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, newPackError(reasonThemeInvalid, "theme.presets 至少要有 1 个预设")
	}

	section := &ThemeSection{Presets: make([]ThemePreset, 0, len(entries))}
	seen := make(map[string]bool, len(entries))
	for index, item := range entries {
		label := fmt.Sprintf("theme.presets[%d]", index)
		if !entryIDPattern.MatchString(item.ID) {
			return nil, newPackError(reasonThemeInvalid,
				"%s.id %q 不符合 ^[a-z][a-z0-9_-]{0,31}$", label, item.ID)
		}
		if seen[item.ID] {
			return nil, newPackError(reasonThemeInvalid, "主题预设 id %q 在本包内重复", item.ID)
		}
		seen[item.ID] = true
		if err := validateLocalized(label+".name", item.Name, maxNameChars); err != nil {
			return nil, inSection(err, "theme")
		}
		if !hexColorPattern.MatchString(item.Primary) {
			return nil, newPackError(reasonThemeInvalid, "%s.primary %q 不是 #rgb 或 #rrggbb", label, item.Primary)
		}
		if !themeModes[item.Mode] {
			return nil, newPackError(reasonThemeInvalid, "%s.mode %q 不是 light|dark", label, item.Mode)
		}
		for channel, value := range item.Tint {
			if !tintKeyPattern.MatchString(channel) {
				return nil, newPackError(reasonThemeInvalid, "%s.tint 通道名 %q 不合法", label, channel)
			}
			if !hexColorPattern.MatchString(value) {
				return nil, newPackError(reasonThemeInvalid, "%s.tint.%s %q 不是 #rgb 或 #rrggbb", label, channel, value)
			}
		}

		bgImage := ""
		if strings.TrimSpace(item.BGImage) != "" {
			if _, isImage := imageMimes[strings.ToLower(filepath.Ext(item.BGImage))]; !isImage {
				return nil, newPackError(reasonPathInvalid, "%s.bg_image %q 不是支持的图片扩展名", label, item.BGImage)
			}
			path, err := resolveInside(packDir, item.BGImage)
			if err != nil {
				return nil, inSection(err, label+".bg_image")
			}
			if !fileWithinCap(path, maxImageBytes) {
				return nil, newPackError(reasonPathTooLarge, "%s.bg_image 超过 %d MiB 上限", label, maxImageBytes>>20)
			}
			bgImage = filepath.ToSlash(item.BGImage)
		}

		section.Presets = append(section.Presets, ThemePreset{
			ID:      item.ID,
			Name:    item.Name,
			Primary: strings.ToLower(item.Primary),
			Mode:    item.Mode,
			Tint:    item.Tint,
			BGImage: bgImage,
			Pack:    packID,
		})
	}
	return section, nil
}

// ---------- kind: script（以及任意 kind 的横切 script 段）----------

type scriptSectionDoc struct {
	Entry  string   `json:"entry"`
	Styles []string `json:"styles"`
}

// validateScriptSection 只确认「前端要读的那几个文件确实在包里、够小、扩展名对」。
// 它不看 JS 的内容——语义是否合法只有真跑起来才知道，那一层的错误由前端的
// 逐包隔离接住（跑到一半炸了的包会被单独禁用，不牵连别人）。
func validateScriptSection(packDir string, raw []byte) (*ScriptSection, error) {
	var doc scriptSectionDoc
	if err := decodeStrict(raw, &doc, reasonScriptInvalid); err != nil {
		return nil, inSection(err, "script")
	}

	entry, err := checkScriptFile(packDir, "script.entry", doc.Entry, scriptExtensions, reasonScriptInvalid)
	if err != nil {
		return nil, err
	}

	var styles []string
	for index, style := range doc.Styles {
		label := fmt.Sprintf("script.styles[%d]", index)
		path, err := checkScriptFile(packDir, label, style, cssExtensions, reasonScriptInvalid)
		if err != nil {
			return nil, err
		}
		styles = append(styles, path)
	}
	return &ScriptSection{Entry: entry, Styles: styles}, nil
}

// checkScriptFile 校验一个 script 段引用的包内文件，返回给前端用的正斜杠相对路径。
// 这里真读一遍：体积上限要在扫描阶段就生效，别让界面拿着一个 5 GB 的路径去注入。
func checkScriptFile(packDir, label, relPath string, allowed map[string]bool, code string) (string, error) {
	if !allowed[strings.ToLower(filepath.Ext(relPath))] {
		return "", newPackError(code, "%s %q 扩展名不支持", label, relPath)
	}
	path, err := resolveInside(packDir, relPath)
	if err != nil {
		return "", inSection(err, label)
	}
	data, err := readCapped(path, maxScriptBytes)
	if err != nil {
		return "", inSection(err, label)
	}
	if strings.TrimSpace(string(data)) == "" {
		return "", newPackError(code, "%s 指向的文件 %q 是空的", label, relPath)
	}
	return filepath.ToSlash(relPath), nil
}
