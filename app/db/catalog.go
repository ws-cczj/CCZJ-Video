package db

import (
	"cczjVideo/app/model"
	cryptorand "crypto/rand"
	"encoding/base64"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/jmoiron/sqlx"
)

// CatalogItem is the intentionally small, durable projection of a source item.
// Detail text and all playback/download URLs must never be added here.
type CatalogItem struct {
	ID             int64  `db:"id" json:"id"`
	SourceKey      string `db:"source_key" json:"source_key"`
	SourceVodID    string `db:"source_vod_id" json:"source_vod_id"`
	GlobalID       int64  `db:"global_id" json:"global_id"`
	SourceTypeID   string `db:"source_type_id" json:"source_type_id"`
	GlobalTypeID   int64  `db:"global_type_id" json:"global_type_id"`
	TypeName       string `db:"type_name" json:"type_name"`
	VodName        string `db:"vod_name" json:"vod_name"`
	VodPic         string `db:"vod_pic" json:"vod_pic"`
	VodRemarks     string `db:"vod_remarks" json:"vod_remarks"`
	VodYear        string `db:"vod_year" json:"vod_year"`
	VodArea        string `db:"vod_area" json:"vod_area"`
	VodTime        string `db:"vod_time" json:"vod_time"`
	LifecycleState string `db:"lifecycle_state" json:"lifecycle_state"`
}

// SettingReviveDeletedCatalogItems gates whether automatic collection writes
// restore entries the user removed from a source (lifecycle_state='deleted').
const SettingReviveDeletedCatalogItems = "catalog_revive_deleted"

// ReviveDeletedCatalogItems reports the current setting. Missing key, empty
// value or a read failure all mean "revive", which is the shipped default.
func ReviveDeletedCatalogItems() bool {
	raw, err := GetSetting(SettingReviveDeletedCatalogItems)
	if err != nil {
		return true
	}
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		return true
	}
	return raw == "true" || raw == "1"
}

func UpsertCatalogItems(sourceKey string, videos []*model.Video) error {
	return upsertCatalogItems(sourceKey, videos, ReviveDeletedCatalogItems())
}

// UpsertCatalogItemsWithRevival ignores the setting and always restores deleted
// entries. Used by explicit import paths: the user asked for this data by hand,
// so a previous deletion (usually produced by the same import earlier) must not
// keep it hidden.
func UpsertCatalogItemsWithRevival(sourceKey string, videos []*model.Video) error {
	return upsertCatalogItems(sourceKey, videos, true)
}

