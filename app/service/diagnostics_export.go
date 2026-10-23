package service

import (
	"archive/zip"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime/pprof"
	"time"

	"cczjVideo/app/apperror"
)

// DiagnosticsExport names the report and profiles written by an explicit export.
type DiagnosticsExport struct {
	BundlePath           string `json:"bundle_path"`
	Path                 string `json:"path"`
	HeapProfilePath      string `json:"heap_profile_path"`
	GoroutineProfilePath string `json:"goroutine_profile_path"`
}

// ExportDiagnosticsReport writes a local report, allocation profile and goroutine
// stacks. It does not force GC, change settings, or transmit diagnostic data.
func (a *App) ExportDiagnosticsReport(frontendJSON string) (*DiagnosticsExport, error) {
	if len(frontendJSON) > 2<<20 || !json.Valid([]byte(frontendJSON)) {
		return nil, apperror.New(apperror.Validation, "前端诊断快照不是有效 JSON 或超过 2 MB")
	}
	a.modeSwitchMu.RLock()
	defer a.modeSwitchMu.RUnlock()
	var envelope struct {
		Mode string `json:"application_mode"`
	}
	if json.Unmarshal([]byte(frontendJSON), &envelope) == nil && envelope.Mode != "" && envelope.Mode != a.currentMode() {
		return nil, apperror.New(apperror.Conflict, "模式已经切换，请重新打开诊断")
	}
	diagnostics, err := a.diagnostics()
	if err != nil {
		return nil, err
	}
	metrics := a.runtimeMetrics()
	report := map[string]any{
		"format_version": 1, "captured_at": time.Now().Format(time.RFC3339), "diagnostics": diagnostics,
		"runtime": metrics, "frontend": json.RawMessage(frontendJSON),
		"profile_scope": "shared_host_process",
		"logs_scope":    "untagged_host_logs_omitted",
	}
	result, err := writeDiagnosticsReport(filepath.Join(a.getDataDir(), "exports", "diagnostics"), report)
	if err != nil {
		return nil, err
	}
	// Bundle only completed recordings: an active writer must not be copied as if final.
	status := metrics.Recording
	if status.Path != "" && !status.Active {
		paths := []string{result.Path, result.HeapProfilePath, result.GoroutineProfilePath, status.Path, status.StartHeapPath, status.StartGoroutinesPath, status.EndHeapPath, status.EndGoroutinesPath}
		result.BundlePath, err = writeDiagnosticsBundle(result.Path, paths)
		if err != nil {
			return nil, apperror.Wrap(apperror.Storage, err, "打包诊断记录失败，原始文件仍已保存")
		}
	}
	return result, nil
}

func writeDiagnosticsBundle(reportPath string, paths []string) (string, error) {
	out, err := os.CreateTemp(filepath.Dir(reportPath), "diagnostics-session-*.zip")
	if err != nil {
		return "", err
	}
	archive := zip.NewWriter(out)
	for _, path := range paths {
		if path == "" {
			continue
		}
		input, openErr := os.Open(path)
		if os.IsNotExist(openErr) {
			continue
		} // A failed recording may have only its partial timeline.
		if openErr != nil {
			err = openErr
			break
		}
		entry, writeErr := archive.Create(filepath.Base(path))
		if writeErr == nil {
			_, writeErr = io.Copy(entry, input)
		}
		closeErr := input.Close()
		if writeErr == nil {
			writeErr = closeErr
		}
		if writeErr != nil {
			err = writeErr
			break
		}
	}
	if closeErr := archive.Close(); err == nil {
		err = closeErr
	}
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		// An incomplete zip cannot be analysed reliably; keep its source files instead.
		if removeErr := os.Remove(out.Name()); removeErr != nil {
			return "", removeErr
		}
		return "", err
	}
	return out.Name(), nil
}

func writeDiagnosticsReport(root string, report any) (*DiagnosticsExport, error) {
	if err := os.MkdirAll(root, 0755); err != nil {
		return nil, apperror.Wrap(apperror.Storage, err, "创建诊断报告目录失败")
	}
	file, err := os.CreateTemp(root, "diagnostics-"+time.Now().Format("20060102-150405")+"-*.json")
	if err != nil {
		return nil, apperror.Wrap(apperror.Storage, err, "创建诊断报告失败")
	}
	path := file.Name()
	err = json.NewEncoder(file).Encode(report)
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(path)
		return nil, apperror.Wrap(apperror.Storage, err, "写入诊断报告失败")
	}
	base := path[:len(path)-len(".json")]
	result := &DiagnosticsExport{Path: path, HeapProfilePath: base + "-heap.pprof", GoroutineProfilePath: base + "-goroutines.txt"}
	for _, item := range []struct {
		name, path string
		debug      int
	}{{"heap", result.HeapProfilePath, 0}, {"goroutine", result.GoroutineProfilePath, 2}} {
		out, err := os.Create(item.path)
		if err != nil {
			return nil, apperror.Wrap(apperror.Storage, err, "创建性能剖析文件失败")
		}
		err = pprof.Lookup(item.name).WriteTo(out, item.debug)
		closeErr := out.Close()
		if err == nil {
			err = closeErr
		}
		if err != nil {
			_ = os.Remove(item.path)
			return nil, apperror.Wrap(apperror.Storage, err, "写入性能剖析文件失败")
		}
	}
	return result, nil
}
