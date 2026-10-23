package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"cczjVideo/app/download"
	"github.com/jmoiron/sqlx"
)

const downloadTasksDDL = `CREATE TABLE IF NOT EXISTS download_tasks (
	task_id TEXT PRIMARY KEY,
	status TEXT NOT NULL,
	payload_version INTEGER NOT NULL DEFAULT 1,
	payload TEXT NOT NULL,
	updated_at INTEGER NOT NULL
)`

const downloadImportsDDL = `CREATE TABLE IF NOT EXISTS download_task_imports (
	name TEXT PRIMARY KEY,
	imported_at INTEGER NOT NULL
)`

func migrateDownloadTasks(tx *sqlx.Tx) error {
	for _, ddl := range []string{downloadTasksDDL, downloadImportsDDL} {
		if _, err := tx.Exec(ddl); err != nil {
			return err
		}
	}
	return nil
}

// DownloadTaskStore binds durable downloads to an explicitly owned connection.
type DownloadTaskStore struct{ database *sqlx.DB }

// NewDownloadTaskStore uses the already-migrated application database.
func NewDownloadTaskStore(database *sqlx.DB) *DownloadTaskStore {
	return &DownloadTaskStore{database: database}
}

func writeDownload(ctx context.Context, executor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, cp download.Checkpoint, importing bool) error {
	if err := cp.Validate(); err != nil {
		return err
	}
	raw, err := json.Marshal(cp)
	if err != nil {
		return err
	}
	conflict := `DO UPDATE SET status=excluded.status,payload_version=1,payload=excluded.payload,updated_at=excluded.updated_at`
	if importing {
		conflict = `DO NOTHING`
	}
	_, err = executor.ExecContext(ctx, `INSERT INTO download_tasks(task_id,status,payload_version,payload,updated_at) VALUES(?,?,1,?,?) ON CONFLICT(task_id) `+conflict, cp.TaskId, cp.Status, string(raw), time.Now().UnixNano())
	return err
}

// Save atomically replaces one checkpoint without rewriting unrelated tasks.
func (s *DownloadTaskStore) Save(ctx context.Context, cp download.Checkpoint) error {
	return writeDownload(ctx, s.database, cp, false)
}

// Delete removes a task; the import marker prevents stale JSON resurrecting it.
func (s *DownloadTaskStore) Delete(ctx context.Context, id string) error {
	_, err := s.database.ExecContext(ctx, `DELETE FROM download_tasks WHERE task_id=?`, id)
	return err
}

func (s *DownloadTaskStore) importLegacy(ctx context.Context, path string) error {
	var marked int
	err := s.database.GetContext(ctx, &marked, `SELECT COUNT(*) FROM download_task_imports WHERE name='downloads.json'`)
	if err != nil || marked != 0 {
		return err
	}
	var items []download.Checkpoint
	file, err := os.Open(path)
	if err == nil {
		defer file.Close()
		const maxLegacyBytes = 64 << 20
		raw, readErr := io.ReadAll(io.LimitReader(file, maxLegacyBytes+1))
		if readErr != nil {
			return readErr
		}
		if len(raw) > maxLegacyBytes {
			return fmt.Errorf("legacy downloads exceed import size limit")
		}
		if err := json.Unmarshal(raw, &items); err != nil {
			return fmt.Errorf("decode legacy downloads: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	seen := make(map[string]bool)
	for _, item := range items {
		if err := item.Validate(); err != nil {
			return fmt.Errorf("legacy task %q: %w", item.TaskId, err)
		}
		if seen[item.TaskId] {
			return fmt.Errorf("duplicate legacy download ID %q", item.TaskId)
		}
		seen[item.TaskId] = true
	}
	tx, err := s.database.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := tx.GetContext(ctx, &marked, `SELECT COUNT(*) FROM download_task_imports WHERE name='downloads.json'`); err != nil {
		return err
	}
	if marked != 0 {
		return tx.Commit()
	}
	for _, cp := range items {
		if err := writeDownload(ctx, tx, cp, true); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO download_task_imports(name,imported_at) VALUES('downloads.json',?) ON CONFLICT(name) DO NOTHING`, time.Now().Unix()); err != nil {
		return err
	}
	return tx.Commit()
}

// Restore imports all legacy records or none, then reconciles interrupted work.
// Legacy JSON is retained unchanged for recovery and is never the active journal.
func (s *DownloadTaskStore) Restore(ctx context.Context, path string) ([]download.Checkpoint, error) {
	if err := s.importLegacy(ctx, path); err != nil {
		return nil, err
	}
	var rows []struct {
		ID      string `db:"task_id"`
		Version int    `db:"payload_version"`
		Payload string `db:"payload"`
	}
	if err := s.database.SelectContext(ctx, &rows, `SELECT task_id,payload_version,payload FROM download_tasks ORDER BY updated_at,task_id`); err != nil {
		return nil, err
	}
	items := make([]download.Checkpoint, 0, len(rows))
	changed := make([]download.Checkpoint, 0)
	for _, row := range rows {
		var cp download.Checkpoint
		if row.Version != 1 {
			return nil, fmt.Errorf("unsupported download checkpoint version %d", row.Version)
		}
		if err := json.Unmarshal([]byte(row.Payload), &cp); err != nil {
			return nil, err
		}
		if err := cp.Validate(); err != nil {
			return nil, err
		}
		if cp.TaskId != row.ID {
			return nil, fmt.Errorf("download checkpoint identity mismatch")
		}
		previousState := cp.Status
		switch cp.Status {
		case "queued", "downloading":
			cp.Status = "paused"
		case "finalizing":
			_, partialErr := os.Stat(cp.SavePath + ".part")
			final, finalErr := os.Stat(cp.SavePath)
			if errors.Is(partialErr, os.ErrNotExist) && finalErr == nil && final.Mode().IsRegular() && final.Size() == cp.FinalSize {
				cp.Status, cp.Downloaded, cp.Total = "done", cp.FinalSize, cp.FinalSize
				if cp.EndTime == 0 {
					cp.EndTime = time.Now().Unix()
				}
			} else if partialErr == nil {
				cp.Status = "paused"
			} else {
				cp.Status, cp.ErrorMsg = "error", "下载文件未完成落位，请检查目标和临时文件"
			}
		}
		items = append(items, cp)
		if previousState != cp.Status {
			changed = append(changed, cp)
		}
	}
	// Validate the entire journal before publishing or updating any recovered row.
	if len(changed) == 0 {
		return items, nil
	}
	tx, err := s.database.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	for _, cp := range changed {
		if err := writeDownload(ctx, tx, cp, false); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return items, nil
}
