// Package resource samples runtime and owned-process resources without a background worker.
package resource

import (
	"runtime"
	"sort"
	"sync"
	"time"
)

// Process describes one process in the application's process tree.
type Process struct {
	PID                  int     `json:"pid"`
	Name                 string  `json:"name"`
	WorkingSetBytes      uint64  `json:"working_set_bytes"`
	PrivateBytes         uint64  `json:"private_bytes"`
	Handles              uint32  `json:"handles"`
	CPUTimeNS            uint64  `json:"-"`
	CreatedAt100NS       uint64  `json:"-"`
	ProportionalBytes    *uint64 `json:"proportional_bytes"`
	PrivateResidentBytes *uint64 `json:"private_resident_bytes"`
	FileDescriptors      *uint32 `json:"file_descriptors"`
}

// Snapshot separates Go heap, native process memory, and system physical memory.
// Tree working sets can double-count shared pages; private bytes are committed memory.
type Snapshot struct {
	SampledAtMS          int64     `json:"sampled_at_ms"`
	HeapAllocBytes       uint64    `json:"heap_alloc_bytes"`
	HeapInuseBytes       uint64    `json:"heap_inuse_bytes"`
	HeapRetainedBytes    uint64    `json:"heap_retained_bytes"`
	GoSysBytes           uint64    `json:"go_sys_bytes"`
	HeapObjects          uint64    `json:"heap_objects"`
	NumGC                uint32    `json:"num_gc"`
	LastGCPauseMS        float64   `json:"last_gc_pause_ms"`
	Goroutines           int       `json:"goroutines"`
	NativeAvailable      bool      `json:"native_available"`
	NativeScope          string    `json:"native_scope"`
	ProportionalBytes    *uint64   `json:"proportional_bytes"`
	PrivateResidentBytes *uint64   `json:"private_resident_bytes"`
	FileDescriptors      *uint32   `json:"file_descriptors"`
	WorkingSetBytes      uint64    `json:"working_set_bytes"`
	PrivateBytes         uint64    `json:"private_bytes"`
	TreeWorkingSetBytes  uint64    `json:"tree_working_set_bytes"`
	TreePrivateBytes     uint64    `json:"tree_private_bytes"`
	SystemTotalBytes     uint64    `json:"system_total_bytes"`
	SystemAvailableBytes uint64    `json:"system_available_bytes"`
	SystemMemoryPercent  float64   `json:"system_memory_percent"`
	WorkingSetPercent    float64   `json:"working_set_percent"`
	CPUPercent           *float64  `json:"cpu_percent"`
	ProcessCount         int       `json:"process_count"`
	Handles              uint32    `json:"handles"`
	Processes            []Process `json:"processes"`
	Notes                []string  `json:"notes"`
}

// Sampler keeps only the previous process counters to derive machine-normalized CPU usage.
type Sampler struct {
	mu       sync.Mutex
	at       time.Time
	previous map[int]uint64
}

// Sample returns a bounded snapshot. It never forces GC or starts subprocesses.
func (s *Sampler) Sample() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	out := Snapshot{
		SampledAtMS: now.UnixMilli(), HeapAllocBytes: mem.HeapAlloc, HeapInuseBytes: mem.HeapInuse,
		HeapRetainedBytes: mem.HeapIdle - mem.HeapReleased, GoSysBytes: mem.Sys, HeapObjects: mem.HeapObjects,
		NumGC: mem.NumGC, Goroutines: runtime.NumGoroutine(), Notes: []string{}, Processes: []Process{},
	}
	if mem.NumGC > 0 {
		out.LastGCPauseMS = float64(mem.PauseNs[(mem.NumGC-1)%256]) / 1e6
	}
	processes, total, available, notes := nativeSample()
	out.Notes = append(out.Notes, notes...)
	out.SystemTotalBytes, out.SystemAvailableBytes = total, available
	if total > 0 {
		out.SystemMemoryPercent = 100 * float64(total-available) / float64(total)
	}
	var delta uint64
	next := make(map[int]uint64, len(processes))
	for _, p := range processes {
		out.TreePrivateBytes += p.PrivateBytes
		out.TreeWorkingSetBytes += p.WorkingSetBytes
		out.Handles += p.Handles
		next[p.PID] = p.CPUTimeNS
		if previous, ok := s.previous[p.PID]; ok && p.CPUTimeNS >= previous {
			delta += p.CPUTimeNS - previous
		}
	}
	if len(processes) > 0 {
		out.NativeAvailable = true
		out.NativeScope = "process_tree"
		if runtime.GOOS == "android" {
			out.NativeScope = "app_process"
		}
		out.ProportionalBytes = processes[0].ProportionalBytes
		out.PrivateResidentBytes = processes[0].PrivateResidentBytes
		out.FileDescriptors = processes[0].FileDescriptors
		out.PrivateBytes, out.WorkingSetBytes = processes[0].PrivateBytes, processes[0].WorkingSetBytes
		if total > 0 {
			out.WorkingSetPercent = 100 * float64(out.WorkingSetBytes) / float64(total)
		}
		if !s.at.IsZero() && now.After(s.at) {
			cpu := 100 * float64(delta) / float64(now.Sub(s.at).Nanoseconds()) / float64(runtime.NumCPU())
			if cpu > 100 {
				cpu = 100
			}
			out.CPUPercent = &cpu
		}
	}
	s.at, s.previous = now, next
	out.ProcessCount = len(processes)
	sort.Slice(processes, func(i, j int) bool { return processes[i].PrivateBytes > processes[j].PrivateBytes })
	if len(processes) > 12 {
		processes = processes[:12]
	}
	out.Processes = append(out.Processes, processes...)
	return out
}
