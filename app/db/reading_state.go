package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"cczjVideo/app/readingstate"
	"github.com/jmoiron/sqlx"
)

var ErrCorruptReadingState = errors.New("corrupt persisted reading state")
var ErrReadingNotHydrated = errors.New("reading state has not been hydrated")

const readingStateDDL = `CREATE TABLE IF NOT EXISTS reading_state (
 kind TEXT NOT NULL, bucket TEXT NOT NULL, record_key TEXT NOT NULL,
 value TEXT NOT NULL, updated_at INTEGER NOT NULL, PRIMARY KEY(kind,bucket,record_key))`
const readingImportsDDL = `CREATE TABLE IF NOT EXISTS reading_state_imports (
 kind TEXT PRIMARY KEY, imported_at INTEGER NOT NULL)`

func migrateReadingState(tx *sqlx.Tx) error {
	for _, ddl := range []string{readingStateDDL, readingImportsDDL} {
		if _, err := tx.Exec(ddl); err != nil {
			return err
		}
	}
	return nil
}

type ReadingStateStore struct{ database *sqlx.DB }

func NewReadingStateStore(database *sqlx.DB) *ReadingStateStore {
	return &ReadingStateStore{database: database}
}

func validateReadingRecords(kind string, records []readingstate.Record) error {
	if err := readingstate.ValidateKind(kind); err != nil {
		return err
	}
	if len(records) > 100000 {
		return fmt.Errorf("%w: too many records", readingstate.ErrInvalidRecord)
	}
	seen := make(map[string]bool, len(records))
	for _, r := range records {
		if err := r.Validate(kind); err != nil {
			return err
		}
		key := r.Bucket + "\x00" + r.Key
		if seen[key] {
			return fmt.Errorf("%w: duplicate record", readingstate.ErrInvalidRecord)
		}
		seen[key] = true
	}
	return nil
}

// Load imports legacy browser records atomically once per mode. Existing durable
// records take precedence; the marker prevents old browser values resurrecting deletes.
func (s *ReadingStateStore) Load(ctx context.Context, kind string, legacy []readingstate.Record) ([]readingstate.Record, error) {
	if err := readingstate.ValidateKind(kind); err != nil {
		return nil, err
	}
	tx, err := s.database.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var imported int
	if err := tx.GetContext(ctx, &imported, `SELECT COUNT(*) FROM reading_state_imports WHERE kind=?`, kind); err != nil {
		return nil, err
	}
	if imported == 0 {
		if err := validateReadingRecords(kind, legacy); err != nil {
			return nil, err
		}
		for _, r := range legacy {
			if _, err := tx.ExecContext(ctx, `INSERT INTO reading_state(kind,bucket,record_key,value,updated_at) VALUES(?,?,?,?,?) ON CONFLICT DO NOTHING`, kind, r.Bucket, r.Key, r.Value, time.Now().UnixMilli()); err != nil {
				return nil, err
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO reading_state_imports(kind,imported_at) VALUES(?,?)`, kind, time.Now().UnixMilli()); err != nil {
			return nil, err
		}
	}
	var rows []struct {
		Bucket string `db:"bucket"`
		Key    string `db:"record_key"`
		Value  string `db:"value"`
	}
	if err := tx.SelectContext(ctx, &rows, `SELECT bucket,record_key,value FROM reading_state WHERE kind=? ORDER BY updated_at DESC,record_key`, kind); err != nil {
		return nil, err
	}
	records := make([]readingstate.Record, 0, len(rows))
	for _, row := range rows {
		r := readingstate.Record{Bucket: row.Bucket, Key: row.Key, Value: row.Value}
		if err := r.Validate(kind); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrCorruptReadingState, err)
		}
		records = append(records, r)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return records, nil
}

// Apply commits a batch of per-identity changes. It never rewrites unrelated
// books, and refuses writes before successful import/hydration.
func (s *ReadingStateStore) Apply(ctx context.Context, kind string, changes []readingstate.Change) error {
	if err := readingstate.ValidateKind(kind); err != nil {
		return err
	}
	if len(changes) > 1000 {
		return fmt.Errorf("%w: batch exceeds limit", readingstate.ErrInvalidRecord)
	}
	for _, c := range changes {
		if err := readingstate.ValidateKey(kind, c.Bucket, c.Key); err != nil {
			return err
		}
		if !c.Delete {
			if err := c.Record.Validate(kind); err != nil {
				return err
			}
		}
	}
	var imported int
	// The import marker is append-only during normal use. Read it before the
	// write transaction: a deferred read transaction cannot upgrade its WAL
	// snapshot while another catalog/probe writer holds the lock (SQLITE_BUSY).
	if err := s.database.GetContext(ctx, &imported, `SELECT COUNT(*) FROM reading_state_imports WHERE kind=?`, kind); err != nil {
		return err
	}
	if imported == 0 {
		return ErrReadingNotHydrated
	}
	tx, err := s.database.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, c := range changes {
		if c.Delete {
			_, err = tx.ExecContext(ctx, `DELETE FROM reading_state WHERE kind=? AND bucket=? AND record_key=?`, kind, c.Bucket, c.Key)
		} else {
			_, err = tx.ExecContext(ctx, `INSERT INTO reading_state(kind,bucket,record_key,value,updated_at) VALUES(?,?,?,?,?) ON CONFLICT(kind,bucket,record_key) DO UPDATE SET value=excluded.value,updated_at=excluded.updated_at`, kind, c.Bucket, c.Key, c.Value, time.Now().UnixMilli())
		}
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}
