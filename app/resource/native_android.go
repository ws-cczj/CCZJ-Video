//go:build android

package resource

import (
	"os"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

// Android isolates WebView renderers under other UIDs. These are app-process
// measurements, never a claim to include the renderer/GPU process tree.
func nativeSample() ([]Process, uint64, uint64, []string) {
	notes := []string{"Android app process only; isolated WebView renderer/GPU processes are excluded. PSS and private resident memory are not Windows private commit."}
	status, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return nil, 0, 0, append(notes, "App resident memory unavailable: /proc/self/status cannot be read")
	}
	values := androidMemoryFields(status)
	rss, ok := values["VmRSS"]
	if !ok {
		return nil, 0, 0, append(notes, "App resident memory unavailable: VmRSS is absent")
	}
	process := Process{PID: os.Getpid(), Name: "Android app", WorkingSetBytes: rss}
	if rollup, readErr := os.ReadFile("/proc/self/smaps_rollup"); readErr == nil {
		fields := androidMemoryFields(rollup)
		if value, found := fields["Pss"]; found {
			process.ProportionalBytes = &value
		}
		clean, hasClean := fields["Private_Clean"]
		dirty, hasDirty := fields["Private_Dirty"]
		if hasClean && hasDirty {
			private := clean + dirty + fields["Private_Hugetlb"]
			process.PrivateResidentBytes = &private
		}
	} else {
		notes = append(notes, "PSS/private resident memory unavailable: smaps_rollup access is restricted")
	}
	if files, readErr := os.ReadDir("/proc/self/fd"); readErr == nil {
		count := uint32(len(files))
		process.FileDescriptors = &count
	}
	var usage unix.Rusage
	if unix.Getrusage(unix.RUSAGE_SELF, &usage) == nil {
		process.CPUTimeNS = uint64(usage.Utime.Sec+usage.Stime.Sec)*1e9 + uint64(usage.Utime.Usec+usage.Stime.Usec)*1e3
	}
	var total, available uint64
	if memory, readErr := os.ReadFile("/proc/meminfo"); readErr == nil {
		fields := androidMemoryFields(memory)
		total, available = fields["MemTotal"], fields["MemAvailable"]
		if available > total {
			available = total
		}
	}
	return []Process{process}, total, available, notes
}

// Only parse explicit kB counters, avoiding the unitless fields in status/stat.
func androidMemoryFields(data []byte) map[string]uint64 {
	fields := make(map[string]uint64)
	for _, line := range strings.Split(string(data), "\n") {
		parts := strings.Fields(line)
		if len(parts) != 3 || parts[2] != "kB" {
			continue
		}
		value, err := strconv.ParseUint(parts[1], 10, 64)
		if err == nil && value <= ^uint64(0)/1024 {
			fields[strings.TrimSuffix(parts[0], ":")] = value * 1024
		}
	}
	return fields
}
