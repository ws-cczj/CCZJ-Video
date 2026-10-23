package operation

import (
	"sort"
	"sync"

	"cczjVideo/app/apperror"
)

// Stat aggregates completed typed operations without retaining request data.
type Stat struct {
	Name      string `json:"name"`
	Mode      string `json:"mode"`
	Pipeline  string `json:"pipeline"`
	Succeeded uint64 `json:"succeeded"`
	Failed    uint64 `json:"failed"`
	Cancelled uint64 `json:"cancelled"`
	TotalMS   int64  `json:"total_ms"`
	MaxMS     int64  `json:"max_ms"`
}

// Metrics is app-owned; names are fixed by service adapters, never user input.
type Metrics struct {
	mu   sync.Mutex
	rows map[Meta]Stat
}

func (m *Metrics) Record(result Result) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.rows == nil {
		m.rows = make(map[Meta]Stat)
	}
	row, exists := m.rows[result.Meta]
	if !exists && len(m.rows) >= 128 {
		return
	}
	row.Name, row.Mode = result.Name, result.Mode
	row.Pipeline = result.Pipeline
	switch {
	case result.Err == nil:
		row.Succeeded++
	case apperror.CodeOf(result.Err) == apperror.Cancelled:
		row.Cancelled++
	default:
		row.Failed++
	}
	ms := result.Duration.Milliseconds()
	row.TotalMS += ms
	row.MaxMS = max(row.MaxMS, ms)
	m.rows[result.Meta] = row
}
func (m *Metrics) Snapshot(mode string) []Stat {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Stat{}
	for _, row := range m.rows {
		if row.Mode == mode || row.Mode == "shared" {
			out = append(out, row)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
