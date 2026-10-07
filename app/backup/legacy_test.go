package backup

import (
	"os"
	"path/filepath"
	"testing"

	"cczjVideo/app/db"
	"cczjVideo/app/model"
)

// legacyDirFromLive 把当前库原样快照进一个临时目录，当作「2.1.0 之前留在 exe 旁边
// 的那份 data」。VACUUM INTO 落的是单文件、已归并好的库，正符合旧目录关掉之后的样子。
func legacyDirFromLive(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "data")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB().Exec(`VACUUM INTO ?`, filepath.Join(dir, legacyDBName)); err != nil {
		t.Fatal(err)
	}
	return dir
}

func favoriteCount(t *testing.T, title string) int {
	t.Helper()
	var count int
	if err := db.DB().QueryRow(`SELECT count(*) FROM favorites f
		JOIN global_video g ON g.id = f.global_id WHERE g.vod_name = ?`, title).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func TestPeekLegacyCountsWhatCanBeRecovered(t *testing.T) {
	resetUserTables(t)
	if err := db.AddSource(&model.Source{SourceKey: "src_old", Name: "旧源", ApiUrl: "https://example.com/api.php"}); err != nil {
		t.Fatal(err)
	}
	favoriteByTitle(t, "src_old", "vod-old", "旧库里的收藏")
	historyByTitle(t, "src_old", "vod-old", "旧库里的收藏", 4, 88.5)
	legacyDir := legacyDirFromLive(t)

	counts, err := NewService().PeekLegacy(legacyDir)
	if err != nil {
		t.Fatal(err)
	}
	if counts.Favorites != 1 || counts.History != 1 || counts.Sources != 1 {
		t.Fatalf("counts = %+v", counts)
	}
	if counts.Empty() {
		t.Fatal("有内容的旧库不该被判成空")
	}

	// 预读只读，不写：当前库一个字节都不该因为数了一遍就变了。
	if got := favoriteCount(t, "旧库里的收藏"); got != 1 {
		t.Fatalf("favorites after peek = %d", got)
	}
}

func TestMergeLegacyBringsBackSourcesFavoritesAndHistory(t *testing.T) {
	resetUserTables(t)
	if err := db.AddSource(&model.Source{SourceKey: "src_old", Name: "旧源", ApiUrl: "https://example.com/api.php"}); err != nil {
		t.Fatal(err)
	}
	favoriteByTitle(t, "src_old", "vod-old", "合并回来的片子")
	historyByTitle(t, "src_old", "vod-old", "合并回来的片子", 6, 120.5)
	legacyDir := legacyDirFromLive(t)

	// 用户升级后看到的是空库：源、收藏、历史都没了，这正是旧库要补的东西。
	resetUserTables(t)
	result, err := NewService().MergeLegacy(legacyDir)
	if err != nil {
		t.Fatal(err)
	}
	if result.SourcesAdded != 1 || result.FavoritesAdded != 1 || result.HistoryApplied != 1 {
		t.Fatalf("merge result = %+v", result)
	}
	if got := favoriteCount(t, "合并回来的片子"); got != 1 {
		t.Fatalf("favorites after merge = %d", got)
	}
	source, err := db.GetSourceByKey("src_old")
	if err != nil || source == nil {
		t.Fatalf("source after merge = %+v, %v", source, err)
	}
}

// 旧库的设置可能是几个版本之前的选择。把用户现在的主题、数据新鲜度盖掉，
// 比少带几项设置糟得多，所以合并一概不带设置。
func TestMergeLegacyNeverImportsSettings(t *testing.T) {
	resetUserTables(t)
	if err := db.AddSource(&model.Source{SourceKey: "src_old", Name: "旧源", ApiUrl: "https://example.com/api.php"}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetSetting("theme_id", "classic"); err != nil {
		t.Fatal(err)
	}
	legacyDir := legacyDirFromLive(t)

	resetUserTables(t)
	if err := db.SetSetting("theme_id", "dark"); err != nil {
		t.Fatal(err)
	}
	result, err := NewService().MergeLegacy(legacyDir)
	if err != nil {
		t.Fatal(err)
	}
	// 内容照样并进来，只是设置一项都不带。
	if result.SourcesAdded != 1 {
		t.Fatalf("merge result = %+v", result)
	}
	if result.SettingsApplied != 0 {
		t.Fatalf("settings applied from a legacy database = %+v", result)
	}
	if value, err := db.GetSetting("theme_id"); err != nil || value != "dark" {
		t.Fatalf("theme_id after merge = %q, %v", value, err)
	}
	// 旧数据处理记账同样是本机的事：跟着备份走到另一台机器，就会把那边还没找回的
	// 旧库判成「已经处理过」，那正是这份找回通道最怕的静默失效。
	if !machineLocalSettings["legacy_data_status"] || !machineLocalSettings["legacy_data_at"] {
		t.Fatal("legacy bookkeeping keys must not travel inside a backup")
	}
}

func TestMergeLegacyRejectsWithoutUserContent(t *testing.T) {
	resetUserTables(t)
	legacyDir := legacyDirFromLive(t)

	counts, err := NewService().PeekLegacy(legacyDir)
	if err != nil {
		t.Fatal(err)
	}
	if !counts.Empty() {
		t.Fatalf("counts = %+v, want empty", counts)
	}
	// 一份空旧库递到合并里什么也带不回来；与其报「合并成功、0 条」，不如当场说清楚。
	if _, err := NewService().MergeLegacy(legacyDir); err == nil {
		t.Fatal("an empty legacy database was accepted for merge")
	}
}

func TestWithLegacyDBRejectsUnusableDirectories(t *testing.T) {
	cases := []struct {
		name string
		dir  func(t *testing.T) string
	}{
		{"没有旧库", func(t *testing.T) string { return t.TempDir() }},
		{"旧库是空文件", func(t *testing.T) string {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, legacyDBName), nil, 0644); err != nil {
				t.Fatal(err)
			}
			return dir
		}},
		{"空路径", func(t *testing.T) string { return "" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewService().PeekLegacy(tc.dir(t)); err == nil {
				t.Fatal("expected the unusable legacy directory to be rejected")
			}
		})
	}
}

