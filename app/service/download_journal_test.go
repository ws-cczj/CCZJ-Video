package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"cczjVideo/app/apperror"
	"cczjVideo/app/db"
	downloadservice "cczjVideo/app/download"
	"cczjVideo/app/lifecycle"
)

func newDownloadTestApp(t *testing.T) *App {
	t.Helper()
	root := t.TempDir()
	t.Setenv("APPDATA", root)
	conn, err := db.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	a := &App{downloads: downloadservice.NewRegistry[downloadTask](), background: lifecycle.NewGroup(), downloadStore: db.NewDownloadTaskStore(conn), downloadDir: downloadservice.NewDirectory(nil)}
	if _, err := a.downloadDir.Set(filepath.Join(root, "output")); err != nil {
		t.Fatal(err)
	}
	a.downloadExecutor = downloadservice.NewExecutor(a.background.Context(), a.background.Go, 2, 64)
	t.Cleanup(func() { a.background.Stop(5 * time.Second); conn.Close() })
	return a
}

type interceptedDownloadStore struct {
	downloadservice.CheckpointStore
	save      func(downloadservice.Checkpoint) error
	removeErr error
}

// Transport tests isolate the wire/file protocol from persistence. Integration
// tests above use a real, independently opened SQLite database.
type transportCheckpointStore struct{}

func (transportCheckpointStore) Save(context.Context, downloadservice.Checkpoint) error { return nil }
func (transportCheckpointStore) Delete(context.Context, string) error                   { return nil }
func (transportCheckpointStore) Restore(context.Context, string) ([]downloadservice.Checkpoint, error) {
	return nil, nil
}
func newTransportTestApp() *App { return &App{downloadStore: transportCheckpointStore{}} }

func (s *interceptedDownloadStore) Save(ctx context.Context, cp downloadservice.Checkpoint) error {
	if s.save != nil {
		if err := s.save(cp); err != nil {
			return err
		}
	}
	return s.CheckpointStore.Save(ctx, cp)
}
func (s *interceptedDownloadStore) Delete(ctx context.Context, id string) error {
	if s.removeErr != nil {
		return s.removeErr
	}
	return s.CheckpointStore.Delete(ctx, id)
}

func TestDownloadJournalFailurePreventsNetworkWork(t *testing.T) {
	a := newDownloadTestApp(t)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests.Add(1) }))
	defer server.Close()
	a.downloadStore = &interceptedDownloadStore{CheckpointStore: a.downloadStore, save: func(downloadservice.Checkpoint) error { return errors.New("disk full") }}
	_, err := a.StartVideoDownload(context.Background(), VideoDownloadReq{TaskId: "failed", Url: server.URL, Filename: "movie.bin"})
	if !apperror.Is(err, apperror.Storage) {
		t.Fatalf("start = %v", err)
	}
	if requests.Load() != 0 || len(a.downloads.Snapshot()) != 0 {
		t.Fatal("failed admission launched work or published a task")
	}
}

func TestDownloadCompletionAfterPublicationAndRestartReconciliation(t *testing.T) {
	a := newDownloadTestApp(t)
	base := a.downloadStore
	var finalPath string
	var completed atomic.Int32
	a.downloadStore = &interceptedDownloadStore{CheckpointStore: base, save: func(cp downloadservice.Checkpoint) error {
		if cp.Status == "finalizing" {
			if _, err := os.Stat(cp.SavePath + ".part"); err != nil {
				t.Error(err)
			}
			if _, err := os.Stat(cp.SavePath); !os.IsNotExist(err) {
				t.Error("file published before intent checkpoint")
			}
		}
		if cp.Status == "done" {
			if data, err := os.ReadFile(cp.SavePath); err != nil || string(data) != "ok" {
				t.Errorf("completion before file publication: %q %v", data, err)
			}
			completed.Add(1)
			return errors.New("completion disk full")
		}
		return nil
	}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "2")
		if r.Method != "HEAD" {
			w.Write([]byte("ok"))
		}
	}))
	defer server.Close()
	s, err := a.StartVideoDownload(context.Background(), VideoDownloadReq{TaskId: "published", Url: server.URL, Filename: "movie.bin"})
	if err != nil {
		t.Fatal(err)
	}
	finalPath = s.SavePath
	task, _ := a.downloads.Get(s.TaskId)
	waitDownloadRun(t, a, task)
	if s := task.snapshot(); s.Status != "done" || s.Error == "" {
		t.Fatalf("completion failure status = %+v", s)
	}
	if completed.Load() != 1 {
		t.Fatalf("completion writes = %d; finalizing must not be overwritten by cleanup", completed.Load())
	}
	items, err := base.Restore(context.Background(), filepath.Join(t.TempDir(), "absent.json"))
	if err != nil || len(items) != 1 || items[0].Status != "done" || items[0].SavePath != finalPath {
		t.Fatalf("recovery = %+v, %v", items, err)
	}
}

