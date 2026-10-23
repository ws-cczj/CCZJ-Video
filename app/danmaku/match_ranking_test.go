package danmaku

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestMatchRanksMetadataInsteadOfRequiringIdentity(t *testing.T) {
	request := MatchRequest{Title: "凡人修仙传", Year: "2020", Kind: "国产动漫", EpisodeLabel: "第01集", DurationMS: 1248000}
	for _, row := range []Candidate{
		{Provider: "bilibili", Ref: "series", Title: "凡人修仙传", Year: "2023", Kind: "电视剧"},
		{Provider: "bilibili", Ref: "series", Title: "凡人修仙传重制版", Year: "2023", Kind: "国创"},
		{Provider: "bilibili", Ref: "series", Title: "凡人修仙傳", Kind: "国创"},
	} {
		if got := matchingWork([]Candidate{row}, request); got == nil {
			t.Errorf("high similarity candidate rejected: %+v", row)
		}
	}
	for _, title := range []string{"凡人修仙传仙界篇", "《凡人修仙传》多人有声剧人界篇灵界篇", "凡人修仙传解说合集", "另一个作品"} {
		if got := matchingWork([]Candidate{{Provider: "bilibili", Ref: "other", Title: title}}, request); got != nil {
			t.Errorf("unrelated or derivative candidate selected: %+v", got)
		}
	}
	if got := matchingWork([]Candidate{{Provider: "bilibili", Ref: "clip", Title: request.Title, Kind: "video"}}, request); got != nil {
		t.Fatal("a user clip must not substitute for the series")
	}
	if workTitleSimilarity("作品第1季", "作品第2季") >= 800 || workTitleSimilarity("三体", "三休") >= 800 {
		t.Fatal("different seasons and short unrelated titles must remain distinct")
	}
	rows := []Episode{
		{Provider: "bilibili", EpisodeID: "preview", Number: "1", Title: "第1集 预告", DurationMS: 1248000},
		{Provider: "bilibili", EpisodeID: "remaster", Number: "1", Title: "1 凡人风起天南1重制版", DurationMS: 1203160},
		{Provider: "bilibili", EpisodeID: "other", Number: "2", Title: "2 正片", DurationMS: 1248000},
	}
	if got := matchingEpisode(rows, request); got == nil || got.EpisodeID != "remaster" {
		t.Fatalf("same numbered remaster should be usable: %+v", got)
	}
	rows = append(rows, Episode{Provider: "bilibili", EpisodeID: "original", Number: "1", Title: "1 原版", DurationMS: 1248000})
	if got := matchingEpisode(rows, request); got == nil || got.EpisodeID != "original" {
		t.Fatalf("closest duration should rank first: %+v", got)
	}
	if got := matchingEpisode(rows[1:2], MatchRequest{EpisodeLabel: "第01集", DurationMS: 1800000}); got == nil {
		t.Fatal("duration difference must not veto a matching episode")
	}
	if got := matchingEpisode(rows, MatchRequest{EpisodeLabel: "第03集"}); got != nil {
		t.Fatal("array position must not substitute for a declared episode number")
	}
}

type rankedMatchProviderFixture struct {
	matchProviderFixture
	rows []Episode
}

func (p *rankedMatchProviderFixture) Episodes(context.Context, string) ([]Episode, error) {
	return p.rows, nil
}

