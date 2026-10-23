package db

import (
	"cczjVideo/app/model"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/jmoiron/sqlx"
)

// libraryItemsDDL 与 libraryChaptersDDL 是小说/漫画的领域表（ADR 0016）。
//
// 两处用到同一份字符串：createTables 给新库直接建到当前版本，迁移 v11 给老库补建。
// 分开写两份 DDL 是这个项目已经踩过的坑——两份会各自漂移，然后"新库有的列老库没有"。
const (
	libraryItemsDDL = `CREATE TABLE IF NOT EXISTS library_items (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		kind TEXT NOT NULL,
		source_key TEXT NOT NULL,
		remote_id TEXT NOT NULL,
		remote_url TEXT NOT NULL DEFAULT '',
		title TEXT NOT NULL DEFAULT '',
		author TEXT NOT NULL DEFAULT '',
		cover TEXT NOT NULL DEFAULT '',
		summary TEXT NOT NULL DEFAULT '',
		genres TEXT NOT NULL DEFAULT '',
		status TEXT NOT NULL DEFAULT '',
		latest TEXT NOT NULL DEFAULT '',
		words TEXT NOT NULL DEFAULT '',
		remote_updated_at TEXT NOT NULL DEFAULT '',
		lifecycle_state TEXT NOT NULL DEFAULT 'active',
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(kind, source_key, remote_id)
	)`

	libraryChaptersDDL = `CREATE TABLE IF NOT EXISTS library_chapters (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		item_id INTEGER NOT NULL,
		remote_id TEXT NOT NULL,
		remote_url TEXT NOT NULL DEFAULT '',
		title TEXT NOT NULL DEFAULT '',
		ord INTEGER NOT NULL DEFAULT 0,
		volume TEXT NOT NULL DEFAULT '',
		is_volume INTEGER NOT NULL DEFAULT 0,
		access TEXT NOT NULL DEFAULT 'unknown',
		remote_uploaded_at TEXT NOT NULL DEFAULT '',
		lifecycle_state TEXT NOT NULL DEFAULT 'active',
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(item_id, remote_id)
	)`
)

// libraryIndexes 的每条都对着真实查询写：
//
//   - idx_li_kind_key_state：一个源的目录列表永远是"这个 kind、这个源、只要 active"，
//     再按 updated_at 倒序取一屏。前缀吃掉筛选，后缀让翻页顺着 B 树走并在 LIMIT 处停。
//   - idx_lc_item_ord：阅读器取目录永远是"这本书、按 ord 升序"，且 ord 由我们算好后写入，
//     所以这条索引同时服务列表与"跳到第 N 章"。
//   - idx_lc_item_state：目录同步要找出"这本书里还 active、但这次没回来的章"，
//     按 lifecycle_state 筛。
var libraryIndexes = []string{
	libraryKindIDIndex,
	libraryKindTitleIndex,
	`CREATE INDEX IF NOT EXISTS idx_li_kind_key_state
		ON library_items (kind, source_key, lifecycle_state, updated_at DESC, id DESC)`,
	`CREATE INDEX IF NOT EXISTS idx_lc_item_ord
		ON library_chapters (item_id, ord)`,
	`CREATE INDEX IF NOT EXISTS idx_lc_item_state
		ON library_chapters (item_id, lifecycle_state)`,
}

// migrateLibraryDomain 给 sources 补 kind 列并建两张领域表。
//
// kind 的默认值必须是 'video'：这一列出现之前库里的每一行都是视频采集源，
// 补列时不给默认值会让它们全部变成空 kind，而空 kind 在视频侧的判定里既不等于
// video 也不等于 novel，源会从采集池里凭空消失。
//
// 加列这个形状与迁移 v10 的 auto_disabled_at 完全相同，也就是 pending-decisions
// 第 10 条那次事故的成因：旧 exe 开新库时 SELECT * 会因为多出来的列当场报
// "missing destination name"，界面显示一整页空数据。那条路径现在由 applySchemaAhead
// 兜住（库版本高于构建时换成 database.Unsafe()），所以这一刀是安全的。
func migrateLibraryDomain(tx *sqlx.Tx) error {
	if err := addColumnIfMissing(tx, "sources", "kind", "TEXT NOT NULL DEFAULT 'video'"); err != nil {
		return err
	}
	for _, ddl := range []string{libraryItemsDDL, libraryChaptersDDL} {
		if _, err := tx.Exec(ddl); err != nil {
			return fmt.Errorf("create library table: %w", err)
		}
	}
	for _, idx := range libraryIndexes {
		if _, err := tx.Exec(idx); err != nil {
			return fmt.Errorf("create library index: %w", err)
		}
	}
	return nil
}