// 旧库留在原地是这份修复的前提：读它、数它、合并它，都不许动它一个字节。
func TestMergeLegacyLeavesTheLegacyDatabaseUntouched(t *testing.T) {
	resetUserTables(t)
	if err := db.AddSource(&model.Source{SourceKey: "src_old", Name: "旧源", ApiUrl: "https://example.com/api.php"}); err != nil {
		t.Fatal(err)
	}
	favoriteByTitle(t, "src_old", "vod-old", "只读的旧库")
	legacyDir := legacyDirFromLive(t)

	path := filepath.Join(legacyDir, legacyDBName)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewService().MergeLegacy(legacyDir); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("the legacy database file was modified")
	}
	if entries, err := os.ReadDir(legacyDir); err != nil {
		t.Fatal(err)
	} else if len(entries) != 1 {
		t.Fatalf("legacy dir after merge = %v, want only the database", entries)
	}
}

// 旧库在 Program Files 之类的地方时，原地只读打开会失败，这时必须退回临时副本，
// 而不是把整条找回通道判死。副本用完要清干净，也不能反过来写坏旧库。
func TestLegacyFallsBackToACopyWhenTheDatabaseIsReadOnly(t *testing.T) {
	resetUserTables(t)
	if err := db.AddSource(&model.Source{SourceKey: "src_ro", Name: "只读旧源", ApiUrl: "https://example.com/api.php"}); err != nil {
		t.Fatal(err)
	}
	favoriteByTitle(t, "src_ro", "vod-ro", "只读目录里的收藏")
	legacyDir := legacyDirFromLive(t)
	path := filepath.Join(legacyDir, legacyDBName)
	if err := os.Chmod(path, 0444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0644) })

	counts, err := NewService().PeekLegacy(legacyDir)
	if err != nil {
		t.Fatalf("a read-only legacy database should still be recoverable: %v", err)
	}
	if counts.Favorites != 1 || counts.Sources != 1 {
		t.Fatalf("counts = %+v", counts)
	}
	if entries, err := os.ReadDir(legacyDir); err != nil {
		t.Fatal(err)
	} else if len(entries) != 1 {
		t.Fatalf("legacy dir after peek = %v, want only the database", entries)
	}
}