func upsertCatalogItems(sourceKey string, videos []*model.Video, revive bool) error {
	if err := model.ValidateSourceKey(sourceKey); err != nil {
		return err
	}
	videos = FilterEnabledCollectVideos(videos)
	if len(videos) == 0 {
		return nil
	}
	tx, err := instance.Beginx()
	if err != nil {
		return fmt.Errorf("begin catalog upsert: %w", err)
	}
	defer tx.Rollback()
	resolver, err := newCatalogIdentityResolver(tx)
	if err != nil {
		return fmt.Errorf("load catalog identities: %w", err)
	}
	// Spelled out in SQL instead of bound per row: sqlite's ON CONFLICT DO
	// UPDATE cannot read the target column through a placeholder without
	// disturbing the VALUES binding order, and both branches are literals here.
	lifecycleOnConflict := "source_videos.lifecycle_state"
	if revive {
		lifecycleOnConflict = "'active'"
	}
	q := fmt.Sprintf(`INSERT INTO source_videos (source_key, source_vod_id, global_id, source_type_id, global_type_id, type_name, vod_name, vod_pic, vod_remarks, vod_year, vod_area, vod_time, lifecycle_state, updated_at)
		VALUES (:source_key,:source_vod_id,:global_id,:source_type_id,:global_type_id,:type_name,:vod_name,:vod_pic,:vod_remarks,:vod_year,:vod_area,:vod_time,'active',CURRENT_TIMESTAMP)
		ON CONFLICT(source_key,source_vod_id) DO UPDATE SET global_id=excluded.global_id, source_type_id=excluded.source_type_id, global_type_id=excluded.global_type_id, type_name=excluded.type_name, vod_name=excluded.vod_name, vod_pic=excluded.vod_pic, vod_remarks=excluded.vod_remarks, vod_year=excluded.vod_year, vod_area=excluded.vod_area, vod_time=excluded.vod_time, lifecycle_state=%s, updated_at=CURRENT_TIMESTAMP`, lifecycleOnConflict)
	rows := make([]catalogUpsertRow, 0, len(videos))
	typeRows := make(map[string]sourceTypeUpsertRow)
	for _, v := range videos {
		if v == nil || strings.TrimSpace(v.VodId.String()) == "" || strings.TrimSpace(v.VodName) == "" {
			continue
		}
		gid, gtid, err := resolver.upsertVideo(v)
		if err != nil {
			return fmt.Errorf("catalog identity for %q: %w", v.VodName, err)
		}
		row := catalogUpsertRow{SourceKey: sourceKey, SourceVodID: strings.TrimSpace(v.VodId.String()), GlobalID: gid,
			SourceTypeID: strings.TrimSpace(v.TypeId.String()), GlobalTypeID: gtid, TypeName: v.TypeName,
			VodName: v.VodName, VodPic: v.VodPic, VodRemarks: v.VodRemarks, VodYear: v.VodYear, VodArea: v.VodArea, VodTime: v.VodTime}
		rows = append(rows, row)
		if row.SourceTypeID != "" {
			typeRows[row.SourceTypeID] = sourceTypeUpsertRow{SourceKey: sourceKey, SourceTypeID: row.SourceTypeID, GlobalTypeID: row.GlobalTypeID, TypeName: row.TypeName}
		}
	}
	if len(rows) == 0 {
		return nil
	}
	if _, err := tx.NamedExec(q, rows); err != nil {
		return fmt.Errorf("upsert source catalog: %w", err)
	}
	if len(typeRows) > 0 {
		rows := make([]sourceTypeUpsertRow, 0, len(typeRows))
		for _, row := range typeRows {
			rows = append(rows, row)
		}
		if _, err := tx.NamedExec(`INSERT INTO source_types(source_key,source_type_id,global_type_id,type_name,updated_at) VALUES (:source_key,:source_type_id,:global_type_id,:type_name,CURRENT_TIMESTAMP)
			ON CONFLICT(source_key,source_type_id) DO UPDATE SET global_type_id=excluded.global_type_id,type_name=excluded.type_name,updated_at=CURRENT_TIMESTAMP`, rows); err != nil {
			return fmt.Errorf("upsert source type mapping: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit catalog upsert: %w", err)
	}
	return nil
}

type catalogUpsertRow struct {
	SourceKey    string `db:"source_key"`
	SourceVodID  string `db:"source_vod_id"`
	GlobalID     int64  `db:"global_id"`
	SourceTypeID string `db:"source_type_id"`
	GlobalTypeID int64  `db:"global_type_id"`
	TypeName     string `db:"type_name"`
	VodName      string `db:"vod_name"`
	VodPic       string `db:"vod_pic"`
	VodRemarks   string `db:"vod_remarks"`
	VodYear      string `db:"vod_year"`
	VodArea      string `db:"vod_area"`
	VodTime      string `db:"vod_time"`
}

type sourceTypeUpsertRow struct {
	SourceKey    string `db:"source_key"`
	SourceTypeID string `db:"source_type_id"`
	GlobalTypeID int64  `db:"global_type_id"`
	TypeName     string `db:"type_name"`
}

type catalogTypeCandidate struct {
	ID   int64  `db:"id"`
	Name string `db:"type_name"`
}

type catalogVideoCandidate struct {
	ID      int64  `db:"id"`
	VodName string `db:"vod_name"`
	TypeID  int64  `db:"type_id"`
	Year    string `db:"year"`
}

// catalogIdentityResolver confines one collection batch's type and video
// identity work to the transaction.  Its in-memory indexes remove repeated
// full-table lookups for duplicate items and previously resolved type names.
type catalogIdentityResolver struct {
	tx              *sqlx.Tx
	types           []catalogTypeCandidate
	videos          []catalogVideoCandidate
	typesByExact    map[string]int64
	videosByExact   map[string][]catalogVideoCandidate
	videosByNorm    map[string][]catalogVideoCandidate
	resolvedVideoID map[string]int64
}

func newCatalogIdentityResolver(tx *sqlx.Tx) (*catalogIdentityResolver, error) {
	r := &catalogIdentityResolver{
		tx:              tx,
		typesByExact:    make(map[string]int64),
		videosByExact:   make(map[string][]catalogVideoCandidate),
		videosByNorm:    make(map[string][]catalogVideoCandidate),
		resolvedVideoID: make(map[string]int64),
	}
	if err := tx.Select(&r.types, `SELECT id,type_name FROM global_types`); err != nil {
		return nil, err
	}
	for _, candidate := range r.types {
		r.typesByExact[candidate.Name] = candidate.ID
	}
	if err := tx.Select(&r.videos, `SELECT id,vod_name,type_id,year FROM global_video`); err != nil {
		return nil, err
	}
	for _, candidate := range r.videos {
		r.indexVideo(candidate)
	}
	return r, nil
}

func (r *catalogIdentityResolver) indexVideo(candidate catalogVideoCandidate) {
	r.videosByExact[candidate.VodName] = append(r.videosByExact[candidate.VodName], candidate)
	normalized := sqlNorm(candidate.VodName)
	r.videosByNorm[normalized] = append(r.videosByNorm[normalized], candidate)
}

func (r *catalogIdentityResolver) refreshVideoCandidate(id, typeID int64, year string) {
	dirtyIndexes := false
	for i := range r.videos {
		if r.videos[i].ID != id {
			continue
		}
		if typeID != 0 && r.videos[i].TypeID != typeID {
			dirtyIndexes = true
			r.videos[i].TypeID = typeID
		}
		if year != "" && r.videos[i].Year != year {
			r.videos[i].Year = year
		}
		break
	}
	if !dirtyIndexes {
		return
	}
	r.videosByExact = make(map[string][]catalogVideoCandidate, len(r.videosByExact))
	r.videosByNorm = make(map[string][]catalogVideoCandidate, len(r.videosByNorm))
	for _, candidate := range r.videos {
		r.indexVideo(candidate)
	}
}

func (r *catalogIdentityResolver) resolveTypeID(typeName string) (int64, error) {
	typeName = strings.TrimSpace(typeName)
	if typeName == "" {
		return 0, nil
	}
	if id := r.typesByExact[typeName]; id > 0 {
		return id, nil
	}
	normalized := normalizeTypeName(typeName)
	for _, candidate := range r.types {
		if normalizeTypeName(candidate.Name) == normalized {
			return candidate.ID, nil
		}
	}
	for _, candidate := range r.types {
		candidateNorm := normalizeTypeName(candidate.Name)
		if candidateNorm == "" || (!strings.Contains(candidateNorm, normalized) && !strings.Contains(normalized, candidateNorm)) {
			continue
		}
		if utf8.RuneCountInString(candidateNorm)-utf8.RuneCountInString(normalized) <= 1 && utf8.RuneCountInString(normalized)-utf8.RuneCountInString(candidateNorm) <= 1 {
			return candidate.ID, nil
		}
	}
	result, err := r.tx.Exec(`INSERT OR IGNORE INTO global_types(type_name) VALUES (?)`, typeName)
	if err != nil {
		return 0, err
	}
	id, err := result.LastInsertId()
	if err != nil || id == 0 {
		if err := r.tx.Get(&id, `SELECT id FROM global_types WHERE type_name=?`, typeName); err != nil {
			return 0, err
		}
	}
	candidate := catalogTypeCandidate{ID: id, Name: typeName}
	r.types = append(r.types, candidate)
	r.typesByExact[typeName] = id
	return id, nil
}

func (r *catalogIdentityResolver) upsertVideo(video *model.Video) (int64, int64, error) {
	typeID, err := r.resolveTypeID(video.TypeName)
	if err != nil {
		return 0, 0, err
	}
	identityKey := video.VodName + "\x00" + video.VodYear + "\x00" + fmt.Sprint(typeID)
	id := r.resolvedVideoID[identityKey]
	if id == 0 {
		id, err = r.resolveVideoID(video.VodName, video.VodYear, typeID)
		if err != nil {
			return 0, 0, err
		}
		r.resolvedVideoID[identityKey] = id
	}
	if _, err := r.tx.Exec(`UPDATE global_video SET
		type_id=CASE WHEN ? != 0 THEN ? ELSE type_id END,
		year=CASE WHEN ? != '' THEN ? ELSE year END,
		area=CASE WHEN ? != '' THEN ? ELSE area END,
		pic=CASE WHEN ? != '' THEN ? ELSE pic END,
		updated_at=CURRENT_TIMESTAMP WHERE id=?`,
		typeID, typeID, video.VodYear, video.VodYear, video.VodArea, video.VodArea, video.VodPic, video.VodPic, id); err != nil {
		return 0, 0, err
	}
	r.refreshVideoCandidate(id, typeID, video.VodYear)
	return id, typeID, nil
}

func (r *catalogIdentityResolver) resolveVideoID(vodName, year string, typeID int64) (int64, error) {
	matchAnyType := typeID == 0
	if id := matchingCatalogVideo(r.videosByExact[vodName], typeID, matchAnyType); id > 0 {
		return id, nil
	}
	normalized := sqlNorm(vodName)
	if id := matchingCatalogVideo(r.videosByNorm[normalized], typeID, matchAnyType); id > 0 {
		return id, nil
	}
	var best *catalogVideoCandidate
	bestSimilarity := 0.0
	for i := range r.videos {
		candidate := &r.videos[i]
		if !matchAnyType && candidate.TypeID != typeID && candidate.TypeID != 0 {
			continue
		}
		similarity := nameSimilarity(vodName, candidate.VodName)
		if similarity < 0.90 || hasSeasonSuffix(vodName, candidate.VodName) {
			continue
		}
		if year == "" || candidate.Year == "" || year == candidate.Year {
			return candidate.ID, nil
		}
		if similarity >= 0.95 && similarity > bestSimilarity {
			bestSimilarity = similarity
			best = candidate
		}
	}
	if best != nil {
		return best.ID, nil
	}

	result, err := r.tx.Exec(`INSERT OR IGNORE INTO global_video(vod_name,type_id,updated_at) VALUES (?,?,CURRENT_TIMESTAMP)`, vodName, typeID)
	if err != nil {
		return 0, err
	}
	id, _ := result.LastInsertId()
	if id == 0 {
		if err := r.tx.Get(&id, fmt.Sprintf(`SELECT id FROM global_video WHERE %s=? AND type_id=? LIMIT 1`, sqlNormExpr()), normalized, typeID); err != nil || id == 0 {
			if err := r.tx.Get(&id, fmt.Sprintf(`SELECT id FROM global_video WHERE %s=? AND type_id=0 LIMIT 1`, sqlNormExpr()), normalized); err != nil {
				return 0, err
			}
		}
	}
	if id <= 0 {
		return 0, fmt.Errorf("resolve inserted global video %q", vodName)
	}
	candidate := catalogVideoCandidate{ID: id, VodName: vodName, TypeID: typeID, Year: year}
	r.videos = append(r.videos, candidate)
	r.indexVideo(candidate)
	return id, nil
}

func matchingCatalogVideo(candidates []catalogVideoCandidate, typeID int64, matchAnyType bool) int64 {
	for _, candidate := range candidates {
		if matchAnyType || candidate.TypeID == typeID || candidate.TypeID == 0 {
			return candidate.ID
		}
	}
	return 0
}

// ExistingCatalogVodIDs reports which of the given upstream vod_ids already have
// an active catalog row for the source. Search handlers call it before caching
// their own hits so the UI can flag results the user previously brought in.
func ExistingCatalogVodIDs(sourceKey string, vodIDs []string) (map[string]bool, error) {
	existing := make(map[string]bool, len(vodIDs))
	args := make([]any, 0, len(vodIDs)+1)
	args = append(args, sourceKey)
	marks := make([]string, 0, len(vodIDs))
	for _, id := range vodIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		marks = append(marks, "?")
		args = append(args, id)
	}
	if len(marks) == 0 {
		return existing, nil
	}
	q := `SELECT source_vod_id FROM source_videos WHERE source_key=? AND lifecycle_state='active' AND source_vod_id IN (` + strings.Join(marks, ",") + `)`
	var found []string
	if err := instance.Select(&found, q, args...); err != nil {
		return nil, err
	}
	for _, id := range found {
		existing[id] = true
	}
	return existing, nil
}

