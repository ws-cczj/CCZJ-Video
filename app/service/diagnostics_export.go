package service

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"cczjVideo/app/apperror"
	"cczjVideo/app/applog"
	"cczjVideo/app/diagnosticrecording"
)

// DiagnosticsExport names the report and profiles written by an explicit export.
type DiagnosticsExport struct {
	BundlePath           string `json:"bundle_path"`
	Path                 string `json:"path"`
	HeapProfilePath      string `json:"heap_profile_path"`
	GoroutineProfilePath string `json:"goroutine_profile_path"`
	SummaryPath          string `json:"summary_path"`
}

// ExportDiagnosticsReport packages bounded local evidence without enabling profiling.
func (a *App) ExportDiagnosticsReport(frontendJSON string) (*DiagnosticsExport, error) {
	if len(frontendJSON) > 2<<20 || !json.Valid([]byte(frontendJSON)) {
		return nil, apperror.New(apperror.Validation, "前端诊断快照不是有效 JSON 或超过 2 MB")
	}
	a.modeSwitchMu.RLock()
	defer a.modeSwitchMu.RUnlock()
	if a.recording == nil {
		return nil, apperror.New(apperror.Internal, "诊断记录器尚未初始化")
	}
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
	logs := applog.Default().Snapshot()
	available := len(logs)
	bytes, first := 0, len(logs)
	for first > 0 {
		size := logRecordBytes(logs[first-1])
		if bytes+size > 8<<20 {
			break
		}
		bytes += size
		first--
	}
	logs = logs[first:]
	report := map[string]any{
		"format_version": 2, "captured_at": time.Now().Format(time.RFC3339), "diagnostics": diagnostics,
		"build": buildIdentity(), "runtime": metrics, "frontend": json.RawMessage(frontendJSON),
		"profile_scope":          "shared_host_process",
		"logs_scope":             "shared_host_process_recent_ring",
		"logs":                   diagnosticLogMetadata(logs),
		"logs_truncated":         first > 0,
		"logs_records_available": available,
		"logs_health":            applog.Default().Health(),
		"logs_capacity":          applog.Default().RingCap(),
	}
	session := a.recording.Session(a.currentMode(), 0)
	report["session"] = session
	report["capabilities"] = map[string]any{
		"timeline_memory_bytes": diagnosticrecording.MaxTimelineBytes, "timeline_max_events": diagnosticrecording.MaxTimelineEvents,
		"event_max_bytes": diagnosticrecording.MaxEventBytes, "writer_queue_events": 128,
		"correlation": session.CorrelationScope, "clock": "host_received_at_ms;frontend_client_at_ms_is_separate",
		"omitted":                   []string{"free_text_logs", "urls", "request_arguments", "content", "settings_values"},
		"recording_active_snapshot": session.Recording.Active, "upload": "manual_local_export_only",
	}
	result, err := writeDiagnosticsReport(filepath.Join(a.getDataDir(), "exports", "diagnostics"), diagnosticMetadata(report, ""))
	if err != nil {
		return nil, err
	}
	result.SummaryPath = strings.TrimSuffix(result.Path, ".json") + "-summary.txt"
	summary := fmt.Sprintf("Diagnostic evidence v2\nMode: %s\nWindow: %d events, %d serialized bytes\nRetention evictions: %d; rejected: %d; recording writer drops: %d\nRecording active at export: %t\nCorrelation: %s\nFrontend events use a separate document clock; host timestamps indicate arrival.\nFindings are observed event counts, not unique incidents or root causes.\nRoutine collection does not retain free text, content, URLs or request arguments.\nThe final frontend batch can be absent after abrupt process termination.\n", a.currentMode(), len(session.Events), session.Bytes, session.Evicted, session.Rejected, session.WriterDropped, session.Recording.Active, session.CorrelationScope)
	for _, finding := range session.Findings {
		summary += fmt.Sprintf("%s: %d; evidence: %s\n", finding.Code, finding.Count, strings.Join(finding.Evidence, ", "))
	}
	if err = os.WriteFile(result.SummaryPath, []byte(summary), 0600); err != nil {
		return nil, apperror.Wrap(apperror.Storage, err, "写入诊断摘要失败")
	}
	paths := []string{result.Path, result.SummaryPath}
	status := session.Recording
	if status.Path != "" && !status.Active {
		paths = append(paths, status.Path)
	}
	paths = append(paths, session.ProfileFiles...)
	// Native startup metadata exists before Go logging and remains bounded by its owner.
	if entries, readErr := os.ReadDir(filepath.Join(a.getDataDir(), "applog")); readErr == nil {
		count := 0
		pattern := regexp.MustCompile(`^(?:startup-native|cast-native)-[0-9]+-[0-9]+\.jsonl$`)
		for i := len(entries) - 1; i >= 0 && count < 8; i-- {
			entry := entries[i]
			if !pattern.MatchString(entry.Name()) {
				continue
			}
			info, statErr := entry.Info()
			if statErr == nil && info.Mode().IsRegular() && info.Size() <= 32<<10 {
				paths = append(paths, filepath.Join(a.getDataDir(), "applog", entry.Name()))
				count++
			}
		}
	}
	result.BundlePath, err = writeDiagnosticsBundle(result.Path, paths)
	if err != nil {
		return nil, apperror.Wrap(apperror.Storage, err, "打包诊断记录失败，原始文件仍已保存")
	}

	return result, nil
}

