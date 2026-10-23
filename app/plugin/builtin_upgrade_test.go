package plugin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const oldDiagnosticsManifest = `{
  "manifest_version": 1,
  "id": "diagnostics-panel",
  "version": "1.0.0",
  "name": { "zh-CN": "诊断面板", "en": "Diagnostics Panel" },
  "description": {
    "zh-CN": "设置页顶部的「诊断」分组：采集源健康度、缓存体积、豆瓣队列这些运行数据。删掉这个文件夹，那一项就没了。",
    "en": "The Diagnostics group on the Settings page: source health, cache size, Douban queue. Delete the folder and the tab goes away."
  },
  "author": "CCZJ Video",
  "icon": "icon.png",
  "kind": "script",
  "script": { "entry": "main.js" }
}`

func TestSeedUpgradesUnmodifiedDiagnosticsAndPreservesCustomPacks(t *testing.T) {
	for _, custom := range []bool{false, true} {
		t.Run(map[bool]string{false: "shipped", true: "modified entry"}[custom], func(t *testing.T) {
			s, root, _ := testService(t)
			if err := s.SeedBuiltin(); err != nil {
				t.Fatal(err)
			}
			manifest := filepath.Join(root, "diagnostics-panel", manifestFileName)
			if err := os.WriteFile(manifest, []byte(oldDiagnosticsManifest), 0644); err != nil {
				t.Fatal(err)
			}
			if custom {
				if err := os.WriteFile(filepath.Join(root, "diagnostics-panel", "main.js"), []byte("// custom diagnostics"), 0644); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(root, builtinMarkerName), []byte("3\ndiagnostics-panel\nlogs-panel\nmotion-effects\n"), 0644); err != nil {
				t.Fatal(err)
			}
			if err := s.SeedBuiltin(); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(manifest)
			if err != nil {
				t.Fatal(err)
			}
			if custom && string(data) != oldDiagnosticsManifest {
				t.Fatal("overwrote customized pack")
			}
			if !custom && !strings.Contains(string(data), `"version": "1.2.0"`) {
				t.Fatal("unchanged old diagnostic package was not updated")
			}
		})
	}
}

func TestSeedUpgradesIconlessDiagnosticsAndPreservesManifestChanges(t *testing.T) {
	for _, changed := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy without icon", true: "changed permissions"}[changed], func(t *testing.T) {
			s, root, _ := testService(t)
			if err := s.SeedBuiltin(); err != nil {
				t.Fatal(err)
			}
			var doc map[string]any
			if err := json.Unmarshal([]byte(oldDiagnosticsManifest), &doc); err != nil {
				t.Fatal(err)
			}
			delete(doc, "icon")
			if changed {
				doc["permissions"] = []string{"write"}
			}
			legacy, _ := json.MarshalIndent(doc, "", "  ")
			manifest := filepath.Join(root, "diagnostics-panel", manifestFileName)
			if err := os.WriteFile(manifest, legacy, 0644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, builtinMarkerName), []byte("4\ndiagnostics-panel\nlogs-panel\nmotion-effects\n"), 0644); err != nil {
				t.Fatal(err)
			}
			if err := s.SeedBuiltin(); err != nil {
				t.Fatal(err)
			}
			actual, err := os.ReadFile(manifest)
			if err != nil {
				t.Fatal(err)
			}
			if changed && string(actual) != string(legacy) {
				t.Fatal("overwrote customized manifest")
			}
			if !changed && !strings.Contains(string(actual), `"version": "1.2.0"`) {
				t.Fatal("iconless legacy pack not upgraded")
			}
			infos, err := s.Scan()
			if err != nil {
				t.Fatal(err)
			}
			for _, info := range infos {
				if info.ID != "diagnostics-panel" {
					continue
				}
				entry, err := s.ReadFile(info.ID, "main.js")
				if err != nil {
					t.Fatal(err)
				}
				matches := info.BuiltinDigest == framedDigest([]string{string(actual), entry})
				if matches == changed {
					t.Fatal("builtin trust must match only shipped manifests")
				}
			}
		})
	}
}
