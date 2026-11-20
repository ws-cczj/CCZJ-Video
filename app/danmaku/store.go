package danmaku

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sync"
	"time"
)

const (
	discoveryFreshness = 2 * time.Hour
	commentFreshness   = time.Hour
	cacheRetention     = 6 * time.Hour
	cacheBudget        = 64 << 20
)

// Schema initializes fresh databases and isolated stores. Existing databases
// receive additions through ordered migrations.
var Schema = []string{
	`CREATE TABLE IF NOT EXISTS danmaku_bindings(local_key TEXT PRIMARY KEY, payload BLOB NOT NULL, updated_at INTEGER NOT NULL)`,
	`CREATE TABLE IF NOT EXISTS danmaku_cache(cache_key TEXT PRIMARY KEY, payload BLOB NOT NULL, fetched_at INTEGER NOT NULL, accessed_at INTEGER NOT NULL, bytes INTEGER NOT NULL)`,
	`CREATE INDEX IF NOT EXISTS idx_danmaku_cache_access ON danmaku_cache(accessed_at)`,
}

// Store resolves the database lazily: application construction precedes opening
// SQLite, while shutdown must drain requests before the same database is closed.
type Store struct {
	database  func() *sql.DB
	sweepMu   sync.Mutex
	lastSweep time.Time
}

type storedBinding struct {
	Binding
	Work *matchedWork `json:"matchWork,omitempty"`
}

func decodeBinding(raw []byte, key string) (*Binding, error) {
	var saved storedBinding
	if err := json.Unmarshal(raw, &saved); err != nil {
		return nil, err
	}
	if err := ValidateBinding(saved.Binding); err != nil {
		return nil, err
	}
	if saved.LocalKey != key {
		return nil, errors.New("danmaku binding identity mismatch")
	}
	saved.Binding.work = saved.Work
	return &saved.Binding, nil
}

func NewStore(database func() *sql.DB) *Store { return &Store{database: database} }
func (s *Store) db() (*sql.DB, error) {
	db := s.database()
	if db == nil {
		return nil, errors.New("danmaku database unavailable")
	}
	return db, nil
}
func (s *Store) Binding(ctx context.Context, key string) (*Binding, error) {
	db, err := s.db()
	if err != nil {
		return nil, err
	}
	var raw []byte
	if err = db.QueryRowContext(ctx, `SELECT payload FROM danmaku_bindings WHERE local_key=?`, key).Scan(&raw); errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	return decodeBinding(raw, key)
}
func (s *Store) SaveBinding(ctx context.Context, b Binding) error {
	if err := ValidateBinding(b); err != nil {
		return err
	}
	db, err := s.db()
	if err != nil {
		return err
	}
	raw, err := json.Marshal(storedBinding{Binding: b, Work: b.work})
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `INSERT INTO danmaku_bindings(local_key,payload,updated_at)VALUES(?,?,?) ON CONFLICT(local_key) DO UPDATE SET payload=excluded.payload,updated_at=excluded.updated_at`, b.LocalKey, raw, time.Now().Unix())
	return err
}
func (s *Store) Unbind(ctx context.Context, key string) error {
	db, err := s.db()
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `DELETE FROM danmaku_bindings WHERE local_key=?`, key)
	return err
}

func (s *Store) Cached(ctx context.Context, key string, now time.Time) ([]byte, time.Time, error) {
	db, err := s.db()
	if err != nil {
		return nil, time.Time{}, err
	}
	if err = s.prune(ctx, db, now); err != nil {
		return nil, time.Time{}, err
	}
	var raw []byte
	var fetched int64
	err = db.QueryRowContext(ctx, `SELECT payload,fetched_at FROM danmaku_cache WHERE cache_key=? AND fetched_at>?`, key, now.Add(-cacheRetention).Unix()).Scan(&raw, &fetched)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, time.Time{}, nil
	}
	if err != nil {
		return nil, time.Time{}, err
	}
	_, err = db.ExecContext(ctx, `UPDATE danmaku_cache SET accessed_at=? WHERE cache_key=?`, now.Unix(), key)
	return raw, time.Unix(fetched, 0), err
}

// Lazy maintenance belongs to active reads, so inactive modes have no timer.
// Retention is measured from fetch time so repeated reads cannot extend it.
func (s *Store) prune(ctx context.Context, db *sql.DB, now time.Time) error {
	s.sweepMu.Lock()
	defer s.sweepMu.Unlock()
	if !s.lastSweep.IsZero() && now.Sub(s.lastSweep) < time.Hour {
		return nil
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = trimCache(ctx, tx, now, ""); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	s.lastSweep = now
	return nil
}
func (s *Store) Put(ctx context.Context, key string, raw []byte, now time.Time) error {
	if len(raw) > cacheBudget {
		return errors.New("danmaku cache payload exceeds capacity")
	}
	db, err := s.db()
	if err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT INTO danmaku_cache(cache_key,payload,fetched_at,accessed_at,bytes)VALUES(?,?,?,?,?) ON CONFLICT(cache_key) DO UPDATE SET payload=excluded.payload,fetched_at=excluded.fetched_at,accessed_at=excluded.accessed_at,bytes=excluded.bytes`, key, raw, now.Unix(), now.Unix(), len(raw)); err != nil {
		return err
	}
	if err = trimCache(ctx, tx, now, key); err != nil {
		return err
	}
	return tx.Commit()
}

func trimCache(ctx context.Context, tx *sql.Tx, now time.Time, writtenKey string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM danmaku_cache WHERE fetched_at<=?`, now.Add(-cacheRetention).Unix()); err != nil {
		return err
	}
	var total int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(bytes),0) FROM danmaku_cache`).Scan(&total); err != nil {
		return err
	}
	if total <= cacheBudget {
		return nil
	}
	_, err := tx.ExecContext(ctx, `DELETE FROM danmaku_cache WHERE cache_key IN (
		SELECT cache_key FROM (
			SELECT cache_key,SUM(bytes) OVER (
				ORDER BY (cache_key=?) DESC,accessed_at DESC,fetched_at DESC,cache_key
			) AS retained_bytes
			FROM danmaku_cache
		) WHERE retained_bytes>?
	)`, writtenKey, cacheBudget)
	return err
}
