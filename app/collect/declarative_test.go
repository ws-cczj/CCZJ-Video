package collect

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"cczjVideo/app/model"
)

// stubTransport answers every request with a canned body and records the exact
// URLs the collector asked for, so declarative sources are exercised end to end
// without a network.
type stubTransport struct {
	requested []string
	respond   func(target string) (int, string)
}

func (s *stubTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	target := req.URL.String()
	s.requested = append(s.requested, target)
	status, body := s.respond(target)
	if status == 0 {
		status = http.StatusOK
	}
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}, nil
}

func withStubResponder(t *testing.T, respond func(target string) (int, string)) *stubTransport {
	t.Helper()
	stub := &stubTransport{respond: respond}
	original := client
	client = &http.Client{Transport: stub}
	t.Cleanup(func() { client = original })
	return stub
}

func withStubBody(t *testing.T, body string) *stubTransport {
	t.Helper()
	return withStubResponder(t, func(string) (int, string) { return http.StatusOK, body })
}

func strategyFromJSON(t *testing.T, api, config string) SourceStrategy {
	t.Helper()
	strategy := CreateStrategyFromSource(&model.Source{SourceKey: "demo", ApiUrl: api, StrategyConfig: config})
	if strategy == nil {
		t.Fatal("CreateStrategyFromSource returned nil")
	}
	return strategy
}

// declarativeConfig is the shape a kind:source pack writes into strategy_config:
// non-MAC parameter names, a non-MAC action key, a nested envelope and source
// field names that are not vod_*.
const declarativeConfig = `{
  "version": 2,
  "strategy": "declarative",
  "list":   {"action": "list", "action_param": "m", "page_param": "pn", "limit_param": "ps", "type_param": "cate", "hours_param": "since", "extra": {"app": "video"}},
  "search": {"action": "search", "action_param": "m", "keyword_param": "q", "page_param": "pn", "extra": {"app": "video"}},
  "detail": {"action": "detail", "action_param": "m", "id_param": "vid", "extra": {"app": "video"}},
  "response": {"list_path": "data.list", "code_path": "code", "ok_codes": ["0"], "msg_path": "data.message", "page_path": "data.pn", "pagecount_path": "data.pages", "total_path": "data.total"},
  "field_mapping": {"vid": "vod_id", "name": "vod_name", "pic": "vod_pic", "cate": "type_name", "play": "vod_play_url"}
}`

const declarativeBody = `{"code":"0","data":{"message":"ok","pn":1,"pages":4,"total":118,"list":[` +
	`{"vid":"a1","name":"影片甲","pic":"https://c/1.jpg","cate":"电影","play":"第1集$https://a/1.m3u8"},` +
	`{"vid":"a2","name":"影片乙","pic":"https://c/2.jpg","cate":"电视剧"}]}}`

func TestDeclarativeBuildsExactQueryStrings(t *testing.T) {
	const api = "https://example.test/v2/videos"
	cases := []struct {
		name    string
		build   func(strategy SourceStrategy) string
		wantURL string
	}{
		{
			name:    "list with limit and type",
			build:   func(s SourceStrategy) string { return s.BuildListUrl(2, FetchOptions{Limit: 30, TypeID: "5"}) },
			wantURL: api + "?app=video&cate=5&m=list&pn=2&ps=30",
		},
		{
			name:    "list bare",
			build:   func(s SourceStrategy) string { return s.BuildListUrl(1, FetchOptions{}) },
			wantURL: api + "?app=video&m=list&pn=1",
		},
		{
			name:    "list with hours param renamed",
			build:   func(s SourceStrategy) string { return s.BuildListUrl(1, FetchOptions{Hours: 6}) },
			wantURL: api + "?app=video&m=list&pn=1&since=6",
		},
		{
			name:    "search uses keyword param and action",
			build:   func(s SourceStrategy) string { return s.BuildSearchUrl("大臣", 2) },
			wantURL: api + "?app=video&m=search&pn=2&q=%E5%A4%A7%E8%87%A3",
		},
		{
			name:    "detail uses id param",
			build:   func(s SourceStrategy) string { return s.BuildDetailUrl("x1") },
			wantURL: api + "?app=video&m=detail&vid=x1",
		},
	}
	strategy := strategyFromJSON(t, api, declarativeConfig)
	if got := strategy.GetStrategyName(); got != "declarative" {
		t.Fatalf("strategy name = %q", got)
	}
	for _, tc := range cases {
		if got := tc.build(strategy); got != tc.wantURL {
			t.Errorf("%s:\n got %s\nwant %s", tc.name, got, tc.wantURL)
		}
	}
}

