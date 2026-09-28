package handler

import (
	"cczjVideo/app/model"
	"testing"
)

func line(index int, name string, urls ...string) *model.PlayLine {
	episodes := make([]*model.Episode, 0, len(urls))
	for i, raw := range urls {
		episodes = append(episodes, &model.Episode{EpNum: i + 1, EpName: raw, EpUrl: raw})
	}
	return &model.PlayLine{Index: index, Name: name, Episodes: episodes}
}

func TestUsableLinesDropsLinesWithoutEpisodes(t *testing.T) {
	got := usableLines([]*model.PlayLine{
		line(0, "qq"),
		nil,
		line(1, "youku", "https://a/1.m3u8"),
		{Index: 2, Name: "empty"},
	})
	if len(got) != 1 || got[0].Name != "youku" {
		t.Fatalf("usable lines = %+v; want only the youku line", got)
	}
}

func TestPickProbeEpisodePrefersCurrentEpisode(t *testing.T) {
	playLine := line(0, "qq", "", "https://a/2.m3u8", "https://a/3.m3u8")
	if ep := pickProbeEpisode(playLine, 3); ep == nil || ep.EpNum != 3 {
		t.Fatalf("current episode = %+v; want episode 3", ep)
	}
	// 该线路没有用户正在看的第 9 集时回首集，而不是返回 nil 让整条线路消失。
	if ep := pickProbeEpisode(playLine, 9); ep == nil || ep.EpNum != 2 {
		t.Fatalf("fallback episode = %+v; want the first non-empty url (episode 2)", ep)
	}
	if ep := pickProbeEpisode(line(1, "dead", ""), 0); ep != nil {
		t.Fatalf("episode = %+v; want nil for a line with no playable url", ep)
	}
}

func TestRankLineSpeedOrdersUsableFastestFirst(t *testing.T) {
	resp := rankLineSpeed([]*PlayLineSpeedItem{
		{Index: 0, Name: "slow", OK: true, BytesPerSec: 1_000, LatencyMS: 40},
		{Index: 1, Name: "dead", OK: false, LatencyMS: 8_000},
		{Index: 2, Name: "fast", OK: true, BytesPerSec: 9_000, LatencyMS: 10},
		{Index: 3, Name: "unmeasured", OK: true, BytesPerSec: 0},
	})
	var order []string
	for _, item := range resp.Items {
		order = append(order, item.Name)
	}
	want := []string{"unmeasured", "fast", "slow", "dead"}
	if len(order) != len(want) {
		t.Fatalf("order = %v; want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order = %v; want %v", order, want)
		}
	}
	if resp.BestIndex != 3 {
		t.Fatalf("best index = %d; want the unmeasured line, which finished faster than the clock could resolve", resp.BestIndex)
	}
}

func TestRankLineSpeedMarksNoBestWhenAllLinesDead(t *testing.T) {
	resp := rankLineSpeed([]*PlayLineSpeedItem{
		{Index: 0, Name: "a", OK: false},
		{Index: 1, Name: "b", OK: false},
	})
	if resp.BestIndex != -1 {
		t.Fatalf("best index = %d; want -1 when nothing is playable", resp.BestIndex)
	}
	if resp.Items[0].Name != "a" {
		t.Fatalf("order = %v; want dead lines kept in declaration order", resp.Items)
	}
}
