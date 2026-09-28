package db

import (
	"cczjVideo/app/model"
	cryptorand "crypto/rand"
	"encoding/base64"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"sync"
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
	return upsertCatalogItems(nil, sourceKey, videos, ReviveDeletedCatalogItems())
}

// UpsertCatalogItemsWithRevival ignores the setting and always restores deleted
// entries. Used by explicit import paths: the user asked for this data by hand,
// so a previous deletion (usually produced by the same import earlier) must not
// keep it hidden.
func UpsertCatalogItemsWithRevival(sourceKey string, videos []*model.Video) error {
	return upsertCatalogItems(nil, sourceKey, videos, true)
}

// UpsertCatalogItemsBatch works like UpsertCatalogItems but reuses the identity
// indexes carried by batch, so a multi-page collection run reads global_video
// once instead of once per page. Callers that loop over pages must pass a batch;
// one-off writes keep using UpsertCatalogItems.
func UpsertCatalogItemsBatch(batch *CatalogBatch, sourceKey string, videos []*model.Video) error {
	return upsertCatalogItems(batch, sourceKey, videos, ReviveDeletedCatalogItems())
}

func upsertCatalogItems(batch *CatalogBatch, sourceKey string, videos []*model.Video, revive bool) (err error) {
	if err := model.ValidateSourceKey(sourceKey); err != nil {
		return err
	}
	videos = FilterEnabledCollectVideos(videos)
	if len(videos) == 0 {
		return nil
	}
	resolver, err := batch.begin()
	if err != nil {
		return fmt.Errorf("load catalog identities: %w", err)
	}
	defer func() { batch.end(err) }()
	tx, err := instance.Beginx()
	if err != nil {
		return fmt.Errorf("begin catalog upsert: %w", err)
	}
	defer tx.Rollback()
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
		gid, gtid, err := resolver.upsertVideo(tx, v)
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

// catalogIdentityResolver 持有一轮采集的身份状态（类型表、global_video 索引、
// 已解析标题）。它不属于任何单个事务，所以写入用的事务由方法参数传进来。
type catalogIdentityResolver struct {
	types           []catalogTypeCandidate
	typesByExact    map[string]int64
	videoIndex      *globalVideoIndex
	resolvedVideoID map[string]int64
}

// CatalogBatch 让一轮采集（几十上百页）只载入一遍 global_video 与 global_types。
// 之前每页 upsert 都新建解析器并重读全表：库里三万条身份时，一次整轮采集就是
// 上万次重复载入和索引重建。
//
// 批次自带互斥，多个 goroutine 共用同一批次是安全的（热榜匹配就是这么用的）。
// 锁一律在 Beginx 之前取得、提交之后释放，不会和 SQLite 写锁形成环等待。
// 任何一页写失败都会作废批次：内存索引里不能留着已回滚事务建出的 global_id。
type CatalogBatch struct {
	mu    sync.Mutex
	ident *catalogIdentityResolver
}

func NewCatalogBatch() *CatalogBatch { return &CatalogBatch{} }

// begin 取锁并按需载入身份；nil 批次表示调用方只写一次，用后即丢。
func (b *CatalogBatch) begin() (*catalogIdentityResolver, error) {
	if b == nil {
		return newCatalogIdentityResolver(instance)
	}
	b.mu.Lock()
	if b.ident == nil {
		ident, err := newCatalogIdentityResolver(instance)
		if err != nil {
			b.mu.Unlock()
			return nil, err
		}
		b.ident = ident
	}
	return b.ident, nil
}

// end 释放锁；err 非空说明这一页的事务没走完，作废整批内存身份。
func (b *CatalogBatch) end(err error) {
	if b == nil {
		return
	}
	if err != nil {
		b.ident = nil
	}
	b.mu.Unlock()
}

func newCatalogIdentityResolver(exec sqlx.Ext) (*catalogIdentityResolver, error) {
	r := &catalogIdentityResolver{
		typesByExact:    make(map[string]int64),
		resolvedVideoID: make(map[string]int64),
	}
	if err := sqlx.Select(exec, &r.types, `SELECT id,type_name FROM global_types`); err != nil {
		return nil, err
	}
	for _, candidate := range r.types {
		r.typesByExact[candidate.Name] = candidate.ID
	}
	candidates, err := loadGlobalCandidates(exec)
	if err != nil {
		return nil, err
	}
	r.videoIndex = newGlobalVideoIndex(candidates)
	return r, nil
}

// syncVideoIndex 在批次把 type_id/year 补进已有行之后同步内存候选，
// 下一批之前的同名记录才不会又走一遍插入。
func (r *catalogIdentityResolver) syncVideoIndex(id, typeID int64, year string) {
	if r.videoIndex == nil {
		return
	}
	r.videoIndex.update(id, typeID, year)
}

func (r *catalogIdentityResolver) resolveTypeID(exec sqlx.Ext, typeName string) (int64, error) {
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
	result, err := exec.Exec(`INSERT OR IGNORE INTO global_types(type_name) VALUES (?)`, typeName)
	if err != nil {
		return 0, err
	}
	id, err := result.LastInsertId()
	if err != nil || id == 0 {
		if err := sqlx.Get(exec, &id, `SELECT id FROM global_types WHERE type_name=?`, typeName); err != nil {
			return 0, err
		}
	}
	candidate := catalogTypeCandidate{ID: id, Name: typeName}
	r.types = append(r.types, candidate)
	r.typesByExact[typeName] = id
	return id, nil
}

func (r *catalogIdentityResolver) upsertVideo(exec sqlx.Ext, video *model.Video) (int64, int64, error) {
	typeID, err := r.resolveTypeID(exec, video.TypeName)
	if err != nil {
		return 0, 0, err
	}
	identityKey := video.VodName + "\x00" + video.VodYear + "\x00" + fmt.Sprint(typeID)
	id := r.resolvedVideoID[identityKey]
	if id == 0 {
		id, _, err = resolveGlobalVideoID(exec, r.videoIndex, video.VodName, video.VodYear, typeID)
		if err != nil {
			return 0, 0, err
		}
		r.resolvedVideoID[identityKey] = id
	}
	// 采集侧带的一切可覆盖字段都在这一条语句里落库，包括源站自己塞进列表的豆瓣
	// ID 和评分。过去这后半截由采集结束后的第二趟 SaveDoubanInfoFromBatch 逐条
	// 另开事务写：同一页数据既重走一遍身份阶梯，又摊成几十次 fsync。
	//
	// 豆瓣两个字段是例外，只做「空位填空」：源站的 vod_douban_id 是从别处抄来的
	// 脏数据，没人验证过，而库里已有的 ID 来自过了 doubanMatchThreshold 的匹配、
	// 或人工修复。让前者覆盖后者，等于用一个错误 ID 换掉一个正确 ID，还会顺着
	// 兄弟记录继承把错挂复制到更多行上。评分同理：它属于它自己那个 ID，
	// 与库里已有 ID 不一致时一并作废。
	srcDoubanID := normalizeSubjectID(video.VodDoubanId.String())
	if _, err := exec.Exec(`UPDATE global_video SET
		type_id=CASE WHEN ? != 0 THEN ? ELSE type_id END,
		year=CASE WHEN ? != '' THEN ? ELSE year END,
		area=CASE WHEN ? != '' THEN ? ELSE area END,
		lang=CASE WHEN ? != '' THEN ? ELSE lang END,
		tag=CASE WHEN ? != '' THEN ? ELSE tag END,
		pic=CASE WHEN ? != '' THEN ? ELSE pic END,
		genre=CASE WHEN ? != '' THEN ? ELSE genre END,
		aka=CASE WHEN ? != '' THEN ? ELSE aka END,
		release_date=CASE WHEN ? != '' THEN ? ELSE release_date END,
		douban_id=CASE WHEN COALESCE(douban_id, '') = '' AND ? != '' THEN ? ELSE douban_id END,
		douban_score=CASE WHEN COALESCE(douban_score, '') = '' AND ? != ''
			AND (COALESCE(douban_id, '') = '' OR douban_id = ?) THEN ? ELSE douban_score END,
		updated_at=CURRENT_TIMESTAMP WHERE id=?`,
		typeID, typeID,
		video.VodYear, video.VodYear,
		video.VodArea, video.VodArea,
		video.VodLang, video.VodLang,
		video.VodTag, video.VodTag,
		video.VodPic, video.VodPic,
		video.VodTag, video.VodTag,
		video.VodSub, video.VodSub,
		video.VodYear, video.VodYear,
		srcDoubanID, srcDoubanID,
		video.VodDoubanScore.String(), srcDoubanID, video.VodDoubanScore.String(),
		id); err != nil {
		return 0, 0, err
	}
	r.syncVideoIndex(id, typeID, video.VodYear)
	return id, typeID, nil
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

// ImportSourceTypes 覆盖导入某个源的类型映射。整批包进一个事务：逐条自动提交
// 不但每次导入都要抢一遍库级写锁，中途失败还会留下半新半旧的映射。
func ImportSourceTypes(sourceKey string, rows []SourceTypeExport) error {
	if len(rows) == 0 {
		return nil
	}
	tx, err := instance.Beginx()
	if err != nil {
		return fmt.Errorf("begin source type import: %w", err)
	}
	defer tx.Rollback()
	params := make([]sourceTypeUpsertRow, 0, len(rows))
	for _, r := range rows {
		params = append(params, sourceTypeUpsertRow{SourceKey: sourceKey, SourceTypeID: r.SourceTypeID, GlobalTypeID: r.GlobalTypeID, TypeName: r.TypeName})
	}
	if _, err := tx.NamedExec(`INSERT INTO source_types(source_key,source_type_id,global_type_id,type_name,updated_at) VALUES (:source_key,:source_type_id,:global_type_id,:type_name,CURRENT_TIMESTAMP)
		ON CONFLICT(source_key,source_type_id) DO UPDATE SET global_type_id=excluded.global_type_id,type_name=excluded.type_name,updated_at=CURRENT_TIMESTAMP`, params); err != nil {
		return fmt.Errorf("import source types: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit source type import: %w", err)
	}
	return nil
}
