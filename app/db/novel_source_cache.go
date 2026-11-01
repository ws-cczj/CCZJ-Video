package db

import (
	"context"
	"fmt"
	"time"

	"cczjVideo/app/model"
	"github.com/jmoiron/sqlx"
)

func migrateNovelSourceCache(tx *sqlx.Tx) error {
	_, err := tx.Exec(`CREATE TABLE IF NOT EXISTS novel_item_source_checks (
	 item_id INTEGER PRIMARY KEY,revision TEXT NOT NULL,checked_at INTEGER NOT NULL DEFAULT 0,
	 observed_at INTEGER NOT NULL DEFAULT 0,state TEXT NOT NULL,error_code TEXT NOT NULL DEFAULT '',
	 FOREIGN KEY(item_id) REFERENCES library_items(id) ON DELETE CASCADE);
	 CREATE TABLE IF NOT EXISTS novel_book_source_refresh (
	 item_id INTEGER PRIMARY KEY,checked_at INTEGER NOT NULL DEFAULT 0,generation INTEGER NOT NULL DEFAULT 0,
	 completed_generation INTEGER NOT NULL DEFAULT 0,
	 FOREIGN KEY(item_id) REFERENCES library_items(id) ON DELETE CASCADE)`)
	return err
}

type NovelSourceCheck struct {
	ItemID     int    `db:"item_id"`
	Revision   string `db:"revision"`
	CheckedAt  int64  `db:"checked_at"`
	ObservedAt int64  `db:"observed_at"`
	State      string `db:"state"`
	ErrorCode  string `db:"error_code"`
}
type NovelSourceRefresh struct {
	CheckedAt           int64 `db:"checked_at"`
	Generation          int   `db:"generation"`
	CompletedGeneration int   `db:"completed_generation"`
}

// The existing work index is durable discovery evidence, not a TTL cache.
// Operational ranking may age it; the book's source choices must not disappear.
func FindNovelSourceItems(ctx context.Context, itemID int, titleKey, authorKey string) ([]*model.LibraryItem, error) {
	rows := []*model.LibraryItem{}
	err := instance.SelectContext(ctx, &rows, `SELECT i.* FROM library_items i
	 LEFT JOIN reading_work_sources w ON w.item_id=i.id AND w.kind='novel'
	 WHERE i.kind='novel' AND (i.id=? OR w.title_key=?)
	 ORDER BY (i.id=?) DESC,(w.author_key=?) DESC,w.seen_at DESC,i.id DESC LIMIT 128`, itemID, titleKey, itemID, authorKey)
	return rows, err
}
func ReadNovelSourceChecks(ctx context.Context, ids []int) (map[int]NovelSourceCheck, error) {
	out := map[int]NovelSourceCheck{}
	if len(ids) == 0 {
		return out, nil
	}
	query, args, err := sqlx.In(`SELECT * FROM novel_item_source_checks WHERE item_id IN (?)`, ids)
	if err != nil {
		return nil, err
	}
	var rows []NovelSourceCheck
	if err = instance.SelectContext(ctx, &rows, query, args...); err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[row.ItemID] = row
	}
	return out, nil
}

type NovelSourceLimit struct {
	ItemID   int    `db:"item_id"`
	Revision string `db:"revision"`
}

func ReadNovelSourceLimits(ctx context.Context, ids []int) (map[string]bool, error) {
	out := map[string]bool{}
	if len(ids) == 0 {
		return out, nil
	}
	query, args, err := sqlx.In(`SELECT DISTINCT c.item_id,e.revision FROM reading_chapter_evidence e JOIN library_chapters c ON c.id=e.chapter_id
	 WHERE e.kind='novel' AND c.item_id IN (?) AND e.evidence IN ('restricted','missing','warning') AND e.checked_at>=?`, ids, time.Now().Add(-7*24*time.Hour).Unix())
	if err != nil {
		return nil, err
	}
	var rows []NovelSourceLimit
	if err = instance.SelectContext(ctx, &rows, query, args...); err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[fmt.Sprintf("%d:%s", row.ItemID, row.Revision)] = true
	}
	return out, nil
}
func ReadNovelSourceRefresh(ctx context.Context, itemID int) (NovelSourceRefresh, error) {
	var rows []NovelSourceRefresh
	err := instance.SelectContext(ctx, &rows, `SELECT checked_at,generation,completed_generation FROM novel_book_source_refresh WHERE item_id=?`, itemID)
	if len(rows) > 0 {
		return rows[0], err
	}
	return NovelSourceRefresh{}, err
}
func SaveNovelSourceRefresh(ctx context.Context, itemID, generation int) error {
	_, err := instance.ExecContext(ctx, `INSERT INTO novel_book_source_refresh(item_id,checked_at,completed_generation) VALUES(?,?,?)
	 ON CONFLICT(item_id) DO UPDATE SET checked_at=excluded.checked_at,completed_generation=excluded.completed_generation`, itemID, time.Now().Unix(), generation)
	return err
}
func SaveNovelSourceCheck(ctx context.Context, row NovelSourceCheck) error {
	if row.ItemID <= 0 || len(row.Revision) != 64 || row.CheckedAt <= 0 {
		return fmt.Errorf("invalid novel source check")
	}
	_, err := instance.ExecContext(ctx, `INSERT INTO novel_item_source_checks(item_id,revision,checked_at,observed_at,state,error_code) VALUES(?,?,?,?,?,?)
	 ON CONFLICT(item_id) DO UPDATE SET revision=excluded.revision,checked_at=excluded.checked_at,
	 observed_at=excluded.observed_at,state=excluded.state,error_code=excluded.error_code
	 WHERE novel_item_source_checks.observed_at<=excluded.observed_at`, row.ItemID, row.Revision, row.CheckedAt, row.ObservedAt, row.State, row.ErrorCode)
	return err
}
