package service

import (
	"cczjVideo/app/apperror"
	"cczjVideo/app/applog"
	backupservice "cczjVideo/app/backup"
	fileservice "cczjVideo/app/files"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
)

// ======================== 全量备份：设置 / 收藏 / 历史 / 迁移归档恢复 ========================

// BackupExport writes settings, favorites, watch history and the metadata they
// point at into one Brotli-compressed JSON file. An empty destination puts the
// file under dataDir/exports and returns that path.
func (a *App) BackupExport(destination string) (string, error) {
	return backupservice.NewService().Export(a.getDataDir(), destination)
}

// BackupImportFromBase64 merges a backup chosen in the browser. Merge only ever
// adds or refreshes: it never deletes local rows.
func (a *App) BackupImportFromBase64(filename string, b64Content string) (backupservice.Result, error) {
	return backupservice.NewService().ImportBase64(filename, b64Content)
}

// BackupArchives lists the pre-migration snapshots the app keeps on disk.
func (a *App) BackupArchives() ([]backupservice.ArchiveInfo, error) {
	return backupservice.NewService().Archives(a.getDataDir())
}

// BackupRestoreArchive merges one snapshot by file name; the path is rebuilt
// under dataDir/schema-backups so only the archive directory is reachable.
func (a *App) BackupRestoreArchive(name string) (backupservice.Result, error) {
	return backupservice.NewService().RestoreArchive(a.getDataDir(), name)
}

// BackupExports lists the backup files already in dataDir/exports, newest first.
// The UI offers these as one-click imports because the WebView2 file dialogs are
// not dependable, and because that is exactly where BackupExport just wrote to.
func (a *App) BackupExports() ([]backupservice.ArchiveInfo, error) {
	return backupservice.NewService().Exports(a.getDataDir())
}

// BackupImportFile merges one file from dataDir/exports by name.
func (a *App) BackupImportFile(name string) (backupservice.Result, error) {
	return backupservice.NewService().ImportFile(a.getDataDir(), name)
}

func (a *App) OpenFolder(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", apperror.New(apperror.Validation, "路径为空")
	}
	if !fileservice.IsWithin(path, a.getDataDir(), a.downloadDir.Get()) {
		return "", apperror.New(apperror.Validation, "path is outside application-managed directories")
	}
	// 如果传入的是文件，则打开其所在目录
	info, err := os.Stat(path)
	if err == nil && !info.IsDir() {
		path = filepath.Dir(path)
	}
	var cmd *exec.Cmd
	switch goruntime.GOOS {
	case "windows":
		cmd = exec.Command("explorer", path)
	case "darwin":
		cmd = exec.Command("open", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	if err := cmd.Start(); err != nil {
		return "", apperror.Wrap(apperror.Unavailable, err, "打开目录失败")
	}
	return "已打开: " + path, nil
}

// safeFilename 把任意字符串变成安全的文件名
func safeFilename(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '-', r == '_':
			out = append(out, r)
		default:
			out = append(out, '_')
		}
	}
	if len(out) == 0 {
		return "unknown"
	}
	return string(out)
}

func logInfo(msg string) {
	applog.Info("%s", msg)
}
