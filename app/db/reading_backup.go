package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/jmoiron/sqlx"
	"strings"
	"time"

	"cczjVideo/app/apperror"
	"cczjVideo/app/model"
	"cczjVideo/app/readingstate"
)

type BackupReadingRecord struct {
	Kind string `json:"kind" db:"kind"`
	readingstate.Record
}
type BackupReading struct {
	Records  []BackupReadingRecord   `json:"records"`
	Items    []*model.LibraryItem    `json:"items"`
	Chapters []*model.LibraryChapter `json:"chapters"`
}

// ReadReadingBackup includes the catalog referenced by durable records. Local
// item/chapter IDs are translated during merge; they are never portable identity.
func (s *ReadingStateStore) ReadBackup(ctx context.Context) (*BackupReading, error) {
	tx, err := s.database.BeginTxx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	result, err := readReadingBackup(ctx, tx)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}

func readReadingBackup(ctx context.Context, tx *sqlx.Tx) (*BackupReading, error) {
	var rows []struct {
		Kind   string `db:"kind"`
		Bucket string `db:"bucket"`
		Key    string `db:"record_key"`
		Value  string `db:"value"`
	}
	if err := tx.SelectContext(ctx, &rows, `SELECT kind,bucket,record_key,value FROM reading_state ORDER BY kind,bucket,record_key`); err != nil {
		return nil, err
	}
	result := &BackupReading{Records: make([]BackupReadingRecord, 0, len(rows))}
	for _, row := range rows {
		r := readingstate.Record{Bucket: row.Bucket, Key: row.Key, Value: row.Value}
		if err := r.Validate(row.Kind); err != nil {
			return nil, err
		}
		result.Records = append(result.Records, BackupReadingRecord{Kind: row.Kind, Record: r})
	}
	linked := `SELECT DISTINCT li.* FROM library_items li JOIN reading_state r ON r.kind=li.kind AND r.bucket IN ('shelf','progress') AND CAST(json_extract(r.value,'$.itemId') AS INTEGER)=li.id`
	if err := tx.SelectContext(ctx, &result.Items, linked); err != nil {
		return nil, err
	}
	if err := tx.SelectContext(ctx, &result.Chapters, `SELECT lc.* FROM library_chapters lc WHERE item_id IN (SELECT li.id FROM library_items li JOIN reading_state r ON r.kind=li.kind AND r.bucket IN ('shelf','progress') AND CAST(json_extract(r.value,'$.itemId') AS INTEGER)=li.id)`); err != nil {
		return nil, err
	}
	// Legacy offline entries may outlive their catalog row. Export their snapshot
	// as an unavailable placeholder instead of losing them or reusing an unrelated ID.
	byKey := make(map[string]*model.LibraryItem)
	maxID := 0
	for _, item := range result.Items {
		byKey[item.Kind+"\x00"+item.SourceKey+"\x00"+item.RemoteID] = item
		if item.Id > maxID {
			maxID = item.Id
		}
	}
	for index, record := range result.Records {
		if record.Bucket == "preferences" {
			continue
		}
		var value map[string]any
		if err := json.Unmarshal([]byte(record.Value), &value); err != nil {
			return nil, err
		}
		item := byKey[record.Key]
		if item == nil {
			maxID++
			parts := strings.Split(record.Key, "\x00")
			snapshot := value
			if record.Bucket == "shelf" && record.Kind == "novel" {
				snapshot = value["item"].(map[string]any)
			}
			title, _ := snapshot["title"].(string)
			if title == "" {
				title = record.Key
			}
			cover, _ := snapshot["cover"].(string)
			item = &model.LibraryItem{Id: maxID, Kind: record.Kind, SourceKey: parts[1], RemoteID: parts[2], Title: title, Cover: cover, LifecycleState: "deleted"}
			byKey[record.Key] = item
			result.Items = append(result.Items, item)
		}
		if int(value["itemId"].(float64)) != item.Id {
			value["itemId"] = item.Id
			if record.Bucket == "shelf" && record.Kind == "novel" {
				value["item"].(map[string]any)["id"] = item.Id
			}
			if record.Bucket == "progress" {
				if record.Kind == "novel" {
					value["chapterId"] = 0
				} else {
					value["episodeId"] = 0
				}
			}
			raw, err := json.Marshal(value)
			if err != nil {
				return nil, err
			}
			result.Records[index].Value = string(raw)
		}
	}
	needed := make(map[string]bool)
	for _, record := range result.Records {
		if record.Bucket != "preferences" {
			needed[record.Key] = true
		}
	}
	keptItems := result.Items[:0]
	keptIDs := make(map[int]bool)
	for _, item := range result.Items {
		if needed[item.Kind+"\x00"+item.SourceKey+"\x00"+item.RemoteID] {
			keptItems = append(keptItems, item)
			keptIDs[item.Id] = true
		}
	}
	result.Items = keptItems
	keptChapters := result.Chapters[:0]
	for _, chapter := range result.Chapters {
		if keptIDs[chapter.ItemID] {
			keptChapters = append(keptChapters, chapter)
		}
	}
	result.Chapters = keptChapters
	return result, nil
}

