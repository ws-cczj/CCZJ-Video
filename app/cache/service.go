// Package cache manages application-owned disk cache and log directories.
package cache

import (
	"fmt"
	"os"
	"path/filepath"
)

// DataDirectory returns the application data directory.
type DataDirectory func() string

// Info describes the storage used by application-owned caches.
//
// 浏览器侧的 localStorage / IndexedDB 不在这个结构里：那部分由前端自己统计，Go 只能报出
// 一个恒为 0 的占位数（以前的 ts_cache 就是这样：目录从来没有被创建过，诊断台却一直显示
// "磁盘缓存 0 B" 加一条不存在的路径）。
type Info struct {
	DatabaseBytes int64  `json:"database_bytes"`
	DatabasePath  string `json:"database_path"`
	LogFileBytes  int64  `json:"log_file_bytes"`
	LogFilePath   string `json:"log_file_path"`
}

// Service owns paths and deletion policy for application cache directories.
type Service struct {
	dataDirectory DataDirectory
}

// NewService creates a cache service using the supplied application data-directory provider.
func NewService(dataDirectory DataDirectory) *Service {
	return &Service{dataDirectory: dataDirectory}
}

// GetInfo returns the current storage usage for cache directories and database files.
func (s *Service) GetInfo() (*Info, error) {
	dataDirectory := s.directory()
	info := &Info{}

	databasePath := filepath.Join(dataDirectory, "cczj_video.db")
	if fileInfo, err := os.Stat(databasePath); err == nil {
		info.DatabaseBytes = fileInfo.Size()
		for _, extension := range []string{"-wal", "-shm"} {
			if sidecarInfo, sidecarErr := os.Stat(databasePath + extension); sidecarErr == nil {
				info.DatabaseBytes += sidecarInfo.Size()
			}
		}
	}
	info.DatabasePath = databasePath

	info.LogFilePath = filepath.Join(dataDirectory, "applog")
	if fileInfo, err := os.Stat(info.LogFilePath); err == nil && fileInfo.IsDir() {
		info.LogFileBytes = directorySize(info.LogFilePath)
	}

	return info, nil
}

// Clear deletes the requested cache category without deleting the active database.
// 只管写到磁盘上的目录：进程内的派生缓存由 invalidate.go 负责。
func (s *Service) Clear(cacheType string) error {
	dataDirectory := s.directory()
	switch cacheType {
	case "logs":
		return os.RemoveAll(filepath.Join(dataDirectory, "applog"))
	case "database":
		return fmt.Errorf("数据库文件正在使用中，请通过「重置数据库」功能操作")
	default:
		return fmt.Errorf("未知缓存类型: %s", cacheType)
	}
}

func (s *Service) directory() string {
	if s.dataDirectory == nil {
		return ""
	}
	return s.dataDirectory()
}

func directorySize(path string) int64 {
	var size int64
	_ = filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			size += info.Size()
		}
		return nil
	})
	return size
}
