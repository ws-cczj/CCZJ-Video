package collect

import "testing"

func TestParseVideoWithMappingUsesStableAliasPrecedence(t *testing.T) {
	raw := []byte(`{"id":42,"title":"stable title","type":"movie","score":"8.5","douban_score":"9.1"}`)
	for i := 0; i < 100; i++ {
		video, err := ParseVideoWithMapping(raw, nil)
		if err != nil {
			t.Fatal(err)
		}
		if got := video.VodId.String(); got != "42" {
			t.Fatalf("vod_id = %q", got)
		}
		if video.VodName != "stable title" || video.TypeName != "movie" {
			t.Fatalf("unexpected title/type: %+v", video)
		}
		if video.VodScore != "8.5" || video.VodDoubanScore != "9.1" {
			t.Fatalf("unexpected scores: score=%q douban=%q", video.VodScore, video.VodDoubanScore)
		}
	}
}

func TestParseVideoWithMappingConfigurationWinsOverDefaultAlias(t *testing.T) {
	video, err := ParseVideoWithMapping([]byte(`{"type":"drama"}`), map[string]string{"type": "vod_class"})
	if err != nil {
		t.Fatal(err)
	}
	if video.VodClass != "drama" || video.TypeName != "" {
		t.Fatalf("configuration mapping was not respected: %+v", video)
	}
}