func TestDeclarativeActionParamAndExtraVariants(t *testing.T) {
	cases := []struct {
		name    string
		api     string
		config  string
		build   func(strategy SourceStrategy) string
		wantURL string
	}{
		{
			// A declarative source that declares no action gets no action
			// parameter at all, which is the only way to switch it off.
			name:    "unset action emits nothing",
			api:     "https://example.test/v2/videos",
			config:  `{"version":2,"strategy":"declarative","list":{"page_param":"pn","extra":{"fmt":"json"}},"search":{"keyword_param":"q"},"detail":{"id_param":"vid"}}`,
			build:   func(s SourceStrategy) string { return s.BuildListUrl(1, FetchOptions{}) },
			wantURL: "https://example.test/v2/videos?fmt=json&pn=1",
		},
		{
			// action_param unset keeps the MAC-CMS ac key.
			name:    "action param defaults to ac",
			api:     "https://example.test/api.php/provide/vod",
			config:  `{"version":2,"strategy":"declarative","list":{"action":"videolist"},"detail":{"action":"detail","id_param":"ids"}}`,
			build:   func(s SourceStrategy) string { return s.BuildDetailUrl("7") },
			wantURL: "https://example.test/api.php/provide/vod?ac=detail&ids=7",
		},
		{
			// The base URL's own ac= is only scrubbed when ac is this
			// operation's action key, so a renamed action leaves it alone.
			name:    "renamed action keeps base ac",
			api:     "https://example.test/v2?ac=legacy&token=abc",
			config:  `{"version":2,"strategy":"declarative","list":{"action":"list","action_param":"m"}}`,
			build:   func(s SourceStrategy) string { return s.BuildListUrl(1, FetchOptions{}) },
			wantURL: "https://example.test/v2?ac=legacy&m=list&pg=1&token=abc",
		},
		{
			name:    "page param replaces the base value",
			api:     "https://example.test/v2?page=1",
			config:  `{"version":2,"strategy":"declarative","list":{"action":"list","action_param":"act","page_param":"page"}}`,
			build:   func(s SourceStrategy) string { return s.BuildListUrl(3, FetchOptions{}) },
			wantURL: "https://example.test/v2?act=list&page=3",
		},
		{
			// values are set after extra, so a declared page number always wins
			// over a stray extra of the same name.
			name:    "extra cannot override the page counter",
			api:     "https://example.test/v2",
			config:  `{"version":2,"strategy":"declarative","list":{"page_param":"pn","extra":{"pn":"999","app":"video"}}}`,
			build:   func(s SourceStrategy) string { return s.BuildListUrl(4, FetchOptions{}) },
			wantURL: "https://example.test/v2?app=video&pn=4",
		},
	}
	for _, tc := range cases {
		strategy := strategyFromJSON(t, tc.api, tc.config)
		extraCase := tc.config
		if got := tc.build(strategy); got != tc.wantURL {
			t.Errorf("%s (%s):\n got %s\nwant %s", tc.name, extraCase, got, tc.wantURL)
		}
	}
}

