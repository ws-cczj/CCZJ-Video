package handler

import (
	"cczjVideo/app/applog"
	"cczjVideo/app/db"
	"cczjVideo/app/model"
	"os"
	"path/filepath"
	"testing"
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "cczj-handler-test-")
	if err != nil {
		panic(err)
	}
	// 日志单例必须抢在第一次 applog 调用之前绑到临时目录：applog.Default() 会在
	// 单例为空时用 %APPDATA% 的生产目录建出来，采集/调度路径一打日志就进了用户真机日志。
	if err := applog.Init(filepath.Join(dir, "applog")); err != nil {
		panic(err)
	}
	if err := db.InitDB(dir); err != nil {
		panic(err)
	}
	code := m.Run()
	applog.Default().Close()
	db.Close()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func TestSearchLocalCatalogDropsRemoteHitsAndCapsResults(t *testing.T) {
	const sourceKey = "local_supplement_test"
	if _, err := db.DB().Exec(`DELETE FROM source_videos WHERE source_key=?`, sourceKey); err != nil {
		t.Fatal(err)
	}
	seed := make([]*model.Video, 0, 3)
	for i, name := range []string{"是，大臣", "是的，首相", "大臣笔记"} {
		seed = append(seed, &model.Video{
			VodId: model.FlexibleString("vod-" + string(rune('a'+i))), TypeId: "1",
			TypeName: "Local supplement type", VodName: name, VodTime: "2026-01-0" + string(rune('1'+i)),
		})
	}
	if err := db.UpsertCatalogItems(sourceKey, seed); err != nil {
		t.Fatal(err)
	}

	// "首相" only exists in the catalog; the upstream search never returns it.
	matches := searchLocalCatalog(sourceKey, "大臣", []string{"vod-a"})
	if len(matches) != 1 {
		t.Fatalf("local matches = %d (%v); want 1 after excluding the remote hit", len(matches), matches)
	}
	if matches[0].VodName != "大臣笔记" {
		t.Fatalf("local match = %q; want 大臣笔记", matches[0].VodName)
	}
	if !matches[0].InCatalog {
		t.Error("local matches must be flagged as already stored")
	}
}
