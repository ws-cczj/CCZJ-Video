package douban

import (
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"cczjVideo/app/applog"
)

// sec.douban.com/c 不是登录墙，而是一道 proof-of-work 验证题。
//
// 302 过去的页面里带着 tok/cha 两个隐藏字段，浏览器 JS 从 nonce=1 开始递增，
// 反复算 sha512(cha+nonce) 的十六进制，直到前 difficulty 位全是 0，然后把 sol
// 表单 POST 回 /c；服务器校验后发一个 Max-Age=120 的 dbsawcv1 Cookie 放行。
// 整条链路没有任何凭据，纯算力，所以 Go 自己算就能过：2026-09-22 实测
// difficulty=4 时 31018 次迭代 / 36ms 解出，随后详情页 200 且 v:votes 齐全。
const (
	secChallengeHost     = "sec.douban.com"
	defaultPowDifficulty = 4
	// powMaxNonce 是放弃阈值。difficulty=4 的期望迭代数是 16^4=65536，400 万次
	// 已经是一万倍余量；真跑满只可能是豆瓣调高了难度，此时不能让一条请求把
	// 补全线程卡住几分钟，直接放弃改走 subject_abstract 兜底。
	powMaxNonce = 4_000_000
)

var (
	challengeTokRe = regexp.MustCompile(`(?is)<input[^>]*\bname\s*=\s*["']tok["'][^>]*\bvalue\s*=\s*["']([^"']*)["']`)
	challengeChaRe = regexp.MustCompile(`(?is)<input[^>]*\bname\s*=\s*["']cha["'][^>]*\bvalue\s*=\s*["']([^"']*)["']`)
	challengeRedRe = regexp.MustCompile(`(?is)<input[^>]*\bname\s*=\s*["']red["'][^>]*\bvalue\s*=\s*["']([^"']*)["']`)
	// 表单是 <form name="sec" id="sec" method="POST" action="/c">，属性顺序不固定，
	// 所以按 id 定位表单后再单独抠 action。
	challengeActionRe = regexp.MustCompile(`(?is)<form[^>]*\bid\s*=\s*["']sec["'][^>]*>`)
	challengeFormAttr = regexp.MustCompile(`(?is)\baction\s*=\s*["']([^"']*)["']`)
	// 难度写在 JS 的默认参数里（process(data, difficulty = 4)）。跟着页面走，
	// 豆瓣改数值时不用重新发版；要求后面紧跟 , 或 ) 以免匹配到比较表达式。
	challengeDifficultyRe = regexp.MustCompile(`(?s)difficulty\s*=\s*(\d+)\s*[,)]`)
)

func isSecChallengeURL(rawurl string) bool {
	u, err := url.Parse(rawurl)
	return err == nil && strings.EqualFold(u.Host, secChallengeHost)
}

// solveProofOfWork 复刻挑战页的 JS：找 nonce 使 sha512(cha+nonce) 的十六进制
// 以 difficulty 个 0 开头。返回 (nonce, 是否解出)。
func solveProofOfWork(cha string, difficulty int) (int, bool) {
	target := strings.Repeat("0", difficulty)
	for nonce := 1; nonce <= powMaxNonce; nonce++ {
		sum := sha512.Sum512([]byte(cha + strconv.Itoa(nonce)))
		if strings.HasPrefix(hex.EncodeToString(sum[:]), target) {
			return nonce, true
		}
	}
	return 0, false
}

func challengeInputValue(re *regexp.Regexp, page string) string {
	m := re.FindStringSubmatch(page)
	if len(m) >= 2 {
		return m[1]
	}
	return ""
}

// challengeFormAction 取出验证表单的 action。
func challengeFormAction(page string) string {
	form := challengeActionRe.FindString(page)
	if form == "" {
		return ""
	}
	a := challengeFormAttr.FindStringSubmatch(form)
	if len(a) >= 2 {
		return a[1]
	}
	return ""
}