// TestStrategyURLRegressionGuard is the byte-for-byte guard: every expected query
// string below is written out literally, because the acceptance criterion is that
// standard_cms and cms_videolist keep producing exactly what they produced before
// the declarative driver existed.
func TestStrategyURLRegressionGuard(t *testing.T) {
	const api = "https://example.test/api.php/provide/vod"
	cases := []struct {
		strategy     string
		config       string
		wantList     string
		wantListBare string
		wantSearch   string
		wantDetail   string
	}{
		{
			strategy:     "standard_cms",
			config:       `{"version":2,"strategy":"standard_cms"}`,
			wantList:     api + "?ac=detail&h=24&limit=20&pg=3&t=1",
			wantListBare: api + "?ac=detail&pg=1",
			wantSearch:   api + "?ac=detail&pg=2&wd=%E5%A4%A7%E8%87%A3",
			wantDetail:   api + "?ac=detail&ids=42",
		},
		{
			strategy:     "cms_videolist",
			config:       `{"version":2,"strategy":"cms_videolist"}`,
			wantList:     api + "?ac=videolist&h=24&limit=20&pg=3&t=1",
			wantListBare: api + "?ac=videolist&pg=1",
			wantSearch:   api + "?ac=videolist&pg=2&wd=%E5%A4%A7%E8%87%A3",
			wantDetail:   api + "?ac=videolist&ids=42",
		},
		{
			strategy:     "empty config falls back to standard_cms",
			config:       ``,
			wantList:     api + "?ac=detail&h=24&limit=20&pg=3&t=1",
			wantListBare: api + "?ac=detail&pg=1",
			wantSearch:   api + "?ac=detail&pg=2&wd=%E5%A4%A7%E8%87%A3",
			wantDetail:   api + "?ac=detail&ids=42",
		},
	}
	for _, tc := range cases {
		strategy := strategyFromJSON(t, api, tc.config)
		if got := strategy.BuildListUrl(3, FetchOptions{Limit: 20, Hours: 24, TypeID: "1"}); got != tc.wantList {
			t.Errorf("%s list:\n got %s\nwant %s", tc.strategy, got, tc.wantList)
		}
		if got := strategy.BuildListUrl(1, FetchOptions{}); got != tc.wantListBare {
			t.Errorf("%s bare list:\n got %s\nwant %s", tc.strategy, got, tc.wantListBare)
		}
		if got := strategy.BuildSearchUrl("大臣", 2); got != tc.wantSearch {
			t.Errorf("%s search:\n got %s\nwant %s", tc.strategy, got, tc.wantSearch)
		}
		if got := strategy.BuildDetailUrl("42"); got != tc.wantDetail {
			t.Errorf("%s detail:\n got %s\nwant %s", tc.strategy, got, tc.wantDetail)
		}
	}
}

// TestLegacyParameterScrubbingGuard pins the pre-existing behaviour of dropping
// stale base-URL parameters for the MAC-CMS strategies.
func TestLegacyParameterScrubbingGuard(t *testing.T) {
	api := "https://example.test/api?keep=%E4%B8%AD%E6%96%87&ac=stale&pg=9&ids=stale&wd=stale&h=99&t=stale&limit=stale"
	strategy := strategyFromJSON(t, api, `{"version":2,"strategy":"standard_cms"}`)
	want := "https://example.test/api?ac=detail&keep=%E4%B8%AD%E6%96%87&pg=1"
	if got := strategy.BuildListUrl(1, FetchOptions{}); got != want {
		t.Fatalf("list scrub:\n got %s\nwant %s", got, want)
	}
}

const cmsEnvelope = `{"code":1,"page":2,"pagecount":7,"limit":20,"total":340,"msg":"ok","list":[` +
	`{"vod_id":"9","vod_name":"某片","type_id":"1","type_name":"电影","vod_pic":"https://p/1.jpg","vod_play_url":"第1集$https://a/1.m3u8"}]}`

