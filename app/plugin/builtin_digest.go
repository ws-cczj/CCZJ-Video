package plugin

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"reflect"
	"strings"
)

// UTF-8 byte-length prefixes match plugins/scriptTrust.ts and keep file
// boundaries unambiguous even when a file contains a NUL byte.
func embeddedScriptDigest(id string) string {
	if !isBuiltinPackID(id) {
		return ""
	}
	root := path.Join(builtinSourceRoot, id)
	manifest, err := fs.ReadFile(builtinFS, path.Join(root, manifestFileName))
	if err != nil {
		return ""
	}
	var doc manifestDoc
	if json.Unmarshal(manifest, &doc) != nil {
		return ""
	}
	var script ScriptSection
	if json.Unmarshal(doc.Script, &script) != nil || script.Entry == "" {
		return ""
	}
	parts := []string{string(manifest)}
	for _, name := range append(append([]string(nil), script.Styles...), script.Entry) {
		data, readErr := fs.ReadFile(builtinFS, path.Join(root, name))
		if readErr != nil {
			return ""
		}
		parts = append(parts, string(data))
	}
	return framedDigest(parts)
}

func framedDigest(parts []string) string {
	hash := sha256.New()
	for _, part := range parts {
		fmt.Fprintf(hash, "%d:", len([]byte(part)))
		hash.Write([]byte(part))
	}
	return hex.EncodeToString(hash.Sum(nil))
}

// Shipped pre-icon manifests and the two previous diagnostics releases remain
// builtins. Compare every manifest field and executable file with our originals
// before describing the exact bytes read by the frontend. An ID alone grants nothing.
func verifiedBuiltinDigest(id, packDir string) string {
	parts, ok := shippedBuiltinParts(id, packDir)
	if !ok {
		return embeddedScriptDigest(id)
	}
	return framedDigest(parts)
}

func normalizedBuiltinText(data []byte) string {
	return strings.TrimSpace(strings.ReplaceAll(string(data), "\r\n", "\n"))
}

func shippedBuiltinParts(id, packDir string) ([]string, bool) {
	if !isBuiltinPackID(id) {
		return nil, false
	}
	read := func(name string) ([]byte, error) {
		file, err := resolveInside(packDir, name)
		if err != nil {
			return nil, err
		}
		return readCapped(file, maxTextReadBytes)
	}
	manifest, err := read(manifestFileName)
	if err != nil {
		return nil, false
	}
	embedded, err := builtinFS.ReadFile(path.Join(builtinSourceRoot, id, manifestFileName))
	if err != nil || !shippedBuiltinManifest(id, manifest, embedded) {
		return nil, false
	}
	var doc manifestDoc
	var script ScriptSection
	if json.Unmarshal(embedded, &doc) != nil || json.Unmarshal(doc.Script, &script) != nil {
		return nil, false
	}
	parts := []string{string(manifest)}
	for _, name := range append(append([]string(nil), script.Styles...), script.Entry) {
		actual, err := read(name)
		if err != nil {
			return nil, false
		}
		original, err := builtinFS.ReadFile(path.Join(builtinSourceRoot, id, name))
		if err != nil || normalizedBuiltinText(actual) != normalizedBuiltinText(original) {
			return nil, false
		}
		parts = append(parts, string(actual))
	}
	return parts, true
}

func shippedBuiltinManifest(id string, actual, original []byte) bool {
	var got, want map[string]any
	if json.Unmarshal(actual, &got) != nil || json.Unmarshal(original, &want) != nil {
		return false
	}
	if _, present := got["icon"]; !present {
		delete(want, "icon")
	}
	if id == "diagnostics-panel" {
		legacy := map[string]map[string]string{
			"1.0.0": {
				"zh-CN": "设置页顶部的「诊断」分组：采集源健康度、缓存体积、豆瓣队列这些运行数据。删掉这个文件夹，那一项就没了。",
				"en":    "The Diagnostics group on the Settings page: source health, cache size, Douban queue. Delete the folder and the tab goes away.",
			},
			"1.1.0": {
				"zh-CN": "运行资源监测：应用与 WebView 内存、CPU、句柄、趋势及诊断报告导出；保留采集源健康、播放、网络、缓存与数据库信息。需要支持资源监测的应用构建。",
				"en":    "App and WebView memory, CPU, handles, trends and diagnostic report export, plus source health, playback, network, cache and database details. Requires an app build with resource monitoring.",
			},
		}
		version, _ := got["version"].(string)
		if description, ok := legacy[version]; ok {
			encoded, _ := json.Marshal(description)
			var expected any
			_ = json.Unmarshal(encoded, &expected)
			if !reflect.DeepEqual(got["description"], expected) {
				return false
			}
			got["description"], got["version"] = want["description"], want["version"]
		}
	}
	return reflect.DeepEqual(got, want)
}
