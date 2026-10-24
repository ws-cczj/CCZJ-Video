package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"cczjVideo/app/apperror"
	downloadservice "cczjVideo/app/download"
	"cczjVideo/app/operation"
)

type observedStore struct {
	downloadservice.CheckpointStore
	mu         sync.Mutex
	identities []operation.Identity
}

func (s *observedStore) Save(ctx context.Context, cp downloadservice.Checkpoint) error {
	if id, ok := operation.IdentityFromContext(ctx); ok {
		s.mu.Lock()
		s.identities = append(s.identities, id)
		s.mu.Unlock()
	}
	return s.CheckpointStore.Save(ctx, cp)
}

func TestAttemptOutlivesAdmissionAndCloseJoinsHTTPAndCheckpoint(t *testing.T) {
	a := newDownloadTestApp(t)
	store := &observedStore{CheckpointStore: a.store}
	a.store = store
	started, closed := make(chan struct{}), make(chan struct{})
	var once sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1048576")
		if r.Method == http.MethodHead {
			return
		}
		_, _ = w.Write([]byte("x"))
		w.(http.Flusher).Flush()
		once.Do(func() { close(started) })
		<-r.Context().Done()
		close(closed)
	}))
	defer server.Close()
	request, parent := operation.WithIdentity(context.Background(), "rpc.StartVideoDownload")
	request, cancelRequest := context.WithCancel(request)
	defer cancelRequest()
	s, err := a.Start(request, Request{TaskId: "owned", Url: server.URL, Filename: "owned.bin"})
	if err != nil {
		t.Fatal(err)
	}
	cancelRequest()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("returning/cancelling admission stopped accepted work")
	}
	if got, _ := a.Progress(s.TaskId); got.Status != "downloading" {
		t.Fatal(got)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := a.Close(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-closed:
	case <-ctx.Done():
		t.Fatal("close left upstream HTTP open")
	}
	if a.Stats().Accepted != 0 {
		t.Fatal("close retained accepted attempts")
	}
	task, _ := a.tasks.Get(s.TaskId)
	id, ok := operation.IdentityFromContext(task.operation)
	if !ok || id.RequestID == parent.RequestID || id.Pipeline != "download.transfer" {
		t.Fatal("missing independent attempt identity", id)
	}
	if operation.Fields(task.operation, nil)["parent_request_id"] != parent.RequestID {
		t.Fatal("attempt lost admission correlation")
	}
	store.mu.Lock()
	for _, observed := range store.identities {
		if observed != id {
			t.Error("checkpoint used another attempt identity", observed)
		}
	}
	if len(store.identities) < 2 {
		t.Error("admission/final checkpoint lost identity")
	}
	store.mu.Unlock()
	if _, err := os.Stat(s.SavePath); !os.IsNotExist(err) {
		t.Fatal("cancelled transfer published an output")
	}
	if _, err := a.Start(context.Background(), Request{TaskId: "late", Url: server.URL, Filename: "late.bin"}); !apperror.Is(err, apperror.Cancelled) {
		t.Fatal("closed engine accepted work", err)
	}
	if _, ok := a.tasks.Get("late"); ok {
		t.Fatal("late admission published a task")
	}
}

func TestReturnedProgressDoesNotExposeMutableTaskChunks(t *testing.T) {
	a := newDownloadTestApp(t)
	task := &downloadTask{status: Status{TaskId: "snapshot", Chunks: []ChunkProgress{{Done: 10}}}}
	a.tasks.Add("snapshot", task)
	status, err := a.Progress("snapshot")
	if err != nil {
		t.Fatal(err)
	}
	status.Chunks[0].Done = 999
	listed, err := a.List()
	if err != nil {
		t.Fatal(err)
	}
	if task.snapshot().Chunks[0].Done != 10 || listed[0].Chunks[0].Done != 10 {
		t.Fatal("public snapshot mutated live task state")
	}
}
