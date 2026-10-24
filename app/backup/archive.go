package backup

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"cczjVideo/app/apperror"
	"cczjVideo/app/applog"
	"cczjVideo/app/db"
	"cczjVideo/app/files"

	"github.com/jmoiron/sqlx"
)

// archiveDirName 是 db.backupBeforeMigrations 放迁移前快照的目录。
// 那些快照一直是死数据：没有导入通道，改坏了 schema 也只能看着。
// 这里把它们接回同一个合并引擎——注意是「读出再合并」，不是文件替换，
// 所以恢复归档永远不会拿旧库盖掉用户现在库里的东西。
const archiveDirName = "schema-backups"

// ArchiveInfo 描述一个可恢复的迁移归档。
type ArchiveInfo struct {
	Name       string `json:"name"`
	ModifiedAt string `json:"modified_at"`
	SizeBytes  int64  `json:"size_bytes"`
}

// Archives lists the snapshot databases, newest first.
func (s *Service) Archives(dataDir string) ([]ArchiveInfo, error) {
	dir := filepath.Join(dataDir, archiveDirName)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []ArchiveInfo{}, nil
		}
		return nil, apperror.Wrap(apperror.Storage, err, "read archive directory")
	}
	archives := make([]ArchiveInfo, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".db") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		archives = append(archives, ArchiveInfo{
			Name:       entry.Name(),
			ModifiedAt: info.ModTime().Format("2006-01-02 15:04:05"),
			SizeBytes:  info.Size(),
		})
	}
	sort.Slice(archives, func(i, j int) bool { return archives[i].Name > archives[j].Name })
	return archives, nil
}

// RestoreArchive merges one snapshot into the live database. The caller passes
// only a file name: the path is rebuilt under dataDir/schema-backups, so the
// frontend cannot ask the backend to open an arbitrary database.
func (s *Service) RestoreArchive(dataDir, name string) (Result, error) {
	return s.RestoreArchiveContext(context.Background(), dataDir, name)
}
func (s *Service) RestoreArchiveContext(ctx context.Context, dataDir, name string) (Result, error) {
	path, err := s.archivePath(dataDir, name)
	if err != nil {
		return Result{}, err
	}
	payload, err := readArchiveContext(ctx, path)
	if err != nil {
		return Result{}, err
	}
	applog.InfoFields("restoring schema archive", applog.Fields{
		"archive":   name,
		"favorites": len(payload.Favorites),
		"history":   len(payload.History),
	})
	return s.ImportContext(ctx, payload, "archive:"+name)
}

func (s *Service) archivePath(dataDir, name string) (string, error) {
	cleaned := strings.TrimSpace(filepath.Base(name))
	if cleaned == "" || cleaned != name || filepath.Ext(cleaned) == "" ||
		!strings.EqualFold(filepath.Ext(cleaned), ".db") ||
		strings.ContainsAny(cleaned, `/\`) || cleaned == "." || cleaned == ".." {
		return "", apperror.Newf(apperror.Validation, "invalid archive name: %q", name)
	}
	dir := filepath.Join(dataDir, archiveDirName)
	path := filepath.Join(dir, cleaned)
	if !withinDir(path, dir) {
		return "", apperror.New(apperror.Validation, "archive is outside the snapshot directory")
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", apperror.Wrap(apperror.NotFound, err, "archive not found")
	}
	if info.IsDir() || info.Size() == 0 {
		return "", apperror.New(apperror.Corrupt, "archive is not a database file")
	}
	return path, nil
}

func withinDir(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// readArchive copies the snapshot into a scratch file and reads that with
// query_only. Reading the original in place would mean trusting its journal mode
// (a WAL snapshot cannot be opened read-only without touching its -shm file),
// and a copy is the only way to be sure the snapshot itself is never written.
func readArchive(path string) (Payload, error) { return readArchiveContext(context.Background(), path) }
func readArchiveContext(ctx context.Context, path string) (Payload, error) {
	database, closeDatabase, err := readOnlyCopyContext(ctx, path)
	if err != nil {
		return Payload{}, err
	}
	defer closeDatabase()
	snapshot, err := db.ReadBackupSnapshot(ctx, database)
	if err != nil {
		return Payload{}, apperror.Wrap(apperror.Corrupt, err, "read archive snapshot")
	}
	return Payload{Kind: payloadKind, Version: payloadVersion, Exported: time.Now().Format(time.RFC3339), Settings: snapshot.Settings, Sources: snapshot.Sources, Videos: snapshot.Videos, Favorites: snapshot.Favorites, History: snapshot.History, Reading: snapshot.Reading}, nil
}

// readOnlyCopy 把源库连同 WAL/SHM 复制成一份临时副本，再以 query_only 打开。
// 返回的关闭函数负责关掉句柄并删掉副本：副本落在源文件旁边的 .restore-scratch 里，
// 用完不留下第二份库。
func readOnlyCopy(path string) (*sqlx.DB, func(), error) {
	return readOnlyCopyContext(context.Background(), path)
}
func readOnlyCopyContext(ctx context.Context, path string) (*sqlx.DB, func(), error) {
	root := filepath.Join(filepath.Dir(path), ".restore-scratch")
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, nil, apperror.Wrap(apperror.Storage, err, "create scratch directory")
	}
	directory, err := os.MkdirTemp(root, "read-")
	if err != nil {
		return nil, nil, err
	}
	cleanup := func() { _ = os.RemoveAll(directory); _ = os.Remove(root) }
	scratch := filepath.Join(directory, "snapshot.db")
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if err := copyFileContext(ctx, path+suffix, scratch+suffix); err != nil {
			cleanup()
			return nil, nil, err
		}
	}
	database, err := sqlx.ConnectContext(ctx, "sqlite", scratch+"?_pragma=busy_timeout(3000)&_pragma=query_only(TRUE)")
	if err != nil {
		cleanup()
		return nil, nil, apperror.Wrap(apperror.Corrupt, err, "open copy read-only")
	}
	return database, func() { _ = database.Close(); cleanup() }, nil
}

func copyFile(from, to string) error { return copyFileContext(context.Background(), from, to) }
func copyFileContext(ctx context.Context, from, to string) error {
	if from == to {
		return nil
	}
	source, err := os.Open(from)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return apperror.Wrap(apperror.Storage, err, fmt.Sprintf("open %s", from))
	}
	defer source.Close()
	target, err := os.OpenFile(to, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return apperror.Wrap(apperror.Storage, err, fmt.Sprintf("create %s", to))
	}
	if _, err := files.CopyContext(ctx, target, source); err != nil {
		_ = target.Close()
		return apperror.Wrap(apperror.Storage, err, fmt.Sprintf("copy %s", from))
	}
	return target.Close()
}
