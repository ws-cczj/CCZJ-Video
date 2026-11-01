package db

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
)

func migrateNovelChapterObservations(tx *sqlx.Tx) error {
	_, err := tx.Exec(`CREATE TABLE IF NOT EXISTS novel_chapter_observations (
	 id INTEGER PRIMARY KEY AUTOINCREMENT, source_key TEXT NOT NULL, chapter_id INTEGER NOT NULL,
	 revision TEXT NOT NULL, checked_at INTEGER NOT NULL, outcome TEXT NOT NULL,
	 FOREIGN KEY(chapter_id) REFERENCES library_chapters(id) ON DELETE CASCADE);
	 CREATE INDEX IF NOT EXISTS idx_novel_observations_source ON novel_chapter_observations(source_key,id);
	 CREATE TABLE IF NOT EXISTS reading_source_observations (
	 id INTEGER PRIMARY KEY AUTOINCREMENT,source_id TEXT NOT NULL,checked_at INTEGER NOT NULL,result_json TEXT NOT NULL);
	 CREATE INDEX IF NOT EXISTS idx_reading_source_observations ON reading_source_observations(source_id,id)`)
	return err
}

// SaveReadingSourceObservation retains original sample timestamps and bounded
// source-check history, including legacy benchmark records before replacement.
func SaveReadingSourceObservation(ctx context.Context, sourceID string, checkedAt int64, result string) (err error) {
	if len(sourceID) != 64 || checkedAt <= 0 || len(result) > 64<<10 || !json.Valid([]byte(result)) {
		return fmt.Errorf("invalid source observation")
	}
	tx, err := instance.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	_, err = tx.ExecContext(ctx, `INSERT INTO reading_source_observations(source_id,checked_at,result_json) VALUES(?,?,?)`, sourceID, checkedAt, result)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `DELETE FROM reading_source_observations WHERE source_id=? AND id NOT IN (SELECT id FROM reading_source_observations WHERE source_id=? ORDER BY id DESC LIMIT 128)`, sourceID, sourceID)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `DELETE FROM reading_source_observations WHERE id <= (SELECT id FROM reading_source_observations ORDER BY id DESC LIMIT 1 OFFSET 1024)`)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// RecordNovelChapterResult preserves the current state per chapter and a bounded
// diagnostic history. Cancellation and local failures never enter this table.
func RecordNovelChapterResult(ctx context.Context, chapterID int, revision, outcome string) (err error) {
	switch outcome {
	case "text", "restricted", "missing", "timeout", "rate_limited", "invalid", "unavailable", "warning":
	default:
		return fmt.Errorf("invalid novel chapter outcome")
	}
	if chapterID <= 0 || len(revision) != 64 {
		return fmt.Errorf("invalid novel chapter identity")
	}
	tx, err := instance.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	now := time.Now().Unix()
	_, err = tx.ExecContext(ctx, `INSERT INTO reading_chapter_evidence(kind,chapter_id,revision,checked_at,evidence)
	 SELECT 'novel',c.id,?,?,? FROM library_chapters c JOIN library_items i ON i.id=c.item_id WHERE c.id=? AND i.kind='novel'
	 ON CONFLICT(kind,chapter_id) DO UPDATE SET revision=excluded.revision,checked_at=excluded.checked_at,evidence=excluded.evidence`, revision, now, outcome, chapterID)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO novel_chapter_observations(source_key,chapter_id,revision,checked_at,outcome)
	 SELECT i.source_key,c.id,?,?,? FROM library_chapters c JOIN library_items i ON i.id=c.item_id WHERE c.id=? AND i.kind='novel'`, revision, now, outcome, chapterID)
	if err != nil {
		return err
	}
	// Keep the latest 512 attempts for this source. Current per-chapter evidence
	// remains independent of history retention and cannot resurrect an old success.
	_, err = tx.ExecContext(ctx, `DELETE FROM novel_chapter_observations WHERE source_key=(SELECT source_key FROM library_items WHERE id=(SELECT item_id FROM library_chapters WHERE id=?))
	 AND id NOT IN (SELECT id FROM novel_chapter_observations WHERE source_key=(SELECT source_key FROM library_items WHERE id=(SELECT item_id FROM library_chapters WHERE id=?)) ORDER BY id DESC LIMIT 512)`, chapterID, chapterID)
	if err != nil {
		return err
	}
	state, code := "read_failed", outcome
	if outcome == "text" {
		state, code = "readable", ""
	}
	var unchanged int
	if err = tx.GetContext(ctx, &unchanged, `SELECT COUNT(*) FROM novel_item_source_checks WHERE item_id=(SELECT item_id FROM library_chapters WHERE id=?) AND revision=? AND state=? AND error_code=?`, chapterID, revision, state, code); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO novel_item_source_checks(item_id,revision,observed_at,state,error_code)
		 SELECT item_id,?,?,?,? FROM library_chapters WHERE id=?
		 ON CONFLICT(item_id) DO UPDATE SET checked_at=CASE WHEN novel_item_source_checks.revision<>excluded.revision THEN 0 ELSE novel_item_source_checks.checked_at END,
		 revision=excluded.revision,observed_at=excluded.observed_at,state=excluded.state,error_code=excluded.error_code`, revision, time.Now().UnixNano(), state, code, chapterID)
	if err != nil {
		return err
	}
	if outcome != "text" && unchanged == 0 {
		_, err = tx.ExecContext(ctx, `INSERT INTO novel_book_source_refresh(item_id,generation) SELECT item_id,1 FROM library_chapters WHERE id=?
		 ON CONFLICT(item_id) DO UPDATE SET generation=novel_book_source_refresh.generation+1`, chapterID)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE novel_book_source_refresh SET generation=generation+1
		 WHERE item_id<>(SELECT item_id FROM library_chapters WHERE id=?) AND item_id IN (
		 SELECT b.item_id FROM reading_work_sources a JOIN reading_work_sources b ON b.kind=a.kind AND b.title_key=a.title_key AND b.author_key=a.author_key
		 WHERE a.kind='novel' AND a.author_key<>'' AND a.item_id=(SELECT item_id FROM library_chapters WHERE id=?))`, chapterID, chapterID)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ReadingObservationCount describes one operation outcome at one rule revision.
// A source-wide count is an observation scope, never a whole-book completeness claim.
type ReadingObservationCount struct {
	SourceKey string `db:"source_key"`
	Revision  string `db:"revision"`
	Outcome   string `db:"outcome"`
	Count     int    `db:"count"`
	CheckedAt int64  `db:"checked_at"`
}

func ReadNovelObservations(ctx context.Context, itemID int) ([]ReadingObservationCount, error) {
	rows := []ReadingObservationCount{}
	err := instance.SelectContext(ctx, &rows, `SELECT i.source_key,e.revision,e.evidence AS outcome,COUNT(*) AS count,MAX(e.checked_at) AS checked_at
	 FROM reading_chapter_evidence e JOIN library_chapters c ON c.id=e.chapter_id JOIN library_items i ON i.id=c.item_id
	 WHERE e.kind='novel' AND i.kind='novel' AND c.lifecycle_state='active' AND i.lifecycle_state='active'
	 AND (?=0 OR i.id=?) GROUP BY i.source_key,e.revision,e.evidence,(e.checked_at>=?)`, itemID, itemID, time.Now().Add(-7*24*time.Hour).Unix())
	return rows, err
}