// libraryItemRow 是 library_items 的写入形状。
//
// 刻意与 model.LibraryItem 分开：写入只需要远端给得出的那些字段，id / 时间戳 /
// lifecycle_state 由 SQL 自己管。共用一个结构体会让"哪些列能被调用方决定"变得看不出来。
type libraryItemRow struct {
	Kind            string `db:"kind"`
	SourceKey       string `db:"source_key"`
	RemoteID        string `db:"remote_id"`
	RemoteURL       string `db:"remote_url"`
	Title           string `db:"title"`
	Author          string `db:"author"`
	Cover           string `db:"cover"`
	Summary         string `db:"summary"`
	Genres          string `db:"genres"`
	Status          string `db:"status"`
	Latest          string `db:"latest"`
	Words           string `db:"words"`
	RemoteUpdatedAt string `db:"remote_updated_at"`
}

// UpsertLibraryItems 写入一批作品并回填它们的本地 id。
//
// revive 为真时把已 retired 的行拉回 active：一个作品从源目录里消失过又回来，
// 不该因为中间那次同步就永远显示成"来源已下架"。
//
// 返回值按入参顺序给出 id，入参里 RemoteID 或 Title 为空的条目会被跳过，
// 对应位置是 0——调用方不能把下标当成"一定写进去了"。
func UpsertLibraryItems(kind model.SourceKind, sourceKey string, items []*model.LibraryItem, revive bool) ([]int, error) {
	if err := model.ValidateSourceKey(sourceKey); err != nil {
		return nil, err
	}
	if err := model.ValidateSourceKind(kind); err != nil {
		return nil, err
	}
	ids := make([]int, len(items))
	rows := make([]libraryItemRow, 0, len(items))
	indexes := make([]int, 0, len(items))
	for i, item := range items {
		if item == nil {
			continue
		}
		remoteID := strings.TrimSpace(item.RemoteID)
		title := strings.TrimSpace(item.Title)
		if remoteID == "" || title == "" {
			continue
		}
		rows = append(rows, libraryItemRow{
			Kind: string(kind.Normalize()), SourceKey: sourceKey, RemoteID: remoteID,
			RemoteURL: strings.TrimSpace(item.RemoteURL), Title: title,
			Author: strings.TrimSpace(item.Author), Cover: strings.TrimSpace(item.Cover),
			Summary: item.Summary, Genres: strings.TrimSpace(item.Genres), Status: strings.TrimSpace(item.Status),
			Latest: strings.TrimSpace(item.Latest), Words: strings.TrimSpace(item.Words),
			RemoteUpdatedAt: strings.TrimSpace(item.RemoteUpdatedAt),
		})
		indexes = append(indexes, i)
	}
	if len(rows) == 0 {
		return ids, nil
	}

	tx, err := instance.Beginx()
	if err != nil {
		return nil, fmt.Errorf("begin library upsert: %w", err)
	}
	defer tx.Rollback()

	// lifecycle_state 的两个分支都写成 SQL 字面量，不绑参数：sqlite 的
	// ON CONFLICT DO UPDATE 里绑具名参数会静默把整批行的值塌缩成第一行的
	// （见 app/db/catalog.go 的同一处注释）。excluded.* 不是绑定参数，可以用。
	lifecycleOnConflict := "library_items.lifecycle_state"
	if revive {
		lifecycleOnConflict = "'active'"
	}
	q := fmt.Sprintf(`INSERT INTO library_items (kind, source_key, remote_id, remote_url, title, author, cover, summary, genres, status, latest, words, remote_updated_at, lifecycle_state, updated_at)
		VALUES (:kind,:source_key,:remote_id,:remote_url,:title,:author,:cover,:summary,:genres,:status,:latest,:words,:remote_updated_at,'active',CURRENT_TIMESTAMP)
		ON CONFLICT(kind,source_key,remote_id) DO UPDATE SET
			remote_url=excluded.remote_url, title=excluded.title, author=excluded.author, cover=excluded.cover, summary=excluded.summary,
			genres=excluded.genres, status=excluded.status, latest=excluded.latest, words=excluded.words,
			remote_updated_at=excluded.remote_updated_at, lifecycle_state=%s, updated_at=CURRENT_TIMESTAMP`,
		lifecycleOnConflict)
	if _, err := tx.NamedExec(q, rows); err != nil {
		return nil, fmt.Errorf("upsert library items: %w", err)
	}

	// 逐行回读 id：last_insert_rowid 在批量 NamedExec 之后只有最后一行的值。
	// 条数是一屏搜索结果或一页目录，量级在几十，逐行查一次是可接受的代价，
	// 而拿错 id 会让书架指到别的书上。
	getID, err := tx.Preparex(`SELECT id FROM library_items WHERE kind=? AND source_key=? AND remote_id=?`)
	if err != nil {
		return nil, fmt.Errorf("prepare library id lookup: %w", err)
	}
	defer getID.Close()
	for n, row := range rows {
		var id int
		if err := getID.Get(&id, row.Kind, row.SourceKey, row.RemoteID); err != nil {
			return nil, fmt.Errorf("read back library item id for %q: %w", row.Title, err)
		}
		ids[indexes[n]] = id
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit library upsert: %w", err)
	}
	return ids, nil
}

