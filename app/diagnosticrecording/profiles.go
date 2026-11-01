package diagnosticrecording

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime/pprof"
	"sort"
	"strings"
	"time"
)

// ProfileStatus reports an explicitly requested host-process capture.
type ProfileStatus struct {
	Mode        string `json:"mode"`
	Kind        string `json:"kind"`
	Active      bool   `json:"active"`
	Path        string `json:"path"`
	Error       string `json:"error"`
	StartedAtMS int64  `json:"started_at_ms"`
	EndedAtMS   int64  `json:"ended_at_ms"`
}

type profileWriter struct {
	out       io.Writer
	remaining int64
	failure   error
}

func (w *profileWriter) Write(p []byte) (int, error) {
	if int64(len(p)) > w.remaining {
		w.failure = errors.New("性能剖析超过 8 MiB 限制")
		return 0, w.failure
	}
	n, err := w.out.Write(p)
	w.remaining -= int64(n)
	if err != nil {
		w.failure = err
	}
	return n, err
}

func (r *Recorder) cancelProfile() {
	r.profileMu.Lock()
	defer r.profileMu.Unlock()
	if r.profileCancel != nil {
		r.profileCancel()
	}
}

// CaptureProfile schedules one bounded profile. CPU capture lasts at most 15 seconds.
func (r *Recorder) CaptureProfile(kind, mode string) (ProfileStatus, error) {
	if kind != "cpu" && kind != "heap" && kind != "goroutine" {
		return ProfileStatus{}, ErrInvalidMarker
	}
	r.profileMu.Lock()
	defer r.profileMu.Unlock()
	if r.profile.Active {
		return r.profile, errors.New("性能剖析正在进行")
	}
	if err := os.MkdirAll(r.root(), 0755); err != nil {
		return ProfileStatus{}, err
	}
	pruneCaptures(r.root(), "profile-", ".pprof", 2, "")
	kept := r.profileFiles[:0]
	for _, profile := range r.profileFiles {
		if _, err := os.Stat(profile.Path); err == nil {
			kept = append(kept, profile)
		}
	}
	r.profileFiles = kept
	file, err := os.CreateTemp(r.root(), "profile-"+kind+"-*.pprof")
	if err != nil {
		return ProfileStatus{}, err
	}
	ctx, cancel := context.WithTimeout(r.group.Context(), 15*time.Second)
	r.profileCancel = cancel
	r.profile = ProfileStatus{Mode: mode, Kind: kind, Active: true, StartedAtMS: time.Now().UnixMilli()}
	if !r.group.Go("diagnostics-profile", func(context.Context) {
		defer cancel()
		writer := &profileWriter{out: file, remaining: 8 << 20}
		var failure error
		if kind == "cpu" {
			failure = pprof.StartCPUProfile(writer)
			if failure == nil {
				<-ctx.Done()
				pprof.StopCPUProfile()
			}
		} else if ctx.Err() == nil {
			failure = pprof.Lookup(kind).WriteTo(writer, 0)
		} else {
			failure = ctx.Err()
		}
		if failure == nil {
			failure = writer.failure
		}
		if closeErr := file.Close(); failure == nil {
			failure = closeErr
		}
		r.profileMu.Lock()
		defer r.profileMu.Unlock()
		r.profile.Active = false
		r.profile.EndedAtMS = time.Now().UnixMilli()
		r.profileCancel = nil
		if failure != nil {
			r.profile.Error = failure.Error()
			_ = os.Remove(file.Name())
		} else {
			r.profile.Path = file.Name()
			r.profileFiles = append(r.profileFiles, r.profile)
			if len(r.profileFiles) > 3 {
				r.profileFiles = r.profileFiles[len(r.profileFiles)-3:]
			}
		}
	}) {
		cancel()
		_ = file.Close()
		_ = os.Remove(file.Name())
		r.profile.Active = false
		r.profileCancel = nil
		return r.profile, errors.New("应用正在退出")
	}
	return r.profile, nil
}

// Only automatically created raw captures are rotated; user-exported bundles remain intact.
func pruneCaptures(root, prefix, suffix string, keep int, protected string) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	type capture struct {
		path string
		at   time.Time
	}
	var files []capture
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), prefix) || !strings.HasSuffix(entry.Name(), suffix) {
			continue
		}
		info, err := entry.Info()
		if err == nil {
			files = append(files, capture{filepath.Join(root, entry.Name()), info.ModTime()})
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].at.After(files[j].at) })
	for index, file := range files {
		if index >= keep && file.path != protected {
			_ = os.Remove(file.path)
		}
	}
}
