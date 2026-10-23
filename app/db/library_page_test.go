package db

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"cczjVideo/app/model"
	"github.com/jmoiron/sqlx"
)

func catalogTestDB(t testing.TB) *sqlx.DB {
	t.Helper()
	conn, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	previous := instance
	instance = conn
	t.Cleanup(func() { instance = previous; conn.Close() })
	return conn
}

func TestCatalogPagingFiltersAcrossWholeCatalogAndKeepsCursorCeiling(t *testing.T) {
	conn := catalogTestDB(t)
	for i := 1; i <= 8; i++ {
		genre, status := "adventure", "serial"
		if i == 1 {
			genre, status = "rare，adventure", "Completed"
		}
		if _, err := conn.Exec(`INSERT INTO library_items(kind,source_key,remote_id,title,genres,status,summary) VALUES('novel','source',?,?,?,?,?)`, fmt.Sprint(i), fmt.Sprintf("Book %02d", i), genre, status, strings.Repeat("long summary", 1000)); err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	q := LibraryPageQuery{Kind: "novel", Limit: 2}
	first, err := PageLibraryItems(ctx, q)
	if err != nil || len(first.Items) != 2 || first.NextCursor == "" {
		t.Fatalf("first = %+v %v", first, err)
	}
	if first.Items[0].RemoteID != "8" || first.Items[1].RemoteID != "7" {
		t.Fatal("unexpected recent ordering")
	}
	if strings.Join(first.Genres, ",") != "adventure,rare" {
		t.Fatalf("facets limited to visible page: %v", first.Genres)
	}
	if _, err := conn.Exec(`INSERT INTO library_items(kind,source_key,remote_id,title) VALUES('novel','source','new','New')`); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{"8": true, "7": true}
	q.Cursor = first.NextCursor
	for q.Cursor != "" {
		page, err := PageLibraryItems(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range page.Items {
			if seen[item.RemoteID] || item.RemoteID == "new" {
				t.Fatalf("duplicate or post-snapshot row: %s", item.RemoteID)
			}
			seen[item.RemoteID] = true
		}
		q.Cursor = page.NextCursor
	}
	if len(seen) != 8 {
		t.Fatalf("lost rows: %v", seen)
	}
	filtered, err := PageLibraryItems(ctx, LibraryPageQuery{Kind: "novel", Genre: "rare", Complete: true, Limit: 2})
	if err != nil || len(filtered.Items) != 1 || filtered.Items[0].RemoteID != "1" {
		t.Fatalf("filter after paging: %+v %v", filtered, err)
	}
	if _, err := PageLibraryItems(ctx, LibraryPageQuery{Kind: "novel", Genre: "different", Cursor: first.NextCursor}); !errors.Is(err, ErrInvalidCatalogQuery) {
		t.Fatalf("cross-query cursor = %v", err)
	}
}

func TestCatalogTitleKeysetHandlesDuplicateCaseAndBatchRefsStayNatural(t *testing.T) {
	conn := catalogTestDB(t)
	for i, title := range []string{"same", "Same", "same", "z"} {
		if _, err := conn.Exec(`INSERT INTO library_items(kind,source_key,remote_id,title) VALUES('comic','source',?,?)`, fmt.Sprint(i), title); err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	q := LibraryPageQuery{Kind: "comic", Sort: "title", Limit: 1}
	seen := make(map[int]bool)
	for {
		page, err := PageLibraryItems(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range page.Items {
			if seen[item.Id] {
				t.Fatal("duplicate title cursor repeated item")
			}
			seen[item.Id] = true
		}
		if page.NextCursor == "" {
			break
		}
		q.Cursor = page.NextCursor
	}
	if len(seen) != 4 {
		t.Fatalf("title keyset lost rows: %d", len(seen))
	}
	refs := []LibraryRef{{Kind: "comic", SourceKey: "source", RemoteID: "0"}, {Kind: "comic", SourceKey: "source", RemoteID: "missing"}, {Kind: "novel", SourceKey: "source", RemoteID: "0"}}
	items, err := GetLibraryItemsByRefs(ctx, refs)
	if err != nil || len(items) != 1 || items[0].Kind != "comic" {
		t.Fatalf("natural batch resolution = %+v %v", items, err)
	}
	counts, err := CountLibraryItemsBySource(model.SourceKindComic)
	if err != nil || counts["source"] != 4 {
		t.Fatalf("grouped counts = %v %v", counts, err)
	}
	itemStats, chapterStats, err := LibraryStats(model.SourceKindNovel, "source")
	if err != nil || itemStats.Active != 0 || chapterStats.Active != 0 {
		t.Fatalf("empty stats = %+v %+v %v", itemStats, chapterStats, err)
	}
}

func BenchmarkCatalogMaterialization(b *testing.B) {
	conn := catalogTestDB(b)
	tx, err := conn.Beginx()
	if err != nil {
		b.Fatal(err)
	}
	stmt, err := tx.Prepare(`INSERT INTO library_items(kind,source_key,remote_id,title,genres,summary) VALUES('novel','source',?,?,'adventure',?)`)
	if err != nil {
		b.Fatal(err)
	}
	for i := 0; i < 10000; i++ {
		if _, err := stmt.Exec(fmt.Sprint(i), fmt.Sprintf("Book %05d", i), strings.Repeat("S", 4096)); err != nil {
			b.Fatal(err)
		}
	}
	stmt.Close()
	if err := tx.Commit(); err != nil {
		b.Fatal(err)
	}
	b.Run("legacy-all-10000", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if _, err := ListLibraryItemsByKind(model.SourceKindNovel, true); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("page-60", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if _, err := PageLibraryItems(context.Background(), LibraryPageQuery{Kind: "novel", Limit: 60, IncludeRetired: true}); err != nil {
				b.Fatal(err)
			}
		}
	})
}