// SearchCatalogByTitle finds active catalog rows whose title contains the
// keyword. Unlike GetCatalogVideoPage it deliberately ignores type and remarks
// so a keyword that happens to name a category does not flood the results.
func SearchCatalogByTitle(sourceKey, keyword string, limit int) ([]*model.Video, error) {
	if limit <= 0 {
		limit = 24
	}
	q := `SELECT source_vod_id,global_id,source_type_id,type_name,vod_name,vod_pic,vod_remarks,vod_year,vod_area,vod_time
		FROM source_videos
		WHERE source_key=? AND lifecycle_state='active' AND ` + catalogTypeVisibilityClause("source_videos") + ` AND vod_name LIKE ?
		ORDER BY vod_time DESC, id DESC LIMIT ?`
	var rows []catalogTitleRow
	if err := instance.Select(&rows, q, sourceKey, "%"+strings.TrimSpace(keyword)+"%", limit); err != nil {
		return nil, err
	}
	out := make([]*model.Video, 0, len(rows))
	for _, r := range rows {
		out = append(out, &model.Video{
			VodId: model.FlexibleString(r.SourceVodID), GlobalId: r.GlobalID,
			TypeId: model.FlexibleString(r.SourceTypeID), TypeName: r.TypeName,
			VodName: r.VodName, VodPic: r.VodPic, VodRemarks: r.VodRemarks,
			VodYear: r.VodYear, VodArea: r.VodArea, VodTime: r.VodTime,
			InCatalog: true,
		})
	}
	return out, nil
}

