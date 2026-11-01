package diagnosticrecording

import (
	"encoding/json"
	"math"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	MaxTimelineEvents = 2048
	MaxTimelineBytes  = 1 << 20
	MaxEventBytes     = 2048
	MaxBatchBytes     = 64 << 10
	MaxBatchEvents    = 64
)

var labelPattern = regexp.MustCompile(`^[A-Za-z0-9_.:-]{0,120}$`)
var routePattern = regexp.MustCompile(`^/[A-Za-z0-9_/:()*.-]*$`)

// Event contains bounded operation metadata, never arbitrary request arguments.
type Event struct {
	ID               string         `json:"id"`
	Sequence         uint64         `json:"sequence"`
	AtMS             int64          `json:"at_ms"`
	ClientAtMS       int64          `json:"client_at_ms,omitempty"`
	SessionID        string         `json:"session_id,omitempty"`
	ActionID         string         `json:"action_id,omitempty"`
	RequestID        string         `json:"request_id,omitempty"`
	FlightID         string         `json:"flight_id,omitempty"`
	BackendRequestID string         `json:"backend_request_id,omitempty"`
	ParentID         string         `json:"parent_id,omitempty"`
	ResourceID       string         `json:"resource_id,omitempty"`
	Mode             string         `json:"application_mode"`
	Kind             string         `json:"kind"`
	Name             string         `json:"name,omitempty"`
	Route            string         `json:"route,omitempty"`
	Outcome          string         `json:"outcome,omitempty"`
	Code             string         `json:"code,omitempty"`
	DurationMS       float64        `json:"duration_ms,omitempty"`
	Data             map[string]any `json:"data,omitempty"`
}

type timelineEntry struct {
	event Event
	wire  []byte
}

// Timeline keeps a fixed-capacity ring with an independent serialized-byte limit.
type Timeline struct {
	mu                          sync.Mutex
	entries                     [MaxTimelineEvents]timelineEntry
	head, count, bytes          int
	sequence, evicted, rejected uint64
	enabled                     bool
	seen                        map[string]struct{}
}

type Finding struct {
	Code      string   `json:"code"`
	Count     int      `json:"count"`
	Evidence  []string `json:"evidence"`
	Certainty string   `json:"certainty"`
}

// Session is a detached observation window; missing evidence is explicit.
type Session struct {
	FormatVersion    int       `json:"format_version"`
	CapturedAtMS     int64     `json:"captured_at_ms"`
	DailyEnabled     bool      `json:"daily_enabled"`
	Events           []Event   `json:"events"`
	Findings         []Finding `json:"findings"`
	Buffered         int       `json:"buffered"`
	Bytes            int       `json:"bytes"`
	Evicted          uint64    `json:"evicted"`
	Rejected         uint64    `json:"rejected"`
	WriterDropped    uint64    `json:"writer_dropped"`
	WindowLimited    bool      `json:"window_limited"`
	CorrelationScope string    `json:"correlation_scope"`
	Recording        Status    `json:"recording"`
	ProfileFiles     []string  `json:"profile_files,omitempty"`
}

func validKind(kind string) bool {
	switch kind {
	case "ui.action", "ui.error", "ui.lifecycle", "route", "request.start", "request.finish", "request.link", "backend.operation", "perf.longtask",
		"playback_load", "first_frame", "stall", "error", "media", "playback_snapshot", "recording_start", "recording_stop", "window_state", "player_closing", "player_closed", "playback_page_closed", "player_media_load", "player_issue", "preview_started", "preview_stopped", "startup_snapshot", "activity_snapshot",
		"short_drama_frame_capture", "short_drama_detail_ready", "short_drama_step", "short_drama_next_failed", "short_drama_prefetch_failed", "short_drama_catalog_failed", "short_drama_provider_empty":
		return true
	}
	return false
}

func cleanLabel(value string) string {
	if labelPattern.MatchString(value) {
		return value
	}
	return ""
}

