package download

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

// Checkpoint is the durable download state. Its JSON fields also read the legacy
// downloads.json format; transport clients and cancellation functions are runtime-only.
type Checkpoint struct {
	TaskId         string    `json:"task_id"`
	Url            string    `json:"url"`
	Filename       string    `json:"filename"`
	SavePath       string    `json:"save_path"`
	Total          int64     `json:"total"`
	Downloaded     int64     `json:"downloaded"`
	Status         string    `json:"status"`
	SegIndex       int       `json:"seg_index"`
	IsM3u8         bool      `json:"is_m3u8"`
	Segments       []string  `json:"segments,omitempty"`
	SegmentDetails []Segment `json:"segment_details,omitempty"`
	StartTime      int64     `json:"start_time"`
	EndTime        int64     `json:"end_time,omitempty"`
	ErrorMsg       string    `json:"error_msg,omitempty"`
	HasTotal       bool      `json:"has_total"`
	FinalSize      int64     `json:"final_size,omitempty"`
	BoundarySynced bool      `json:"boundary_synced,omitempty"`
}

// CheckpointStore owns atomic task writes, deletion and one-time legacy import.
// Restore preserves terminal states and reconciles interrupted file publication.
type CheckpointStore interface {
	Save(context.Context, Checkpoint) error
	Delete(context.Context, string) error
	Restore(context.Context, string) ([]Checkpoint, error)
}

// Validate rejects malformed persisted state without discarding the source file.
func (c Checkpoint) Validate() error {
	if strings.TrimSpace(c.TaskId) == "" || strings.TrimSpace(c.Url) == "" || !filepath.IsAbs(c.SavePath) {
		return fmt.Errorf("download checkpoint requires an ID, URL and absolute destination")
	}
	if c.Downloaded < 0 || c.SegIndex < 0 || c.FinalSize < 0 {
		return fmt.Errorf("download checkpoint contains negative progress")
	}
	switch c.Status {
	case "queued", "downloading", "paused", "error", "cancelled", "done", "finalizing":
	default:
		return fmt.Errorf("unknown download checkpoint state %q", c.Status)
	}
	return nil
}