type catalogTitleRow struct {
	SourceVodID  string `db:"source_vod_id"`
	SourceTypeID string `db:"source_type_id"`
	TypeName     string `db:"type_name"`
	VodName      string `db:"vod_name"`
	VodPic       string `db:"vod_pic"`
	VodRemarks   string `db:"vod_remarks"`
	VodYear      string `db:"vod_year"`
	VodArea      string `db:"vod_area"`
	VodTime      string `db:"vod_time"`
	GlobalID     int64  `db:"global_id"`
}

func GetCatalogItem(sourceKey, vodID string) (*CatalogItem, error) {
	var item CatalogItem
	err := instance.Get(&item, `SELECT id,source_key,source_vod_id,global_id,source_type_id,global_type_id,type_name,vod_name,vod_pic,vod_remarks,vod_year,vod_area,vod_time,lifecycle_state FROM source_videos WHERE source_key=? AND source_vod_id=? AND lifecycle_state='active' AND `+catalogTypeVisibilityClause("source_videos"), sourceKey, vodID)
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func GetCatalogItemByGlobalID(sourceKey string, globalID int64) (*CatalogItem, error) {
	var item CatalogItem
	err := instance.Get(&item, `SELECT id,source_key,source_vod_id,global_id,source_type_id,global_type_id,type_name,vod_name,vod_pic,vod_remarks,vod_year,vod_area,vod_time,lifecycle_state FROM source_videos WHERE source_key=? AND global_id=? AND lifecycle_state='active' AND `+catalogTypeVisibilityClause("source_videos")+` ORDER BY updated_at DESC LIMIT 1`, sourceKey, globalID)
	if err != nil {
		return nil, err
	}
	return &item, nil
}

type CatalogPage struct {
	Videos     []*model.Video
	Total      int
	NextCursor string
}

// GetCatalogVideos retains the legacy page-number signature for internal
// callers. New browser requests should use GetCatalogVideoPage with its
// opaque cursor so deep pages never pay an OFFSET scan.
func GetCatalogVideos(sourceKey string, filter FilterParams) ([]*model.Video, int, error) {
	page, err := GetCatalogVideoPage(sourceKey, filter)
	if err != nil {
		return nil, 0, err
	}
	return page.Videos, page.Total, nil
}

func GetCatalogVideoPage(sourceKey string, filter FilterParams) (*CatalogPage, error) {
	if filter.PageSize <= 0 {
		filter.PageSize = 20
	}
	where := []string{"source_key=?", "lifecycle_state='active'", catalogTypeVisibilityClause("source_videos")}
	args := []any{sourceKey}
	if strings.TrimSpace(filter.TypeId) != "" && filter.TypeId != "all" {
		where = append(where, "source_type_id=?")
		args = append(args, filter.TypeId)
	}
	if strings.TrimSpace(filter.Year) != "" && filter.Year != "all" {
		where = append(where, "vod_year=?")
		args = append(args, filter.Year)
	}
	if strings.TrimSpace(filter.Area) != "" && filter.Area != "all" {
		where = append(where, "vod_area=?")
		args = append(args, filter.Area)
	}
	if strings.TrimSpace(filter.Keyword) != "" {
		where = append(where, "(vod_name LIKE ? OR vod_remarks LIKE ? OR type_name LIKE ?)")
		like := "%" + strings.TrimSpace(filter.Keyword) + "%"
		args = append(args, like, like, like)
	}
	if filter.RecentDays == 1 || filter.RecentDays == 7 || filter.RecentDays == 30 {
		where = append(where, "updated_at >= datetime('now', ?)")
		args = append(args, fmt.Sprintf("-%d days", filter.RecentDays))
	}
	clause := strings.Join(where, " AND ")
	var total int
	if err := instance.Get(&total, "SELECT COUNT(*) FROM source_videos WHERE "+clause, args...); err != nil {
		return nil, err
	}
	orderColumn := "vod_time"
	if filter.RecentDays > 0 {
		orderColumn = "updated_at"
	}
	if filter.Cursor != "" {
		cursorValue, cursorID, err := decodeCatalogCursor(filter.Cursor)
		if err != nil {
			return nil, fmt.Errorf("invalid catalog cursor: %w", err)
		}
		where = append(where, "("+orderColumn+" < ? OR ("+orderColumn+" = ? AND id < ?))")
		args = append(args, cursorValue, cursorValue, cursorID)
		clause = strings.Join(where, " AND ")
	}
	q := "SELECT id,source_vod_id,global_id,source_type_id,type_name,vod_name,vod_pic,vod_remarks,vod_year,vod_area,vod_time," + orderColumn + " AS cursor_value FROM source_videos WHERE " + clause + " ORDER BY " + orderColumn + " DESC, id DESC LIMIT ?"
	args = append(args, filter.PageSize+1)
	type row struct {
		ID          int    `db:"id"`
		VodID       string `db:"source_vod_id"`
		GlobalID    int64  `db:"global_id"`
		TypeID      string `db:"source_type_id"`
		TypeName    string `db:"type_name"`
		VodName     string `db:"vod_name"`
		VodPic      string `db:"vod_pic"`
		VodRemarks  string `db:"vod_remarks"`
		VodYear     string `db:"vod_year"`
		VodArea     string `db:"vod_area"`
		VodTime     string `db:"vod_time"`
		CursorValue string `db:"cursor_value"`
	}
	var rows []row
	if err := instance.Select(&rows, q, args...); err != nil {
		return nil, err
	}
	hasMore := len(rows) > filter.PageSize
	if hasMore {
		rows = rows[:filter.PageSize]
	}
	out := make([]*model.Video, 0, len(rows))
	for _, r := range rows {
		out = append(out, &model.Video{Id: r.ID, VodId: model.FlexibleString(r.VodID), GlobalId: r.GlobalID, TypeId: model.FlexibleString(r.TypeID), TypeName: r.TypeName, VodName: r.VodName, VodPic: r.VodPic, VodRemarks: r.VodRemarks, VodYear: r.VodYear, VodArea: r.VodArea, VodTime: r.VodTime})
	}
	nextCursor := ""
	if hasMore && len(rows) > 0 {
		last := rows[len(rows)-1]
		nextCursor = encodeCatalogCursor(last.CursorValue, last.ID)
	}
	return &CatalogPage{Videos: out, Total: total, NextCursor: nextCursor}, nil
}

func encodeCatalogCursor(value string, id int) string {
	return base64.RawURLEncoding.EncodeToString([]byte(value + "\x00" + strconv.Itoa(id)))
}

func decodeCatalogCursor(cursor string) (string, int, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return "", 0, err
	}
	parts := strings.Split(string(decoded), "\x00")
	if len(parts) != 2 || parts[0] == "" {
		return "", 0, fmt.Errorf("malformed cursor")
	}
	id, err := strconv.Atoi(parts[1])
	if err != nil || id <= 0 {
		return "", 0, fmt.Errorf("malformed cursor id")
	}
	return parts[0], id, nil
}

