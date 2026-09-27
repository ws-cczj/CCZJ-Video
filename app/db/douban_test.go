package db

import "testing"

func TestTitleAliasKeyNormalizesDisplayPunctuation(t *testing.T) {
	if got, want := titleAliasKey("年会不能停2！"), titleAliasKey("年会不能停！2"); got != want {
		t.Fatalf("titleAliasKey() differs for punctuation variants: %q != %q", got, want)
	}
}

func TestTitleAliasKeyKeepsSeasonIdentity(t *testing.T) {
	if titleAliasKey("权力的游戏 第三季") == titleAliasKey("权力的游戏 第八季") {
		t.Fatal("titleAliasKey() collapsed different seasons")
	}
}
