package installedresources

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"cczjVideo/app/apperror"
	"cczjVideo/app/applog"
	"cczjVideo/app/files"
	"cczjVideo/app/operation"
)

// Catalog entries are reviewed application data, never caller-supplied URLs or paths.
type Entry struct {
	ID        string `json:"id"`
	Version   string `json:"version"`
	Size      int64  `json:"size"`
	Installed bool   `json:"installed"`
	Corrupt   bool   `json:"corrupt"`
	URL       string `json:"url"`
}
type Catalog struct {
	Directory string  `json:"directory"`
	Items     []Entry `json:"items"`
}
type definition struct {
	Entry
	digest, filename, contentType string
}

var catalog = []definition{{Entry: Entry{ID: "danmaku-source-han", Version: "a4f7cf94-7087698d", Size: 7995716},
	digest: "7087698d52240614659957608d8c4ad446759aee3208409dc9f566412e7af8f4", filename: "font.woff2", contentType: "font/woff2"}}

// Service owns legacy resource discovery, compatibility reads and explicit removal.
type Service struct {
	root func() string
	gate chan struct{}
}

func New(root func() string) *Service {
	return &Service{root: root, gate: make(chan struct{}, 1)}
}
func (s *Service) lock(ctx context.Context) (func(), error) {
	select {
	case s.gate <- struct{}{}:
		return func() { <-s.gate }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func (s *Service) directory() string { return filepath.Join(s.root(), "resources") }
func (s *Service) path(d definition) string {
	return filepath.Join(s.directory(), d.ID, d.Version, d.filename)
}
func find(id string) (definition, error) {
	for _, d := range catalog {
		if d.ID == id {
			return d, nil
		}
	}
	return definition{}, apperror.New(apperror.NotFound, "资源不存在")
}
func (s *Service) inspect(ctx context.Context, d definition) (Entry, error) {
	row := d.Entry
	info, err := os.Lstat(s.path(d))
	if errors.Is(err, os.ErrNotExist) {
		return row, nil
	}
	if err != nil {
		return row, apperror.Wrap(apperror.Storage, err, "无法读取安装资源")
	}
	if !info.Mode().IsRegular() || info.Size() != d.Size {
		row.Corrupt = true
		return row, nil
	}
	file, err := os.Open(s.path(d))
	if err != nil {
		return row, apperror.Wrap(apperror.Storage, err, "无法读取安装资源")
	}
	defer file.Close()
	hash := sha256.New()
	if _, err = files.CopyContext(ctx, hash, io.LimitReader(file, d.Size+1)); err != nil {
		return row, err
	}
	if hex.EncodeToString(hash.Sum(nil)) != d.digest {
		row.Corrupt = true
		return row, nil
	}
	row.Installed = true
	row.URL = "/__cczj/resource/" + d.ID + "/" + d.Version + "/" + d.filename
	return row, nil
}
func (s *Service) List(ctx context.Context) (Catalog, error) {
	release, err := s.lock(ctx)
	if err != nil {
		return Catalog{}, err
	}
	defer release()
	result := Catalog{Directory: s.directory(), Items: make([]Entry, 0, len(catalog))}
	for _, d := range catalog {
		row, err := s.inspect(ctx, d)
		if err != nil {
			return Catalog{}, err
		}
		result.Items = append(result.Items, row)
	}
	return result, nil
}

// Install is retained for older callers that still know the optional font.
func (s *Service) Install(ctx context.Context, id string) (Entry, error) {
	d, err := find(id)
	if err != nil {
		return Entry{}, err
	}
	release, err := s.lock(ctx)
	if err != nil {
		return Entry{}, err
	}
	defer release()
	row, err := s.inspect(ctx, d)
	if err != nil || row.Installed {
		return row, err
	}
	return row, apperror.New(apperror.Unsupported, "弹幕字体已内置，无需安装")
}
func (s *Service) Remove(ctx context.Context, id string) error {
	d, err := find(id)
	if err != nil {
		return err
	}
	release, err := s.lock(ctx)
	if err != nil {
		return err
	}
	defer release()
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = os.Remove(s.path(d)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return apperror.Wrap(apperror.Storage, err, "资源删除失败")
	}
	operation.Stage(ctx, "resources.removed", applog.Fields{"resource_id": id})
	return nil
}
func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "Method not allowed", 405)
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/__cczj/resource/"), "/")
	if len(parts) != 3 {
		http.NotFound(w, r)
		return
	}
	d, err := find(parts[0])
	if err != nil || parts[1] != d.Version || parts[2] != d.filename {
		http.NotFound(w, r)
		return
	}
	release, err := s.lock(r.Context())
	if err != nil {
		return
	}
	defer release()
	row, err := s.inspect(r.Context(), d)
	if err != nil || !row.Installed {
		http.NotFound(w, r)
		return
	}
	file, err := os.Open(s.path(d))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()
	w.Header().Set("Content-Type", d.contentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	http.ServeContent(w, r, d.filename, time.Time{}, file)
}
