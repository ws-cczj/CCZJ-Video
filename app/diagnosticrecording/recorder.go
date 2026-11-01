// Package diagnosticrecording persists opt-in resource sessions independently of the UI.
package diagnosticrecording

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"cczjVideo/app/lifecycle"
	"cczjVideo/app/resource"
)

const maxMarkers = 10000

// ErrInvalidMarker distinguishes malformed observations from storage failures.
var ErrInvalidMarker = errors.New("诊断事件格式错误")

// Status describes the current or most recently completed session. Files survive exit.
type Status struct {
	Mode                string        `json:"mode"`
	Active              bool          `json:"active"`
	Path                string        `json:"path"`
	StartedAtMS         int64         `json:"started_at_ms"`
	EndedAtMS           int64         `json:"ended_at_ms"`
	IntervalSeconds     int           `json:"interval_seconds"`
	MaxDurationSeconds  int           `json:"max_duration_seconds"`
	Samples             int           `json:"samples"`
	Markers             int           `json:"markers"`
	Reason              string        `json:"reason"`
	Error               string        `json:"error"`
	StartHeapPath       string        `json:"start_heap_path"`
	EndHeapPath         string        `json:"end_heap_path"`
	StartGoroutinesPath string        `json:"start_goroutines_path"`
	EndGoroutinesPath   string        `json:"end_goroutines_path"`
	WriterDropped       uint64        `json:"writer_dropped"`
	Profile             ProfileStatus `json:"profile"`
}

type recordSink struct {
	mu        sync.Mutex
	accepting bool
	queue     chan json.RawMessage
	dropped   atomic.Uint64
	mode      string
}

func (s *recordSink) offer(wire []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.accepting {
		return
	}
	select {
	case s.queue <- json.RawMessage(wire):
	default:
		s.dropped.Add(1)
	}
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
	timeline           Timeline
	sink               atomic.Pointer[recordSink]
	profileMu          sync.Mutex
	profile            ProfileStatus
	profileCancel      context.CancelFunc
	profileFiles       []ProfileStatus
}

// New enables a bounded memory window. Disk sampling requires an explicit session.
func New(root func() string, group *lifecycle.Group, samplers ...*resource.Sampler) *Recorder {
	r := &Recorder{root: root, group: group, interval: 15 * time.Second, duration: 3 * time.Minute, maxBytes: 32 << 20}
	r.sampler = &resource.Sampler{}
	if len(samplers) > 0 && samplers[0] != nil {
		r.sampler = samplers[0]
	}
	r.timeline.setEnabled(true)
	return r
}

// Status returns a detached, small snapshot.
func (r *Recorder) Status() Status {
	r.mu.Lock()
	status := r.status
	r.mu.Unlock()
	if sink := r.sink.Load(); sink != nil {
		status.WriterDropped = sink.dropped.Load()
	}
	r.profileMu.Lock()
	status.Profile = r.profile
	r.profileMu.Unlock()
	return status
}

func (r *Recorder) SetDailyEnabled(enabled bool) { r.timeline.setEnabled(enabled) }
func (r *Recorder) DailyEnabled() bool           { return r.timeline.Enabled() }

// Record does no disk I/O. A saturated recording writer drops events explicitly.
func (r *Recorder) Record(event Event) bool {
	sink := r.sink.Load()
	if sink != nil && sink.mode != "" && sink.mode != event.Mode {
		sink = nil
	}
	if sink == nil && !r.timeline.Enabled() {
		return false
	}
	wire, accepted := r.timeline.append(event, sink != nil)
	if accepted && sink != nil && wire != nil {
		sink.offer(wire)
	}
	return accepted
}

