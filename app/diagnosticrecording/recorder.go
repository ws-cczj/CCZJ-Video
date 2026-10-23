// Package diagnosticrecording persists opt-in resource sessions independently of the UI.
package diagnosticrecording

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"runtime"
	"runtime/pprof"
	"strings"
	"sync"
	"time"

	"cczjVideo/app/lifecycle"
	"cczjVideo/app/resource"
)

const maxMarkers = 10000

// ErrInvalidMarker distinguishes malformed observations from storage failures.
var ErrInvalidMarker = errors.New("诊断事件格式错误")

// Status describes the current or most recently completed session. Files survive exit.
type Status struct {
	Mode                string `json:"mode"`
	Active              bool   `json:"active"`
	Path                string `json:"path"`
	StartedAtMS         int64  `json:"started_at_ms"`
	EndedAtMS           int64  `json:"ended_at_ms"`
	IntervalSeconds     int    `json:"interval_seconds"`
	MaxDurationSeconds  int    `json:"max_duration_seconds"`
	Samples             int    `json:"samples"`
	Markers             int    `json:"markers"`
	Reason              string `json:"reason"`
	Error               string `json:"error"`
	StartHeapPath       string `json:"start_heap_path"`
	EndHeapPath         string `json:"end_heap_path"`
	StartGoroutinesPath string `json:"start_goroutines_path"`
	EndGoroutinesPath   string `json:"end_goroutines_path"`
}

// Recorder holds one writer and sampler, never the accumulated timeline.
type Recorder struct {
	mu                 sync.Mutex
	root               func() string
	group              *lifecycle.Group
	status             Status
	file               *os.File
	encoder            *json.Encoder
	cancel             context.CancelFunc
	done               chan struct{}
	sampler            *resource.Sampler
	interval, duration time.Duration
	bytes              int64
	maxBytes           int64
}

// New creates a dormant recorder. Start explicitly enables five-second sampling, at most six hours.
func New(root func() string, group *lifecycle.Group) *Recorder {
	return &Recorder{root: root, group: group, interval: 5 * time.Second, duration: 6 * time.Hour, maxBytes: 32 << 20}
}

// Status returns a detached, small snapshot.
func (r *Recorder) Status() Status { r.mu.Lock(); defer r.mu.Unlock(); return r.status }

// Start creates a new session, or returns the current session if already recording.
func (r *Recorder) Start() (Status, error) { return r.StartMode("") }

func (r *Recorder) StartMode(mode string) (Status, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.status.Active {
		return r.status, nil
	}
	if r.group.Context().Err() != nil {
		return r.status, errors.New("应用正在退出，不能开始诊断记录")
	}
	root := r.root()
	if err := os.MkdirAll(root, 0755); err != nil {
		return r.status, err
	}
	file, err := os.CreateTemp(root, "recording-"+time.Now().Format("20060102-150405")+"-*.jsonl")
	if err != nil {
		return r.status, err
	}
	base := strings.TrimSuffix(file.Name(), ".jsonl")
	r.status = Status{Mode: mode, Active: true, Path: file.Name(), StartedAtMS: time.Now().UnixMilli(), IntervalSeconds: int(r.interval / time.Second), MaxDurationSeconds: int(r.duration / time.Second)}
	r.file, r.encoder, r.sampler, r.bytes = file, json.NewEncoder(file), &resource.Sampler{}, 0
	ctx, cancel := context.WithCancel(r.group.Context())
	r.cancel, r.done = cancel, make(chan struct{})
	if err = r.writeLocked(map[string]any{"type": "start", "at_ms": r.status.StartedAtMS, "format_version": 1, "pid": os.Getpid(), "go_version": runtime.Version(), "status": r.status, "resource_scope": "shared_host_process"}); err == nil {
		r.status.StartHeapPath, r.status.StartGoroutinesPath, err = writeProfiles(base + "-start")
	}
	if err == nil {
		err = r.sampleLocked()
	}
	if err != nil {
		r.finishLocked("error", err)
		return r.status, err
	}
	if !r.group.Go("diagnostics-recording", func(context.Context) { r.run(ctx) }) {
		err = errors.New("应用正在退出，不能开始诊断记录")
		r.finishLocked("shutdown", err)
		return r.status, err
	}
	return r.status, nil
}

// Stop cancels and waits for the final sample, end profiles and closed timeline.
func (r *Recorder) Stop(ctx context.Context) (Status, error) { return r.StopReason(ctx, "manual") }

func (r *Recorder) StopReason(ctx context.Context, reason string) (Status, error) {
	r.mu.Lock()
	if !r.status.Active {
		status := r.status
		r.mu.Unlock()
		return status, nil
	}
	r.status.Reason = reason
	r.cancel()
	done := r.done
	r.mu.Unlock()
	select {
	case <-done:
		status := r.Status()
		if status.Error != "" {
			return status, errors.New(status.Error)
		}
		return status, nil
	case <-ctx.Done():
		return r.Status(), ctx.Err()
	}
}

