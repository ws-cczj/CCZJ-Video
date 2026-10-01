package backup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"cczjVideo/app/db"
	"cczjVideo/app/model"
)

// testDirs holds the one data directory this binary shares: db.InitDB is
// once-guarded, so every test works on the same SQLite file.
var testDirs struct {
	data string
}

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "cczj-backup-test")
	if err != nil {
		panic(err)
	}
	testDirs.data = dir
	if err := db.InitDB(dir); err != nil {
		panic(err)
	}
	code := m.Run()
	db.Close()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func resetUserTables(t *testing.T) {
	t.Helper()
	database := db.DB()
	if _, err := database.Exec(`DELETE FROM favorites; DELETE FROM watch_history; DELETE FROM global_video; DELETE FROM settings; DELETE FROM sources`); err != nil {
		t.Fatal(err)
	}
}

// favoriteByTitle / historyByTitle 是测试夹具：产品代码只按已解析的目录身份写收藏与
// 历史，这里用标题换一次 global_id，只为省掉每条用例手工建 global_video 行的样板。
func favoriteByTitle(t *testing.T, sourceKey, vodID, name string) {
	t.Helper()
	globalID, err := db.GetOrCreateGlobalID(name, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AddFavoriteByIdentity(globalID, sourceKey, vodID); err != nil {
		t.Fatal(err)
	}
}

func historyByTitle(t *testing.T, sourceKey, vodID, name string, epNum int, position float64) int64 {
	t.Helper()
	globalID, err := db.GetOrCreateGlobalID(name, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SaveWatchHistoryByIdentity(globalID, sourceKey, vodID, epNum, position); err != nil {
		t.Fatal(err)
	}
	return globalID
}

func TestBackupExportImportRoundTrip(t *testing.T) {
	resetUserTables(t)
	if err := db.AddSource(&model.Source{SourceKey: "src_a", Name: "A", ApiUrl: "https://example.com/api.php"}); err != nil {
		t.Fatal(err)
	}
	favoriteByTitle(t, "src_a", "vod-1", "备份测试片")
	historyByTitle(t, "src_a", "vod-1", "备份测试片", 3, 42.5)
	if err := db.SetSetting("theme_id", "dark"); err != nil {
		t.Fatal(err)
	}
	if err := db.SetSetting("window_width", "1234"); err != nil {
		t.Fatal(err)
	}

	service := NewService()
	path, err := service.Export(testDirs.data, "")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(path) != filepath.Join(testDirs.data, "exports") {
		t.Fatalf("export landed in %q", path)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) == 0 {
		t.Fatal("export file is empty")
	}

	// 模拟换机：收藏、历史、全局条目和源全没了，窗口宽度是本机自己的值。
	if _, err := db.DB().Exec(`DELETE FROM favorites; DELETE FROM watch_history; DELETE FROM global_video; DELETE FROM sources; UPDATE settings SET value='light' WHERE key='theme_id'`); err != nil {
		t.Fatal(err)
	}
	if err := db.SetSetting("window_width", "999"); err != nil {
		t.Fatal(err)
	}

	result, err := service.ImportBytes(filepath.Base(path), raw, "file")
	if err != nil {
		t.Fatal(err)
	}
	if result.FavoritesAdded != 1 || result.HistoryApplied != 1 || result.SourcesAdded != 1 || result.Unresolved != 0 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result.SettingsApplied == 0 {
		t.Fatal("no settings applied")
	}

	favorites, err := db.GetFavorites(1, 10)
	if err != nil || len(favorites) != 1 || favorites[0].VodName != "备份测试片" {
		t.Fatalf("favorites after import = %+v, %v", favorites, err)
	}
	history, err := db.GetRecentHistory(10)
	if err != nil || len(history) != 1 || history[0].Position != 42.5 || history[0].EpNum != 3 {
		t.Fatalf("history after import = %+v, %v", history, err)
	}
	if value, _ := db.GetSetting("theme_id"); value != "dark" {
		t.Fatalf("theme_id after import = %q", value)
	}
	if value, _ := db.GetSetting("window_width"); value != "999" {
		t.Fatalf("machine-local window_width was overwritten with %q", value)
	}
}

func TestBackupImportNeverDeletesAndKeepsNewerProgress(t *testing.T) {
	resetUserTables(t)
	if err := db.AddSource(&model.Source{SourceKey: "src_a", Name: "A", ApiUrl: "https://example.com/api.php"}); err != nil {
		t.Fatal(err)
	}
	favoriteByTitle(t, "src_a", "vod-old", "本机保留")
	historyID := historyByTitle(t, "src_a", "vod-x", "同一部片", 1, 10)

	service := NewService()
	path, err := service.Export(testDirs.data, "")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	// 备份落盘后本机进度继续往前走过；导入旧备份不能把它倒回去。
	if _, err := db.DB().Exec(`UPDATE watch_history SET updated_at='2999-01-01 00:00:00'`); err != nil {
		t.Fatal(err)
	}

	// 备份里再塞一条本机没有的收藏，模拟「旧备份 + 新本机」并存。
	decoded, err := decompress(filepath.Base(path), raw)
	if err != nil {
		t.Fatal(err)
	}
	var payload Payload
	if err := json.Unmarshal(decoded, &payload); err != nil {
		t.Fatal(err)
	}
	payload.Favorites = append(payload.Favorites, db.BackupFavorite{
		VodName: "只存在于备份", Year: "", SourceKey: "src_a", VodID: "vod-new", CreatedAt: "2026-01-01 00:00:00",
	})
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}

	result, err := service.ImportBytes("backup.json", encoded, "test")
	if err != nil {
		t.Fatal(err)
	}
	if result.FavoritesAdded != 1 {
		t.Fatalf("expected the missing favorite to be added, got %+v", result)
	}
	if result.HistoryApplied != 0 {
		t.Fatalf("import must not overwrite newer local progress: %+v", result)
	}
	favorites, err := db.GetFavorites(1, 10)
	if err != nil || len(favorites) != 2 {
		t.Fatalf("favorites = %+v, %v", favorites, err)
	}
	position, err := db.GetWatchHistoryByIdentity(historyID, "src_a", "vod-x", 1)
	if err != nil || position != 10 {
		t.Fatalf("position = %v, %v", position, err)
	}
}

func TestRestoreArchiveMergesWithoutReplacing(t *testing.T) {
	resetUserTables(t)
	if err := db.AddSource(&model.Source{SourceKey: "src_a", Name: "A", ApiUrl: "https://example.com/api.php"}); err != nil {
		t.Fatal(err)
	}
	favoriteByTitle(t, "src_a", "vod-1", "归档里的收藏")
	historyByTitle(t, "src_a", "vod-1", "归档里的收藏", 2, 5)

	archiveDir := filepath.Join(testDirs.data, archiveDirName)
	if err := os.MkdirAll(archiveDir, 0755); err != nil {
		t.Fatal(err)
	}
	archiveName := "cczj_video_pre_migration_test.db"
	if _, err := db.DB().Exec(`VACUUM INTO ?`, filepath.Join(archiveDir, archiveName)); err != nil {
		t.Fatal(err)
	}

	archives, err := NewService().Archives(testDirs.data)
	if err != nil {
		t.Fatal(err)
	}
	// InitDB 自己就会在首次迁移前留一份快照，这里只要求本次写入的归档在列。
	var listed bool
	for _, archive := range archives {
		if archive.Name == archiveName {
			listed = true
		}
	}
	if !listed {
		t.Fatalf("archives = %+v", archives)
	}
	if _, err := NewService().RestoreArchive(testDirs.data, filepath.Join("..", "..", archiveName)); err == nil {
		t.Fatal("path traversal was accepted")
	}

	// 用户删掉了收藏与历史，归档应当把它们找回来。
	if _, err := db.DB().Exec(`DELETE FROM favorites; DELETE FROM watch_history`); err != nil {
		t.Fatal(err)
	}
	result, err := NewService().RestoreArchive(testDirs.data, archiveName)
	if err != nil {
		t.Fatal(err)
	}
	if result.FavoritesAdded != 1 || result.HistoryApplied != 1 || result.SourcesAdded != 0 {
		t.Fatalf("restore result = %+v", result)
	}
	favorites, err := db.GetFavorites(1, 10)
	if err != nil || len(favorites) != 1 || favorites[0].VodName != "归档里的收藏" {
		t.Fatalf("favorites = %+v, %v", favorites, err)
	}
	// 归档目录之外什么都不该被写：读取用的临时副本必须清理干净。
	if entries, err := os.ReadDir(archiveDir); err != nil || len(entries) != len(archives) {
		t.Fatalf("archive dir after restore = %+v, %v (listed %d)", entries, err, len(archives))
	}
}

func TestImportRejectsForeignPayload(t *testing.T) {
	if _, err := NewService().ImportBytes("backup.json", []byte(`{"kind":"other"}`), "test"); err == nil {
		t.Fatal("a non-backup payload was accepted")
	}
	if _, err := NewService().ImportBytes("backup.json", []byte(`{"kind":"`+payloadKind+`","version":99}`), "test"); err == nil {
		t.Fatal("an unsupported backup version was accepted")
	}
}

// 导出走不了原生保存对话框，所以界面改成列 exports 目录、按文件名导入。
// 这条测试盯住两件事：刚导出的文件必须在列表里，且文件名不能变成路径。
func TestExportsListAndImportByName(t *testing.T) {
	resetUserTables(t)
	if err := db.AddSource(&model.Source{SourceKey: "src_a", Name: "A", ApiUrl: "https://example.com/api.php"}); err != nil {
		t.Fatal(err)
	}
	favoriteByTitle(t, "src_a", "vod-9", "备份清单片")

	service := NewService()
	path, err := service.Export(testDirs.data, "")
	if err != nil {
		t.Fatal(err)
	}
	files, err := service.Exports(testDirs.data)
	if err != nil {
		t.Fatal(err)
	}
	name := filepath.Base(path)
	found := false
	for _, item := range files {
		if item.Name == name {
			found = true
			if item.SizeBytes <= 0 || item.ModifiedAt == "" {
				t.Fatalf("listed %q without size or time: %+v", name, item)
			}
		}
	}
	if !found {
		t.Fatalf("exported file %q missing from %v", name, files)
	}

	if _, err := db.DB().Exec(`DELETE FROM favorites; DELETE FROM global_video; DELETE FROM sources`); err != nil {
		t.Fatal(err)
	}
	result, err := service.ImportFile(testDirs.data, name)
	if err != nil {
		t.Fatal(err)
	}
	if result.FavoritesAdded != 1 || result.SourcesAdded != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}

	for _, bad := range []string{"", "not-a-backup.txt", "../" + name, "..\\" + name, "sub/" + name} {
		if _, err := service.ImportFile(testDirs.data, bad); err == nil {
			t.Fatalf("ImportFile(%q) was accepted", bad)
		}
	}

	// 再导一次同一份备份：本机已经全都有了，计数必须归零，
	// 否则界面会报出一堆实际没发生的改动。
	again, err := service.ImportFile(testDirs.data, name)
	if err != nil {
		t.Fatal(err)
	}
	if again.FavoritesAdded != 0 || again.HistoryApplied != 0 || again.SourcesAdded != 0 || again.SettingsApplied != 0 {
		t.Fatalf("re-import reported changes: %+v", again)
	}
}