func cleanEvent(event Event) (Event, bool) {
	if !validKind(event.Kind) || (event.Mode != "video" && event.Mode != "novel" && event.Mode != "comic") {
		return Event{}, false
	}
	event.ID, event.SessionID = cleanLabel(event.ID), cleanLabel(event.SessionID)
	event.ActionID, event.RequestID, event.FlightID = cleanLabel(event.ActionID), cleanLabel(event.RequestID), cleanLabel(event.FlightID)
	event.BackendRequestID, event.ParentID = cleanLabel(event.BackendRequestID), cleanLabel(event.ParentID)
	event.ResourceID = cleanLabel(event.ResourceID)
	event.Name, event.Code = cleanLabel(event.Name), cleanLabel(event.Code)
	if len(event.Route) > 120 || !routePattern.MatchString(event.Route) {
		event.Route = ""
	}
	if event.Outcome != "success" && event.Outcome != "failed" && event.Outcome != "cancelled" && event.Outcome != "started" {
		event.Outcome = ""
	}
	if math.IsNaN(event.DurationMS) || math.IsInf(event.DurationMS, 0) || event.DurationMS < 0 {
		event.DurationMS = 0
	}
	data := make(map[string]any)
	for key, value := range event.Data {
		if len(data) >= 16 {
			break
		}
		if !labelPattern.MatchString(key) || len(key) > 48 {
			continue
		}
		// Text content and arbitrary identifiers cannot enter the event payload.
		switch strings.ToLower(key) {
		case "url", "path", "message", "detail", "stack", "headers", "cookie", "token", "text", "title", "keyword", "source_key", "vod_id", "global_id":
			continue
		}
		switch value := value.(type) {
		case bool:
			data[key] = value
		case float64:
			if !math.IsNaN(value) && !math.IsInf(value, 0) {
				data[key] = value
			}
		case int:
			data[key] = value
		case int64:
			data[key] = value
		case string:
			if (key == "phase" || key == "engine" || key == "state" || key == "presentation") && len(value) <= 64 && labelPattern.MatchString(value) {
				data[key] = value
			}
		}
	}
	event.Data = data
	return event, true
}

func (t *Timeline) setEnabled(enabled bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.enabled = enabled
	if !enabled {
		t.clearLocked()
	}
}
func (t *Timeline) Enabled() bool { t.mu.Lock(); defer t.mu.Unlock(); return t.enabled }
func (t *Timeline) clearLocked() {
	t.entries = [MaxTimelineEvents]timelineEntry{}
	t.head = 0
	t.count = 0
	t.bytes = 0
	t.evicted = 0
	t.rejected = 0
	t.seen = nil
}

func (t *Timeline) append(event Event, force bool) ([]byte, bool) {
	event, valid := cleanEvent(event)
	t.mu.Lock()
	defer t.mu.Unlock()
	if !valid {
		t.rejected++
		return nil, false
	}
	if !t.enabled && !force {
		return nil, false
	}
	if event.ID != "" {
		if _, duplicate := t.seen[event.ID]; duplicate {
			return nil, true
		}
	}
	t.sequence++
	event.Sequence = t.sequence
	event.AtMS = time.Now().UnixMilli()
	if event.ID == "" {
		event.ID = "host-" + strconv.FormatUint(t.sequence, 10)
	}
	wire, err := json.Marshal(event)
	if err != nil || len(wire) > MaxEventBytes {
		t.rejected++
		return nil, false
	}
	for t.count > 0 && (t.count == MaxTimelineEvents || t.bytes+len(wire) > MaxTimelineBytes) {
		t.bytes -= len(t.entries[t.head].wire)
		delete(t.seen, t.entries[t.head].event.ID)
		t.entries[t.head] = timelineEntry{}
		t.head = (t.head + 1) % MaxTimelineEvents
		t.count--
		t.evicted++
	}
	t.entries[(t.head+t.count)%MaxTimelineEvents] = timelineEntry{event: event, wire: wire}
	t.count++
	t.bytes += len(wire)
	if t.seen == nil {
		t.seen = make(map[string]struct{})
	}
	t.seen[event.ID] = struct{}{}
	return wire, true
}

func (t *Timeline) snapshot(mode string, limit int) Session {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := Session{FormatVersion: 2, CapturedAtMS: time.Now().UnixMilli(), DailyEnabled: t.enabled, Events: []Event{}, Findings: []Finding{}, Buffered: t.count, Bytes: t.bytes, Evicted: t.evicted, Rejected: t.rejected, CorrelationScope: "frontend_action_and_flight;backend_request_id_on_errors;other_backend_spans_unlinked"}
	for i := 0; i < t.count; i++ {
		event := t.entries[(t.head+i)%MaxTimelineEvents].event
		if mode == "" || event.Mode == mode {
			data := make(map[string]any, len(event.Data))
			for key, value := range event.Data {
				data[key] = value
			}
			event.Data = data
			out.Events = append(out.Events, event)
		}
	}
	if limit > 0 && len(out.Events) > limit {
		out.WindowLimited = true
		out.Events = out.Events[len(out.Events)-limit:]
	}
	for _, rule := range []struct {
		code   string
		accept func(Event) bool
	}{
		{"operation_failed", func(e Event) bool { return e.Outcome == "failed" || e.Kind == "ui.error" }},
		{"slow_operation", func(e Event) bool {
			return e.DurationMS >= 2000 && (e.Kind == "request.finish" || e.Kind == "backend.operation")
		}},
		{"main_thread_longtask", func(e Event) bool { return e.Kind == "perf.longtask" }},
	} {
		finding := Finding{Code: rule.code, Evidence: []string{}, Certainty: "observed"}
		for _, event := range out.Events {
			if rule.accept(event) {
				finding.Count++
				if len(finding.Evidence) < 8 {
					finding.Evidence = append(finding.Evidence, event.ID)
				}
			}
		}
		if finding.Count > 0 {
			out.Findings = append(out.Findings, finding)
		}
	}
	return out
}
