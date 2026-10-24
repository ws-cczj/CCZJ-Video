package backup

import (
	"os"
	"path/filepath"
	"testing"

	"cczjVideo/app/apperror"
	"cczjVideo/app/db"
)

func TestExportDoesNotPublishBackupItsImporterRejects(t *testing.T) {
	resetUserTables(t)
	t.Cleanup(func() { resetUserTables(t) })
	_, err := db.DB().Exec(`WITH RECURSIVE entries(n) AS (
		VALUES(1) UNION ALL SELECT n+1 FROM entries WHERE n<?
	) INSERT INTO settings(key,value) SELECT 'backup_export_limit_'||n,'value' FROM entries`, maxBackupItems+1)
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	if path, err := NewService().Export(directory, ""); apperror.CodeOf(err) != apperror.Validation {
		t.Fatalf("export published an unimportable backup: path=%q error=%v", path, err)
	}
	files, err := os.ReadDir(filepath.Join(directory, exportDirName))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatalf("rejected export left a published file: %#v", files)
	}
}
