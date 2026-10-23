package collect

import (
	"cczjVideo/app/applog"
	"cczjVideo/app/db"
	"cczjVideo/app/model"
	"path/filepath"
	"testing"
)

func TestCollectionDoesNotCountOrAnnounceDisabledTypes(t *testing.T) {
	dir := t.TempDir()
	if err := applog.Init(filepath.Join(dir, "applog")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { applog.Default().Close() })
	if err := db.InitDB(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	if _, err := db.DB().Exec(`INSERT INTO global_types(type_name,collect_enabled) VALUES ('伦理片',0),('倫理片',1),('福利',0),('福利片',1)`); err != nil {
		t.Fatal(err)
	}
	if err := db.AddSource(&model.Source{SourceKey: "policy_count", Name: "Policy fixture", ApiUrl: "https://example.test/api", Enabled: 1}); err != nil {
		t.Fatal(err)
	}
	withKeywordSource(t, func(string) string {
		return `{"code":1,"page":1,"pagecount":1,"total":3,"list":[
	 {"vod_id":"1","type_name":"倫理片","vod_name":"blocked ethics"},
	 {"vod_id":"2","type_name":"福利片","vod_name":"blocked welfare"},
	 {"vod_id":"3","type_name":"剧情片","vod_name":"allowed"}]}`
	})
	var names []string
	engine := NewEngineV2("policy_count", nil, nil, func(_ int, page []string) { names = append(names, page...) }, WithCollectMode(model.CollectModeOnce))
	stats, err := engine.Run()
	if err != nil {
		t.Fatal(err)
	}
	if stats.Saved != 1 {
		t.Errorf("saved=%d; disabled types must not be counted", stats.Saved)
	}
	if len(names) != 1 || names[0] != "allowed" {
		t.Errorf("announced=%v; want only allowed item", names)
	}
}
