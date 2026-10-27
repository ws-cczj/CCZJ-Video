package douban

import (
	"testing"

	"cczjVideo/app/db"
	"cczjVideo/app/model"
)

func TestHomeChartDoesNotFetchWithoutVideoCatalog(t *testing.T) {
	isolateChartCache(t)
	if err := db.InitDB(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	sources, err := db.GetSourcesByKind(model.SourceKindVideo)
	if err != nil || len(sources) != 0 {
		t.Fatalf("fresh install seeded video sources: %v %v", sources, err)
	}
	calls := 0
	fetchChartPage = func(string) (string, error) { calls++; return "", nil }
	for _, withSource := range []bool{false, true} {
		if withSource {
			if err := db.AddSource(&model.Source{SourceKey: "empty_video", Name: "Empty", ApiUrl: "https://source.example/api", Enabled: 1}); err != nil {
				t.Fatal(err)
			}
		}
		rows, err := GetChartVideos()
		if err != nil || len(rows) != 0 || calls != 0 {
			t.Fatalf("empty catalog fetched chart: rows=%v calls=%d err=%v", rows, calls, err)
		}
	}
}
