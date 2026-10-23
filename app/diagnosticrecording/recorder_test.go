package diagnosticrecording

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cczjVideo/app/lifecycle"
)

func newTestRecorder(t *testing.T) (*Recorder, *lifecycle.Group) {
	t.Helper()
	root := t.TempDir()
	group := lifecycle.NewGroup()
	r := New(func() string { return root }, group)
	r.interval = 10 * time.Millisecond
	t.Cleanup(func() { group.Stop(3 * time.Second) })
	return r, group
}

func readRecords(t *testing.T, path string) []map[string]any {
	t.Helper()
	out, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	var records []map[string]any
	scanner := bufio.NewScanner(out)
	for scanner.Scan() {
		var record map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			t.Fatal(err)
		}
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return records
}

func awaitSamples(t *testing.T, r *Recorder, count int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if r.Status().Samples >= count {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("background sampling stalled: %+v", r.Status())
}

func TestRecordingSamplesWithoutUIAndPersistsWhileActive(t *testing.T) {
	r, group := newTestRecorder(t)
	first, err := r.Start()
	if err != nil {
		t.Fatal(err)
	}
	second, err := r.Start()
	if err != nil || first.Path != second.Path {
		t.Fatalf("duplicate start: %+v %v", second, err)
	}
	awaitSamples(t, r, 3) // No frontend requests, panel or visible-window callbacks exist here.
	if len(readRecords(t, first.Path)) < 4 {
		t.Fatal("active timeline was buffered instead of persisted")
	}
	if ok, err := r.Mark("stall", `{"ms":1200}`); !ok || err != nil {
		t.Fatalf("marker: %v %v", ok, err)
	}
	status, err := r.Stop(context.Background())
	if err != nil || status.Active || status.Reason != "manual" || status.Markers != 1 {
		t.Fatalf("stop: %+v %v", status, err)
	}
	records := readRecords(t, status.Path)
	if records[0]["type"] != "start" || records[len(records)-1]["type"] != "end" {
		t.Fatal("missing session boundaries")
	}
	for _, path := range []string{status.StartHeapPath, status.StartGoroutinesPath, status.EndHeapPath, status.EndGoroutinesPath} {
		if info, err := os.Stat(path); err != nil || info.Size() == 0 {
			t.Fatalf("missing profile %s: %v", path, err)
		}
	}
	// Stop waits for file closure: on Windows rename fails if the writer is still open.
	if err := os.Rename(status.Path, status.Path+".closed"); err != nil {
		t.Fatal(err)
	}
	group.Stop(3 * time.Second)
	if len(group.ActiveTasks()) != 0 {
		t.Fatal("recording worker leaked after stop")
	}
}

func TestRecordingShutdownAndLimits(t *testing.T) {
	for _, reason := range []string{"shutdown", "duration_limit", "size_limit"} {
		t.Run(reason, func(t *testing.T) {
			r, group := newTestRecorder(t)
			if reason == "duration_limit" {
				r.duration = 20 * time.Millisecond
			}
			if reason == "size_limit" {
				r.maxBytes = 1
			}
			if _, err := r.Start(); err != nil {
				t.Fatal(err)
			}
			if reason == "shutdown" {
				group.Stop(3 * time.Second)
			} else {
				select {
				case <-r.done:
				case <-time.After(3 * time.Second):
					t.Fatal("bounded session did not stop")
				}
			}
			status := r.Status()
			if status.Active || status.Reason != reason || status.Error != "" {
				t.Fatalf("unexpected status: %+v", status)
			}
			if records := readRecords(t, status.Path); records[len(records)-1]["type"] != "end" {
				t.Fatal("exit did not flush end")
			}
		})
	}
}

func TestRecordingRejectsUnboundedMarkersAndHandlesDiskFailure(t *testing.T) {
	r, _ := newTestRecorder(t)
	if _, err := r.Start(); err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct{ kind, data string }{{"bad_kind", "{}"}, {"stall", "invalid"}, {"stall", `"` + strings.Repeat("x", 8192) + `"`}} {
		if _, err := r.Mark(item.kind, item.data); err == nil {
			t.Fatalf("accepted invalid marker %s", item.kind)
		}
	}
	r.mu.Lock()
	if err := r.file.Close(); err != nil {
		t.Fatal(err)
	} // Disk/writer failure, not a UI failure.
	r.mu.Unlock()
	if _, err := r.Mark("stall", "{}"); err == nil {
		t.Fatal("write failure was hidden")
	}
	select {
	case <-r.done:
	case <-time.After(3 * time.Second):
		t.Fatal("failed writer kept sampling")
	}
	if status := r.Status(); status.Active || status.Error == "" {
		t.Fatalf("missing error status: %+v", status)
	}
	if _, err := os.Stat(filepath.Dir(r.Status().Path)); err != nil {
		t.Fatal(err)
	}
	// A failed session must not prevent a later recording with a fresh writer.
	if status, err := r.Start(); err != nil || !status.Active {
		t.Fatalf("cannot recover: %+v %v", status, err)
	}
	if _, err := r.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// The frontend's resource ownership markers must survive the native boundary,
// otherwise a recording can look healthy while missing every close counter.
func TestPlayerArchitectureMarkersReachTimeline(t *testing.T) {
	r, _ := newTestRecorder(t)
	if _, err := r.Start(); err != nil {
		t.Fatal(err)
	}
	kinds := []string{"player_media_load", "playback_page_closed", "player_issue", "preview_started", "preview_stopped", "startup_snapshot", "activity_snapshot"}
	for _, kind := range kinds {
		if accepted, err := r.Mark(kind, `{"player_session_id":"integration","count":2}`); err != nil || !accepted {
			t.Fatalf("%s rejected at native boundary: accepted=%v err=%v", kind, accepted, err)
		}
	}
	status, err := r.Stop(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, record := range readRecords(t, status.Path) {
		if kind, ok := record["kind"].(string); ok {
			seen[kind] = true
		}
	}
	for _, kind := range kinds {
		if !seen[kind] {
			t.Fatalf("accepted marker %s missing from persisted timeline", kind)
		}
	}
}
