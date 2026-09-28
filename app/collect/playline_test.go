package collect

import (
	"cczjVideo/app/model"
	"fmt"
	"strings"
	"testing"
)

func TestParsePlayLinesSplitsLinesBeforeEpisodes(t *testing.T) {
	playURL := "第1集$https://a.example/1.m3u8#第2集$https://a.example/2.m3u8$$$第1集$https://b.example/1.m3u8#第2集$https://b.example/2.m3u8"
	lines := ParsePlayLines(playURL, "qq$$$youku", model.FlexibleString("42"))
	if len(lines) != 2 {
		t.Fatalf("lines = %d; want 2", len(lines))
	}
	if got := []string{lines[0].Name, lines[1].Name}; got[0] != "qq" || got[1] != "youku" {
		t.Fatalf("line names = %v", got)
	}
	if got := []int{lines[0].Index, lines[1].Index}; got[0] != 0 || got[1] != 1 {
		t.Fatalf("line indexes = %v", got)
	}
	for i, line := range lines {
		if len(line.Episodes) != 2 {
			t.Fatalf("line %d episodes = %d; want 2", i, len(line.Episodes))
		}
		for j, ep := range line.Episodes {
			if ep.EpNum != j+1 {
				t.Fatalf("line %d ep %d ep_num = %d; want positional", i, j, ep.EpNum)
			}
			if want := fmt.Sprintf("第%d集", j+1); ep.EpName != want {
				t.Fatalf("line %d ep %d name = %q; want %q", i, j, ep.EpName, want)
			}
			if ep.VodId.String() != "42" {
				t.Fatalf("line %d ep %d vod_id = %q", i, j, ep.VodId.String())
			}
			// 旧实现只按 # 切，于是每条线路末集的地址会把下一条线路整段吞进来。
			if strings.Contains(ep.EpUrl, "$$$") || strings.Contains(ep.EpUrl, "#") {
				t.Fatalf("line %d ep %d url leaked another line: %q", i, j, ep.EpUrl)
			}
			if !strings.HasPrefix(ep.EpUrl, "https://") {
				t.Fatalf("line %d ep %d url = %q; want a bare URL", i, j, ep.EpUrl)
			}
		}
	}
	if lines[0].Episodes[1].EpUrl != "https://a.example/2.m3u8" {
		t.Fatalf("first line last episode = %q", lines[0].Episodes[1].EpUrl)
	}
	if lines[1].Episodes[0].EpUrl != "https://b.example/1.m3u8" {
		t.Fatalf("second line first episode = %q", lines[1].Episodes[0].EpUrl)
	}
}

func TestParsePlayLinesSingleLineVariants(t *testing.T) {
	cases := []struct {
		name     string
		playURL  string
		playFrom string
		wantURL  string
		wantName string
		wantEps  int
	}{
		{name: "trailing hash", playURL: "HD$https://a/1.mp4#第2集$https://a/2.mp4#", playFrom: "m3u8", wantName: "m3u8", wantEps: 2},
		{name: "bare url without name", playURL: "https://a/1.m3u8", wantURL: "https://a/1.m3u8", wantEps: 1},
		{name: "dangling line separator", playURL: "正片$https://a/1.mp4$$$", playFrom: "m3u8$$$", wantName: "m3u8", wantEps: 1},
		{name: "empty play url", playURL: "", wantEps: 0},
	}
	for _, tc := range cases {
		lines := ParsePlayLines(tc.playURL, tc.playFrom, model.FlexibleString("7"))
		if tc.wantEps == 0 {
			if len(lines) != 0 {
				t.Fatalf("%s: lines = %d; want none", tc.name, len(lines))
			}
			continue
		}
		if len(lines) != 1 {
			t.Fatalf("%s: lines = %d; want 1", tc.name, len(lines))
		}
		if lines[0].Name != tc.wantName {
			t.Fatalf("%s: line name = %q; want %q", tc.name, lines[0].Name, tc.wantName)
		}
		if len(lines[0].Episodes) != tc.wantEps {
			t.Fatalf("%s: episodes = %d; want %d", tc.name, len(lines[0].Episodes), tc.wantEps)
		}
		if tc.wantURL != "" {
			if got := lines[0].Episodes[0].EpUrl; got != tc.wantURL {
				t.Fatalf("%s: ep url = %q; want %q", tc.name, got, tc.wantURL)
			}
			if got := lines[0].Episodes[0].EpName; got != "" {
				t.Fatalf("%s: ep name = %q; want blank for a bare address", tc.name, got)
			}
		}
	}
}

// 源站常给三条线路但 vod_play_from 只有两条名字，缺名的线路必须留空而不是错位。
func TestParsePlayLinesNameShortageFallsBackToBlank(t *testing.T) {
	lines := ParsePlayLines("A$https://a/1$$$B$https://b/1$$$C$https://c/1", "first$$$second", model.FlexibleString("9"))
	if len(lines) != 3 {
		t.Fatalf("lines = %d; want 3", len(lines))
	}
	if lines[0].Name != "first" || lines[1].Name != "second" || lines[2].Name != "" {
		t.Fatalf("names = %q/%q/%q", lines[0].Name, lines[1].Name, lines[2].Name)
	}
}

// 集名可以带 $（例如价格符号或 token），只有首个 $ 才是名址分隔符。
func TestParsePlayLinesKeepsExtraDollarInURL(t *testing.T) {
	lines := ParsePlayLines("第1集$https://a/1.m3u8?token=$1x2", "m3u8", model.FlexibleString("1"))
	if len(lines) != 1 || len(lines[0].Episodes) != 1 {
		t.Fatalf("lines = %+v", lines)
	}
	if got := lines[0].Episodes[0].EpUrl; got != "https://a/1.m3u8?token=$1x2" {
		t.Fatalf("ep url = %q", got)
	}
}
