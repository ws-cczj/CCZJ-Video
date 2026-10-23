package plugin

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuiltinDigestMatchesExecutableBytes(t *testing.T) {
	s, _, _ := testService(t)
	if err := s.SeedBuiltin(); err != nil {
		t.Fatal(err)
	}
	infos, err := s.Scan()
	if err != nil {
		t.Fatal(err)
	}
	for _, info := range infos {
		if info.Script == nil {
			continue
		}
		parts := []string{}
		for _, name := range append(append([]string{manifestFileName}, info.Script.Styles...), info.Script.Entry) {
			text, readErr := s.ReadFile(info.ID, name)
			if readErr != nil {
				t.Fatal(readErr)
			}
			parts = append(parts, text)
		}
		hash := sha256.New()
		for _, part := range parts {
			fmt.Fprintf(hash, "%d:", len([]byte(part)))
			hash.Write([]byte(part))
		}
		if info.BuiltinDigest != hex.EncodeToString(hash.Sum(nil)) {
			t.Fatalf("%s: builtin trust must describe exact executable bytes", info.ID)
		}
	}
}

func TestBuiltinDigestDoesNotTrustModifiedOrRenamedCode(t *testing.T) {
	s, _, _ := testService(t)
	if err := s.SeedBuiltin(); err != nil {
		t.Fatal(err)
	}
	const id = "logs-panel"
	baseline := embeddedScriptDigest(id)
	if baseline == "" {
		t.Fatal("missing embedded digest")
	}
	if err := os.WriteFile(filepath.Join(s.Directory(), id, "main.js"), []byte("export default () => { throw 'modified' }"), 0600); err != nil {
		t.Fatal(err)
	}
	infos, err := s.Scan()
	if err != nil {
		t.Fatal(err)
	}
	for _, info := range infos {
		if info.ID == id && info.BuiltinDigest != baseline {
			t.Fatal("disk changes must not redefine the trusted embedded digest")
		}
	}
	if embeddedScriptDigest("third-party") != "" {
		t.Fatal("third-party directory must not receive builtin trust")
	}
}

func TestBuiltinDigestSerializesInRegistry(t *testing.T) {
	data, err := json.Marshal(Info{BuiltinDigest: "expected"})
	if err != nil || !strings.Contains(string(data), `"builtin_digest":"expected"`) {
		t.Fatalf("missing frontend trust contract: %s %v", data, err)
	}
}

func TestBuiltinTrustRecognizesShippedLegacyManifestWithoutIcon(t *testing.T) {
	s, root, _ := testService(t)
	if err := s.SeedBuiltin(); err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(oldDiagnosticsManifest), &doc); err != nil {
		t.Fatal(err)
	}
	delete(doc, "icon")
	legacy, _ := json.MarshalIndent(doc, "", "  ")
	if err := os.WriteFile(filepath.Join(root, "diagnostics-panel", manifestFileName), legacy, 0644); err != nil {
		t.Fatal(err)
	}
	infos, err := s.Scan()
	if err != nil {
		t.Fatal(err)
	}
	for _, info := range infos {
		if info.ID != "diagnostics-panel" {
			continue
		}
		entry, err := s.ReadFile(info.ID, info.Script.Entry)
		if err != nil {
			t.Fatal(err)
		}
		hash := sha256.New()
		for _, part := range []string{string(legacy), entry} {
			fmt.Fprintf(hash, "%d:", len([]byte(part)))
			hash.Write([]byte(part))
		}
		if info.BuiltinDigest != hex.EncodeToString(hash.Sum(nil)) {
			t.Fatal("shipped iconless 1.0.0 pack would prompt for trust on startup")
		}
		return
	}
	t.Fatal("legacy diagnostics missing from registry")
}

func TestBuiltinTrustRecognizesPreIconLogsAndMotion(t *testing.T) {
	for _, id := range []string{"logs-panel", "motion-effects"} {
		t.Run(id, func(t *testing.T) {
			s, root, _ := testService(t)
			if err := s.SeedBuiltin(); err != nil {
				t.Fatal(err)
			}
			manifest := filepath.Join(root, id, manifestFileName)
			data, err := os.ReadFile(manifest)
			if err != nil {
				t.Fatal(err)
			}
			var doc map[string]any
			if err := json.Unmarshal(data, &doc); err != nil {
				t.Fatal(err)
			}
			delete(doc, "icon")
			data, _ = json.MarshalIndent(doc, "", "  ")
			if err := os.WriteFile(manifest, data, 0644); err != nil {
				t.Fatal(err)
			}
			parts, ok := shippedBuiltinParts(id, filepath.Join(root, id))
			if !ok || verifiedBuiltinDigest(id, filepath.Join(root, id)) != framedDigest(parts) {
				t.Fatal("shipped pre-icon pack not recognized")
			}
		})
	}
}
