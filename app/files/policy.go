// Package files contains filesystem authorization helpers for Wails-facing operations.
package files

import (
	"path/filepath"
	"strings"
)

// IsWithin reports whether path resolves inside one of roots.
func IsWithin(path string, roots ...string) bool {
	target, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	for _, root := range roots {
		if strings.TrimSpace(root) == "" {
			continue
		}
		base, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(base, target)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
			continue
		}
		return true
	}
	return false
}