// libraryChapterRow 是 library_chapters 的写入形状，理由同 libraryItemRow。
type libraryChapterRow struct {
	ItemID           int    `db:"item_id"`
	RemoteID         string `db:"remote_id"`
	RemoteURL        string `db:"remote_url"`
	Title            string `db:"title"`
	Ord              int    `db:"ord"`
	Volume           string `db:"volume"`
	IsVolume         int    `db:"is_volume"`
	Access           string `db:"access"`
	RemoteUploadedAt string `db:"remote_uploaded_at"`
}

// ReplaceLibraryChapters 用一份新目录同步一本书的章节，返回新增、复用与失效的条数。
//
// 匹配分两级，两级都失败才算新章：
//
//  1. **remote_id 精确匹配**。这是 Mihon 唯一的一级（SyncChaptersWithSource.kt:105-109
//     只做 dbChaptersByUrl[chapter.url]）。我们的 remote_id 是规范化过的去域名路径，
//     所以换域名不影响匹配。
//  2. **归一化标题 + 序 匹配**。远端改了 URL 路径时靠这一层把旧行认回来，
//     改的是那一行的 remote_id，而不是插一条新的。少了这一层，源站一次改版
//     就会让目录里每一章都出现两遍。
//
// 远端不再返回的章转 deleted，**不删行**：用户的阅读进度指着它，删行会把
// "这一章读不下去了"变成"这本书从来没这一章"。Mihon 在这里是物理删除
// （SyncChaptersWithSource.kt:223-227 → removeChaptersWithIds），我们不跟。
func ReplaceLibraryChapters(itemID int, chapters []*model.LibraryChapter) (added, reused, retired int, err error) {
	if itemID <= 0 {
		return 0, 0, 0, fmt.Errorf("item id is required")
	}
	rows := make([]libraryChapterRow, 0, len(chapters))
	seen := make(map[string]bool, len(chapters))
	for _, c := range chapters {
		if c == nil {
			continue
		}
		remoteID := strings.TrimSpace(c.RemoteID)
		if remoteID == "" || seen[remoteID] {
			// 空 remote_id 没有可匹配的身份；重复 remote_id 会让 UNIQUE(item_id,remote_id)
			// 在同一批里自己撞自己。两种都跳过：目录少一行是可恢复的，写坏一批不是。
			continue
		}
		seen[remoteID] = true
		rows = append(rows, libraryChapterRow{
			ItemID: itemID, RemoteID: remoteID, RemoteURL: strings.TrimSpace(c.RemoteURL),
			Title: strings.TrimSpace(c.Title), Ord: c.Ord,
			Volume: strings.TrimSpace(c.Volume), IsVolume: boolToInt(c.IsVolume),
			Access: string(model.NormalizeAccess(c.Access)), RemoteUploadedAt: strings.TrimSpace(c.RemoteUploadedAt),
		})
	}
	if len(rows) == 0 {
		// 空目录不是"这本书没有章节"，是这一次没解析出东西。Legado 专门为它抛
		// TocEmptyException（BookChapterList.kt:116-118）而不是当正常结果，
		// 因为一次规则失效与一本真的完结无章的书在界面上必须能分开。
		return 0, 0, 0, fmt.Errorf("chapter list is empty")
	}

	tx, err := instance.Beginx()
	if err != nil {
		return 0, 0, 0, fmt.Errorf("begin chapter sync: %w", err)
	}
	defer tx.Rollback()

	var existing []struct {
		Id       int    `db:"id"`
		RemoteID string `db:"remote_id"`
		Title    string `db:"title"`
		Ord      int    `db:"ord"`
	}
	if err := tx.Select(&existing,
		`SELECT id, remote_id, title, ord FROM library_chapters WHERE item_id=? AND lifecycle_state='active'`,
		itemID); err != nil {
		return 0, 0, 0, fmt.Errorf("read existing chapters: %w", err)
	}
	byRemote := make(map[string]int, len(existing))
	// 标题兜底的候选表按「归一化标题 + 序」索引。同一本书里同名同序的两章
	// （重发、分卷重印）会撞键，这里只留第一个：认错了顶多是把进度指到相邻的
	// 同名章，认不出来则退回插新行，两种都不会丢数据。
	byTitle := make(map[string]int, len(existing))
	for _, e := range existing {
		byRemote[e.RemoteID] = e.Id
		key := chapterTitleKey(e.Title, e.Ord)
		if _, ok := byTitle[key]; !ok {
			byTitle[key] = e.Id
		}
	}

	// 第一级：remote_id 命中的走批量 upsert。
	matched := make([]libraryChapterRow, 0, len(rows))
	// 第二级：remote_id 没命中的，先看标题+序能不能认回一行旧的。
	unmatched := make([]libraryChapterRow, 0, len(rows))
	for _, row := range rows {
		if _, ok := byRemote[row.RemoteID]; ok {
			matched = append(matched, row)
			continue
		}
		key := chapterTitleKey(row.Title, row.Ord)
		id, ok := byTitle[key]
		if !ok {
			unmatched = append(unmatched, row)
			continue
		}
		// 认回来了：改这一行的 remote_id，而不是插新行。id 不变，
		// 所以任何指着 chapter id 的东西（下载任务、缓存）都还认得它。
		if _, err := tx.Exec(
			`UPDATE library_chapters SET remote_id=?, remote_url=?, title=?, ord=?, volume=?, is_volume=?, access=?,
				remote_uploaded_at=?, lifecycle_state='active', updated_at=CURRENT_TIMESTAMP WHERE id=?`,
			row.RemoteID, row.RemoteURL, row.Title, row.Ord, row.Volume, row.IsVolume, row.Access,
			row.RemoteUploadedAt, id); err != nil {
			return 0, 0, 0, fmt.Errorf("retitle chapter %d: %w", id, err)
		}
		// 这一行的旧 remote_id 从此不再由任何远端章指着，要从"现存"里摘掉，
		// 否则下面那一步会把它当"这次没回来的"再判一次失效。
		delete(byRemote, existingRemoteOf(existing, id))
		byRemote[row.RemoteID] = id
		delete(byTitle, key)
		reused++
	}

	upsert := matched
	if err := namedUpsertChapters(tx, upsert); err != nil {
		return 0, 0, 0, err
	}
	if err := namedUpsertChapters(tx, unmatched); err != nil {
		return 0, 0, 0, err
	}
	added = len(unmatched)

	missing := make([]string, 0, len(existing))
	for remoteID, id := range byRemote {
		if seen[remoteID] {
			continue
		}
		_ = id
		missing = append(missing, remoteID)
	}
	for start := 0; start < len(missing); start += libraryChapterBatchSize {
		end := min(start+libraryChapterBatchSize, len(missing))
		query, args, err := sqlx.In(
			`UPDATE library_chapters SET lifecycle_state='deleted', updated_at=CURRENT_TIMESTAMP
			 WHERE item_id=? AND lifecycle_state='active' AND remote_id IN (?)`, itemID, missing[start:end])
		if err != nil {
			return 0, 0, 0, fmt.Errorf("build chapter retire query: %w", err)
		}
		if _, err := tx.Exec(instance.Rebind(query), args...); err != nil {
			return 0, 0, 0, fmt.Errorf("retire missing chapters: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, 0, 0, fmt.Errorf("commit chapter sync: %w", err)
	}
	return added, reused, len(missing), nil
}

// namedUpsertChapters 批量写入章节。空批次直接返回：NamedExec 收到空切片会
// 报"cannot execute ... with empty slice"，而"这一级没有要写的行"是正常情况。
// Nine parameters per row stay below even SQLite builds with a 999-variable
// limit. All chunks share the caller's transaction, including retirements.
const libraryChapterBatchSize = 100

func namedUpsertChapters(tx *sqlx.Tx, rows []libraryChapterRow) error {
	if len(rows) == 0 {
		return nil
	}
	// lifecycle_state 在两个分支里都写成字面量 'active'，不绑参数：sqlite 的
	// ON CONFLICT DO UPDATE 里绑具名参数会静默把整批行的值塌缩成第一行的
	// （见 app/db/catalog.go 的同一处注释与 catalog_revive_test.go）。
	// excluded.* 是 sqlite 的关键字引用，不是绑定参数，可以安全使用。
	const q = `INSERT INTO library_chapters (item_id, remote_id, remote_url, title, ord, volume, is_volume, access, remote_uploaded_at, lifecycle_state, updated_at)
		VALUES (:item_id,:remote_id,:remote_url,:title,:ord,:volume,:is_volume,:access,:remote_uploaded_at,'active',CURRENT_TIMESTAMP)
		ON CONFLICT(item_id,remote_id) DO UPDATE SET
			remote_url=excluded.remote_url, title=excluded.title, ord=excluded.ord, volume=excluded.volume, is_volume=excluded.is_volume,
			access=excluded.access, remote_uploaded_at=excluded.remote_uploaded_at,
			lifecycle_state='active', updated_at=CURRENT_TIMESTAMP`
	for start := 0; start < len(rows); start += libraryChapterBatchSize {
		if _, err := tx.NamedExec(q, rows[start:min(start+libraryChapterBatchSize, len(rows))]); err != nil {
			return fmt.Errorf("upsert chapters: %w", err)
		}
	}
	return nil
}

// chapterTitleKey 是标题兜底匹配用的键：小写、去首尾与重复空白、拼上序。
// 带上序是因为「第一章 起」这种标题在一本书里可能出现多次（分卷重印），
// 只有标题会认错行。
func chapterTitleKey(title string, ord int) string {
	fields := strings.Fields(strings.ToLower(title))
	return strings.Join(fields, " ") + "\x00" + strconv.Itoa(ord)
}

// existingRemoteOf 找出某个 id 对应的旧 remote_id。existing 是一本书的目录，
// 量级在几千以内，线性扫一次比再建一张反向表便宜。
func existingRemoteOf(existing []struct {
	Id       int    `db:"id"`
	RemoteID string `db:"remote_id"`
	Title    string `db:"title"`
	Ord      int    `db:"ord"`
}, id int) string {
	for _, e := range existing {
		if e.Id == id {
			return e.RemoteID
		}
	}
	return ""
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

// GetLibraryChapter 按本地 id 取一章。
func GetLibraryChapter(id int) (*model.LibraryChapter, error) {
	var chapter model.LibraryChapter
	if err := instance.Get(&chapter, `SELECT * FROM library_chapters WHERE id=?`, id); err != nil {
		return nil, err
	}
	return &chapter, nil
}

// GetLibraryChapterByOrd 按展示序取一章，没有则返回 (nil, nil)。
//
// 正文提取要"下一章的地址"来防止把下一章吞进本章（provider.Pipeline.Content
// 的 nextChapterURL 参数）。取不到不是错误：那只是一个额外的防吞保护，
// 没有它正文提取照样工作。所以这里用 (nil, nil) 而不是 sql.ErrNoRows，
// 免得调用方为了一个可选信息去分辨"没有下一章"和"查库失败"。
func GetLibraryChapterByOrd(itemID, ord int) (*model.LibraryChapter, error) {
	var chapter model.LibraryChapter
	err := instance.Get(&chapter,
		`SELECT * FROM library_chapters WHERE item_id=? AND ord=? AND lifecycle_state='active' LIMIT 1`,
		itemID, ord)
	if err != nil {
		if isNoRows(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read chapter by ord: %w", err)
	}
	return &chapter, nil
}

func isNoRows(err error) bool {
	// 判错误类型用 errors.Is，不用文案匹配：apperror 的包注释里明写了这条约定，
	// 而 database/sql 的错误文案是会随驱动版本改的。
	return errors.Is(err, sql.ErrNoRows)
}

// ListLibraryItems 取一个源的目录。includeRetired 为真时连已下架的一起返回，
// 书架要它：用户收藏的书从源里消失了，界面上得显示成"来源已下架"而不是凭空不见。
func ListLibraryItems(kind model.SourceKind, sourceKey string, includeRetired bool) ([]*model.LibraryItem, error) {
	if err := model.ValidateSourceKey(sourceKey); err != nil {
		return nil, err
	}
	query := `SELECT * FROM library_items WHERE kind=? AND source_key=?`
	args := []any{string(kind.Normalize()), sourceKey}
	if !includeRetired {
		query += ` AND lifecycle_state='active'`
	}
	query += ` ORDER BY updated_at DESC, id DESC`
	var items []*model.LibraryItem
	if err := instance.Select(&items, query, args...); err != nil {
		return nil, fmt.Errorf("list library items: %w", err)
	}
	return items, nil
}

// ListLibraryItemsByKind 取一个模式下**所有源**的作品。
//
// 书城与分类页要它：那里展示的是"已经入库的全部作品"，不属于任何单个源。
// 与 ListLibraryItems 的区别只在有没有 source_key 这一层筛选。
func ListLibraryItemsByKind(kind model.SourceKind, includeRetired bool) ([]*model.LibraryItem, error) {
	query := `SELECT * FROM library_items WHERE kind=?`
	args := []any{string(kind.Normalize())}
	if !includeRetired {
		query += ` AND lifecycle_state='active'`
	}
	query += ` ORDER BY updated_at DESC, id DESC`
	var items []*model.LibraryItem
	if err := instance.Select(&items, query, args...); err != nil {
		return nil, fmt.Errorf("list library items by kind: %w", err)
	}
	return items, nil
}

// GetLibraryItem 按本地 id 取一部作品。书架与进度存的是复合键，但界面内部导航
// 拿着的是本地 id，两条路都要能走。
func GetLibraryItem(id int) (*model.LibraryItem, error) {
	var item model.LibraryItem
	if err := instance.Get(&item, `SELECT * FROM library_items WHERE id=?`, id); err != nil {
		return nil, err
	}
	return &item, nil
}

// GetLibraryItemByRemote 按自然键取一部作品。前端存的书架键是
// {kind, sourceKey, remoteID}，重新进应用时要靠它把条目找回来。
func GetLibraryItemByRemote(kind model.SourceKind, sourceKey, remoteID string) (*model.LibraryItem, error) {
	if err := model.ValidateSourceKey(sourceKey); err != nil {
		return nil, err
	}
	var item model.LibraryItem
	err := instance.Get(&item,
		`SELECT * FROM library_items WHERE kind=? AND source_key=? AND remote_id=?`,
		string(kind.Normalize()), sourceKey, strings.TrimSpace(remoteID))
	if err != nil {
		return nil, err
	}
	return &item, nil
}

// ListLibraryChapters 取一本书的目录，按 ord 升序。
//
// includeRetired 为真时连已下架的章一起返回：阅读进度指着某个 ord，
// 那一章从源里消失了也要能在界面上显示成"这一章取不到了"，
// 否则用户看到的是目录里凭空少一章、进度却停在不存在的位置上。
func ListLibraryChapters(itemID int, includeRetired bool) ([]*model.LibraryChapter, error) {
	query := `SELECT * FROM library_chapters WHERE item_id=?`
	args := []any{itemID}
	if !includeRetired {
		query += ` AND lifecycle_state='active'`
	}
	query += ` ORDER BY ord, id`
	var chapters []*model.LibraryChapter
	if err := instance.Select(&chapters, query, args...); err != nil {
		return nil, fmt.Errorf("list library chapters: %w", err)
	}
	return chapters, nil
}

// CountLibraryItems 数一个源里 active 的作品条数，源列表要显示它。
func CountLibraryItems(kind model.SourceKind, sourceKey string) (int, error) {
	if err := model.ValidateSourceKey(sourceKey); err != nil {
		return 0, err
	}
	var count int
	err := instance.Get(&count,
		`SELECT COUNT(*) FROM library_items WHERE kind=? AND source_key=? AND lifecycle_state='active'`,
		string(kind.Normalize()), sourceKey)
	if err != nil {
		return 0, fmt.Errorf("count library items: %w", err)
	}
	return count, nil
}

// CountLibraryItemsBySource replaces one count query per source with one grouped scan.
func CountLibraryItemsBySource(kind model.SourceKind) (map[string]int, error) {
	var rows []struct {
		SourceKey string `db:"source_key"`
		Count     int    `db:"count"`
	}
	if err := instance.Select(&rows, `SELECT source_key,COUNT(*) AS count FROM library_items WHERE kind=? AND lifecycle_state='active' GROUP BY source_key`, string(kind.Normalize())); err != nil {
		return nil, err
	}
	counts := make(map[string]int, len(rows))
	for _, row := range rows {
		counts[row.SourceKey] = row.Count
	}
	return counts, nil
}

// RetireLibraryItemsOfSource 在一个源被删除时把它的所有作品与章节转 deleted。
//
// 与 DeleteSource 对 source_videos 的处理同构：不删行，因为书架与进度还指着它们。
// 界面上这些条目会变成"来源已下架"，用户自己决定要不要从书架里移走。
func RetireLibraryItemsOfSource(tx *sqlx.Tx, sourceKey string) error {
	if err := model.ValidateSourceKey(sourceKey); err != nil {
		return err
	}
	if _, err := tx.Exec(
		`UPDATE library_chapters SET lifecycle_state='deleted', updated_at=CURRENT_TIMESTAMP
		 WHERE lifecycle_state='active' AND item_id IN (SELECT id FROM library_items WHERE source_key=?)`,
		sourceKey); err != nil {
		return fmt.Errorf("retire library chapters: %w", err)
	}
	if _, err := tx.Exec(
		`UPDATE library_items SET lifecycle_state='deleted', updated_at=CURRENT_TIMESTAMP
		 WHERE source_key=? AND lifecycle_state='active'`, sourceKey); err != nil {
		return fmt.Errorf("retire library items: %w", err)
	}
	return nil
}

// libraryStats 给诊断页与源列表用：一个源下每种状态各有多少条。
type libraryStats struct {
	Active  int `db:"active" json:"active"`
	Retired int `db:"retired" json:"retired"`
}

// LibraryStats 统计一个源的作品与章节存量。
func LibraryStats(kind model.SourceKind, sourceKey string) (items, chapters libraryStats, err error) {
	if err := model.ValidateSourceKey(sourceKey); err != nil {
		return items, chapters, err
	}
	kindText := string(kind.Normalize())
	if err := instance.Get(&items,
		`SELECT COALESCE(SUM(lifecycle_state='active'),0) AS active, COALESCE(SUM(lifecycle_state='deleted'),0) AS retired
		 FROM library_items WHERE kind=? AND source_key=?`, kindText, sourceKey); err != nil {
		return items, chapters, fmt.Errorf("library item stats: %w", err)
	}
	if err := instance.Get(&chapters,
		`SELECT COALESCE(SUM(c.lifecycle_state='active'),0) AS active, COALESCE(SUM(c.lifecycle_state='deleted'),0) AS retired
		 FROM library_chapters c JOIN library_items i ON i.id=c.item_id
		 WHERE i.kind=? AND i.source_key=?`, kindText, sourceKey); err != nil {
		return items, chapters, fmt.Errorf("library chapter stats: %w", err)
	}
	return items, chapters, nil
}