func (r *Recorder) Session(mode string, limit int) Session {
	out := r.timeline.snapshot(mode, limit)
	out.Recording = r.Status()
	if out.Recording.Mode != "" && out.Recording.Mode != mode {
		out.Recording = Status{Profile: out.Recording.Profile}
	}
	if out.Recording.Profile.Mode != mode {
		out.Recording.Profile = ProfileStatus{}
	}
	r.profileMu.Lock()
	for _, profile := range r.profileFiles {
		if profile.Mode == mode {
			out.ProfileFiles = append(out.ProfileFiles, profile.Path)
		}
	}
	r.profileMu.Unlock()
	out.WriterDropped = out.Recording.WriterDropped
	return out
}

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
	pruneCaptures(root, "recording-", ".jsonl", 4, r.status.Path)
	file, err := os.CreateTemp(root, "recording-"+time.Now().Format("20060102-150405")+"-*.jsonl")
	if err != nil {
		return r.status, err
	}
	r.status = Status{Mode: mode, Active: true, Path: file.Name(), StartedAtMS: time.Now().UnixMilli(), IntervalSeconds: int(r.interval / time.Second), MaxDurationSeconds: int(r.duration / time.Second)}
	r.file, r.encoder, r.bytes = file, json.NewEncoder(file), 0
	ctx, cancel := context.WithCancel(r.group.Context())
	r.cancel, r.done = cancel, make(chan struct{})
	err = r.writeLocked(map[string]any{"type": "start", "at_ms": r.status.StartedAtMS, "format_version": 2, "pid": os.Getpid(), "go_version": runtime.Version(), "status": r.status, "resource_scope": "shared_host_process"})
	if err == nil {
		err = r.sampleLocked()
	}
	if err != nil {
		r.finishLocked("error", err)
		return r.status, err
	}
	sink := &recordSink{accepting: true, queue: make(chan json.RawMessage, 128), mode: mode}
	r.sink.Store(sink)
	if !r.group.Go("diagnostics-recording", func(context.Context) { r.run(ctx, sink) }) {
		err = errors.New("应用正在退出，不能开始诊断记录")
		r.finishLocked("shutdown", err)
		return r.status, err
	}
	return r.status, nil
}

// Stop cancels and waits for queued events, the final sample and file closure.
func (r *Recorder) Stop(ctx context.Context) (Status, error) { return r.StopReason(ctx, "manual") }

func (r *Recorder) StopReason(ctx context.Context, reason string) (Status, error) {
	if reason == "mode_changed" {
		r.cancelProfile()
	}
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
	if !validKind(kind) {
		return false, fmt.Errorf("%w：未知事件类型", ErrInvalidMarker)
	}
	if r.sink.Load() == nil {
		return false, nil
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(payload), &data); err != nil {
		return false, ErrInvalidMarker
	}
	mode, _ := data["application_mode"].(string)
	if mode == "" {
		mode = r.Status().Mode
	}
	if mode == "" {
		mode = "video"
	}
	return r.Record(Event{Mode: mode, Kind: kind, Data: data}), nil
}

func (r *Recorder) run(ctx context.Context, sink *recordSink) {
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	deadline := time.NewTimer(r.duration)
	defer deadline.Stop()
	for {
		reason := ""
		var event json.RawMessage
		select {
		case <-ctx.Done():
			reason = "shutdown"
		case <-deadline.C:
			reason = "duration_limit"
		case <-ticker.C:
		case event = <-sink.queue:
		}
		r.mu.Lock()
		if r.status.Reason != "" {
			reason = r.status.Reason
		}
		if reason == "" && r.bytes >= r.maxBytes {
			reason = "size_limit"
		}
		if reason == "" && r.status.Markers >= maxMarkers {
			reason = "marker_limit"
		}
		if reason != "" {
			if event != nil {
				sink.dropped.Add(1)
			}
			r.finishLocked(reason, nil)
			r.mu.Unlock()
			return
		}
		var err error
		if event != nil {
			err = r.writeEventLocked(event)
		} else {
			err = r.sampleLocked()
		}
		if err != nil {
			r.finishLocked("error", err)
			r.mu.Unlock()
			return
		}
		r.mu.Unlock()
	}
}

func (r *Recorder) writeEventLocked(wire json.RawMessage) error {
	err := r.writeLocked(map[string]any{"type": "event", "event": wire})
	if err == nil {
		r.status.Markers++
	}
	return err
}

func (r *Recorder) sampleLocked() error {
	err := r.writeLocked(map[string]any{"type": "sample", "resources": r.sampler.SampleCached(10 * time.Second)})
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
	if sink := r.sink.Swap(nil); sink != nil {
		sink.mu.Lock()
		sink.accepting = false
		sink.mu.Unlock()
		for len(sink.queue) > 0 {
			wire := <-sink.queue
			if failure == nil && r.bytes < r.maxBytes && r.status.Markers < maxMarkers {
				failure = r.writeEventLocked(wire)
			} else {
				sink.dropped.Add(1)
			}
		}
		r.status.WriterDropped = sink.dropped.Load()
	}
	r.status.Reason = reason
	if failure == nil && r.status.Error == "" {
		failure = r.sampleLocked()
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
	r.file, r.encoder = nil, nil
	close(r.done)
}