func TestMatchAutomaticallyBindsRemasterAfterOldPolicyFailure(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)
	database, err := store.db()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO settings(key,value) VALUES(?,?)`, SourceSettingsKey, `{"disabled":["qq","iqiyi"]}`); err != nil {
		t.Fatal(err)
	}
	s := matchService(t, store)
	request := MatchRequest{LocalKey: "source:video:1", Title: "凡人修仙传", Year: "2020", Kind: "国产动漫", EpisodeLabel: "第01集", DurationMS: 1248000}
	p := &rankedMatchProviderFixture{
		matchProviderFixture: matchProviderFixture{name: "bilibili", search: func(context.Context) ([]Candidate, error) {
			return []Candidate{{Provider: "bilibili", Ref: "ss28747", Title: "凡人修仙传", Year: "2023", Kind: "国创"}}, nil
		}},
		rows: []Episode{{Provider: "bilibili", SeriesID: "28747", EpisodeID: "12345", Number: "1", Title: "1 凡人风起天南1重制版", DurationMS: 1203160}},
	}
	s.providers["bilibili"] = p
	// Old derived rejections can be reevaluated without deleting the ledger or
	// requesting search metadata again. The network policy remains independent.
	if _, err := s.Search(ctx, "bilibili", request.Title); err != nil {
		t.Fatal(err)
	}
	if err := store.saveMatchFailure(ctx, request.LocalKey, "bilibili", strings.TrimPrefix(matchFingerprint(request), matchPolicy+":"), "EPISODE_SPECIAL_REJECTED", time.Now()); err != nil {
		t.Fatal(err)
	}
	value, err := s.Match(ctx, request)
	if err != nil || value.Binding == nil || value.Binding.Episode.EpisodeID != "12345" || value.Window == nil {
		t.Fatal("same-number remaster did not reach playback automatically", value, err)
	}
	if len(value.Attempts) != 1 || value.Attempts[0].Status != "available" || !strings.Contains(value.Attempts[0].Detail, "similarity=1000/1000; number=1") || p.searches.Load() != 1 {
		t.Fatal("ranking evidence missing or cached search repeated", value.Attempts, p.searches.Load())
	}
	binding, err := store.Binding(ctx, request.LocalKey)
	if err != nil || binding == nil || binding.Episode.EpisodeID != "12345" {
		t.Fatal("replaceable comment binding was not saved", binding, err)
	}
}

func TestMatchRankingTiesDoNotRequireUserSelection(t *testing.T) {
	request := matchRequest()
	works := []Candidate{{Provider: "qq", Ref: "b", Title: "sample"}, {Provider: "qq", Ref: "a", Title: "sample"}}
	episodes := []Episode{{Provider: "qq", EpisodeID: "b", Number: "1"}, {Provider: "qq", EpisodeID: "a", Number: "1"}}
	w, e := matchingWork(works, request), matchingEpisode(episodes, request)
	if w == nil || e == nil {
		t.Fatal("equal ranking should select automatically")
	}
	works[0], works[1] = works[1], works[0]
	episodes[0], episodes[1] = episodes[1], episodes[0]
	w2, e2 := matchingWork(works, request), matchingEpisode(episodes, request)
	if w2 == nil || e2 == nil || w.Ref != w2.Ref || e.EpisodeID != e2.EpisodeID {
		t.Fatal("equal ranking should remain stable across upstream reorder")
	}
}

func TestBilibiliSameNumberPreviewBadgeDoesNotOverrideFullEpisode(t *testing.T) {
	client := NewClient(&http.Client{Transport: fakeTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/pgc/view/web/season" || r.URL.Query().Get("season_id") != "28747" {
			t.Fatalf("unexpected request: %s", r.URL)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"code":0,"result":{"season_id":28747,"episodes":[
			{"id":1,"cid":101,"aid":11,"title":"196","long_title":"","badge":"预告","duration":1248000},
			{"id":2,"cid":102,"aid":12,"title":"196","long_title":"","badge":"会员","duration":1200000}
		]}}`)), Header: http.Header{}}, nil
	})})
	p := &bilibiliProvider{client: client}
	rows, err := p.Episodes(context.Background(), "ss28747")
	if err != nil {
		t.Fatal(err)
	}
	got := matchingEpisode(rows, MatchRequest{EpisodeLabel: "第196集", DurationMS: 1248000})
	if got == nil || got.EpisodeID != "102" {
		t.Fatalf("preview badge lost or membership excluded: %+v", got)
	}
}

func TestNewMatchingPolicyReevaluatesDecisionsButPreservesNetworkCooldown(t *testing.T) {
	for _, code := range []string{"NO_EPISODE_MATCH", "EPISODE_SPECIAL_REJECTED", "WORK_YEAR_MISMATCH", "TIMEOUT", "RATE_LIMITED", "EMPTY_POOL"} {
		t.Run(code, func(t *testing.T) {
			store := testStore(t)
			now := time.Now()
			request := matchRequest()
			fingerprint := matchFingerprint(request)
			legacy := strings.TrimPrefix(fingerprint, matchPolicy+":")
			if err := store.saveMatchFailure(context.Background(), request.LocalKey, "bilibili", legacy, code, now); err != nil {
				t.Fatal(err)
			}
			got, err := store.matchFailure(context.Background(), request.LocalKey, "bilibili", fingerprint, now)
			if err != nil {
				t.Fatal(err)
			}
			rejectedByOldPolicy := code == "NO_EPISODE_MATCH" || code == "EPISODE_SPECIAL_REJECTED" || code == "WORK_YEAR_MISMATCH"
			if rejectedByOldPolicy && got != nil {
				t.Fatal("old matching policy still blocked reevaluation", got)
			}
			if !rejectedByOldPolicy && (got == nil || got.Code != code || got.RetryAt != now.Add(24*time.Hour).Unix()) {
				t.Fatal("upstream cooldown changed", got)
			}
			changed := request
			changed.Title = "different work"
			got, err = store.matchFailure(context.Background(), request.LocalKey, "bilibili", matchFingerprint(changed), now)
			if err != nil || got != nil {
				t.Fatal("another work inherited cooldown", got, err)
			}
		})
	}
}
