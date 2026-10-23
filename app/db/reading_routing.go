package db

import (
	"context"
	"fmt"
	"time"

	"cczjVideo/app/model"
	"github.com/jmoiron/sqlx"
)

// Reading evidence is keyed by mode and source revision. A search hit never
// becomes proof of readable chapters, and a failed probe never deletes coverage.
var readingRoutingSchema = []string{
	`CREATE INDEX IF NOT EXISTS idx_reading_chapter_url ON library_chapters(remote_url,item_id)`,
	`CREATE TABLE IF NOT EXISTS reading_chapter_alignments (
	 kind TEXT NOT NULL CHECK(kind IN ('novel','comic')), from_item INTEGER NOT NULL, to_item INTEGER NOT NULL,
	 from_revision TEXT NOT NULL,to_revision TEXT NOT NULL, checked_at INTEGER NOT NULL, result_json TEXT NOT NULL,
	 PRIMARY KEY(kind,from_item,to_item), FOREIGN KEY(from_item) REFERENCES library_items(id) ON DELETE CASCADE,
	 FOREIGN KEY(to_item) REFERENCES library_items(id) ON DELETE CASCADE)`,
	`CREATE TABLE IF NOT EXISTS reading_source_checks (
	 kind TEXT NOT NULL CHECK(kind IN ('novel','comic')), source_id TEXT NOT NULL,
	 checked_at INTEGER NOT NULL, deep_at INTEGER NOT NULL DEFAULT 0,
	 state TEXT NOT NULL, score INTEGER NOT NULL DEFAULT -1,
	 confidence TEXT NOT NULL DEFAULT 'insufficient', summary_json TEXT NOT NULL DEFAULT '',
	 failures INTEGER NOT NULL DEFAULT 0,retry_after INTEGER NOT NULL DEFAULT 0,
	 PRIMARY KEY(kind,source_id))`,
	`CREATE INDEX IF NOT EXISTS idx_reading_checks_due ON reading_source_checks(kind,checked_at)`,
	`CREATE TABLE IF NOT EXISTS reading_work_sources (
	 kind TEXT NOT NULL CHECK(kind IN ('novel','comic')), item_id INTEGER NOT NULL,
	 source_key TEXT NOT NULL, revision TEXT NOT NULL, title_key TEXT NOT NULL,
	 author_key TEXT NOT NULL, seen_at INTEGER NOT NULL,
	 PRIMARY KEY(kind,item_id), FOREIGN KEY(item_id) REFERENCES library_items(id) ON DELETE CASCADE)`,
	`CREATE INDEX IF NOT EXISTS idx_reading_work_route ON reading_work_sources(kind,title_key,source_key)`,
	`CREATE TABLE IF NOT EXISTS reading_chapter_evidence (
	 kind TEXT NOT NULL CHECK(kind IN ('novel','comic')), chapter_id INTEGER NOT NULL,
	 revision TEXT NOT NULL, checked_at INTEGER NOT NULL, evidence TEXT NOT NULL,
	 PRIMARY KEY(kind,chapter_id), FOREIGN KEY(chapter_id) REFERENCES library_chapters(id) ON DELETE CASCADE)`,
}

func SaveReadingAlignment(ctx context.Context, kind model.SourceKind, fromItem, toItem int, fromRevision, toRevision, data string) error {
	if (kind != model.SourceKindNovel && kind != model.SourceKindComic) || len(data) > 16<<20 {
		return fmt.Errorf("invalid reading alignment")
	}
	_, err := instance.ExecContext(ctx, `INSERT INTO reading_chapter_alignments(kind,from_item,to_item,from_revision,to_revision,checked_at,result_json)
	 VALUES(?,?,?,?,?,?,?) ON CONFLICT(kind,from_item,to_item) DO UPDATE SET from_revision=excluded.from_revision,to_revision=excluded.to_revision,checked_at=excluded.checked_at,result_json=excluded.result_json`, kind, fromItem, toItem, fromRevision, toRevision, time.Now().Unix(), data)
	return err
}

func migrateReadingRouting(tx *sqlx.Tx) error {
	for _, query := range readingRoutingSchema {
		if _, err := tx.Exec(query); err != nil {
			return err
		}
	}
	return nil
}

