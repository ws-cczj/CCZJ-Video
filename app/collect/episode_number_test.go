package collect

import (
	"testing"

	"cczjVideo/app/model"
)

func TestEpisodeNumberFromNames(t *testing.T) {
	cases := []struct {
		name string
		want int
	}{
		{"第01集", 1},
		{"第 12 集", 12},
		{"第十一集", 11},
		{"第108集", 108},
		{"第24集 国语", 24},
		{"05集", 5},
		{"EP07", 7},
		{"E7", 7},
		{"S01E05", 5},
		{"s2_ep12", 12},
		{"第０３集", 3},
		{"03", 3},
		{"第01集序章", 1},
		// 画质/版本标记里全是数字，不能被读成集号。
		{"HD中字", 0},
		{"1080P", 0},
		{"BD国语1080P", 0},
		{"正片", 0},
		{"全集", 0},
		{"TC中字", 0},
		{"HD粤语1280P", 0},
		{"PRE001", 0},
		{"", 0},
	}
	for _, tc := range cases {
		if got := episodeNumber(tc.name); got != tc.want {
			t.Errorf("episodeNumber(%q) = %d, want %d", tc.name, got, tc.want)
		}
	}
}

func TestParseLineEpisodesUsesNamedNumbers(t *testing.T) {
	// 新集排在最前是常见排法：按位置编号会把「第24集」记成第 1 集。
	block := "第24集$https://a/24.m3u8#第23集$https://a/23.m3u8#第22集$https://a/22.m3u8"
	eps := parseLineEpisodes(block, "9")
	if len(eps) != 3 {
		t.Fatalf("episodes = %d, want 3", len(eps))
	}
	for i, want := range []int{24, 23, 22} {
		if eps[i].EpNum != want {
			t.Errorf("eps[%d].EpNum = %d, want %d", i, eps[i].EpNum, want)
		}
	}
}

func TestParseLineEpisodesFallsBackOnDuplicates(t *testing.T) {
	block := "第1集$https://a/1.m3u8#第1集 番外$https://a/2.m3u8#HD中字$https://a/3.m3u8"
	eps := parseLineEpisodes(block, "9")
	if len(eps) != 3 {
		t.Fatalf("episodes = %d, want 3", len(eps))
	}
	seen := map[int]string{}
	for _, ep := range eps {
		if prev, dup := seen[ep.EpNum]; dup {
			t.Fatalf("duplicate ep_num %d for %q and %q", ep.EpNum, prev, ep.EpName)
		}
		seen[ep.EpNum] = ep.EpName
	}
	if eps[0].EpNum != 1 || eps[1].EpNum != 2 || eps[2].EpNum != 3 {
		t.Errorf("ep_nums = %d/%d/%d, want 1/2/3", eps[0].EpNum, eps[1].EpNum, eps[2].EpNum)
	}
}

func TestParsePlayLinesKeepsPerLineNumbering(t *testing.T) {
	url := "第2集$https://a/2.m3u8#第1集$https://a/1.m3u8$$$HD$https://b/1.m3u8"
	lines := ParsePlayLines(url, "x$$$y", model.FlexibleString("3"))
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want 2", len(lines))
	}
	if lines[0].Episodes[0].EpNum != 2 || lines[0].Episodes[1].EpNum != 1 {
		t.Errorf("line0 ep nums = %d/%d, want 2/1", lines[0].Episodes[0].EpNum, lines[0].Episodes[1].EpNum)
	}
	if lines[1].Episodes[0].EpNum != 1 {
		t.Errorf("line1 ep num = %d, want 1", lines[1].Episodes[0].EpNum)
	}
}
