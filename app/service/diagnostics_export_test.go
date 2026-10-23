package service

import (
	"archive/zip"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiagnosticsBundleIncludesTimelineAndProfiles(t *testing.T) {
	root := t.TempDir()
	report, err := writeDiagnosticsReport(root, map[string]any{"format_version": 1})
	if err != nil {
		t.Fatal(err)
	}
	timeline := filepath.Join(root, "recording.jsonl")
	if err := os.WriteFile(timeline, []byte("{\"type\":\"sample\"}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	bundle, err := writeDiagnosticsBundle(report.Path, []string{report.Path, report.HeapProfilePath, report.GoroutineProfilePath, timeline, "", filepath.Join(root, "missing-end-profile")})
	if err != nil {
		t.Fatal(err)
	}
	archive, err := zip.OpenReader(bundle)
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	if len(archive.File) != 4 {
		t.Fatalf("lost diagnostic inputs: %v", archive.File)
	}
	for _, file := range archive.File {
		if filepath.Base(file.Name) != file.Name || file.UncompressedSize64 == 0 {
			t.Fatalf("invalid bundle entry: %+v", file)
		}
	}
}

func TestDiagnosticsExportWritesUsableLocalProfiles(t *testing.T) {
	result, err := writeDiagnosticsReport(t.TempDir(), map[string]any{"format_version": 1, "fixture": "memory audit"})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(result.Path)
	if err != nil || !json.Valid(data) {
		t.Fatalf("unreadable report: %v", err)
	}
	heap, err := os.ReadFile(result.HeapProfilePath)
	if err != nil || len(heap) < 2 || heap[0] != 0x1f || heap[1] != 0x8b {
		t.Fatalf("heap profile is not gzip pprof data: %v", err)
	}
	stacks, err := os.ReadFile(result.GoroutineProfilePath)
	if err != nil || !strings.Contains(string(stacks), "goroutine") {
		t.Fatalf("missing goroutine stacks: %v", err)
	}
}

func TestDiagnosticsExportRejectsMalformedOrUnboundedFrontendData(t *testing.T) {
	app := &App{}
	for _, value := range []string{"", "not JSON", strings.Repeat(" ", 2<<20) + "{}"} {
		if _, err := app.ExportDiagnosticsReport(value); err == nil {
			t.Fatal("invalid payload accepted")
		}
	}
}
