package db

import (
	"cczjVideo/app/model"
	"fmt"
	"sync"
	"testing"
)

func TestUpsertLibrarySearchPageWithLargeResultSet(t *testing.T) {
	if err := InitDB(boundaryTestDBDir); err != nil {
		t.Fatal(err)
	}
	const sourceKey = "lib_large_search_test"
	cleanupLibraryFixture(t, sourceKey)
	items := make([]*model.LibraryItem, 135)
	for i := range items {
		items[i] = &model.LibraryItem{RemoteID: fmt.Sprintf("book/%03d", i), Title: fmt.Sprintf("Book %03d", i)}
	}
	ids, err := UpsertLibraryItems(model.SourceKindNovel, sourceKey, items, true)
	if err != nil {
		t.Fatal(err)
	}
	seen := make(map[int]bool)
	for _, id := range ids {
		if id <= 0 || seen[id] {
			t.Fatalf("invalid or duplicate identity %d", id)
		}
		seen[id] = true
	}
	if len(seen) != len(items) {
		t.Fatalf("persisted %d items, want %d", len(seen), len(items))
	}
}

func TestConcurrentLibrarySearchPages(t *testing.T) {
	if err := InitDB(boundaryTestDBDir); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for source := 0; source < 2; source++ {
		key := fmt.Sprintf("lib_parallel_%d", source)
		cleanupLibraryFixture(t, key)
		wg.Add(1)
		go func() {
			defer wg.Done()
			items := make([]*model.LibraryItem, 135)
			for i := range items {
				items[i] = &model.LibraryItem{RemoteID: fmt.Sprint(i), Title: fmt.Sprint(i)}
			}
			if _, err := UpsertLibraryItems(model.SourceKindNovel, key, items, true); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
}

// 这一组测试守的是三件静默失效的事，它们都不会让语句报错、也不会让事务回滚：
//
//  1. 批量 UPSERT 在 ON CONFLICT DO UPDATE 里绑具名参数会把整批行塌缩成第一行
//     （见 app/db/catalog.go 的同一处注释）。只有断言行数才抓得到。
//  2. 目录同步的两级匹配如果退化成一级，源站改一次 URL 路径就会让每章出现两遍。
//  3. GetEnabledSources 少了 kind 过滤，一个启用的书源会被视频调度器当 CMS 接口拉分页。
//
// 三条都只能靠行数与身份断言暴露，所以这里的断言一律数行、比 id，不看"有没有报错"。

func TestUpsertLibraryItemsWritesEveryRow(t *testing.T) {
	if err := InitDB(boundaryTestDBDir); err != nil {
		t.Fatal(err)
	}
	const sourceKey = "lib_items_test"
	cleanupLibraryFixture(t, sourceKey)

	items := make([]*model.LibraryItem, 0, 5)
	for i := 0; i < 5; i++ {
		items = append(items, &model.LibraryItem{
			RemoteID: string(rune('a'+i)) + "-book",
			Title:    "书",
			Author:   "同一个作者",
		})
	}
	ids, err := UpsertLibraryItems(model.SourceKindNovel, sourceKey, items, false)
	if err != nil {
		t.Fatal(err)
	}

	var count int
	if err := DB().Get(&count, `SELECT COUNT(*) FROM library_items WHERE source_key=?`, sourceKey); err != nil {
		t.Fatal(err)
	}
	if count != 5 {
		t.Fatalf("wrote %d rows, want 5 — a batch collapse would leave exactly 1", count)
	}
	seen := map[int]bool{}
	for i, id := range ids {
		if id == 0 {
			t.Fatalf("row %d got no id back", i)
		}
		if seen[id] {
			t.Fatalf("row %d reused id %d; ids must be distinct", i, id)
		}
		seen[id] = true
	}

	// 再写一遍同样五条：应当命中 UNIQUE 走 UPDATE，行数不变，id 也不变。
	idsAgain, err := UpsertLibraryItems(model.SourceKindNovel, sourceKey, items, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := DB().Get(&count, `SELECT COUNT(*) FROM library_items WHERE source_key=?`, sourceKey); err != nil {
		t.Fatal(err)
	}
	if count != 5 {
		t.Fatalf("re-upsert produced %d rows, want 5", count)
	}
	for i := range ids {
		if ids[i] != idsAgain[i] {
			t.Fatalf("row %d changed id on re-upsert (%d -> %d); identity must be stable", i, ids[i], idsAgain[i])
		}
	}
}

func TestUpsertLibraryItemsSkipsRowWithoutIdentity(t *testing.T) {
	if err := InitDB(boundaryTestDBDir); err != nil {
		t.Fatal(err)
	}
	const sourceKey = "lib_skip_test"
	cleanupLibraryFixture(t, sourceKey)

	items := []*model.LibraryItem{
		{RemoteID: "ok", Title: "有身份"},
		{RemoteID: "", Title: "没有远端标识"},
		{RemoteID: "no-title", Title: "   "},
		nil,
	}
	ids, err := UpsertLibraryItems(model.SourceKindNovel, sourceKey, items, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != len(items) {
		t.Fatalf("returned %d ids for %d inputs; positions must line up", len(ids), len(items))
	}
	if ids[0] == 0 {
		t.Fatal("the valid row was skipped")
	}
	for i := 1; i < len(ids); i++ {
		if ids[i] != 0 {
			t.Fatalf("input %d has no identity but got id %d", i, ids[i])
		}
	}
	var count int
	if err := DB().Get(&count, `SELECT COUNT(*) FROM library_items WHERE source_key=?`, sourceKey); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("wrote %d rows, want 1", count)
	}
}

func TestReplaceLibraryChaptersMatchesByRemoteIDFirst(t *testing.T) {
	if err := InitDB(boundaryTestDBDir); err != nil {
		t.Fatal(err)
	}
	const sourceKey = "lib_chap_test"
	cleanupLibraryFixture(t, sourceKey)
	itemID := mustInsertLibraryItem(t, model.SourceKindComic, sourceKey, "series-1")

	first := []*model.LibraryChapter{
		{RemoteID: "/c/1", Title: "第一话", Ord: 0, Access: model.ChapterAccessAvailable},
		{RemoteID: "/c/2", Title: "第二话", Ord: 1, Access: model.ChapterAccessAvailable},
	}
	added, reused, retired, err := ReplaceLibraryChapters(itemID, first)
	if err != nil {
		t.Fatal(err)
	}
	if added != 2 || reused != 0 || retired != 0 {
		t.Fatalf("first sync: added=%d reused=%d retired=%d, want 2/0/0", added, reused, retired)
	}
	idsBefore := chapterIDs(t, itemID)

	// 同样的 remote_id 再来一遍，只改标题：应当全部走 UPDATE，一行不多一行不少。
	second := []*model.LibraryChapter{
		{RemoteID: "/c/1", Title: "第一话（修）", Ord: 0, Access: model.ChapterAccessAvailable},
		{RemoteID: "/c/2", Title: "第二话（修）", Ord: 1, Access: model.ChapterAccessLogin},
	}
	added, reused, retired, err = ReplaceLibraryChapters(itemID, second)
	if err != nil {
		t.Fatal(err)
	}
	if added != 0 || retired != 0 {
		t.Fatalf("re-sync: added=%d retired=%d, want 0/0", added, retired)
	}
	if got := countChapters(t, itemID, ""); got != 2 {
		t.Fatalf("re-sync left %d rows, want 2", got)
	}
	assertSameIDs(t, idsBefore, chapterIDs(t, itemID))

	var access string
	if err := DB().Get(&access, `SELECT access FROM library_chapters WHERE item_id=? AND remote_id=?`, itemID, "/c/2"); err != nil {
		t.Fatal(err)
	}
	if access != string(model.ChapterAccessLogin) {
		t.Fatalf("access = %q, want login; the UPDATE branch did not run", access)
	}
}

func TestReplaceLibraryChaptersFallsBackToTitleAndOrder(t *testing.T) {
	if err := InitDB(boundaryTestDBDir); err != nil {
		t.Fatal(err)
	}
	const sourceKey = "lib_retitle_test"
	cleanupLibraryFixture(t, sourceKey)
	itemID := mustInsertLibraryItem(t, model.SourceKindNovel, sourceKey, "book-1")

	if _, _, _, err := ReplaceLibraryChapters(itemID, []*model.LibraryChapter{
		{RemoteID: "/old/1", Title: "第一章 起风", Ord: 0, Access: model.ChapterAccessAvailable},
		{RemoteID: "/old/2", Title: "第二章 落雨", Ord: 1, Access: model.ChapterAccessAvailable},
	}); err != nil {
		t.Fatal(err)
	}
	idsBefore := chapterIDs(t, itemID)

	// 源站改版：路径全变了，标题与序不变。一级匹配（remote_id）全落空，
	// 必须由二级匹配把旧行认回来——否则目录里每章都会出现两遍。
	added, reused, retired, err := ReplaceLibraryChapters(itemID, []*model.LibraryChapter{
		{RemoteID: "/new/1", Title: "第一章 起风", Ord: 0, Access: model.ChapterAccessAvailable},
		{RemoteID: "/new/2", Title: "第二章 落雨", Ord: 1, Access: model.ChapterAccessAvailable},
	})
	if err != nil {
		t.Fatal(err)
	}
	if reused != 2 {
		t.Fatalf("reused=%d, want 2 — the title fallback did not fire", reused)
	}
	if added != 0 || retired != 0 {
		t.Fatalf("added=%d retired=%d, want 0/0", added, retired)
	}
	if got := countChapters(t, itemID, ""); got != 2 {
		t.Fatalf("retitle left %d rows, want 2 (a duplicate per chapter means the fallback failed)", got)
	}
	assertSameIDs(t, idsBefore, chapterIDs(t, itemID))

	var remoteID string
	if err := DB().Get(&remoteID, `SELECT remote_id FROM library_chapters WHERE item_id=? AND ord=0`, itemID); err != nil {
		t.Fatal(err)
	}
	if remoteID != "/new/1" {
		t.Fatalf("remote_id = %q, want /new/1; the reused row kept the stale identity", remoteID)
	}
}

func TestReplaceLibraryChaptersRetiresMissingWithoutDeleting(t *testing.T) {
	if err := InitDB(boundaryTestDBDir); err != nil {
		t.Fatal(err)
	}
	const sourceKey = "lib_retire_test"
	cleanupLibraryFixture(t, sourceKey)
	itemID := mustInsertLibraryItem(t, model.SourceKindNovel, sourceKey, "book-2")

	if _, _, _, err := ReplaceLibraryChapters(itemID, []*model.LibraryChapter{
		{RemoteID: "/c/1", Title: "第一章", Ord: 0},
		{RemoteID: "/c/2", Title: "第二章", Ord: 1},
		{RemoteID: "/c/3", Title: "第三章", Ord: 2},
	}); err != nil {
		t.Fatal(err)
	}

	// 中间那一章从源里消失了。
	_, _, retired, err := ReplaceLibraryChapters(itemID, []*model.LibraryChapter{
		{RemoteID: "/c/1", Title: "第一章", Ord: 0},
		{RemoteID: "/c/3", Title: "第三章", Ord: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	if retired != 1 {
		t.Fatalf("retired=%d, want 1", retired)
	}
	// 行必须还在，只是转了状态：阅读进度指着它，删行会把"这章读不到了"
	// 变成"这本书从来没这章"。Mihon 在这里是物理删除，我们不跟。
	if got := countChapters(t, itemID, ""); got != 3 {
		t.Fatalf("total rows = %d, want 3; a retired chapter must not be deleted", got)
	}
	if got := countChapters(t, itemID, "active"); got != 2 {
		t.Fatalf("active rows = %d, want 2", got)
	}
	var state string
	if err := DB().Get(&state, `SELECT lifecycle_state FROM library_chapters WHERE item_id=? AND remote_id=?`, itemID, "/c/2"); err != nil {
		t.Fatal(err)
	}
	if state != "deleted" {
		t.Fatalf("missing chapter state = %q, want deleted", state)
	}

	// 它又回来了：必须复活成 active，而不是插一条新的。
	before := countChapters(t, itemID, "")
	if _, _, _, err := ReplaceLibraryChapters(itemID, []*model.LibraryChapter{
		{RemoteID: "/c/1", Title: "第一章", Ord: 0},
		{RemoteID: "/c/2", Title: "第二章", Ord: 1},
		{RemoteID: "/c/3", Title: "第三章", Ord: 2},
	}); err != nil {
		t.Fatal(err)
	}
	if got := countChapters(t, itemID, ""); got != before {
		t.Fatalf("revival added rows: %d -> %d", before, got)
	}
	if got := countChapters(t, itemID, "active"); got != 3 {
		t.Fatalf("active rows after revival = %d, want 3", got)
	}
}

func TestReplaceLibraryChaptersRejectsEmptyList(t *testing.T) {
	if err := InitDB(boundaryTestDBDir); err != nil {
		t.Fatal(err)
	}
	const sourceKey = "lib_empty_test"
	cleanupLibraryFixture(t, sourceKey)
	itemID := mustInsertLibraryItem(t, model.SourceKindNovel, sourceKey, "book-3")

	if _, _, _, err := ReplaceLibraryChapters(itemID, []*model.LibraryChapter{
		{RemoteID: "/c/1", Title: "第一章", Ord: 0},
	}); err != nil {
		t.Fatal(err)
	}

	// 空目录不是"这本书没有章节"，是这次没解析出东西。Legado 专门为它抛
	// TocEmptyException 而不是当正常结果：一次规则失效与一本真的完结无章的书
	// 在界面上必须能分开，否则规则一坏用户就看到"本书无章节"。
	if _, _, _, err := ReplaceLibraryChapters(itemID, nil); err == nil {
		t.Fatal("empty chapter list must fail, not silently retire every chapter")
	}
	if got := countChapters(t, itemID, "active"); got != 1 {
		t.Fatalf("active rows = %d after a rejected sync, want 1; the existing toc must be untouched", got)
	}
}

func TestGetEnabledSourcesExcludesReadingSources(t *testing.T) {
	if err := InitDB(boundaryTestDBDir); err != nil {
		t.Fatal(err)
	}
	const (
		videoKey = "lib_kind_video"
		novelKey = "lib_kind_novel"
		comicKey = "lib_kind_comic"
	)
	t.Cleanup(func() {
		for _, key := range []string{videoKey, novelKey, comicKey} {
			_, _ = DB().Exec(`DELETE FROM sources WHERE source_key=?`, key)
		}
	})
	for _, key := range []string{videoKey, novelKey, comicKey} {
		_, _ = DB().Exec(`DELETE FROM sources WHERE source_key=?`, key)
	}

	insert := func(key string, kind string) {
		t.Helper()
		if _, err := DB().Exec(
			`INSERT INTO sources (source_key, name, api_url, enabled, kind) VALUES (?, ?, ?, 1, ?)`,
			key, key, "https://example.test/api", kind); err != nil {
			t.Fatal(err)
		}
	}
	insert(videoKey, "video")
	insert(novelKey, "novel")
	insert(comicKey, "comic")

	// 这一条守的是真实故障：调度器与豆瓣匹配都拿 GetEnabledSources 的返回值
	// 当 MAC-CMS 接口去拉分页，一个启用的书源进了这个列表就会被发一轮
	// ?ac=detail&pg=1，然后连败三次被自动停用。
	sources, err := GetEnabledSources()
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range sources {
		if s.SourceKey == novelKey || s.SourceKey == comicKey {
			t.Fatalf("GetEnabledSources returned %s (kind=%q); the video collection pool must not contain reading sources", s.SourceKey, s.Kind)
		}
	}
	found := false
	for _, s := range sources {
		if s.SourceKey == videoKey {
			found = true
			if s.GetKind() != model.SourceKindVideo {
				t.Fatalf("video source kind = %q, want video", s.GetKind())
			}
		}
	}
	if !found {
		t.Fatal("GetEnabledSources dropped the video source")
	}

	for _, tc := range []struct {
		key  string
		kind model.SourceKind
	}{
		{novelKey, model.SourceKindNovel},
		{comicKey, model.SourceKindComic},
	} {
		byKind, err := GetEnabledSourcesByKind(tc.kind)
		if err != nil {
			t.Fatal(err)
		}
		if len(byKind) != 1 || byKind[0].SourceKey != tc.key {
			t.Fatalf("GetEnabledSourcesByKind(%s) = %d source(s), want exactly %s", tc.kind, len(byKind), tc.key)
		}
	}
}

func TestSourceKindDefaultsToVideoForLegacyRows(t *testing.T) {
	if err := InitDB(boundaryTestDBDir); err != nil {
		t.Fatal(err)
	}
	const sourceKey = "lib_kind_legacy"
	t.Cleanup(func() { _, _ = DB().Exec(`DELETE FROM sources WHERE source_key=?`, sourceKey) })
	_, _ = DB().Exec(`DELETE FROM sources WHERE source_key=?`, sourceKey)

	// 不写 kind，模拟迁移 v11 之前就在库里的那一行。
	if _, err := DB().Exec(
		`INSERT INTO sources (source_key, name, api_url, enabled) VALUES (?, ?, ?, 1)`,
		sourceKey, sourceKey, "https://example.test/api"); err != nil {
		t.Fatal(err)
	}
	s, err := GetSourceByKey(sourceKey)
	if err != nil {
		t.Fatal(err)
	}
	if s.GetKind() != model.SourceKindVideo {
		t.Fatalf("legacy row kind = %q, want video; without this the source vanishes from the collection pool", s.GetKind())
	}
	if !s.IsVideo() {
		t.Fatal("legacy row must count as a video source")
	}
}

func TestRetireLibraryItemsOfSourceKeepsRows(t *testing.T) {
	if err := InitDB(boundaryTestDBDir); err != nil {
		t.Fatal(err)
	}
	const sourceKey = "lib_retire_source"
	cleanupLibraryFixture(t, sourceKey)
	itemID := mustInsertLibraryItem(t, model.SourceKindComic, sourceKey, "series-9")
	if _, _, _, err := ReplaceLibraryChapters(itemID, []*model.LibraryChapter{
		{RemoteID: "/c/1", Title: "第一话", Ord: 0},
	}); err != nil {
		t.Fatal(err)
	}

	tx, err := DB().Beginx()
	if err != nil {
		t.Fatal(err)
	}
	if err := RetireLibraryItemsOfSource(tx, sourceKey); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	var items, chapters int
	if err := DB().Get(&items, `SELECT COUNT(*) FROM library_items WHERE source_key=?`, sourceKey); err != nil {
		t.Fatal(err)
	}
	if err := DB().Get(&chapters, `SELECT COUNT(*) FROM library_chapters WHERE item_id=?`, itemID); err != nil {
		t.Fatal(err)
	}
	if items != 1 || chapters != 1 {
		t.Fatalf("retiring a source deleted rows: items=%d chapters=%d, want 1/1", items, chapters)
	}
	var state string
	if err := DB().Get(&state, `SELECT lifecycle_state FROM library_items WHERE id=?`, itemID); err != nil {
		t.Fatal(err)
	}
	if state != "deleted" {
		t.Fatalf("item lifecycle_state = %q, want deleted", state)
	}
	// 书架要能把它取回来并显示成"来源已下架"，所以带 includeRetired 的读取必须还看得见它。
	listed, err := ListLibraryItems(model.SourceKindComic, sourceKey, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 {
		t.Fatalf("ListLibraryItems(includeRetired) = %d, want 1", len(listed))
	}
	active, err := ListLibraryItems(model.SourceKindComic, sourceKey, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 0 {
		t.Fatalf("ListLibraryItems(active only) = %d, want 0", len(active))
	}
}

func cleanupLibraryFixture(t *testing.T, sourceKey string) {
	t.Helper()
	t.Cleanup(func() {
		_, _ = DB().Exec(`DELETE FROM library_chapters WHERE item_id IN (SELECT id FROM library_items WHERE source_key=?)`, sourceKey)
		_, _ = DB().Exec(`DELETE FROM library_items WHERE source_key=?`, sourceKey)
		_, _ = DB().Exec(`DELETE FROM sources WHERE source_key=?`, sourceKey)
	})
	if _, err := DB().Exec(`DELETE FROM library_chapters WHERE item_id IN (SELECT id FROM library_items WHERE source_key=?)`, sourceKey); err != nil {
		t.Fatal(err)
	}
	if _, err := DB().Exec(`DELETE FROM library_items WHERE source_key=?`, sourceKey); err != nil {
		t.Fatal(err)
	}
	if _, err := DB().Exec(`DELETE FROM sources WHERE source_key=?`, sourceKey); err != nil {
		t.Fatal(err)
	}
}

func mustInsertLibraryItem(t *testing.T, kind model.SourceKind, sourceKey, remoteID string) int {
	t.Helper()
	ids, err := UpsertLibraryItems(kind, sourceKey, []*model.LibraryItem{
		{RemoteID: remoteID, Title: "测试作品"},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] == 0 {
		t.Fatalf("fixture item got no id: %v", ids)
	}
	return ids[0]
}

func countChapters(t *testing.T, itemID int, state string) int {
	t.Helper()
	var count int
	query := `SELECT COUNT(*) FROM library_chapters WHERE item_id=?`
	args := []any{itemID}
	if state != "" {
		query += ` AND lifecycle_state=?`
		args = append(args, state)
	}
	if err := DB().Get(&count, query, args...); err != nil {
		t.Fatal(err)
	}
	return count
}

func chapterIDs(t *testing.T, itemID int) []int {
	t.Helper()
	var ids []int
	if err := DB().Select(&ids, `SELECT id FROM library_chapters WHERE item_id=? ORDER BY ord, id`, itemID); err != nil {
		t.Fatal(err)
	}
	return ids
}

func assertSameIDs(t *testing.T, before, after []int) {
	t.Helper()
	if len(before) != len(after) {
		t.Fatalf("row count changed: %d -> %d", len(before), len(after))
	}
	for i := range before {
		if before[i] != after[i] {
			t.Fatalf("row %d changed id (%d -> %d); chapter identity must survive a toc refresh", i, before[i], after[i])
		}
	}
}