// ReadingSourceCheck retains the latest light status and latest independent
// deep result. Score -1 means unmeasured, not zero quality.
type ReadingSourceCheck struct {
	Kind        string `db:"kind"`
	SourceID    string `db:"source_id"`
	CheckedAt   int64  `db:"checked_at"`
	DeepAt      int64  `db:"deep_at"`
	State       string `db:"state"`
	Score       int    `db:"score"`
	Confidence  string `db:"confidence"`
	SummaryJSON string `db:"summary_json"`
	Failures    int    `db:"failures"`
	RetryAfter  int64  `db:"retry_after"`
}

func ReadReadingSourceChecks(ctx context.Context, kind model.SourceKind) (map[string]ReadingSourceCheck, error) {
	if kind != model.SourceKindNovel && kind != model.SourceKindComic {
		return nil, fmt.Errorf("invalid reading mode")
	}
	var rows []ReadingSourceCheck
	if err := instance.SelectContext(ctx, &rows, `SELECT * FROM reading_source_checks WHERE kind=?`, kind); err != nil {
		return nil, err
	}
	out := make(map[string]ReadingSourceCheck, len(rows))
	for _, row := range rows {
		out[row.SourceID] = row
	}
	return out, nil
}

func SaveReadingSourceCheck(ctx context.Context, row ReadingSourceCheck) error {
	if (row.Kind != "novel" && row.Kind != "comic") || row.SourceID == "" || row.CheckedAt <= 0 || row.Score < -1 || row.Score > 100 || len(row.SummaryJSON) > 2<<20 {
		return fmt.Errorf("invalid source check")
	}
	failed := 0
	switch row.State {
	case "network_unknown", "invalid_address", "discovery_unfinished", "deep_unfinished", "search_unavailable":
		failed = 1
	}
	_, err := instance.ExecContext(ctx, `INSERT INTO reading_source_checks(kind,source_id,checked_at,deep_at,state,score,confidence,summary_json,failures,retry_after)
	 VALUES(?,?,?,?,?,?,?,?,?,0) ON CONFLICT(kind,source_id) DO UPDATE SET checked_at=excluded.checked_at,state=excluded.state,
	 failures=CASE WHEN excluded.failures>0 THEN MIN(reading_source_checks.failures+1,100) ELSE 0 END,
	 retry_after=CASE WHEN excluded.failures>0 AND reading_source_checks.failures>=2 THEN excluded.checked_at+172800 ELSE 0 END,
	 deep_at=CASE WHEN excluded.deep_at>0 THEN excluded.deep_at ELSE reading_source_checks.deep_at END,
	 score=CASE WHEN excluded.deep_at>0 THEN excluded.score ELSE reading_source_checks.score END,
	 confidence=CASE WHEN excluded.deep_at>0 THEN excluded.confidence ELSE reading_source_checks.confidence END,
	 summary_json=CASE WHEN excluded.deep_at>0 THEN excluded.summary_json ELSE reading_source_checks.summary_json END`, row.Kind, row.SourceID, row.CheckedAt, row.DeepAt, row.State, row.Score, row.Confidence, row.SummaryJSON, failed)
	return err
}

type ReadingWorkEvidence struct {
	ItemID              int
	TitleKey, AuthorKey string
}

