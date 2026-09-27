package service

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
)

// directDownloadManifest is the durable source of truth for a parallel
// download. The sparse .part file is deliberately not used as a progress
// indicator: WriteAt can extend it beyond ranges that have actually finished.
type directDownloadManifest struct {
	URL    string             `json:"url"`
	Total  int64              `json:"total"`
	Chunks []directChunkState `json:"chunks"`
}

type directChunkState struct {
	Start int64 `json:"start"`
	End   int64 `json:"end"`
	Done  int64 `json:"done"`
}

func directManifestPath(tmpPath string) string {
	return tmpPath + ".ranges.json"
}

func newDirectDownloadManifest(url string, total int64, connections int) (*directDownloadManifest, error) {
	if total <= 0 || connections <= 0 {
		return nil, fmt.Errorf("invalid direct download size or connection count")
	}
	if int64(connections) > total {
		connections = int(total)
	}
	chunks := make([]directChunkState, 0, connections)
	baseSize := total / int64(connections)
	remainder := total % int64(connections)
	var start int64
	for i := 0; i < connections; i++ {
		size := baseSize
		if int64(i) < remainder {
			size++
		}
		chunks = append(chunks, directChunkState{Start: start, End: start + size - 1})
		start += size
	}
	m := &directDownloadManifest{URL: url, Total: total, Chunks: chunks}
	return m, m.validate(url, total)
}

func loadDirectDownloadManifest(path, url string, total int64) (*directDownloadManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var manifest directDownloadManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("decode direct download manifest: %w", err)
	}
	if err := manifest.validate(url, total); err != nil {
		return nil, err
	}
	return &manifest, nil
}

func saveDirectDownloadManifest(path string, manifest *directDownloadManifest) error {
	data, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, data, 0644); err != nil {
		return err
	}
	if err := os.Rename(temporary, path); err == nil {
		return nil
	}
	// Windows does not always replace an existing file during Rename. Losing a
	// manifest is safe (the next run falls back to a fresh/single download), so
	// prefer a best-effort replacement over recording progress ahead of disk.
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Rename(temporary, path)
}

func (m *directDownloadManifest) validate(url string, total int64) error {
	if m == nil || m.URL != url || m.Total != total || total <= 0 || len(m.Chunks) == 0 {
		return fmt.Errorf("direct download manifest does not match request")
	}
	var nextStart int64
	for _, chunk := range m.Chunks {
		if chunk.Start != nextStart || chunk.End < chunk.Start || chunk.Done < 0 || chunk.Done > chunk.End-chunk.Start+1 {
			return fmt.Errorf("invalid direct download range manifest")
		}
		nextStart = chunk.End + 1
	}
	if nextStart != total {
		return fmt.Errorf("direct download manifest does not cover file")
	}
	return nil
}

func (m *directDownloadManifest) downloaded() int64 {
	var total int64
	for _, chunk := range m.Chunks {
		total += chunk.Done
	}
	return total
}

func (m *directDownloadManifest) complete() bool {
	return m.downloaded() == m.Total
}

func (m *directDownloadManifest) requiredFileSize() int64 {
	var size int64
	for _, chunk := range m.Chunks {
		if chunk.Done == 0 {
			continue
		}
		if end := chunk.Start + chunk.Done; end > size {
			size = end
		}
	}
	return size
}

func (m *directDownloadManifest) clone() *directDownloadManifest {
	copyOfChunks := append([]directChunkState(nil), m.Chunks...)
	return &directDownloadManifest{URL: m.URL, Total: m.Total, Chunks: copyOfChunks}
}

// validatePartialContent proves that the upstream actually honoured an exact
// byte range. Accepting a 200 response here can silently corrupt sparse files.
func validatePartialContent(resp *http.Response, wantStart, wantEnd, wantTotal int64) error {
	if resp.StatusCode != http.StatusPartialContent {
		return fmt.Errorf("range request returned HTTP %d", resp.StatusCode)
	}
	start, end, total, err := parseContentRange(resp.Header.Get("Content-Range"))
	if err != nil {
		return err
	}
	if start != wantStart || end != wantEnd || (wantTotal > 0 && total != wantTotal) {
		return fmt.Errorf("unexpected Content-Range bytes %d-%d/%d", start, end, total)
	}
	if resp.ContentLength >= 0 && resp.ContentLength != wantEnd-wantStart+1 {
		return fmt.Errorf("unexpected range body size %d", resp.ContentLength)
	}
	return nil
}

func validateResumeContent(resp *http.Response, wantStart, wantTotal int64) (int64, error) {
	if resp.StatusCode != http.StatusPartialContent {
		return 0, fmt.Errorf("range request returned HTTP %d", resp.StatusCode)
	}
	start, end, total, err := parseContentRange(resp.Header.Get("Content-Range"))
	if err != nil {
		return 0, err
	}
	if start != wantStart || end < start || (wantTotal > 0 && total != wantTotal) {
		return 0, fmt.Errorf("unexpected Content-Range bytes %d-%d/%d", start, end, total)
	}
	if resp.ContentLength >= 0 && resp.ContentLength != end-start+1 {
		return 0, fmt.Errorf("unexpected range body size %d", resp.ContentLength)
	}
	return total, nil
}

func parseContentRange(raw string) (int64, int64, int64, error) {
	parts := strings.Fields(strings.TrimSpace(raw))
	if len(parts) != 2 || !strings.EqualFold(parts[0], "bytes") {
		return 0, 0, 0, fmt.Errorf("invalid Content-Range")
	}
	rangeAndTotal := strings.SplitN(parts[1], "/", 2)
	if len(rangeAndTotal) != 2 || rangeAndTotal[1] == "*" {
		return 0, 0, 0, fmt.Errorf("invalid Content-Range")
	}
	limits := strings.SplitN(rangeAndTotal[0], "-", 2)
	if len(limits) != 2 {
		return 0, 0, 0, fmt.Errorf("invalid Content-Range")
	}
	start, startErr := strconv.ParseInt(limits[0], 10, 64)
	end, endErr := strconv.ParseInt(limits[1], 10, 64)
	total, totalErr := strconv.ParseInt(rangeAndTotal[1], 10, 64)
	if startErr != nil || endErr != nil || totalErr != nil || start < 0 || end < start || total <= end {
		return 0, 0, 0, fmt.Errorf("invalid Content-Range")
	}
	return start, end, total, nil
}
