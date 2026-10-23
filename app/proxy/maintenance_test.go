package proxy

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestCachePruningIsScheduledOnceAndDoesNotBlockWriter(t *testing.T) {
	dir := t.TempDir()
	store := newBlobStore(func() string { return dir }, "cache", time.Hour, 1<<20)
	var scheduled []func(context.Context)
	store.maintenance = func(_ string, work func(context.Context)) bool { scheduled = append(scheduled, work); return true }
	store.write("old", []byte("old"), "text/plain")
	oldPath := store.pathFor("old")
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(oldPath, old, old); err != nil {
		t.Fatal(err)
	}
	store.write("new", []byte("new"), "text/plain")
	if len(scheduled) != 1 {
		t.Fatalf("maintenance jobs = %d, want one", len(scheduled))
	}
	if _, err := os.Stat(oldPath); err != nil {
		t.Fatal("writer performed pruning inline")
	}
	scheduled[0](context.Background())
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatal("scheduled maintenance did not expire old entry")
	}
	if data, _, ok := store.read("new"); !ok || string(data) != "new" {
		t.Fatal("maintenance deleted fresh entry")
	}
}

func TestRejectedCacheMaintenanceDoesNotStickPendingFlag(t *testing.T) {
	dir := t.TempDir()
	store := newBlobStore(func() string { return dir }, "cache", time.Hour, 1<<20)
	count := 0
	store.maintenance = func(string, func(context.Context)) bool { count++; return false }
	store.write("one", []byte("one"), "text/plain")
	store.write("two", []byte("two"), "text/plain")
	if count != 2 || store.pruneRunning {
		t.Fatal("rejected maintenance retained a pending job")
	}
}