func RecordReadingWorks(ctx context.Context, kind model.SourceKind, sourceKey, revision string, rows []ReadingWorkEvidence) (err error) {
	if kind != model.SourceKindNovel && kind != model.SourceKindComic {
		return fmt.Errorf("invalid reading mode")
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
	for _, row := range rows {
		if row.ItemID <= 0 || row.TitleKey == "" {
			continue
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO reading_work_sources(kind,item_id,source_key,revision,title_key,author_key,seen_at)
		 SELECT ?,id,?,?,?,?,? FROM library_items WHERE id=? AND kind=? AND source_key=?
		 ON CONFLICT(kind,item_id) DO UPDATE SET revision=excluded.revision,title_key=excluded.title_key,author_key=excluded.author_key,seen_at=excluded.seen_at`, kind, sourceKey, revision, row.TitleKey, row.AuthorKey, time.Now().Unix(), row.ItemID, kind, sourceKey)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func RecordReadingChapter(ctx context.Context, kind model.SourceKind, chapterID int, revision, evidence string) error {
	if kind != model.SourceKindNovel && kind != model.SourceKindComic {
		return fmt.Errorf("invalid reading mode")
	}
	if evidence != "text" && evidence != "page_list" && evidence != "image" {
		return fmt.Errorf("invalid reading evidence")
	}
	if (kind == model.SourceKindNovel && evidence != "text") || (kind == model.SourceKindComic && evidence == "text") {
		return fmt.Errorf("evidence belongs to a different reading mode")
	}
	_, err := instance.ExecContext(ctx, `INSERT INTO reading_chapter_evidence(kind,chapter_id,revision,checked_at,evidence)
	 SELECT ?,c.id,?,?,? FROM library_chapters c JOIN library_items i ON i.id=c.item_id WHERE c.id=? AND i.kind=?
	 ON CONFLICT(kind,chapter_id) DO UPDATE SET revision=excluded.revision,
	 checked_at=CASE WHEN reading_chapter_evidence.evidence='image' AND excluded.evidence='page_list' AND reading_chapter_evidence.revision=excluded.revision THEN reading_chapter_evidence.checked_at ELSE excluded.checked_at END,
	 evidence=CASE WHEN reading_chapter_evidence.evidence='image' AND excluded.evidence='page_list' AND reading_chapter_evidence.revision=excluded.revision THEN 'image' ELSE excluded.evidence END`, kind, revision, time.Now().Unix(), evidence, chapterID, kind)
	return err
}

// A validated image only confirms a sampled image, never an entire episode.
// Bind it to an existing exact chapter referer; do not infer IDs from CDN URLs.
func RecordReadingImage(ctx context.Context, sourceKey, revision, referer string) error {
	_, err := instance.ExecContext(ctx, `INSERT INTO reading_chapter_evidence(kind,chapter_id,revision,checked_at,evidence)
	 SELECT 'comic',c.id,?,?,'image' FROM library_chapters c JOIN library_items i ON i.id=c.item_id
	 WHERE i.kind='comic' AND i.source_key=? AND c.remote_url=? AND c.lifecycle_state='active'
	 ON CONFLICT(kind,chapter_id) DO UPDATE SET revision=excluded.revision,checked_at=excluded.checked_at,evidence=excluded.evidence
	 WHERE reading_chapter_evidence.checked_at < excluded.checked_at-3600 OR reading_chapter_evidence.revision<>excluded.revision OR reading_chapter_evidence.evidence<>'image'`, revision, time.Now().Unix(), sourceKey, referer)
	return err
}

type ReadingRouteEvidence struct {
	SourceKey string `db:"source_key"`
	Revision  string `db:"revision"`
	Readable  int    `db:"readable"`
}

// KnownReadingRoutes returns observed exact-title coverage without asserting
// that sources not in this sparse table lack the work.
func KnownReadingRoutes(ctx context.Context, kind model.SourceKind, titleKey string) ([]ReadingRouteEvidence, error) {
	var rows []ReadingRouteEvidence
	err := instance.SelectContext(ctx, &rows, `SELECT w.source_key,w.revision,
	 COUNT(CASE WHEN e.evidence IN ('text','image') THEN e.chapter_id END) AS readable FROM reading_work_sources w
	 JOIN library_items i ON i.id=w.item_id AND i.kind=w.kind
	 LEFT JOIN library_chapters c ON c.item_id=w.item_id AND c.lifecycle_state='active'
	 LEFT JOIN reading_chapter_evidence e ON e.chapter_id=c.id AND e.kind=w.kind AND e.revision=w.revision AND e.checked_at>=?
	 WHERE w.kind=? AND w.title_key=? AND i.lifecycle_state='active' AND w.seen_at>=?
	 GROUP BY w.source_key,w.revision`, time.Now().Add(-7*24*time.Hour).Unix(), kind, titleKey, time.Now().Add(-30*24*time.Hour).Unix())
	return rows, err
}