func (s *ReadingStateStore) MergeBackup(ctx context.Context, backup *BackupReading) (int, error) {
	if backup == nil {
		return 0, nil
	}
	tx, err := s.database.BeginTxx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	applied, err := mergeReadingBackup(ctx, tx, backup)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return applied, nil
}

func mergeReadingBackup(ctx context.Context, tx *sqlx.Tx, backup *BackupReading) (int, error) {
	if backup == nil {
		return 0, nil
	}
	if len(backup.Records)+len(backup.Items)+len(backup.Chapters) > 200000 {
		return 0, apperror.Wrap(apperror.Corrupt, fmt.Errorf("reading backup exceeds item limit"), "")
	}
	for _, r := range backup.Records {
		if err := r.Validate(r.Kind); err != nil {
			return 0, apperror.Wrap(apperror.Corrupt, err, "")
		}
	}
	items := make(map[int]*model.LibraryItem)
	chaptersByID := make(map[int]*model.LibraryChapter)
	for _, chapter := range backup.Chapters {
		if chapter != nil {
			if chaptersByID[chapter.Id] != nil {
				return 0, apperror.Wrap(apperror.Corrupt, fmt.Errorf("duplicate backup chapter identity"), "")
			}
			chaptersByID[chapter.Id] = chapter
		}
	}
	for _, item := range backup.Items {
		if item == nil || item.Id <= 0 || item.RemoteID == "" || item.Title == "" {
			return 0, apperror.Wrap(apperror.Corrupt, fmt.Errorf("invalid reading backup item"), "")
		}
		if err := readingstate.ValidateKind(item.Kind); err != nil {
			return 0, apperror.Wrap(apperror.Corrupt, err, "")
		}
		if items[item.Id] != nil {
			return 0, apperror.Wrap(apperror.Corrupt, fmt.Errorf("duplicate backup item identity"), "")
		}
		items[item.Id] = item
	}
	for _, r := range backup.Records {
		if r.Bucket == "preferences" {
			continue
		}
		var value struct {
			ItemID int `json:"itemId"`
		}
		json.Unmarshal([]byte(r.Value), &value)
		item := items[value.ItemID]
		if item == nil || item.Kind+"\x00"+item.SourceKey+"\x00"+item.RemoteID != r.Key {
			return 0, apperror.Wrap(apperror.Corrupt, fmt.Errorf("reading backup record has no matching catalog identity"), "")
		}
		if r.Bucket == "progress" {
			var fields map[string]any
			json.Unmarshal([]byte(r.Value), &fields)
			field := "chapterId"
			if r.Kind == "comic" {
				field = "episodeId"
			}
			chapter := chaptersByID[int(fields[field].(float64))]
			if chapter != nil && chapter.ItemID != value.ItemID {
				return 0, apperror.Wrap(apperror.Corrupt, fmt.Errorf("reading progress references another item's chapter"), "")
			}
		}
	}
	ids := make(map[int]int)
	chapterIDs := make(map[int]int)
	for oldID, item := range items {
		_, err := tx.ExecContext(ctx, `INSERT INTO library_items(kind,source_key,remote_id,remote_url,title,author,cover,summary,genres,status,latest,words,remote_updated_at,lifecycle_state) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(kind,source_key,remote_id) DO NOTHING`, item.Kind, item.SourceKey, item.RemoteID, item.RemoteURL, item.Title, item.Author, item.Cover, item.Summary, item.Genres, item.Status, item.Latest, item.Words, item.RemoteUpdatedAt, item.LifecycleState)
		if err != nil {
			return 0, err
		}
		var id int
		if err := tx.GetContext(ctx, &id, `SELECT id FROM library_items WHERE kind=? AND source_key=? AND remote_id=?`, item.Kind, item.SourceKey, item.RemoteID); err != nil {
			return 0, err
		}
		ids[oldID] = id
	}
	for _, chapter := range backup.Chapters {
		if chapter == nil || chapter.Id <= 0 || ids[chapter.ItemID] == 0 || chapter.RemoteID == "" {
			return 0, apperror.Wrap(apperror.Corrupt, fmt.Errorf("invalid reading backup chapter"), "")
		}
		if chapterIDs[chapter.Id] != 0 {
			return 0, apperror.Wrap(apperror.Corrupt, fmt.Errorf("duplicate backup chapter identity"), "")
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO library_chapters(item_id,remote_id,remote_url,title,ord,volume,is_volume,access,remote_uploaded_at,lifecycle_state) VALUES(?,?,?,?,?,?,?,?,?,?) ON CONFLICT(item_id,remote_id) DO NOTHING`, ids[chapter.ItemID], chapter.RemoteID, chapter.RemoteURL, chapter.Title, chapter.Ord, chapter.Volume, boolToInt(chapter.IsVolume), chapter.Access, chapter.RemoteUploadedAt, chapter.LifecycleState)
		if err != nil {
			return 0, err
		}
		var id int
		if err := tx.GetContext(ctx, &id, `SELECT id FROM library_chapters WHERE item_id=? AND remote_id=?`, ids[chapter.ItemID], chapter.RemoteID); err != nil {
			return 0, err
		}
		chapterIDs[chapter.Id] = id
	}
	applied := 0
	for _, r := range backup.Records {
		var value map[string]any
		json.Unmarshal([]byte(r.Value), &value)
		if r.Bucket != "preferences" {
			oldID := int(value["itemId"].(float64))
			value["itemId"] = ids[oldID]
			if r.Bucket == "shelf" && r.Kind == "novel" {
				value["item"].(map[string]any)["id"] = ids[oldID]
			}
			if r.Bucket == "progress" {
				field := "chapterId"
				remoteField := "chapterRemoteId"
				if r.Kind == "comic" {
					field, remoteField = "episodeId", "episodeRemoteId"
				}
				oldChapter := int(value[field].(float64))
				if mapped := chapterIDs[oldChapter]; mapped != 0 {
					value[field] = mapped
				} else {
					// An absent chapter remains explicitly unavailable, never points
					// at an unrelated local row with the same numeric ID.
					value[field] = 0
					if remote, ok := value[remoteField].(string); ok && remote != "" {
						var id int
						err := tx.GetContext(ctx, &id, `SELECT id FROM library_chapters WHERE item_id=? AND remote_id=?`, ids[oldID], remote)
						if err == nil {
							value[field] = id
						}
					}
				}
			}
		}
		raw, err := json.Marshal(value)
		if err != nil {
			return 0, err
		}
		result, err := tx.ExecContext(ctx, `INSERT INTO reading_state(kind,bucket,record_key,value,updated_at) VALUES(?,?,?,?,?) ON CONFLICT DO NOTHING`, r.Kind, r.Bucket, r.Key, string(raw), time.Now().UnixMilli())
		if err != nil {
			return 0, err
		}
		n, _ := result.RowsAffected()
		applied += int(n)
		if _, err := tx.ExecContext(ctx, `INSERT INTO reading_state_imports(kind,imported_at) VALUES(?,?) ON CONFLICT DO NOTHING`, strings.TrimSpace(r.Kind), time.Now().UnixMilli()); err != nil {
			return 0, err
		}
	}
	return applied, nil
}
