package download

import (
	"cczjVideo/app/apperror"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// SettingSaver persists a download preference without coupling the service to a database package.
type SettingSaver func(key, value string) error

// Directory owns the mutable download-directory preference.
type Directory struct {
	mu      sync.RWMutex
	custom  string
	save    SettingSaver
	homeDir func() (string, error)
}

func NewDirectory(save SettingSaver) *Directory {
	return &Directory{save: save, homeDir: os.UserHomeDir}
}

// Get returns the configured directory when it exists, otherwise the platform default.
func (d *Directory) Get() string {
	d.mu.RLock()
	configured := d.custom
	d.mu.RUnlock()
	if configured != "" {
		if info, err := os.Stat(configured); err == nil && info.IsDir() {
			return configured
		}
	}
	home, err := d.homeDir()
	if err != nil || home == "" {
		home = "."
	}
	return filepath.Join(home, "Downloads")
}

// Set validates, creates and persists a custom directory. An empty directory resets to default.
func (d *Directory) Set(directory string) (string, error) {
	directory = strings.TrimSpace(directory)
	if directory != "" {
		if info, err := os.Stat(directory); err == nil && !info.IsDir() {
			return d.Get(), apperror.New(apperror.Validation, "download path is not a directory")
		} else if os.IsNotExist(err) {
			if err := os.MkdirAll(directory, 0755); err != nil {
				return d.Get(), apperror.Wrap(apperror.Storage, err, "create download directory")
			}
		} else if err != nil {
			return d.Get(), apperror.Wrap(apperror.Storage, err, "inspect download directory")
		}
	}

	d.mu.Lock()
	d.custom = directory
	d.mu.Unlock()
	if d.save != nil {
		if err := d.save("download_dir", directory); err != nil {
			return d.Get(), apperror.Wrap(apperror.Storage, err, "persist download directory")
		}
	}
	return d.Get(), nil
}