// TestCMSEnvelopeRegressionGuard asserts the decoded envelope of the two built-in
// strategies is exactly what it was before the response block existed, including
// the error text of a failing page.
func TestCMSEnvelopeRegressionGuard(t *testing.T) {
	const api = "https://example.test/api.php/provide/vod"
	for _, config := range []string{`{"version":2,"strategy":"standard_cms"}`, `{"version":2,"strategy":"cms_videolist"}`} {
		strategy := strategyFromJSON(t, api, config)
		if _, ok := strategy.(ResponseShaper); !ok {
			t.Fatalf("configured strategy must advertise ResponseShaper")
		}
		if shape, present := strategy.(ResponseShaper).ResponseShape(); present || shape != nil {
			t.Fatalf("a config without a response block must decode the legacy envelope: %+v", shape)
		}
		stub := withStubBody(t, cmsEnvelope)

		result, err := FetchWithStrategy(context.Background(), strategy, strategy.BuildListUrl(3, FetchOptions{Limit: 20}))
		if err != nil {
			t.Fatalf("%s: fetch: %v", config, err)
		}
		wantURL := api + "?ac=detail&limit=20&pg=3"
		if config == `{"version":2,"strategy":"cms_videolist"}` {
			wantURL = api + "?ac=videolist&limit=20&pg=3"
		}
		if len(stub.requested) != 1 || stub.requested[0] != wantURL {
			t.Fatalf("%s requested = %v, want [%s]", config, stub.requested, wantURL)
		}
		if result.Code != 1 || result.CodeText != "" || !result.OK {
			t.Fatalf("%s code fields = %+v", config, result)
		}
		if result.Page.Int() != 2 || result.Pagecount.Int() != 7 || result.Limit.Int() != 20 || result.Total.Int() != 340 {
			t.Fatalf("%s paging = %+v", config, result)
		}
		if result.Msg != "ok" {
			t.Fatalf("%s msg = %q", config, result.Msg)
		}
		if len(result.List) != 1 {
			t.Fatalf("%s list = %d records", config, len(result.List))
		}
		video := result.List[0]
		if video.VodId.String() != "9" || video.VodName != "某片" || video.TypeId.String() != "1" ||
			video.TypeName != "电影" || video.VodPic != "https://p/1.jpg" || video.VodPlayUrl != "第1集$https://a/1.m3u8" {
			t.Fatalf("%s video = %+v", config, video)
		}

		// A failing MAC page must keep its historical error text verbatim.
		failure := `{"code":0,"page":0,"pagecount":0,"total":0,"msg":"数据为空","list":[]}`
		withStubBody(t, failure)
		_, err = FetchWithStrategy(context.Background(), strategy, strategy.BuildListUrl(1, FetchOptions{}))
		if err == nil || !strings.Contains(err.Error(), "api error: code=0 msg=数据为空") {
			t.Fatalf("%s failure error = %v", config, err)
		}

		// A body without any code field used to fail with code=0 and must still
		// fail that way: the legacy envelope treats a missing code as an error.
		withStubBody(t, `{"list":[{"vod_id":"1","vod_name":"无码"}]}`)
		_, err = FetchWithStrategy(context.Background(), strategy, strategy.BuildListUrl(1, FetchOptions{}))
		if err == nil || !strings.Contains(err.Error(), "api error: code=0 msg=") {
			t.Fatalf("%s codeless error = %v", config, err)
		}

		// Non-JSON bodies keep the pre-check message.
		withStubBody(t, `<html>blocked</html>`)
		_, err = FetchWithStrategy(context.Background(), strategy, strategy.BuildListUrl(1, FetchOptions{}))
		if err == nil || !strings.Contains(err.Error(), "源站返回了非JSON响应") {
			t.Fatalf("%s non-JSON error = %v", config, err)
		}
	}
}

func TestDeclarativeCollectsNestedEnvelope(t *testing.T) {
	strategy := strategyFromJSON(t, "https://example.test/v2/videos", declarativeConfig)
	shaper, ok := strategy.(ResponseShaper)
	if !ok {
		t.Fatal("declarative strategy must implement ResponseShaper")
	}
	shape, present := shaper.ResponseShape()
	if !present || shape == nil || shape.ListPath != "data.list" || shape.CodePath != "code" {
		t.Fatalf("shape = %+v present=%v", shape, present)
	}
	if len(shape.OKCodes) != 1 || shape.OKCodes[0] != "0" {
		t.Fatalf("ok_codes = %v", shape.OKCodes)
	}

	withStubBody(t, declarativeBody)
	result, err := FetchWithStrategy(context.Background(), strategy, strategy.BuildListUrl(1, FetchOptions{Limit: 30}))
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if !result.OK || result.CodeText != "0" || result.Msg != "ok" {
		t.Fatalf("envelope = %+v", result)
	}
	if result.Page.Int() != 1 || result.Pagecount.Int() != 4 || result.Total.Int() != 118 {
		t.Fatalf("paging = %+v", result)
	}
	if len(result.List) != 2 {
		t.Fatalf("list = %d records, want 2", len(result.List))
	}
	first := result.List[0]
	if first.VodId.String() != "a1" || first.VodName != "影片甲" || first.VodPic != "https://c/1.jpg" ||
		first.TypeName != "电影" || first.VodPlayUrl != "第1集$https://a/1.m3u8" {
		t.Fatalf("first record = %+v", first)
	}
	if result.List[1].VodName != "影片乙" || result.List[1].VodPic != "https://c/2.jpg" {
		t.Fatalf("second record = %+v", result.List[1])
	}

	// Search goes through the same declared envelope, so a source whose results
	// live under data.list is searchable without any Go change.
	searchStub := withStubBody(t, declarativeBody)
	found, err := FetchSearchPage(strategy, "大臣", 2)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(found.List) != 2 {
		t.Fatalf("search list = %d", len(found.List))
	}
	wantSearch := "https://example.test/v2/videos?app=video&m=search&pn=2&q=%E5%A4%A7%E8%87%A3"
	if len(searchStub.requested) != 1 || searchStub.requested[0] != wantSearch {
		t.Fatalf("search requested = %v", searchStub.requested)
	}

	// Detail decodes from the declared list path too.
	withStubBody(t, `{"code":"0","data":{"message":"ok","list":[{"vid":"a1","name":"影片甲详情","pic":"https://c/1.jpg"}]}}`)
	detail, err := FetchVideoDetailWithStrategy(context.Background(), strategy, strategy.BuildDetailUrl("a1"))
	if err != nil {
		t.Fatalf("detail: %v", err)
	}
	if detail.VodName != "影片甲详情" || detail.VodId.String() != "a1" {
		t.Fatalf("detail = %+v", detail)
	}
}

