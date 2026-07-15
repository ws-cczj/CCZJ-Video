package db

import (
	"cczjVideo/app/model"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
)

type schemaMigration struct {
	version int
	name    string
	up      func(*sql.Tx) error
}

var schemaMigrations = []schemaMigration{
	{
		version: 1,
		name:    "validate_source_keys",
		up: func(tx *sql.Tx) error {
			rows, err := tx.Query(`SELECT source_key FROM sources`)
			if err != nil {
				return fmt.Errorf("read source keys: %w", err)
			}
			defer rows.Close()
			for rows.Next() {
				var key string
				if err := rows.Scan(&key); err != nil {
					return fmt.Errorf("scan source key: %w", err)
				}
				if err := model.ValidateSourceKey(key); err != nil {
					return fmt.Errorf("stored source_key %q is unsafe; rename it before upgrading: %w", key, err)
				}
			}
			return rows.Err()
		},
	},
}

func runSchemaMigrations(database *sqlx.DB, backup func() error) error {
	if _, err := database.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		name TEXT NOT NULL,
		checksum TEXT NOT NULL,
		applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	type appliedMigration struct {
		Version  int    `db:"version"`
		Checksum string `db:"checksum"`
	}
	var appliedRows []appliedMigration
	if err := database.Select(&appliedRows, `SELECT version, checksum FROM schema_migrations`); err != nil {
		return fmt.Errorf("read applied migrations: %w", err)
	}
	applied := make(map[int]string, len(appliedRows))
	for _, migration := range appliedRows {
		applied[migration.Version] = migration.Checksum
	}

	migrations := append([]schemaMigration(nil), schemaMigrations...)
	sort.Slice(migrations, func(i, j int) bool { return migrations[i].version < migrations[j].version })
	backedUp := false
	for _, migration := range migrations {
		checksum := migrationChecksum(migration)
		if appliedChecksum, ok := applied[migration.version]; ok {
			if appliedChecksum != checksum {
				return fmt.Errorf("migration %d (%s) checksum changed after application", migration.version, migration.name)
			}
			continue
		}
		if !backedUp && backup != nil {
			if err := backup(); err != nil {
				return fmt.Errorf("backup before migration %d (%s): %w", migration.version, migration.name, err)
			}
			backedUp = true
		}

		tx, err := database.Begin()
		if err != nil {
			return fmt.Errorf("begin migration %d (%s): %w", migration.version, migration.name, err)
		}
		if err := migration.up(tx); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("migration %d (%s): %w", migration.version, migration.name, err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations (version, name, checksum) VALUES (?, ?, ?)`, migration.version, migration.name, checksum); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record migration %d (%s): %w", migration.version, migration.name, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %d (%s): %w", migration.version, migration.name, err)
		}
		logInfo(fmt.Sprintf("applied schema migration %d: %s", migration.version, migration.name))
	}
	return nil
}

func migrationChecksum(migration schemaMigration) string {
	// Version and immutable migration name make accidental version reuse visible.
	// SQL/function bodies must never be edited after release; add a new version.
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d:%s", migration.version, migration.name)))
	return fmt.Sprintf("%x", sum[:])
}

func backupDatabaseBeforeMigration(database *sqlx.DB, databasePath string) error {
	info, err := os.Stat(databasePath)
	if err != nil {
		return fmt.Errorf("stat database: %w", err)
	}
	if info.Size() == 0 {
		return nil
	}

	backupDir := filepath.Join(filepath.Dir(databasePath), "backups")
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		return fmt.Errorf("create backup directory: %w", err)
	}
	backupPath := filepath.Join(backupDir, "cczj_video_"+time.Now().Format("20060102_150405")+".db")
	escapedPath := strings.ReplaceAll(filepath.ToSlash(backupPath), "'", "''")
	if _, err := database.Exec("VACUUM INTO '" + escapedPath + "'"); err != nil {
		return fmt.Errorf("create sqlite backup: %w", err)
	}
	logInfo("created database backup: " + backupPath)
	return nil
}