func writeDiagnosticsBundle(reportPath string, paths []string) (string, error) {
	out, err := os.CreateTemp(filepath.Dir(reportPath), "diagnostics-session-*.zip")
	if err != nil {
		return "", err
	}
	archive := zip.NewWriter(out)
	var total int64
	for _, path := range paths {
		if path == "" {
			continue
		}
		input, openErr := os.Open(path)
		if openErr != nil {
			err = openErr
			break
		}
		info, statErr := input.Stat()
		if statErr != nil {
			_ = input.Close()
			err = statErr
			break
		}
		total += info.Size()
		if info.Size() > 40<<20 || total > 64<<20 {
			_ = input.Close()
			err = errors.New("诊断包超出容量限制")
			break
		}
		entry, writeErr := archive.Create(filepath.Base(path))
		if writeErr == nil {
			_, writeErr = io.Copy(entry, io.LimitReader(input, info.Size()))
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
	return &DiagnosticsExport{Path: path}, nil
}

var diagnosticLabel = regexp.MustCompile(`^[A-Za-z0-9_.:/; -]{0,180}$`)
var diagnosticLogField = regexp.MustCompile(`(request_id|parent_request_id|operation|pipeline|code|outcome|duration_ms|mode|stage|cast_session_id|cast_device_id|transport_state|transport_status|relay_health|confirmation_state|position_state|resource_kind|method|phase|reason|seek_ready|has_position|position_seconds|duration_seconds|target_seconds|http_status|upstream_status|bytes|elapsed_ms|failed|has_range|requests|failed_requests|recovered_requests|attempt|transport|build_revision|build_modified|build_identity_source|receiver_mismatch|resource_revoked|concurrency_limit|observation_scope|sampled|hls_master|hls_encrypted|hls_init_map|hls_separate_audio|hls_endlist|hls_version|codec_avc|codec_hevc|codec_av1|codec_aac|codec_ac3)="([A-Za-z0-9_.:-]{0,120})"`)

func diagnosticLogMetadata(logs []applog.Record) []map[string]any {
	rows := make([]map[string]any, 0, len(logs))
	for _, record := range logs {
		row := map[string]any{"seq": record.Seq, "ts": record.TS, "level": record.Level, "persisted": record.Persisted}
		for _, match := range diagnosticLogField.FindAllStringSubmatch(record.Message, 64) {
			key, value := match[1], match[2]
			switch key {
			case "sampled", "hls_master", "hls_encrypted", "hls_init_map", "hls_separate_audio", "hls_endlist", "codec_avc", "codec_hevc", "codec_av1", "codec_aac", "codec_ac3", "seek_ready", "has_position", "failed", "has_range", "build_modified":
				if flag, err := strconv.ParseBool(value); err == nil {
					row[key] = flag
				}
			case "hls_version", "duration_ms", "position_seconds", "duration_seconds", "target_seconds", "http_status", "upstream_status", "bytes", "elapsed_ms", "requests", "failed_requests", "recovered_requests", "attempt", "receiver_mismatch", "resource_revoked", "concurrency_limit":
				if number, err := strconv.ParseFloat(value, 64); err == nil {
					row[key] = number
				}
			default:
				row[key] = value
			}
		}
		rows = append(rows, row)
	}
	return rows
}

// Strings are exported only for documented metadata fields; arbitrary text is omitted.
func diagnosticMetadata(value any, key string) any {
	data, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var decoded any
	if json.Unmarshal(data, &decoded) != nil {
		return nil
	}
	return filterDiagnosticMetadata(decoded, key)
}
func filterDiagnosticMetadata(value any, key string) any {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, item := range v {
			if cleaned := filterDiagnosticMetadata(item, k); cleaned != nil {
				out[k] = cleaned
			}
		}
		return out
	case []any:
		out := make([]any, 0, len(v))
		for _, item := range v {
			if cleaned := filterDiagnosticMetadata(item, key); cleaned != nil {
				out = append(out, cleaned)
			}
		}
		return out
	case string:
		switch key {
		case "application_mode", "mode", "kind", "name", "code", "outcome", "route", "id", "resource_id", "session_id", "action_id", "request_id", "cast_session_id", "cast_device_id", "parent_request_id", "stage", "transport_state", "transport_status", "relay_health", "confirmation_state", "position_state", "resource_kind", "observation_scope", "method", "transport", "build_revision", "build_identity_source", "flight_id", "backend_request_id", "parent_id", "pipeline", "operation", "level", "source", "reason", "scope", "resource_scope", "native_scope", "profile_scope", "logs_scope", "captured_at", "ts", "go_version", "app_version", "version", "os", "arch", "phase", "certainty", "evidence", "correlation_scope", "correlation", "clock", "upload", "omitted":
			if diagnosticLabel.MatchString(v) && !strings.Contains(v, "://") {
				return v
			}
		}
		return nil
	default:
		return value
	}
}