func TestDeclarativeFailingEnvelopeReportsConfiguredMsg(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		wantErr string
	}{
		{"code outside ok_codes", `{"code":"5000","data":{"message":"签名已失效","list":[]}}`, "api error: code=5000 msg=签名已失效"},
		{"failure page carries no list at all", `{"code":"5000","data":{"message":"权限不足"}}`, "api error: code=5000 msg=权限不足"},
		{"numeric code outside ok_codes", `{"code":1,"data":{"message":"应当失败","list":[]}}`, "api error: code=1 msg=应当失败"},
	}
	for _, tc := range cases {
		strategy := strategyFromJSON(t, "https://example.test/v2/videos", declarativeConfig)
		withStubBody(t, tc.body)
		_, err := FetchWithStrategy(context.Background(), strategy, strategy.BuildListUrl(1, FetchOptions{}))
		if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
			t.Errorf("%s: err = %v, want %q", tc.name, err, tc.wantErr)
		}
	}
}

// TestDeclarativeWithoutPagecountTerminates covers the safety rail that a missing
// paging field cannot produce an endless page loop: the engine pages only to
// Pagecount, so 0 means "the first page was the only page".
func TestDeclarativeWithoutPagecountTerminates(t *testing.T) {
	strategy := strategyFromJSON(t, "https://example.test/v2/videos", declarativeConfig)
	stub := withStubBody(t, `{"code":"0","data":{"message":"ok","list":[{"vid":"a1","name":"只有首页"}]}}`)

	opts := FetchOptions{FieldMapping: strategy.GetFieldMapping(), Response: shapeOfStrategy(strategy)}
	first, err := fetchPageWithRetry(context.Background(), strategy.BuildListUrl(1, opts), 1, opts, nil)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if first.Pagecount.Int() != 0 {
		t.Fatalf("pagecount = %d, want 0 for a response without the field", first.Pagecount.Int())
	}
	pages := 1
	for p := 2; p <= first.Pagecount.Int(); p++ {
		pages++
	}
	if pages != 1 || len(stub.requested) != 1 {
		t.Fatalf("pages = %d, requested = %v", pages, stub.requested)
	}
	if opts.Response == nil || opts.Response.ListPath != "data.list" {
		t.Fatalf("opts envelope lost: %+v", opts.Response)
	}
}

// TestDeclarativeMappingBeatsAliasTable proves an explicit mapping wins over the
// built-in vod_* alias table even for objects read out of list_path.
func TestDeclarativeMappingBeatsAliasTable(t *testing.T) {
	config := `{"version":2,"strategy":"declarative","response":{"list_path":"rows"},` +
		`"field_mapping":{"label":"vod_name","shot":"vod_pic"}}`
	strategy := strategyFromJSON(t, "https://example.test/v2/rows", config)
	body := `{"code":1,"rows":[{"label":"映射优先","shot":"https://c/map.jpg","vod_name":"别名标题","vod_pic":"https://c/alias.jpg"}]}`
	withStubBody(t, body)

	result, err := FetchWithStrategy(context.Background(), strategy, strategy.BuildListUrl(1, FetchOptions{}))
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(result.List) != 1 {
		t.Fatalf("list = %d records", len(result.List))
	}
	video := result.List[0]
	if video.VodName != "映射优先" {
		t.Fatalf("vod_name = %q, want the mapped key to win", video.VodName)
	}
	if video.VodPic != "https://c/map.jpg" {
		t.Fatalf("vod_pic = %q, want the mapped key to win", video.VodPic)
	}
}

