package plugin

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// manifestDoc 是 plugin.json 的严格形状（docs/plugins.md §2）。四个 kind 段先收成
// json.RawMessage：未知字段要在「本层」判定，段内则各自再跑一次严格解码。
type manifestDoc struct {
	ManifestVersion int               `json:"manifest_version"`
	ID              string            `json:"id"`
	Version         string            `json:"version"`
	Name            map[string]string `json:"name"`
	Description     map[string]string `json:"description"`
	Author          string            `json:"author"`
	Kind            string            `json:"kind"`
	Icon            string            `json:"icon"`
	Permissions     []string          `json:"permissions"`
	Source          json.RawMessage   `json:"source"`
	Shader          json.RawMessage   `json:"shader"`
	Theme           json.RawMessage   `json:"theme"`
	Script          json.RawMessage   `json:"script"`
}

// decodeStrict 用 json.Decoder + DisallowUnknownFields 解码，未知键报 unknown_field
// （消息里带解码器原文，即出错的键名），其余失败按 fallbackCode 报。
// 尾随内容（`{}{}`、两个 JSON 串在一起）按 json_invalid 处理。
func decodeStrict(raw []byte, target any, fallbackCode string) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(target); err != nil {
		return classifyJSONError(err, fallbackCode)
	}
	if dec.More() {
		return newPackError(reasonJSONInvalid, "文件末尾还有多余内容")
	}
	return nil
}

func classifyJSONError(err error, fallbackCode string) error {
	msg := err.Error()
	// encoding/json 的 fieldError 是未导出类型，只能按它固定的前缀认。
	if strings.HasPrefix(msg, "json: unknown field ") {
		return newPackError(reasonUnknownField, "%s", msg)
	}
	return newPackError(fallbackCode, "%s", msg)
}

// errManifestAbsent 表示目录里没有 plugin.json：那不是扩展包，跳过即可，
// 不能报成 invalid，否则随便一个杂物文件夹都会变成一张错误卡片。
var errManifestAbsent = errors.New("plugin: no manifest in directory")

// readManifest 用 io.LimitReader 读 plugin.json：一个 5 GB 的恶意文件也只能读到
// 上限+1 字节，读完即判 manifest_too_large，绝不整份吸进内存。
func readManifest(packDir string) ([]byte, error) {
	path := filepath.Join(packDir, manifestFileName)
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, errManifestAbsent
		}
		return nil, newPackError(reasonManifestUnreadable, "无法读取 %s: %v", manifestFileName, err)
	}
	if !info.Mode().IsRegular() {
		return nil, newPackError(reasonManifestUnreadable, "%s 不是普通文件", manifestFileName)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, newPackError(reasonManifestUnreadable, "无法打开 %s: %v", manifestFileName, err)
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, maxManifestBytes+1))
	if err != nil {
		return nil, newPackError(reasonManifestUnreadable, "读取 %s 失败: %v", manifestFileName, err)
	}
	if int64(len(data)) > maxManifestBytes {
		return nil, newPackError(reasonManifestTooLarge, "%s 超过 %d KiB 上限", manifestFileName, maxManifestBytes/1024)
	}
	return data, nil
}

// parseManifest 读 + 严格解码，只做与段无关的通用字段校验（§2 的表）。
func parseManifest(packDir, dirName string) (*manifestDoc, error) {
	raw, err := readManifest(packDir)
	if err != nil {
		return nil, err
	}
	var doc manifestDoc
	if err := decodeStrict(raw, &doc, reasonJSONInvalid); err != nil {
		return nil, inSection(err, manifestFileName)
	}
	if err := validateManifestFields(&doc, dirName); err != nil {
		return nil, err
	}
	return &doc, nil
}

func validateManifestFields(doc *manifestDoc, dirName string) error {
	if doc.ManifestVersion != 1 {
		return newPackError(reasonManifestVersion, "manifest_version 只能是 1，当前是 %d", doc.ManifestVersion)
	}
	if !packIDPattern.MatchString(doc.ID) {
		return newPackError(reasonIDInvalid, "id %q 不符合 ^[a-z0-9][a-z0-9_-]{0,63}$", doc.ID)
	}
	if doc.ID != dirName {
		return newPackError(reasonIDDirMismatch, "id %q 与所在目录名 %q 不一致", doc.ID, dirName)
	}
	if !versionPattern.MatchString(doc.Version) {
		return newPackError(reasonVersionInvalid, "version %q 不符合 x.y.z 三段数字格式", doc.Version)
	}
	if err := validateLocalized("name", doc.Name, maxNameChars); err != nil {
		return err
	}
	if doc.Description != nil {
		if err := validateLocalized("description", doc.Description, maxDescriptionChars); err != nil {
			return err
		}
	}
	if len([]rune(doc.Author)) > maxAuthorChars {
		return newPackError(reasonLimitExceeded, "author 超过 %d 字符上限", maxAuthorChars)
	}
	if !kinds[doc.Kind] {
		return newPackError(reasonKindInvalid, "kind %q 不是 source|shader|theme|script", doc.Kind)
	}
	perms, err := normalizePermissions(doc.Permissions)
	if err != nil {
		return err
	}
	doc.Permissions = perms
	return nil
}

// normalizePermissions 校验 manifest.permissions（§2.1）并归一成固定顺序：
// 未知名字、重复项都整包判 invalid——授权闸门最怕的是「写错了名字，于是这条
// 能力静默地没被声明」，那样包会以为自己有权限，而运行时永远不会问用户。
// 顺序固定成 permissionNames 的顺序，界面上的条目才不会随作者手写顺序跳动。
func normalizePermissions(names []string) ([]string, error) {
	if len(names) == 0 {
		return nil, nil
	}
	seen := make(map[string]bool, len(names))
	for _, name := range names {
		if !packPermissions[name] {
			return nil, newPackError(reasonPermissionInvalid,
				"permissions 里的 %q 不认识，可用：%s", name, strings.Join(permissionNames, "、"))
		}
		if seen[name] {
			return nil, newPackError(reasonPermissionInvalid, "permissions 里的 %q 重复声明", name)
		}
		seen[name] = true
	}
	out := make([]string, 0, len(names))
	for _, name := range permissionNames {
		if seen[name] {
			out = append(out, name)
		}
	}
	return out, nil
}

// validateLocalized 实现 §2 对 name / description 的规则：zh-CN 与 en 至少给一个，
// 每个语言的取值不超过 cap 字符。缺语言时界面自行回落另一个。
func validateLocalized(field string, values map[string]string, cap int) error {
	if len(values) == 0 {
		return newPackError(reasonNameMissing, "%s 至少要给 zh-CN 或 en 一个", field)
	}
	if strings.TrimSpace(values["zh-CN"]) == "" && strings.TrimSpace(values["en"]) == "" {
		return newPackError(reasonNameMissing, "%s 里 zh-CN 与 en 都为空，至少要给一个", field)
	}
	for lang, value := range values {
		if len([]rune(value)) > cap {
			return newPackError(reasonLimitExceeded, "%s.%s 超过 %d 字符上限", field, lang, cap)
		}
	}
	return nil
}
