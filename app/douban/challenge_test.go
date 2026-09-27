package douban

import (
	"crypto/sha512"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// liveChallengePage 是 2026-09-22 从 sec.douban.com/c 原样截下来的表单块。
// tok/cha 是一次性的匿名验证题（放行 Cookie 只有 120 秒，且不含任何账号信息），
// 留作固定输入可以稳定复现 nonce，不需要联网也能验证算法实现是否和 JS 一致。
const liveChallengePage = `
            <form name="sec" id="sec" method="POST" action="/c">
              <input type="hidden" id="tok" name="tok" value="1790062377@e7b7c968562c924bb6d499d4b7c55d3c92200de3c5e730ca8c77b064233766c0ff6a2136bf246d81b0af79c0b0f0a1822bdf55337e796c3cd908486bad583c7d@aHR0cHM6Ly9tb3ZpZS5kb3ViYW4uY29tL3N1YmplY3QvMjY1ODQxODMv@3451be77232e6920677e1d0b530c3cb271f0e698d8dba63ca58909c71b56e921" />
              <input type="hidden" id="cha" name="cha" value="4d23f1e7b76c5ef5831a44313ff11b07ccddcf20b5c249af54d3a829aa8c14c88ebbd986b8e8531cf5619494c4a374124e1cb2644bd0613df8e0dc674cdc504f" />
              <input type="hidden" id="sol" name="sol" value="" />
              <input type="hidden" id="red" name="red" value="https://movie.douban.com/subject/26584183/">
            </form>
async function process(data, difficulty = 4) {
    let nonce = 0;
    const targetSubStr = Array(difficulty+1).join('0');
    do {
        nonce += 1;
        hash = await sha512(data + nonce);
    } while(hash.substr(0, difficulty) !== targetSubStr);
    return nonce;
}
`

func powHash(cha string, nonce int) string {
	sum := sha512.Sum512([]byte(cha + strconv.Itoa(nonce)))
	return hex.EncodeToString(sum[:])
}

func TestParseChallengePageFields(t *testing.T) {
	wantTokPrefix := "1790062377@e7b7c968"
	cha := challengeInputValue(challengeChaRe, liveChallengePage)
	if cha == "" {
		t.Fatal("cha must parse from the live challenge page")
	}
	if tok := challengeInputValue(challengeTokRe, liveChallengePage); !strings.HasPrefix(tok, wantTokPrefix) {
		t.Errorf("tok = %q, want prefix %q", tok, wantTokPrefix)
	}
	if got := challengeInputValue(challengeRedRe, liveChallengePage); got != "https://movie.douban.com/subject/26584183/" {
		t.Errorf("red = %q", got)
	}
	if got := challengeFormAction(liveChallengePage); got != "/c" {
		t.Errorf("action = %q, want /c", got)
	}
	m := challengeDifficultyRe.FindStringSubmatch(liveChallengePage)
	if len(m) != 2 || m[1] != "4" {
		t.Errorf("difficulty = %q, want 4", m)
	}
}

// 解出来的 nonce 必须同时满足“前 difficulty 位为 0”和“上一个 nonce 不满足”，
// 这才说明实现和页面 JS 的 do-while 是同一个人。
func TestSolveProofOfWorkMatchesChallengePageJS(t *testing.T) {
	cha := challengeInputValue(challengeChaRe, liveChallengePage)
	nonce, ok := solveProofOfWork(cha, defaultPowDifficulty)
	if !ok {
		t.Fatal("proof of work must solve at difficulty 4")
	}
	if got := powHash(cha, nonce); !strings.HasPrefix(got, "0000") {
		t.Fatalf("hash for nonce %d = %s, want 0000 prefix", nonce, got)
	}
	if got := powHash(cha, nonce-1); strings.HasPrefix(got, "0000") {
		t.Fatalf("nonce %d is not minimal: %s already matches", nonce-1, got)
	}
}

func TestSolveProofOfWorkGivesUpInsteadOfHanging(t *testing.T) {
	// 难度 8 的期望迭代数是 16^8，远超 powMaxNonce，必须放弃而不是把补全线程卡死。
	if _, ok := solveProofOfWork("deadbeef", 8); ok {
		t.Fatal("difficulty 8 must report unsolved rather than spin forever")
	}
}

func TestChallengeParsersTolerateMissingFields(t *testing.T) {
	if got := challengeInputValue(challengeChaRe, "<html>改版了</html>"); got != "" {
		t.Errorf("cha = %q, want empty", got)
	}
	if got := challengeFormAction("<html>改版了</html>"); got != "" {
		t.Errorf("action = %q, want empty", got)
	}
}

func TestIsSecChallengeURL(t *testing.T) {
	cases := map[string]bool{
		"https://sec.douban.com/c?r=https%3A%2F%2Fmovie.douban.com%2Fsubject%2F26584183%2F&a=1": true,
		"https://movie.douban.com/subject/26584183/":                                            false,
		"https://accounts.douban.com/passport/login":                                            false,
		"": false,
	}
	for raw, want := range cases {
		if got := isSecChallengeURL(raw); got != want {
			t.Errorf("isSecChallengeURL(%q) = %v, want %v", raw, got, want)
		}
	}
}

// TestSolveDoubanChallengeAgainstLiveDouban 走真实链路：撞验证页 → 解题 → 重试。
// 需要联网，默认跳过。
func TestSolveDoubanChallengeAgainstLiveDouban(t *testing.T) {
	if os.Getenv("CCZJ_DOUBAN_LIVE") == "" {
		t.Skip("set CCZJ_DOUBAN_LIVE=1 to verify against douban")
	}
	const subject = "https://movie.douban.com/subject/26584183/"

	req, err := http.NewRequest(http.MethodGet, subject, nil)
	if err != nil {
		t.Fatal(err)
	}
	applyDoubanHeaders(req, referer)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Log("detail page already unchallenged")
		return
	}
	location := resp.Header.Get("Location")
	if !isSecChallengeURL(location) {
		t.Skipf("HTTP %d -> %s is not a solvable PoW challenge", resp.StatusCode, location)
	}
	if err := solveDoubanChallenge(location, subject); err != nil {
		t.Fatalf("solve: %v", err)
	}

	retry, err := http.NewRequest(http.MethodGet, subject, nil)
	if err != nil {
		t.Fatal(err)
	}
	applyDoubanHeaders(retry, location)
	resp2, err := client.Do(retry)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("retry after solving = HTTP %d", resp2.StatusCode)
	}
	doc, err := io.ReadAll(io.LimitReader(resp2.Body, 4<<20))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	body := string(doc)
	if !strings.Contains(body, `property="v:average"`) {
		t.Fatalf("retry returned %d bytes without v:average", len(doc))
	}
	if votes := liveVotesRe.FindStringSubmatch(body); len(votes) < 2 || votes[1] == "" {
		t.Fatal("votes missing after bypass")
	} else {
		t.Logf("bypass OK, votes=%s, %d bytes", votes[1], len(doc))
	}
}

var liveVotesRe = regexp.MustCompile(`(?s)<span[^>]*property="v:votes"[^>]*>([0-9]+)</span>`)
