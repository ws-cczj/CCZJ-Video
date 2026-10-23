package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cczjVideo/app/db"
	"cczjVideo/app/lifecycle"
	"cczjVideo/app/netstats"
)

func TestReadingModeDiagnosticsDoNotQueryVideoTables(t *testing.T) {
	if err := db.InitDB(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	a := NewApp()
	t.Cleanup(func() { a.modeWork().Stop(time.Second); a.background.Stop(time.Second); db.Close() })
	a.startMode("novel")
	if _, err := db.DB().Exec(`INSERT INTO library_items(kind,source_key,remote_id) VALUES ('novel','nov','1'),('comic','com','1'),('comic','com','2')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB().Exec(`INSERT INTO library_chapters(item_id,remote_id) SELECT id,'chapter' FROM library_items`); err != nil {
		t.Fatal(err)
	}
	// A query of an inactive video domain would now fail. The reading report
	// must still have complete mode-specific counts and no video error notes.
	if _, err := db.DB().Exec(`DROP TABLE global_video`); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"novel", "comic"} {
		if mode != a.currentMode() {
			if err := a.SetSetting(applicationModeKey, mode); err != nil {
				t.Fatal(err)
			}
		}
		diag, err := a.GetDiagnostics()
		if err != nil {
			t.Fatal(err)
		}
		if diag.Scope.Mode != mode || diag.Douban != nil || diag.Collect != nil || len(diag.Sources) != 0 || len(diag.Cache) != 0 || len(diag.Notes) != 0 {
			t.Fatalf("reading report crossed mode boundary: %+v", diag)
		}
		for _, table := range diag.Tables {
			if table.Name == "library_items" || table.Name == "library_chapters" {
				want := int64(1)
				if mode == "comic" {
					want = 2
				}
				if table.Rows != want {
					t.Fatalf("mixed kind count: %s %+v", mode, table)
				}
			}
			if strings.Contains(table.Name, "video") || table.Name == "download_tasks" {
				t.Fatalf("inactive table: %+v", table)
			}
		}
		metrics := a.GetRuntimeMetrics()
		if metrics.DownloadExecutor != nil || len(metrics.Cache) != 0 {
			t.Fatalf("inactive video work exposed: %+v", metrics)
		}
		for task := range metrics.BackgroundTasks {
			if task != "reading-source-audit:"+mode {
				t.Fatalf("wrong mode task exposed: %s", task)
			}
		}
	}
}

func TestModeSwitchCancelsOldOwnerAndClosesItsRecording(t *testing.T) {
	if err := db.InitDB(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	a := NewApp()
	t.Setenv("APPDATA", t.TempDir())
	t.Cleanup(func() { a.modeWork().Stop(time.Second); a.background.Stop(time.Second); db.Close() })
	a.startMode("novel")
	old := a.modeWork()
	finished := make(chan struct{})
	old.Go("novel-request", func(ctx context.Context) { <-ctx.Done(); close(finished) })
	status, err := a.StartDiagnosticsRecording()
	if err != nil {
		t.Fatal(err)
	}
	if status.Mode != "novel" {
		t.Fatal(status)
	}
	if err := a.SetSetting(applicationModeKey, "comic"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-finished:
	default:
		t.Fatal("mode published before old owner stopped")
	}
	if old.Go("late", func(context.Context) {}) {
		t.Fatal("old owner accepted work")
	}
	ended := a.recording.Status()
	if ended.Active || ended.Reason != "mode_changed" {
		t.Fatal(ended)
	}
	if a.GetDiagnosticsRecordingStatus().Path != "" {
		t.Fatal("another mode's recording leaked")
	}
	if saved, err := db.GetSetting(applicationModeKey); err != nil || saved != "comic" {
		t.Fatal(saved, err)
	}
	if _, err := a.ExportDiagnosticsReport(`{"application_mode":"novel"}`); err == nil {
		t.Fatal("late frontend report accepted")
	}
}

func TestModeNetworkTotalsExcludeOtherDomains(t *testing.T) {
	netstats.Record(netstats.CategoryNovel, 11, 1, nil)
	netstats.Record(netstats.CategoryComic, 22, 1, nil)
	netstats.Record(netstats.CategoryPlayback, 33, 1, nil)
	a := &App{mode: "novel", background: lifecycle.NewGroup()}
	defer a.background.Stop(time.Second)
	for _, mode := range []string{"novel", "comic", "video"} {
		a.mode = mode
		network := a.runtimeNetwork()
		var sum int64
		for _, row := range network.Categories {
			if mode != "video" && row.Category != mode {
				t.Fatal(row)
			}
			if mode == "video" && (row.Category == "novel" || row.Category == "comic") {
				t.Fatal(row)
			}
			sum += row.Bytes
		}
		if network.TotalBytes != sum {
			t.Fatal("total is not mode scoped")
		}
	}
	data, _ := json.Marshal(a.diagnosticScope())
	if !strings.Contains(string(data), "shared_host_process") {
		t.Fatal("host ownership not explicit")
	}
}

func TestModeSwitchPausesDownloadAndRetainsManualResumeCheckpoint(t *testing.T) {
	root := t.TempDir()
	t.Setenv("APPDATA", root)
	if err := db.InitDB(root); err != nil {
		t.Fatal(err)
	}
	a := NewApp()
	a.downloadStore = db.NewDownloadTaskStore(db.DB())
	a.modeTasks = lifecycle.NewGroup()
	t.Cleanup(func() { a.modeWork().Stop(time.Second); a.background.Stop(time.Second); db.Close() })
	if _, err := a.downloadDir.Set(filepath.Join(root, "downloads")); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1048576")
		if r.Method == http.MethodHead {
			return
		}
		w.Write([]byte("x"))
		w.(http.Flusher).Flush()
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()
	if _, err := a.StartVideoDownload(context.Background(), VideoDownloadReq{TaskId: "mode-pause", Url: server.URL, Filename: "pause.bin"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("download did not start")
	}
	if err := a.SetSetting(applicationModeKey, "novel"); err != nil {
		t.Fatal(err)
	}
	task, _ := a.downloads.Get("mode-pause")
	if task.snapshot().Status != "paused" || !runFinished(task.run) || a.downloadExecutor.Stats().Accepted != 0 {
		t.Fatal("switch left download running", task.snapshot())
	}
	if _, err := a.ResumeDownload(context.Background(), "mode-pause"); err == nil {
		t.Fatal("reading mode resumed video download")
	}
	// The same recovery path used at process restart must retain the pause.
	if err := a.loadPersistedTasks(); err != nil {
		t.Fatal(err)
	}
	restored, _ := a.downloads.Get("mode-pause")
	if restored.snapshot().Status != "paused" {
		t.Fatal("recovery resumed a paused download")
	}
}
