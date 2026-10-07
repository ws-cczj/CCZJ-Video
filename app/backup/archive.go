package backup

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"cczjVideo/app/apperror"
	"cczjVideo/app/applog"
	"cczjVideo/app/db"

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
	path, err := s.archivePath(dataDir, name)
	if err != nil {
		return Result{}, err
	}
	payload, err := readArchive(path)
	if err != nil {
		return Result{}, err
	}
	applog.InfoFields("restoring schema archive", applog.Fields{
		"archive":   name,
		"favorites": len(payload.Favorites),
		"history":   len(payload.History),
	})
	return s.Import(payload, "archive:"+name)
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
func readArchive(path string) (Payload, error) {
	database, closeDatabase, err := readOnlyCopy(path)
	if err != nil {
		return Payload{}, err
	}
	defer closeDatabase()

	payload := Payload{Kind: payloadKind, Version: payloadVersion, Exported: time.Now().Format(time.RFC3339)}
	settings, err := db.ReadAllSettings(database)
	if err != nil {
		return Payload{}, apperror.Wrap(apperror.Corrupt, err, "archive has no readable settings table")
	}
	favorites, err := db.ReadFavorites(database)
	if err != nil {
		return Payload{}, apperror.Wrap(apperror.Corrupt, err, "archive has no readable favorites")
	}
	history, err := db.ReadHistory(database)
	if err != nil {
		return Payload{}, apperror.Wrap(apperror.Corrupt, err, "archive has no readable history")
	}
	videos, err := db.ReadLinkedVideoMeta(database)
	if err != nil {
		return Payload{}, apperror.Wrap(apperror.Corrupt, err, "archive has no readable metadata")
	}
	// 归档里的源只有定义没有目录，且合并阶段对本机已有的源不动，
	// 所以这里读的是归档自己的 sources 表。
	sources, err := db.ReadSources(database)
	if err != nil {
		return Payload{}, apperror.Wrap(apperror.Corrupt, err, "archive has no readable sources")
	}
	payload.Settings = settings
	payload.Favorites = favorites
	payload.History = history
	payload.Videos = videos
	payload.Sources = sources
	return payload, nil
}

// readOnlyCopy 把源库连同 WAL/SHM 复制成一份临时副本，再以 query_only 打开。
// 返回的关闭函数负责关掉句柄并删掉副本：副本落在源文件旁边的 .restore-scratch 里，
// 用完不留下第二份库。
func readOnlyCopy(path string) (*sqlx.DB, func(), error) {
	scratchDir := filepath.Join(filepath.Dir(path), ".restore-scratch")
	if err := os.MkdirAll(scratchDir, 0755); err != nil {
		return nil, nil, apperror.Wrap(apperror.Storage, err, "create scratch directory")
	}
	scratch := filepath.Join(scratchDir, "read-"+fmt.Sprintf("%d", time.Now().UnixNano())+".db")
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if err := copyFile(path+suffix, scratch+suffix); err != nil {
			_ = os.RemoveAll(scratchDir)
			return nil, nil, err
		}
	}
	database, err := sqlx.Connect("sqlite", scratch+"?_pragma=busy_timeout(3000)&_pragma=query_only(TRUE)")
	if err != nil {
		_ = os.RemoveAll(scratchDir)
		return nil, nil, apperror.Wrap(apperror.Corrupt, err, "open copy read-only")
	}
	return database, func() {
		_ = database.Close()
		_ = os.RemoveAll(scratchDir)
	}, nil
}

func copyFile(from, to string) error {
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
	if _, err := io.Copy(target, source); err != nil {
		_ = target.Close()
		return apperror.Wrap(apperror.Storage, err, fmt.Sprintf("copy %s", from))
	}
	return target.Close()
}