// solveDoubanChallenge 解一道验证题并回传。成功时放行 Cookie 已经落进 client
// 的 cookie jar，调用方重试原地址即可拿到真实页面。
func solveDoubanChallenge(challengeURL, sourceURL string) error {
	req, err := http.NewRequest(http.MethodGet, challengeURL, nil)
	if err != nil {
		return err
	}
	// 浏览器是从被拦的地址导航到验证页的，Referer 就是原地址。
	applyDoubanHeaders(req, sourceURL)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("挑战页 HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if err != nil {
		return err
	}
	page := string(body)

	cha := challengeInputValue(challengeChaRe, page)
	if cha == "" {
		return fmt.Errorf("挑战页没有 cha 字段，可能已改版")
	}
	difficulty := defaultPowDifficulty
	if m := challengeDifficultyRe.FindStringSubmatch(page); len(m) == 2 {
		if n, convErr := strconv.Atoi(m[1]); convErr == nil && n > 0 && n <= 8 {
			difficulty = n
		}
	}

	start := time.Now()
	nonce, ok := solveProofOfWork(cha, difficulty)
	if !ok {
		recordChallengeSolve(difficulty, time.Since(start), false)
		return fmt.Errorf("难度 %d 下 %d 次迭代未解出", difficulty, powMaxNonce)
	}
	recordChallengeSolve(difficulty, time.Since(start), true)
	applog.Info("[Douban] 验证题已解出：难度 %d，nonce=%d，耗时 %s",
		difficulty, nonce, time.Since(start).Round(time.Millisecond))

	form := url.Values{
		"tok": {challengeInputValue(challengeTokRe, page)},
		"cha": {cha},
		"sol": {strconv.Itoa(nonce)},
		"red": {challengeInputValue(challengeRedRe, page)},
	}
	postURL := challengeURL
	if action := challengeFormAction(page); action != "" {
		if strings.HasPrefix(action, "http") {
			postURL = action
		} else if u, parseErr := url.Parse(challengeURL); parseErr == nil {
			postURL = u.Scheme + "://" + u.Host + action
		}
	}

	postReq, err := http.NewRequest(http.MethodPost, postURL, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	applyDoubanHeaders(postReq, challengeURL)
	postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if u, parseErr := url.Parse(challengeURL); parseErr == nil {
		postReq.Header.Set("Origin", u.Scheme+"://"+u.Host)
	}
	// 表单提交是 POST，浏览器此时不再声明“用户导航”。
	postReq.Header.Del("Upgrade-Insecure-Requests")

	postResp, err := client.Do(postReq)
	if err != nil {
		return err
	}
	defer postResp.Body.Close()
	loc := postResp.Header.Get("Location")
	if postResp.StatusCode >= 300 && postResp.StatusCode < 400 && !isSecChallengeURL(loc) {
		return nil
	}
	if postResp.StatusCode >= 300 && postResp.StatusCode < 400 {
		return fmt.Errorf("提交后仍被导向验证页")
	}
	return fmt.Errorf("提交验证 HTTP %d", postResp.StatusCode)
}

// 验证题的最近一次求解情况只留在内存里，供诊断页回答「豆瓣还在考我们吗」。
// 落一次日志就丢一个时间戳，重启后归零，所以这里只承诺「本次运行期间」。
var (
	challengeMu       sync.Mutex
	challengeAt       time.Time
	challengeDiffic   int
	challengeElapsed  time.Duration
	challengeSolveOKB bool
)

func recordChallengeSolve(difficulty int, elapsed time.Duration, ok bool) {
	challengeMu.Lock()
	defer challengeMu.Unlock()
	challengeAt = time.Now()
	challengeDiffic = difficulty
	challengeElapsed = elapsed
	challengeSolveOKB = ok
}

// LastChallenge 返回本次运行内最近一次遇到 sec.douban 验证题的时间、难度、
// 解题耗时与是否解出。从未遇到过时时间为零值。
func LastChallenge() (time.Time, int, time.Duration, bool) {
	challengeMu.Lock()
	defer challengeMu.Unlock()
	return challengeAt, challengeDiffic, challengeElapsed, challengeSolveOKB
}