func waitDownloadRun(t *testing.T, a *App, task *downloadTask) {
	t.Helper()
	a.downloadMu.Lock()
	run := task.run
	a.downloadMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := run.Wait(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestDownloadRemoveFailurePreservesRecordAndLateCheckpointCannotResurrect(t *testing.T) {
	a := newDownloadTestApp(t)
	task := &downloadTask{status: VideoDownloadStatus{TaskId: "removed", Url: "https://example.test/v", SavePath: filepath.Join(a.downloadDir.Get(), "v.bin"), Status: "paused"}}
	a.downloads.Add("removed", task)
	if !a.checkpoint(task) {
		t.Fatal("initial checkpoint")
	}
	base := a.downloadStore
	a.downloadStore = &interceptedDownloadStore{CheckpointStore: base, removeErr: errors.New("disk full")}
	if ok, err := a.RemoveDownload(context.Background(), "removed"); ok || !apperror.Is(err, apperror.Storage) {
		t.Fatalf("remove = %v, %v", ok, err)
	}
	if _, ok := a.downloads.Get("removed"); !ok {
		t.Fatal("failed deletion hid the task")
	}
	a.downloadStore = base
	if ok, err := a.RemoveDownload(context.Background(), "removed"); !ok || err != nil {
		t.Fatalf("remove = %v, %v", ok, err)
	}
	a.checkpoint(task)
	items, err := base.Restore(context.Background(), filepath.Join(t.TempDir(), "absent.json"))
	if err != nil || len(items) != 0 {
		t.Fatalf("stale worker resurrected record: %+v %v", items, err)
	}
}

func TestDownloadPauseJoinsAttemptBeforeResumeAndCancelRemainsTerminal(t *testing.T) {
	a := newDownloadTestApp(t)
	started := make(chan struct{}, 2)
	var resume atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "2")
		if r.Method == "HEAD" {
			return
		}
		started <- struct{}{}
		if !resume.Load() {
			<-r.Context().Done()
			return
		}
		w.Write([]byte("ok"))
	}))
	defer server.Close()
	s, err := a.StartVideoDownload(context.Background(), VideoDownloadReq{TaskId: "joined", Url: server.URL, Filename: "v.bin"})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("download did not start")
	}
	task, _ := a.downloads.Get(s.TaskId)
	a.downloadMu.Lock()
	first := task.run
	a.downloadMu.Unlock()
	if ok, err := a.PauseDownload(context.Background(), s.TaskId); !ok || err != nil {
		t.Fatalf("pause = %v %v", ok, err)
	}
	if !runFinished(first) {
		t.Fatal("pause acknowledged before attempt released file")
	}
	if s := task.snapshot(); s.Status != "paused" {
		t.Fatalf("pause = %+v", s)
	}
	// A different URL must still be excluded from the same destination.
	if _, err := a.StartVideoDownload(context.Background(), VideoDownloadReq{TaskId: "collision", Url: server.URL + "/other", Filename: "v.bin"}); !apperror.Is(err, apperror.DownloadDuplicate) {
		t.Fatalf("destination collision = %v", err)
	}
	resume.Store(true)
	if ok, err := a.ResumeDownload(context.Background(), s.TaskId); !ok || err != nil {
		t.Fatalf("resume = %v %v", ok, err)
	}
	waitDownloadRun(t, a, task)
	if data, err := os.ReadFile(s.SavePath); err != nil || string(data) != "ok" {
		t.Fatalf("resumed file = %q %v", data, err)
	}
	// Restored terminal cancellation must not become resumable on startup.
	other := &downloadTask{status: VideoDownloadStatus{TaskId: "cancelled", Url: server.URL + "/later", SavePath: filepath.Join(a.downloadDir.Get(), "later.bin"), Status: "paused"}, paused: true}
	a.downloads.Add("cancelled", other)
	if !a.checkpoint(other) {
		t.Fatal("checkpoint")
	}
	if ok, err := a.CancelDownload(context.Background(), "cancelled"); !ok || err != nil {
		t.Fatalf("cancel = %v %v", ok, err)
	}
	restored := &App{downloads: downloadservice.NewRegistry[downloadTask](), downloadStore: a.downloadStore}
	if err := restored.loadPersistedTasks(); err != nil {
		t.Fatal(err)
	}
	got, _ := restored.downloads.Get("cancelled")
	if s := got.snapshot(); s.Status != "cancelled" {
		t.Fatalf("restored cancellation = %+v", s)
	}
	if got.httpClient != nil {
		t.Fatal("restoring history allocated a private transport")
	}
}