// CatalogExportRow is the source-owned, portable catalog projection used by
// streaming exports. It deliberately excludes remote detail and playback data.
type CatalogExportRow struct {
	SourceVodID  string `db:"source_vod_id"`
	SourceTypeID string `db:"source_type_id"`
	TypeName     string `db:"type_name"`
	VodName      string `db:"vod_name"`
	VodPic       string `db:"vod_pic"`
	VodRemarks   string `db:"vod_remarks"`
	VodYear      string `db:"vod_year"`
	VodArea      string `db:"vod_area"`
	VodTime      string `db:"vod_time"`
}

// StreamCatalogExport visits a source's active catalog one row at a time.
// Callers can encode directly to an output stream without constructing an
// unbounded in-memory slice or relying on a synthetic oversized page size.
func StreamCatalogExport(sourceKey string, visit func(CatalogExportRow) error) error {
	if err := model.ValidateSourceKey(sourceKey); err != nil {
		return err
	}
	if visit == nil {
		return fmt.Errorf("catalog export visitor is required")
	}
	rows, err := instance.Queryx(`SELECT source_vod_id,source_type_id,type_name,vod_name,vod_pic,vod_remarks,vod_year,vod_area,vod_time
		FROM source_videos WHERE source_key=? AND lifecycle_state='active' AND `+catalogTypeVisibilityClause("source_videos")+` ORDER BY id`, sourceKey)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var row CatalogExportRow
		if err := rows.StructScan(&row); err != nil {
			return err
		}
		if err := visit(row); err != nil {
			return err
		}
	}
	return rows.Err()
}

