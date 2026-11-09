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
	source, digest, filename, contentType string
}

var catalog = []definition{{Entry: Entry{ID: "danmaku-source-han", Version: "a4f7cf94-7087698d", Size: 7995716},
	source: "https://raw.githubusercontent.com/adobe-fonts/source-han-sans/a4f7cf94edfb9d7ffbdfc4841de276358bd7e0f2/Variable/WOFF2/OTF/Subset/SourceHanSansCN-VF.otf.woff2",
	digest: "7087698d52240614659957608d8c4ad446759aee3208409dc9f566412e7af8f4", filename: "font.woff2", contentType: "font/woff2"}}

// Service serializes bounded install/read/delete work. Callers own cancellation;
// the application's host-request lifetime drains I/O before shutdown.
type Service struct {
	root   func() string
	client *http.Client
	gate   chan struct{}
}

func New(root func() string, client *http.Client) *Service {
	return &Service{root: root, client: client, gate: make(chan struct{}, 1)}
}
func (s *Service) Close() { s.client.CloseIdleConnections() }
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
func (s *Service) Install(ctx context.Context, id string) (row Entry, failure error) {
	d, err := find(id)
	if err != nil {
		return row, err
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	release, err := s.lock(ctx)
	if err != nil {
		return row, err
	}
	defer release()
	started := time.Now()
	var received int64
	defer func() {
		fields := applog.Fields{"resource_id": id, "duration_ms": time.Since(started).Milliseconds(), "expected_bytes": d.Size, "bytes": received, "failed": failure != nil}
		if failure != nil {
			fields["code"] = string(apperror.CodeOf(failure))
		}
		operation.Stage(ctx, "resources.install.finished", fields)
	}()
	if row, err = s.inspect(ctx, d); err != nil {
		return row, err
	}
	if row.Installed {
		return row, nil
	}
	operation.Stage(ctx, "resources.install.started", applog.Fields{"resource_id": id, "bytes": d.Size})
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, d.source, nil)
	if err != nil {
		return row, err
	}
	request.Header.Set("Accept-Encoding", "identity")
	response, err := s.client.Do(request)
	if err != nil {
		return row, apperror.Wrap(apperror.Unavailable, err, "资源下载失败")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return row, apperror.New(apperror.Unavailable, "资源服务器暂不可用")
	}
	if response.ContentLength > 0 && response.ContentLength != d.Size {
		return row, apperror.New(apperror.Corrupt, "资源长度不匹配")
	}
	target := s.path(d)
	if err = os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		return row, apperror.Wrap(apperror.Storage, err, "无法创建资源目录")
	}
	// Interrupted process exits may leave only this resource's owned part files.
	// Enumerate in small batches, without touching unrelated installation data.
	directory, err := os.Open(filepath.Dir(target))
	if err != nil {
		return row, apperror.Wrap(apperror.Storage, err, "无法读取资源目录")
	}
	for {
		if err = ctx.Err(); err != nil {
			_ = directory.Close()
			return row, err
		}
		entries, readErr := directory.ReadDir(32)
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasPrefix(entry.Name(), "."+d.filename+".") && strings.HasSuffix(entry.Name(), ".part") {
				if err = os.Remove(filepath.Join(filepath.Dir(target), entry.Name())); err != nil {
					_ = directory.Close()
					return row, apperror.Wrap(apperror.Storage, err, "无法清理未完成资源")
				}
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			_ = directory.Close()
			return row, readErr
		}
	}
	_ = directory.Close()
	err = files.WriteAtomic(ctx, target, func(writer io.Writer) error {
		hash := sha256.New()
		n, err := files.CopyContext(ctx, io.MultiWriter(writer, hash), io.LimitReader(response.Body, d.Size+1))
		received = n
		if err != nil {
			return err
		}
		if n != d.Size || hex.EncodeToString(hash.Sum(nil)) != d.digest {
			return apperror.New(apperror.Corrupt, "资源校验失败")
		}
		// Only an already-invalid destination can reach this branch. Never remove a
		// working resource before a replacement has downloaded and verified.
		if err = os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	})
	if err != nil {
		return row, apperror.Wrap(apperror.Storage, err, "资源安装失败")
	}
	return s.inspect(ctx, d)
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