// TestDeclarativePackDirectionMappingAcceptsCanonicalFirst documents that a pack
// may write field_mapping either way round, while the legacy strategies keep the
// historical source-key-first reading.
func TestDeclarativePackDirectionMappingAcceptsCanonicalFirst(t *testing.T) {
	config := `{"version":2,"strategy":"declarative","response":{"list_path":"items"},` +
		`"field_mapping":{"vod_name":"title","vod_pic":"cover","vod_id":"video_id","vod_play_url":"url","type_name":"category"}}`
	strategy := strategyFromJSON(t, "https://example.test/v2/items", config)

	want := map[string]string{"title": "vod_name", "cover": "vod_pic", "video_id": "vod_id", "url": "vod_play_url", "category": "type_name"}
	got := strategy.GetFieldMapping()
	if len(got) != len(want) {
		t.Fatalf("mapping = %v, want %v", got, want)
	}
	for key, value := range want {
		if got[key] != value {
			t.Fatalf("mapping = %v, want %v", got, want)
		}
	}

	body := `{"code":1,"items":[{"title":"声明式标题","cover":"https://c/9.jpg","video_id":"9","url":"第1集$https://a/1.m3u8","category":"综艺"}]}`
	withStubBody(t, body)
	result, err := FetchWithStrategy(context.Background(), strategy, strategy.BuildListUrl(1, FetchOptions{}))
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(result.List) != 1 {
		t.Fatalf("list = %d records", len(result.List))
	}
	video := result.List[0]
	if video.VodName != "声明式标题" || video.VodPic != "https://c/9.jpg" || video.VodId.String() != "9" ||
		video.TypeName != "综艺" || video.VodPlayUrl == "" {
		t.Fatalf("video = %+v", video)
	}

	// A source key that happens to spell a canonical field name still resolves:
	// {"vod_id":"id"} rotates because nothing can assign Video.Id.
	rotate := strategyFromJSON(t, "https://example.test/v2/items", `{"version":2,"strategy":"declarative","response":{"list_path":"items"},"field_mapping":{"vod_id":"id"}}`)
	if got := rotate.GetFieldMapping(); len(got) != 1 || got["id"] != "vod_id" {
		t.Fatalf("rotated mapping = %v", got)
	}
	if canonicalVideoFields["id"] || canonicalVideoFields["global_id"] || canonicalVideoFields["in_catalog"] {
		t.Error("non-string fields must not count as mappable canonical names")
	}

	// The legacy paths keep the direction they have always had: no rotation.
	legacy := strategyFromJSON(t, "https://example.test/api", `{"version":2,"strategy":"standard_cms","field_mapping":{"vod_name":"title"}}`)
	if got := legacy.GetFieldMapping(); len(got) != 1 || got["vod_name"] != "title" {
		t.Fatalf("standard_cms mapping must stay untouched: %v", got)
	}
}

func TestCMSStrategiesHonourDeclaredResponseBlock(t *testing.T) {
	// The envelope layer keys off the presence of "response", not off the strategy
	// name, so a CMS-typed source can point its list somewhere else too.
	config := `{"version":2,"strategy":"cms_videolist","response":{"list_path":"data.list","ok_codes":["0"]}}`
	strategy := strategyFromJSON(t, "https://example.test/api.php/provide/vod", config)
	withStubBody(t, `{"code":"0","data":{"list":[{"vod_id":"3","vod_name":"嵌套的 CMS"}]}}`)
	result, err := FetchWithStrategy(context.Background(), strategy, strategy.BuildListUrl(1, FetchOptions{}))
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(result.List) != 1 || result.List[0].VodName != "嵌套的 CMS" {
		t.Fatalf("list = %+v", result.List)
	}
	if !result.OK {
		t.Fatalf("declared response block must decode as success")
	}
}