func GetCatalogYearsAndAreas(sourceKey string) ([]string, []string, error) {
	var years, areas []string
	visibility := `lifecycle_state='active' AND ` + catalogTypeVisibilityClause("source_videos")
	if err := instance.Select(&years, `SELECT DISTINCT vod_year FROM source_videos WHERE source_key=? AND `+visibility+` AND vod_year!='' ORDER BY vod_year DESC`, sourceKey); err != nil {
		return nil, nil, err
	}
	if err := instance.Select(&areas, `SELECT DISTINCT vod_area FROM source_videos WHERE source_key=? AND `+visibility+` AND vod_area!='' ORDER BY vod_area`, sourceKey); err != nil {
		return nil, nil, err
	}
	return years, areas, nil
}

func GetCatalogRecommend(sourceKey string, limit int, excludeIDs []string) ([]*model.Video, error) {
	if limit <= 0 {
		return []*model.Video{}, nil
	}
	args := []any{sourceKey}
	where := "source_key=? AND lifecycle_state='active' AND " + catalogTypeVisibilityClause("source_videos")
	if len(excludeIDs) > 0 {
		marks := make([]string, len(excludeIDs))
		for i, id := range excludeIDs {
			marks[i] = "?"
			args = append(args, id)
		}
		where += " AND source_vod_id NOT IN (" + strings.Join(marks, ",") + ")"
	}
	var maxID int64
	if err := instance.Get(&maxID, "SELECT COALESCE(MAX(id),0) FROM source_videos WHERE "+where, args...); err != nil {
		return nil, err
	}
	if maxID == 0 {
		return []*model.Video{}, nil
	}
	start := randomCatalogStart(maxID)
	type row struct {
		VodID      string `db:"source_vod_id"`
		GlobalID   int64  `db:"global_id"`
		TypeID     string `db:"source_type_id"`
		TypeName   string `db:"type_name"`
		VodName    string `db:"vod_name"`
		VodPic     string `db:"vod_pic"`
		VodRemarks string `db:"vod_remarks"`
		VodYear    string `db:"vod_year"`
		VodArea    string `db:"vod_area"`
		VodTime    string `db:"vod_time"`
	}
	rows := make([]row, 0, limit)
	selectRows := func(extra string, extraArgs ...any) error {
		queryArgs := append(append([]any{}, args...), extraArgs...)
		var batch []row
		if err := instance.Select(&batch, "SELECT source_vod_id,global_id,source_type_id,type_name,vod_name,vod_pic,vod_remarks,vod_year,vod_area,vod_time FROM source_videos WHERE "+where+extra+" ORDER BY id LIMIT ?", append(queryArgs, limit-len(rows))...); err != nil {
			return err
		}
		rows = append(rows, batch...)
		return nil
	}
	if err := selectRows(" AND id >= ?", start); err != nil {
		return nil, err
	}
	if len(rows) < limit {
		if err := selectRows(" AND id < ?", start); err != nil {
			return nil, err
		}
	}
	if len(rows) == 0 {
		return []*model.Video{}, nil
	}
	if len(rows) > limit {
		rows = rows[:limit]
	}
	out := make([]*model.Video, 0, len(rows))
	for _, r := range rows {
		out = append(out, &model.Video{VodId: model.FlexibleString(r.VodID), GlobalId: r.GlobalID, TypeId: model.FlexibleString(r.TypeID), TypeName: r.TypeName, VodName: r.VodName, VodPic: r.VodPic, VodRemarks: r.VodRemarks, VodYear: r.VodYear, VodArea: r.VodArea, VodTime: r.VodTime})
	}
	return out, nil
}