func TestDownloadFinalizingWriteFailureKeepsPartialAndDoesNotPublish(t *testing.T) {
	a := newDownloadTestApp(t)
	a.downloadStore = &interceptedDownloadStore{CheckpointStore: a.downloadStore, save: func(cp downloadservice.Checkpoint) error {
		if cp.Status == "finalizing" {
			return errors.New("disk full")
		}
		return nil
	}}
	path := filepath.Join(a.downloadDir.Get(), "v.bin")
	if err := os.WriteFile(path+".part", []byte("ok"), 0600); err != nil {
		t.Fatal(err)
	}
	task := &downloadTask{status: VideoDownloadStatus{TaskId: "finalize", Url: "https://example.test/v", SavePath: path, Status: "downloading"}}
	a.downloads.Add("finalize", task)
	a.finalizeDownload(task, path+".part", path)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("published without durable intent")
	}
	if data, err := os.ReadFile(path + ".part"); err != nil || string(data) != "ok" {
		t.Fatalf("partial lost: %q %v", data, err)
	}
	if s := task.snapshot(); s.Status != "error" || s.Error == "" {
		t.Fatalf("failed finalization = %+v", s)
	}
}

func TestDownloadForceWaitsForPreviousAttemptBeforeReusingDestination(t *testing.T) {
	a := newDownloadTestApp(t)
	started := make(chan struct{})
	var gets atomic.Int32
	var oldRun atomic.Pointer[downloadservice.Run]
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "2")
		if r.Method == "HEAD" {
			return
		}
		if gets.Add(1) == 1 {
			w.Write([]byte("x"))
			w.(http.Flusher).Flush()
			close(started)
			<-r.Context().Done()
			return
		}
		if !runFinished(oldRun.Load()) {
			t.Error("replacement accessed destination before previous attempt finished")
		}
		w.Write([]byte("ok"))
	}))
	defer server.Close()
	req := VideoDownloadReq{TaskId: "original", Url: server.URL, Filename: "v.bin"}
	s, err := a.StartVideoDownload(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("original download did not start")
	}
	old, _ := a.downloads.Get(s.TaskId)
	a.downloadMu.Lock()
	oldRun.Store(old.run)
	a.downloadMu.Unlock()
	// Invalid overwrite parameters must not cancel or remove the original.
	invalid := req
	invalid.Force = true
	invalid.SaveDir = filepath.Join(t.TempDir(), "outside")
	if _, err := a.StartVideoDownload(context.Background(), invalid); !apperror.Is(err, apperror.Validation) {
		t.Fatalf("invalid force = %v", err)
	}
	if runFinished(oldRun.Load()) {
		t.Fatal("invalid force cancelled the original")
	}
	req.TaskId, req.Force = "replacement", true
	s, err = a.StartVideoDownload(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := a.downloads.Get("original"); exists {
		t.Fatal("old record survived successful replacement")
	}
	current, _ := a.downloads.Get(s.TaskId)
	waitDownloadRun(t, a, current)
	if data, err := os.ReadFile(s.SavePath); err != nil || string(data) != "ok" {
		t.Fatalf("replacement file = %q %v", data, err)
	}
}
