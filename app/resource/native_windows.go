//go:build windows

package resource

import (
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	getMemoryInfo   = windows.NewLazySystemDLL("psapi.dll").NewProc("GetProcessMemoryInfo")
	getMemoryStatus = windows.NewLazySystemDLL("kernel32.dll").NewProc("GlobalMemoryStatusEx")
	getHandleCount  = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetProcessHandleCount")
)

type memoryCounters struct {
	Size, PageFaults                                                     uint32
	PeakWorkingSet, WorkingSet, PeakPagedPool, PagedPool                 uintptr
	PeakNonPagedPool, NonPagedPool, Pagefile, PeakPagefile, PrivateUsage uintptr
}

type memoryStatus struct {
	Length, Load                                       uint32
	TotalPhys, AvailPhys, TotalPageFile, AvailPageFile uint64
	TotalVirtual, AvailVirtual, AvailExtendedVirtual   uint64
}

func readProcess(pid uint32, name string) (Process, error) {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_INFORMATION|windows.PROCESS_VM_READ, false, pid)
	if err != nil {
		return Process{}, err
	}
	defer windows.CloseHandle(handle)
	var memory memoryCounters
	memory.Size = uint32(unsafe.Sizeof(memory))
	if ok, _, err := getMemoryInfo.Call(uintptr(handle), uintptr(unsafe.Pointer(&memory)), uintptr(memory.Size)); ok == 0 {
		return Process{}, err
	}
	p := Process{PID: int(pid), Name: name, PrivateBytes: uint64(memory.PrivateUsage), WorkingSetBytes: uint64(memory.WorkingSet)}
	_, _, _ = getHandleCount.Call(uintptr(handle), uintptr(unsafe.Pointer(&p.Handles)))
	var created, exited, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(handle, &created, &exited, &kernel, &user); err != nil {
		return Process{}, fmt.Errorf("process creation time unavailable: %w", err)
	}
	p.CreatedAt100NS = uint64(created.HighDateTime)<<32 | uint64(created.LowDateTime)
	p.CPUTimeNS = ((uint64(kernel.HighDateTime)<<32 | uint64(kernel.LowDateTime)) + (uint64(user.HighDateTime)<<32 | uint64(user.LowDateTime))) * 100
	return p, nil
}

func nativeSample() ([]Process, uint64, uint64, []string) {
	notes := []string{}
	var state memoryStatus
	state.Length = uint32(unsafe.Sizeof(state))
	if ok, _, err := getMemoryStatus.Call(uintptr(unsafe.Pointer(&state))); ok == 0 {
		notes = append(notes, fmt.Sprintf("System memory unavailable: %v", err))
	}
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, state.TotalPhys, state.AvailPhys, append(notes, fmt.Sprintf("Process snapshot unavailable: %v", err))
	}
	defer windows.CloseHandle(snapshot)
	var entries []processTreeEntry
	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	for err := windows.Process32First(snapshot, &entry); err == nil; err = windows.Process32Next(snapshot, &entry) {
		entries = append(entries, processTreeEntry{entry.ProcessID, entry.ParentProcessID, windows.UTF16ToString(entry.ExeFile[:])})
	}
	root := uint32(os.Getpid())
	processes, skipped, stale := collectProcessTree(root, entries, readProcess)
	if len(processes) == 0 {
		return nil, state.TotalPhys, state.AvailPhys, append(notes, "Current process memory unavailable")
	}
	if skipped > 0 {
		notes = append(notes, fmt.Sprintf("%d descendant processes exited or could not be read; tree totals are partial", skipped))
	}
	if stale > 0 {
		notes = append(notes, fmt.Sprintf("%d older processes excluded because their recorded parent PID was reused", stale))
	}
	return processes, state.TotalPhys, state.AvailPhys, notes
}