// randomCatalogStart chooses an index boundary without asking SQLite to sort
// every eligible row. The two ordered range scans wrap around that boundary.
func randomCatalogStart(maxID int64) int64 {
	if maxID <= 1 {
		return 1
	}
	n, err := cryptorand.Int(cryptorand.Reader, big.NewInt(maxID))
	if err != nil {
		return 1
	}
	return n.Int64() + 1
}
func DeleteCatalogVideo(sourceKey, vodID string) error {
	_, err := instance.Exec(`UPDATE source_videos SET lifecycle_state='deleted',updated_at=CURRENT_TIMESTAMP WHERE source_key=? AND source_vod_id=?`, sourceKey, vodID)
	return err
}

type SourceTypeExport struct {
	SourceTypeID string `db:"source_type_id"`
	GlobalTypeID int64  `db:"global_type_id"`
	TypeName     string `db:"type_name"`
}

func ExportSourceTypes(sourceKey string) ([]SourceTypeExport, error) {
	var out []SourceTypeExport
	err := instance.Select(&out, `SELECT source_type_id,COALESCE(global_type_id,0) global_type_id,type_name FROM source_types WHERE source_key=? ORDER BY source_type_id`, sourceKey)
	return out, err
}
func ImportSourceTypes(sourceKey string, rows []SourceTypeExport) error {
	for _, r := range rows {
		if _, err := instance.Exec(`INSERT INTO source_types(source_key,source_type_id,global_type_id,type_name,updated_at) VALUES(?,?,?,?,CURRENT_TIMESTAMP) ON CONFLICT(source_key,source_type_id) DO UPDATE SET global_type_id=excluded.global_type_id,type_name=excluded.type_name,updated_at=CURRENT_TIMESTAMP`, sourceKey, r.SourceTypeID, r.GlobalTypeID, r.TypeName); err != nil {
			return err
		}
	}
	return nil
}
