package backup

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"cczjVideo/app/applog"
)

// exportDirName 是 Export 默认落盘的目录，也是「备份文件」列表读取的目录。
// WebView2 里的原生保存对话框不可靠（点下去既没有窗口也没有报错），所以导出一律
// 先写进这里，界面上再给「打开所在文件夹」；导入同理，允许直接点列表里的文件。
const exportDirName = "exports"

// Exports lists backup files already sitting in the exports directory, newest first.
func (s *Service) Exports(dataDir string) ([]ArchiveInfo, error) {
	dir := filepath.Join(dataDir, exportDirName)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []ArchiveInfo{}, nil
		}
		return nil, fmt.Errorf("read export directory: %w", err)
	}
	files := make([]ArchiveInfo, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !isBackupFileName(entry.Name()) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		files = append(files, ArchiveInfo{
			Name:       entry.Name(),
			ModifiedAt: info.ModTime().Format("2006-01-02 15:04:05"),
			SizeBytes:  info.Size(),
		})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name > files[j].Name })
	return files, nil
}

// ImportFile merges one file from the exports directory. The caller passes a file
// name only, so the backend decides which directory may be read.
func (s *Service) ImportFile(dataDir, name string) (Result, error) {
	path, err := s.exportPath(dataDir, name)
	if err != nil {
		return Result{}, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return Result{}, fmt.Errorf("read backup file: %w", err)
	}
	applog.InfoFields("importing backup file", applog.Fields{"file": name, "bytes": len(raw)})
	return s.ImportBytes(name, raw, "exports:"+name)
}

func (s *Service) exportPath(dataDir, name string) (string, error) {
	cleaned := strings.TrimSpace(filepath.Base(name))
	if cleaned == "" || cleaned != name || !isBackupFileName(cleaned) {
		return "", fmt.Errorf("invalid backup file name: %q", name)
	}
	dir := filepath.Join(dataDir, exportDirName)
	path := filepath.Join(dir, cleaned)
	if !withinDir(path, dir) {
		return "", fmt.Errorf("backup file is outside the export directory")
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("backup file not found: %w", err)
	}
	if info.IsDir() || info.Size() == 0 {
		return "", fmt.Errorf("backup file is empty")
	}
	return path, nil
}

func isBackupFileName(name string) bool {
	lower := strings.ToLower(name)
	for _, suffix := range []string{".json", ".br", ".gz"} {
		if strings.HasSuffix(lower, suffix) {
			return true
		}
	}
	return false
}