// Mark adds a bounded playback/window event. An ended session returns false without an error.
// Resource sampling remains in Go even if the WebView throttles its own event delivery.
func (r *Recorder) Mark(kind, payload string) (bool, error) {
	if len(payload) > 8192 || !json.Valid([]byte(payload)) {
		return false, fmt.Errorf("%w：必须是有效 JSON，且不超过 8 KB", ErrInvalidMarker)
	}
	switch kind {
	case "playback_load", "first_frame", "stall", "error", "media", "playback_snapshot", "recording_start", "recording_stop", "window_state", "player_closing", "player_closed",
		"playback_page_closed", "player_media_load", "player_issue", "preview_started", "preview_stopped", "startup_snapshot", "activity_snapshot":
	default:
		return false, fmt.Errorf("%w：未知事件类型", ErrInvalidMarker)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.status.Active || r.status.Reason != "" {
		return false, nil
	}
	if r.bytes >= r.maxBytes {
		r.status.Reason = "size_limit"
		r.cancel()
		return false, nil
	}
	if r.status.Markers >= maxMarkers {
		r.status.Reason = "marker_limit"
		r.cancel()
		return false, nil
	}
	err := r.writeLocked(map[string]any{"type": "event", "at_ms": time.Now().UnixMilli(), "kind": kind, "data": json.RawMessage(payload)})
	if err != nil {
		r.status.Error = err.Error()
		r.status.Reason = "error"
		r.cancel()
		return false, err
	}
	r.status.Markers++
	return true, nil
}

func (r *Recorder) run(ctx context.Context) {
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	deadline := time.NewTimer(r.duration)
	defer deadline.Stop()
	for {
		reason := ""
		select {
		case <-ctx.Done():
			reason = "shutdown"
		case <-deadline.C:
			reason = "duration_limit"
		case <-ticker.C:
		}
		r.mu.Lock()
		if r.status.Reason != "" {
			reason = r.status.Reason
		}
		if reason == "" && r.bytes >= r.maxBytes {
			reason = "size_limit"
		}
		if reason != "" {
			r.finishLocked(reason, nil)
			r.mu.Unlock()
			return
		}
		if err := r.sampleLocked(); err != nil {
			r.finishLocked("error", err)
			r.mu.Unlock()
			return
		}
		r.mu.Unlock()
	}
}

func (r *Recorder) sampleLocked() error {
	err := r.writeLocked(map[string]any{"type": "sample", "resources": r.sampler.Sample()})
	if err == nil {
		r.status.Samples++
	}
	return err
}

func (r *Recorder) writeLocked(value any) error {
	// Direct writes make complete lines readable after a process crash; no growing buffer.
	if err := r.encoder.Encode(value); err != nil {
		return err
	}
	position, err := r.file.Seek(0, 1)
	if err == nil {
		r.bytes = position
	}
	return err
}

func (r *Recorder) finishLocked(reason string, failure error) {
	r.cancel()
	r.status.Reason = reason
	if failure == nil && r.status.Error == "" {
		failure = r.sampleLocked()
	}
	base := strings.TrimSuffix(r.status.Path, ".jsonl")
	if failure == nil && r.status.Error == "" {
		r.status.EndHeapPath, r.status.EndGoroutinesPath, failure = writeProfiles(base + "-end")
	}
	r.status.Active, r.status.EndedAtMS = false, time.Now().UnixMilli()
	if failure != nil {
		r.status.Error = failure.Error()
		r.status.Reason = "error"
	}
	if err := r.writeLocked(map[string]any{"type": "end", "at_ms": r.status.EndedAtMS, "status": r.status, "resource_scope": "shared_host_process"}); err != nil {
		r.status.Error = err.Error()
		r.status.Reason = "error"
	}
	if err := r.file.Sync(); err != nil {
		r.status.Error = err.Error()
		r.status.Reason = "error"
	}
	if err := r.file.Close(); err != nil {
		r.status.Error = err.Error()
		r.status.Reason = "error"
	}
	r.file, r.encoder, r.sampler = nil, nil, nil
	close(r.done)
}

func writeProfiles(base string) (heap, goroutines string, failure error) {
	heap, goroutines = base+"-heap.pprof", base+"-goroutines.txt"
	for _, item := range []struct {
		name, path string
		debug      int
	}{{"heap", heap, 0}, {"goroutine", goroutines, 2}} {
		out, err := os.Create(item.path)
		if err != nil {
			return heap, goroutines, err
		}
		err = pprof.Lookup(item.name).WriteTo(out, item.debug)
		closeErr := out.Close()
		if err == nil {
			err = closeErr
		}
		if err != nil {
			return heap, goroutines, fmt.Errorf("写入 %s 失败: %w", item.name, err)
		}
	}
	return heap, goroutines, nil
}
