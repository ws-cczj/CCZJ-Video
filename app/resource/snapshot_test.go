package resource

import (
	"encoding/json"
	"runtime"
	"testing"
)

func TestSnapshotIsBoundedAndDoesNotRetainNativeHandles(t *testing.T) {
	previousProcs := runtime.GOMAXPROCS(2)
	defer runtime.GOMAXPROCS(previousProcs)
	var sampler Sampler
	// ReadMemStats can lazily start Go runtime workers; warm those before checking
	// native handle retention caused by the sampler itself.
	for i := 0; i < 20; i++ {
		sampler.Sample()
	}
	first := sampler.Sample()
	if first.HeapAllocBytes == 0 || first.Goroutines == 0 {
		t.Fatal("missing Go runtime metrics")
	}
	if runtime.GOOS == "windows" && (!first.NativeAvailable || first.SystemTotalBytes == 0 || first.PrivateBytes == 0) {
		t.Fatalf("missing native metrics: %+v", first)
	}
	for i := 0; i < 50; i++ {
		sampler.Sample()
	}
	last := sampler.Sample()
	if len(last.Processes) > 12 {
		t.Fatal("unbounded process details")
	}
	if first.NativeAvailable && last.Handles > first.Handles+4 {
		t.Fatalf("sampling leaked handles: %d -> %d", first.Handles, last.Handles)
	}
	if _, err := json.Marshal(last); err != nil {
		t.Fatal(err)
	}
	t.Logf("native_available=%v private_mb=%.1f working_set_mb=%.1f heap_mb=%.1f goroutines=%d handles=%d system_used=%.1f%%", last.NativeAvailable, float64(last.PrivateBytes)/(1<<20), float64(last.WorkingSetBytes)/(1<<20), float64(last.HeapAllocBytes)/(1<<20), last.Goroutines, last.Handles, last.SystemMemoryPercent)
}

func BenchmarkResourceSnapshot(b *testing.B) {
	var sampler Sampler
	for i := 0; i < b.N; i++ {
		sampler.Sample()
	}
}
