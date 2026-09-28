package douban

import (
	"encoding/json"
	"strconv"
	"strings"
)

// 豆瓣搜索页把结果从 HTML 节点改成了内嵌 JSON：页面上已经没有 item-root /
// title-text，候选全部住在 window.__DATA__ 的 items 数组里。HTML 正则路径因此
// 整体拿到 0 条候选，只能退回「整页只指向一个 subject」的兜底，多结果时一律失败
// 并累计 24 小时冷却——豆瓣搜索等于下线。
//
// JSON 反而比剥 HTML 可靠：字段名稳定，不用猜 class 组合，也不用处理属性转义。
// 所以这里优先走 JSON，HTML 两条路径留着应付旧页面和抓取失败的片段。

// searchPageDataAnchor 是内嵌数据的赋值起点。这一行给的就是一个 JSON 对象，
// 后面跟着 ';' 和 </script>，交给 json.Decoder 读第一个值即可，不需要自己配平括号。
const searchPageDataAnchor = "window.__DATA__"

// searchPageItem 只声明用得到的字段。豆瓣还会回 rating / cover_url / topics /
// interest 等，都不属于 SearchCandidate，列出来只是给读者添乱。
type searchPageItem struct {
	Title     string `json:"title"`
	URL       string `json:"url"`
	Abstract  string `json:"abstract"`
	Abstract2 string `json:"abstract_2"`
	MoreURL   string `json:"more_url"`
	Labels    []struct {
		Text string `json:"text"`
	} `json:"labels"`
}

// searchPageSubjectID 优先信 url：那是给人看的链接，格式多年未变。
// id 字段是 JSON number，反序列化成字符串会带上小数点，所以不取，
// 改从 more_url 的 JS 参数里拿同一个 ID 兜底。
func (i searchPageItem) subjectID() string {
	if id := extractSubjectID(i.URL); id != "" {
		return id
	}
	return extractSubjectID(i.MoreURL)
}

func (i searchPageItem) series() bool {
	for _, label := range i.Labels {
		if label.Text == "剧集" {
			return true
		}
	}
	return strings.Contains(i.MoreURL, `is_tv:'1'`) || strings.Contains(i.MoreURL, `is_tv:"1"`)
}

// searchPagePayload 是 window.__DATA__ 里我们用得到的两部分。
// ErrorInfo 是豆瓣的回话字段：限流时它写「搜索访问太频繁。」，同时回一个
// 结构完整的正常页面和 HTTP 200，光看 HTML 和状态码都发现不了（非 ASCII 全部
// \u 转义，连 antiCrawlPatterns 的中文串都匹配不上）。
type searchPagePayload struct {
	ErrorInfo string           `json:"error_info"`
	Items     []searchPageItem `json:"items"`
}

// decodeSearchPage 读出页面内嵌的 __DATA__。第二个返回值为假表示这页没有可读的
// 内嵌数据（老页面、被截断的响应、或改版后换了挂载点），调用方该退回 HTML 路径。
func decodeSearchPage(page string) (*searchPagePayload, bool) {
	start := strings.Index(page, searchPageDataAnchor)
	if start < 0 {
		return nil, false
	}
	brace := strings.IndexByte(page[start:], '{')
	if brace < 0 {
		return nil, false
	}
	var payload searchPagePayload
	// 解析失败多半是被截断的响应或又换了一种结构；交给 HTML 路径和上层的退避处理，
	// 这里不值得为它区分错误类型。
	if err := json.NewDecoder(strings.NewReader(page[start+brace:])).Decode(&payload); err != nil {
		return nil, false
	}
	return &payload, true
}

func parseSearchCandidatesFromJSON(page string) []SearchCandidate {
	payload, ok := decodeSearchPage(page)
	if !ok || len(payload.Items) == 0 {
		return nil
	}

	var candidates []SearchCandidate
	seen := make(map[string]bool, len(payload.Items))
	for _, item := range payload.Items {
		subjectID := item.subjectID()
		if subjectID == "" || seen[subjectID] {
			continue
		}
		seen[subjectID] = true

		year := 0
		if ym := searchYearRegex.FindStringSubmatch(item.Title); len(ym) >= 2 {
			year, _ = strconv.Atoi(ym[1])
		}
		director, actor := parseDirectorActor(item.Abstract2)
		candidates = append(candidates, SearchCandidate{
			SubjectID: subjectID,
			Title:     cleanTitle(item.Title),
			Year:      year,
			IsSeries:  item.series(),
			Meta:      item.Abstract,
			Director:  director,
			Actor:     actor,
		})
	}
	return candidates
}