// TestHostileDeclarativePathsDegradeToError is the panic guard: every one of these
// comes from an untrusted pack document and must produce a clear typed error.
func TestHostileDeclarativePathsDegradeToError(t *testing.T) {
	overlong := strings.Repeat("segment.", 200)
	cases := []struct {
		name string
		path string
		want error
	}{
		{"parent traversal", "../../etc/passwd", ErrPathSyntax},
		{"double dot inside", "data..list", ErrPathSyntax},
		{"leading dot", ".list", ErrPathSyntax},
		{"trailing dot", "list.", ErrPathSyntax},
		{"only dots", "...", ErrPathSyntax},
		{"too many segments", overlong, ErrPathSyntax},
		{"overlong single segment", "data." + strings.Repeat("x", 600), ErrPathSyntax},
		{"negative array index", "items.-1.list", ErrPathType},
		{"word array index", "items.zero.list", ErrPathType},
		{"index overflow", "items.99999999999999999999999", ErrPathType},
		{"missing key", "data.nope.list", ErrPathNotFound},
		{"out of range", "items.404.list", ErrPathNotFound},
		{"descend into string", "data.list.0.name.extra", ErrPathType},
	}
	body := `{"code":1,"data":{"list":[{"name":"甲"}]},"items":[{"name":"乙"}]}`
	for _, tc := range cases {
		for _, field := range []struct {
			name   string
			mutate func(config *ResponseShape)
		}{
			{"list_path", func(s *ResponseShape) { s.ListPath = tc.path }},
			{"code_path", func(s *ResponseShape) { s.CodePath = tc.path }},
			{"pagecount_path", func(s *ResponseShape) { s.PagecountPath = tc.path }},
			{"msg_path", func(s *ResponseShape) { s.MsgPath = tc.path }},
		} {
			shape := &ResponseShape{}
			field.mutate(shape)
			result, err := decodeShapedEnvelope([]byte(body), shape, map[string]string{"name": "vod_name"})
			if err == nil {
				t.Errorf("%s via %s: no error, result = %+v", tc.name, field.name, result)
				continue
			}
			if !errors.Is(err, tc.want) {
				t.Errorf("%s via %s: err = %v, want %v", tc.name, field.name, err, tc.want)
			}
			if strings.Contains(err.Error(), "\x00") {
				t.Errorf("%s: error text is not printable: %q", tc.name, err.Error())
			}
		}
	}
}

// TestHostileDeclarativeConfigOverHTTP repeats the guard through the real fetch
// path, so a bad pack can never take the collector down with it.
func TestHostileDeclarativeConfigOverHTTP(t *testing.T) {
	cases := []struct {
		field string
		path  string
	}{
		{"list_path", "data..list"},
		{"list_path", "data.list.0.name.extra"},
		{"code_path", "../../etc/passwd"},
		{"code_path", "data.list"},
		{"msg_path", "data." + strings.Repeat("x", 600)},
		{"pagecount_path", "data.list.99999999999999999999"},
	}
	const body = `{"code":1,"data":{"message":"ok","list":[{"name":"甲"}]}}`
	for _, tc := range cases {
		entries := []string{fmt.Sprintf(`%q:%q`, tc.field, tc.path)}
		if tc.field != "list_path" {
			entries = append(entries, `"list_path":"data.list"`)
		}
		config := `{"version":2,"strategy":"declarative","response":{` + strings.Join(entries, ",") + `}}`
		strategy := strategyFromJSON(t, "https://example.test/v2/videos", config)
		if strategy.GetStrategyName() != "declarative" {
			t.Fatalf("%s: config did not parse as declarative: %s", tc.field, config)
		}
		withStubBody(t, body)
		if _, err := FetchWithStrategy(context.Background(), strategy, strategy.BuildListUrl(1, FetchOptions{})); err == nil {
			t.Errorf("%s = %q: expected a clear error, got none", tc.field, tc.path)
		} else if !errors.Is(err, ErrPathSyntax) && !errors.Is(err, ErrPathType) && !errors.Is(err, ErrPathNotFound) {
			t.Errorf("%s = %q: err = %v, want a typed path error", tc.field, tc.path, err)
		}
	}
}
