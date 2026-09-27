package detail

import (
	"cczjVideo/app/collect"
	"cczjVideo/app/db"
	"cczjVideo/app/model"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var testDBDir string

func TestMain(m *testing.M) {
	testDBDir, _ = os.MkdirTemp("", "cczj-detail-test-")
	code := m.Run()
	db.Close()
	_ = os.RemoveAll(testDBDir)
	os.Exit(code)
}

func TestDetailRequestDoesNotPersistDetailFields(t *testing.T) {
	if err := db.InitDB(testDBDir); err != nil {
		t.Fatal(err)
	}
	src := &model.Source{SourceKey: "detail_1", Name: "Detail", ApiUrl: "https://example.test"}
	if err := db.AddSource(src); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertCatalogItems(src.SourceKey, []*model.Video{{VodId: "v1", TypeId: "t1", TypeName: "Movie", VodName: "Title"}}); err != nil {
		t.Fatal(err)
	}
	var columns []string
	if err := db.DB().Select(&columns, `SELECT name FROM pragma_table_info('global_video') WHERE name IN ('content','actor','director')`); err != nil {
		t.Fatal(err)
	}
	if len(columns) != 0 {
		t.Fatalf("detail columns remain: %v", columns)
	}
	s := New(time.Minute)
	s.requestFn = func(*db.CatalogItem) (*model.Video, error) {
		return &model.Video{VodId: "v1", VodName: "Title", VodContent: "detail", VodActor: "actor", VodDirector: "director", VodPlayUrl: "play", VodDownUrl: "download"}, nil
	}
	if _, err := s.Get(src.SourceKey, "v1"); err != nil {
		t.Fatal(err)
	}
	if err := db.DB().Select(&columns, `SELECT name FROM pragma_table_info('global_video') WHERE name IN ('content','actor','director')`); err != nil {
		t.Fatal(err)
	}
	if len(columns) != 0 {
		t.Fatalf("detail request restored detail columns: %v", columns)
	}
}

func TestCatalogDetailCacheAndKeyedSingleflight(t *testing.T) {
	s := New(time.Minute)
	var calls atomic.Int32
	s.fetchFn = func(c *db.CatalogItem) (*Result, error) {
		calls.Add(1)
		time.Sleep(20 * time.Millisecond)
		return &Result{Video: &model.Video{GlobalId: c.GlobalID, VodName: c.VodName}, Catalog: c}, nil
	}
	catalog := &db.CatalogItem{SourceKey: "s", GlobalID: 9, SourceVodID: "upstream", VodName: "title"}
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := s.getCatalog(catalog)
			if err != nil || r.Video.GlobalId != 9 {
				t.Errorf("result=%+v err=%v", r, err)
			}
		}()
	}
	wg.Wait()
	if got := calls.Load(); got != 1 {
		t.Fatalf("source calls=%d, want 1", got)
	}
	if _, err := s.getCatalog(catalog); err != nil {
		t.Fatal(err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("cache miss calls=%d", got)
	}
}

func TestDetailRetryClassification(t *testing.T) {
	for _, tc := range []struct {
		err   error
		retry bool
	}{{&collect.HTTPError{StatusCode: 404}, false}, {&collect.HTTPError{StatusCode: 429}, true}, {&Error{Message: "timeout"}, true}} {
		if got := isRetryable(tc.err); got != tc.retry {
			t.Fatalf("%v retry=%v", tc.err, got)
		}
	}
}

func TestDetailRetriesThenReturnsCatalogFallback(t *testing.T) {
	s := New(time.Second)
	s.retryDelay = func(int) time.Duration { return 0 }
	var attempts atomic.Int32
	s.requestFn = func(*db.CatalogItem) (*model.Video, error) { attempts.Add(1); return nil, &Error{Message: "timeout"} }
	r, err := s.fetch(&db.CatalogItem{SourceKey: "s", GlobalID: 7, SourceVodID: "v", VodName: "fallback", VodPic: "cover"})
	if r == nil || r.Video == nil || r.Video.VodName != "fallback" {
		t.Fatalf("fallback=%+v", r)
	}
	structured, ok := err.(*Error)
	if !ok || structured.Attempts != 3 || !structured.Retryable {
		t.Fatalf("error=%#v", err)
	}
	if attempts.Load() != 3 {
		t.Fatalf("attempts=%d", attempts.Load())
	}
}

func TestDetailDoesNotRetryNotFound(t *testing.T) {
	s := New(time.Second)
	s.retryDelay = func(int) time.Duration { return 0 }
	var attempts atomic.Int32
	s.requestFn = func(*db.CatalogItem) (*model.Video, error) {
		attempts.Add(1)
		return nil, &collect.HTTPError{StatusCode: 404}
	}
	_, err := s.fetch(&db.CatalogItem{SourceKey: "s", GlobalID: 7, SourceVodID: "v"})
	structured := err.(*Error)
	if structured.Retryable || attempts.Load() != 1 {
		t.Fatalf("retryable=%v attempts=%d", structured.Retryable, attempts.Load())
	}
}
