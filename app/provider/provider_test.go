package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"cczjVideo/app/apperror"
	"cczjVideo/app/model"
	"cczjVideo/app/proxy"
)

// 提取引擎是手写的解析器，所以这一组测试用真实形状的响应做夹具，
// 而不是拿实现去反推期望值。每个夹具都对应 reports/06 里核实过的一种真实形态。

func TestMain(m *testing.M) {
	// httptest 绑的是 127.0.0.1，而 SSRF 闸门默认拒绝私网地址。
	// 这不是绕过校验，是用它自带的用户开关（自建源与 NAS 靠它才连得上）。
	proxy.SetAllowPrivateTargets(true)
	code := m.Run()
	proxy.SetAllowPrivateTargets(false)
	if code != 0 {
		// 非零退出时下面这行不会执行，os.Exit 由 m.Run 的结果决定。
		return
	}
}

// ---------------------------------------------------------------------------
// HTML 提取
// ---------------------------------------------------------------------------

// 这一份夹具同时覆盖三种真实形态：
//   - 目录是一个 <ul> 里的 <li><a>（最常见的小说站形状）
//   - 标题里嵌了一个 <span> 标记 VIP，要靠 ownText 才能只取到章名
//   - 页底有一个"下一页"链接
const tocHTML = `<!DOCTYPE html><html><head><meta charset="utf-8"><title>目录</title></head><body>
<div class="box">
  <ul class="chapterlist">
    <li class="head">最新章节</li>
    <li><a href="/book/1/1.html">第一章 起风<span class="vip">VIP</span></a></li>
    <li><a href="/book/1/2.html">第二章 落雨</a></li>
    <li><a href="/book/1/3.html">第三章 转晴</a></li>
  </ul>
  <a class="next" href="/book/1/index_2.html">下一页</a>
</div>
<script>var track = "a<b>c";</script>
</body></html>`

// 第二页保持与第一页相同的 root 结构：root 在后续页上选不中东西是**要报错**的，
// 那意味着规则写错了或者源站改了版，静默截断目录会让用户以为这本书只有三章。
const tocHTMLPage2 = `<!DOCTYPE html><html><body><div class="box">
<ul class="chapterlist">
  <li><a href="/book/1/4.html">第四章 起雾</a></li>
</ul>
</div></body></html>`

func TestExtractChaptersFromHTML(t *testing.T) {
	doc := &Document{Version: DocumentVersion, Kind: "novel"}
	op := &Operation{
		Format: "html",
		Root:   &Selector{Query: "div.box"},
		List:   &Selector{Query: "ul.chapterlist > li"},
		Fields: map[string]Selector{
			FieldTitle:      {Query: "a", Attr: attrOwnText},
			FieldChapterURL: {Query: "a", Attr: "href"},
		},
		Next: &Selector{Query: "a.next", Attr: "href"},
	}
	resp := &Response{Text: tocHTML, FinalURL: "https://example.test/book/1/"}

	chapters, nexts, err := ExtractChapters(doc, op, resp)
	if err != nil {
		t.Fatal(err)
	}
	// "最新章节"那一行没有 <a>，标题取不到，必须被筛掉——目录页里的卷分隔、
	// 广告位、"返回顶部"都会被一个宽泛的选择器选中。
	if len(chapters) != 3 {
		t.Fatalf("got %d chapters, want 3 (the header row must be dropped)", len(chapters))
	}
	// ownText 只取元素自己的文本，不含 <span>VIP</span>。
	if chapters[0].Title != "第一章 起风" {
		t.Fatalf("title = %q, want 第一章 起风; ownText leaked the nested span", chapters[0].Title)
	}
	// 相对地址必须按 FinalURL 绝对化，否则存进库的 remote_id 是一个没法请求的路径。
	if chapters[0].URL != "https://example.test/book/1/1.html" {
		t.Fatalf("url = %q, want an absolute address", chapters[0].URL)
	}
	if chapters[0].RemoteID != "/book/1/1.html" {
		t.Fatalf("remote_id = %q, want the domain-stripped path", chapters[0].RemoteID)
	}
	if len(nexts) != 1 || nexts[0] != "https://example.test/book/1/index_2.html" {
		t.Fatalf("next = %v, want the page-2 link", nexts)
	}
}

func TestRootScopeNarrowsTheTree(t *testing.T) {
	// 页面里有两个 ul.chapterlist：一个在正文区、一个是页脚的"推荐阅读"。
	// root 先把范围缩到 div.box，否则页脚那一份会混进目录。
	page := `<html><body>
	<div class="box"><ul class="chapterlist"><li><a href="/a.html">真章节</a></li></ul></div>
	<div class="footer"><ul class="chapterlist"><li><a href="/ad.html">广告位</a></li></ul></div>
	</body></html>`
	op := &Operation{
		Format: "html",
		Root:   &Selector{Query: "div.box"},
		List:   &Selector{Query: "ul.chapterlist > li"},
		Fields: map[string]Selector{FieldTitle: {Query: "a"}, FieldChapterURL: {Query: "a", Attr: "href"}},
	}
	chapters, _, err := ExtractChapters(&Document{Version: DocumentVersion, Kind: "novel"}, op,
		&Response{Text: page, FinalURL: "https://example.test/"})
	if err != nil {
		t.Fatal(err)
	}
	if len(chapters) != 1 || chapters[0].Title != "真章节" {
		t.Fatalf("root scope did not narrow the tree: %+v", chapters)
	}
}

// Concat 在取值级要把各子结果拼成**一个**字符串。
//
// 这一种是真实需要的：「<span class="num">第1话</span><span class="name">起风</span>」
// 要合成一个标题，而不是两个值里取第一个。Legado 的 && 在 getString 上也是这个语义。
func TestConcatJoinsIntoOneValue(t *testing.T) {
	page := `<html><body><div class="ch"><span class="num">第1话</span><span class="name">起风</span></div></body></html>`
	op := &Operation{Format: "html", Fields: map[string]Selector{
		FieldTitle: {
			Separator: " ",
			Concat:    []Selector{{Query: "span.num"}, {Query: "span.name"}},
		},
	}}
	extractor, err := NewExtractor("html", page)
	if err != nil {
		t.Fatal(err)
	}
	title, err := field(extractor, extractor.Root(), op, FieldTitle, "")
	if err != nil {
		t.Fatal(err)
	}
	if title != "第1话 起风" {
		t.Fatalf("title = %q, want 第1话 起风", title)
	}
}

// Concat 在节点级仍然是追加合并：正文分散在两个容器里是常见形态。
func TestConcatAppendsNodes(t *testing.T) {
	page := `<html><body><div id="p1"><p>上半</p></div><div id="p2"><p>下半</p></div></body></html>`
	extractor, err := NewExtractor("html", page)
	if err != nil {
		t.Fatal(err)
	}
	nodes, err := extractor.Nodes(extractor.Root(), &Selector{Concat: []Selector{
		{Query: "div#p1 > p"}, {Query: "div#p2 > p"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 2 {
		t.Fatalf("got %d nodes, want 2", len(nodes))
	}
}

// 只写 attr、不写 query 的选择器表示"就取当前这个节点"。
// root 已经把范围缩到正文容器时，text 不需要再写一层选择器。
func TestSelfLeafTakesTheCurrentNode(t *testing.T) {
	page := `<html><body><div id="content"><p>第一段</p><p>第二段</p></div></body></html>`
	op := &Operation{
		Format: "html",
		Root:   &Selector{Query: "div#content"},
		Text:   &Selector{Attr: attrText},
	}
	text, _, err := ExtractText(&Document{Version: DocumentVersion, Kind: "novel"}, op,
		&Response{Text: page, FinalURL: "https://example.test/"})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(SplitParagraphs(text), "|"); got != "第一段|第二段" {
		t.Fatalf("text = %q, want 第一段|第二段", got)
	}
	// 空选择器（既没有 path/query，也没有 attr）仍然是错的：
	// 那既没说取哪个节点、也没说取什么。
	if err := (&Selector{}).Validate(1); err == nil {
		t.Fatal("a completely empty selector must be rejected")
	}
	if err := (&Selector{Attr: attrText}).Validate(1); err != nil {
		t.Fatalf("a self leaf must be valid: %v", err)
	}
}

func TestFallbackStopsAtFirstNonEmpty(t *testing.T) {
	page := `<html><body><div class="book"><span class="author-new">新写法</span><span class="author">旧写法</span></div></body></html>`
	op := &Operation{
		Format: "html",
		Fields: map[string]Selector{
			FieldTitle: {Query: "div.book", Attr: attrOwnText},
			FieldAuthor: {Fallback: []Selector{
				{Query: "span.missing"},
				{Query: "span.author-new"},
				{Query: "span.author"},
			}},
		},
	}
	extractor, err := NewExtractor("html", page)
	if err != nil {
		t.Fatal(err)
	}
	author, err := field(extractor, extractor.Root(), op, FieldAuthor, "https://example.test/")
	if err != nil {
		t.Fatal(err)
	}
	if author != "新写法" {
		t.Fatalf("author = %q, want 新写法; fallback must stop at the first non-empty result", author)
	}
}

func TestConcatAppendsAndIndexPicks(t *testing.T) {
	page := `<html><body><ul><li>a</li><li>b</li><li>c</li><li>d</li></ul></body></html>`
	extractor, err := NewExtractor("html", page)
	if err != nil {
		t.Fatal(err)
	}
	// 排除开头两个（常见的广告位/卷标题），再取剩下里的第一个与最后一个。
	values, err := extractor.Strings(extractor.Root(), &Selector{
		Query: "ul > li",
		Index: &IndexSpec{Indices: []int{0, 1}, Exclude: true},
		Attr:  attrText,
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(values, ",") != "c,d" {
		t.Fatalf("exclude index gave %v, want [c d]", values)
	}
	values, err = extractor.Strings(extractor.Root(), &Selector{
		Query: "ul > li",
		Index: &IndexSpec{Indices: []int{-1}},
		Attr:  attrText,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 1 || values[0] != "d" {
		t.Fatalf("negative index gave %v, want [d]", values)
	}
	// 越界下标静默丢弃：一条"跳过开头两个"的规则在只有两条结果的页面上
	// 不该整页失败。
	values, err = extractor.Strings(extractor.Root(), &Selector{
		Query: "ul > li",
		Index: &IndexSpec{Indices: []int{99}},
		Attr:  attrText,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 0 {
		t.Fatalf("out-of-range index gave %v, want empty", values)
	}
}

func TestRegexReplaceKeepsTheRestOfTheText(t *testing.T) {
	page := `<html><body><div id="c">广告A正文广告B</div></body></html>`
	extractor, err := NewExtractor("html", page)
	if err != nil {
		t.Fatal(err)
	}
	op := &Operation{Format: "html", Fields: map[string]Selector{
		FieldTitle: {Query: "div#c", Attr: attrText, Regex: &Replace{Pattern: "广告[AB]", Replacement: ""}},
	}}
	value, err := field(extractor, extractor.Root(), op, FieldTitle, "")
	if err != nil {
		t.Fatal(err)
	}
	if value != "正文" {
		t.Fatalf("value = %q, want 正文", value)
	}

	// First 必须是字面意思：只替换第一处，其余原文保留。
	// Legado 的 ### 形式会把"第一处匹配之外"的整段丢掉（AnalyzeRule.kt:441-452），
	// 那是个陷阱，我们刻意不复制。
	firstOnly := &Operation{Format: "html", Fields: map[string]Selector{
		FieldTitle: {Query: "div#c", Attr: attrText,
			Regex: &Replace{Pattern: "广告[AB]", Replacement: "", First: true}},
	}}
	value, err = field(extractor, extractor.Root(), firstOnly, FieldTitle, "")
	if err != nil {
		t.Fatal(err)
	}
	if value != "正文广告B" {
		t.Fatalf("first-only replace gave %q, want 正文广告B", value)
	}
}

func TestTextSkipsScriptAndKeepsParagraphBreaks(t *testing.T) {
	page := `<html><body><div id="content"><p>第一段</p><script>var x = 1;</script><p>第二段</p>第三行<br>第四行</div></body></html>`
	extractor, err := NewExtractor("html", page)
	if err != nil {
		t.Fatal(err)
	}
	text, err := extractor.String(extractor.Root(), &Selector{Query: "div#content", Attr: attrText})
	if err != nil {
		t.Fatal(err)
	}
	paragraphs := SplitParagraphs(text)
	want := []string{"第一段", "第二段", "第三行", "第四行"}
	if len(paragraphs) != len(want) {
		t.Fatalf("paragraphs = %q, want %q", paragraphs, want)
	}
	for i := range want {
		if paragraphs[i] != want[i] {
			t.Fatalf("paragraph %d = %q, want %q", i, paragraphs[i], want[i])
		}
	}
	// script 里的内容不能混进正文，否则一屏 JS 代码会被当成小说。
	if strings.Contains(text, "var x") {
		t.Fatalf("script content leaked into the text: %q", text)
	}
	// 清洗阶段不加缩进：Legado 在这里就给每行补两个全角空格
	// （BookContent.kt:139-144），那是把排版烧进了数据（ADR 0016）。
	for _, paragraph := range paragraphs {
		if strings.HasPrefix(paragraph, "　") {
			t.Fatalf("paragraph got a hardcoded indent: %q", paragraph)
		}
	}
}

// ---------------------------------------------------------------------------
// JSON 提取
// ---------------------------------------------------------------------------

const searchJSON = `{"code":1,"msg":"ok","data":{"list":[
  {"id":"900000000000000001","name":"示例书","author":"示例作者","pic":"//cdn.example.test/c.jpg","remark":"更新至第2章"},
  {"id":"900000000000000002","name":"另一本","author":"另一个人","pic":"/c2.jpg"}
],"page":1,"pagecount":3}}`

func TestExtractItemsFromJSON(t *testing.T) {
	op := &Operation{
		Format: "json",
		List:   &Selector{Path: "data.list"},
		Fields: map[string]Selector{
			FieldID:     {Path: "id"},
			FieldTitle:  {Path: "name"},
			FieldAuthor: {Path: "author"},
			FieldCover:  {Path: "pic"},
			FieldLatest: {Path: "remark"},
		},
	}
	items, _, err := ExtractItems(&Document{Version: DocumentVersion, Kind: "novel"}, op,
		&Response{Text: searchJSON, FinalURL: "https://example.test/api"})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2", len(items))
	}
	// 20 位的远端 ID 必须原样活下来。转成 float64 会丢精度，
	// 而 reports/04:22 明确警告过"损失精度后再转字符串无法修复"。
	if items[0].RemoteID != "900000000000000001" {
		t.Fatalf("remote_id = %q, want the exact 18-digit string", items[0].RemoteID)
	}
	// 协议相对地址（//cdn…）要按当前页补全协议。
	if items[0].Cover != "https://cdn.example.test/c.jpg" {
		t.Fatalf("cover = %q, want a protocol-completed absolute address", items[0].Cover)
	}
	if items[1].Cover != "https://example.test/c2.jpg" {
		t.Fatalf("cover = %q, want an absolute address resolved against the final URL", items[1].Cover)
	}
	if items[0].Latest != "更新至第2章" {
		t.Fatalf("latest = %q", items[0].Latest)
	}
}

func TestJSONScalarOnObjectIsAnError(t *testing.T) {
	// 配置指错层级时必须报错，不能静默返回空串：
	// 空串在界面上是"这个源没有作者"，而真正的问题是路径写错了。
	extractor, err := NewExtractor("json", `{"data":{"list":[{"name":"x"}]}}`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := extractor.String(extractor.Root(), &Selector{Path: "data"}); err == nil {
		t.Fatal("reading a scalar out of an object must fail, not return empty")
	}
	if _, err := extractor.String(extractor.Root(), &Selector{Path: "data.missing"}); err == nil {
		t.Fatal("a missing path must fail")
	}
	// 路径语法错误要在读文档之前就报出来，不能被当成"这个键不存在"。
	if _, err := extractor.String(extractor.Root(), &Selector{Path: "data..list"}); err == nil {
		t.Fatal("a malformed path must be a syntax error")
	}
}

// ---------------------------------------------------------------------------
// 失败识别（reports/06 §4.4：两个参照项目都没有通用判据，这一组是自己定的）
// ---------------------------------------------------------------------------

func TestGuardFormatSeparatesErrorPageFromBadConfig(t *testing.T) {
	jsonOp := &Operation{Format: "json", List: &Selector{Path: "data.list"}}

	// 源站返回 HTML 错误页而配置声明的是 json：这是"源坏了"，不是"配置写错了"。
	err := guardFormat(jsonOp, &Response{Text: "<html><body>502 Bad Gateway</body></html>"})
	if !apperror.Is(err, apperror.Corrupt) {
		t.Fatalf("HTML body against a json config gave %v, want CORRUPT", err)
	}

	// 登录页要能被单独认出来：它的处置是"这个源要登录，我们不支持"，
	// 重试或换规则都没用。
	loginPage := `<html><head><title>用户登录</title></head><body>
	<form><input type="password" name="pw"></form></body></html>`
	err = guardFormat(jsonOp, &Response{Text: loginPage})
	if !apperror.Is(err, apperror.Unsupported) {
		t.Fatalf("login page gave %v, want UNSUPPORTED", err)
	}

	// 反过来：JSON 源被配成 html，要报"把 format 改成 json"。
	// 不报的话它会解析出一棵几乎空的树，用户看到的是"搜不到任何东西"。
	htmlOp := &Operation{Format: "html", List: &Selector{Query: "li"}}
	err = guardFormat(htmlOp, &Response{Text: searchJSON})
	if err == nil || !strings.Contains(err.Error(), "改成 json") {
		t.Fatalf("JSON body against an html config gave %v, want a hint to change format", err)
	}

	if err := guardFormat(jsonOp, &Response{Text: "   "}); err == nil {
		t.Fatal("an empty body must not be treated as a successful empty result")
	}
}

func TestGuardTextPageFlagsLoginWall(t *testing.T) {
	loginPage := `<html><head><title>请登录</title></head><body><p>请登录后阅读本章</p></body></html>`
	if err := guardTextPage(&Response{Text: loginPage}, "请登录后阅读本章"); err == nil {
		t.Fatal("a short login-shaped page must be reported, not shown as the chapter text")
	}
	// 短但没有登录特征：有的章本来就短，不能凭长度判死。
	if err := guardTextPage(&Response{Text: "<p>本章完</p>"}, "本章完"); err != nil {
		t.Fatalf("a short but ordinary page must not be flagged: %v", err)
	}
}

func TestDetectFontObfuscation(t *testing.T) {
	// 字体码位混淆（reports/06 §6）：站点下发一份码位↔字形被打乱的 WebFont，
	// 浏览器渲染正常，抽出来的却是**码点有效、字义错误的汉字**。
	// 不是方块、不是编码乱码，用户完全无法自查，所以至少要能报出来。
	obfuscated := strings.Repeat("\uE000\uE001\uE002\uE003", 40)
	if !DetectFontObfuscation(obfuscated) {
		t.Fatal("private-use codepoints must be flagged as possible font obfuscation")
	}
	if DetectFontObfuscation("这是一段正常的中文正文，没有任何私用区码位。") {
		t.Fatal("ordinary Chinese text must not be flagged")
	}
}

func TestDecodeHandlesGBKAndDetectsGarbling(t *testing.T) {
	// 中文小说站大量是 GBK。"第一章"的 GBK 字节。
	gbk := []byte{0xB5, 0xDA, 0xD2, 0xBB, 0xD5, 0xC2}

	// 头里声明了 charset。
	resp := &http.Response{Header: http.Header{"Content-Type": {"text/html; charset=gbk"}}}
	text, err := Decode(gbk, resp, "")
	if err != nil {
		t.Fatal(err)
	}
	if text != "第一章" {
		t.Fatalf("header charset gave %q, want 第一章", text)
	}

	// 头里没有，靠 <meta charset>。
	meta := append([]byte(`<html><head><meta charset="gbk"></head><body>`), gbk...)
	text, err = Decode(meta, &http.Response{Header: http.Header{}}, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "第一章") {
		t.Fatalf("meta charset gave %q, want it to contain 第一章", text)
	}

	// 显式声明优先于探测：Legado 的 URL option charset 根本不参与响应解码
	// （AnalyzeUrl.kt:242,287-298），我们这一级是真的生效的。
	text, err = Decode(gbk, &http.Response{Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}}}, "gbk")
	if err != nil {
		t.Fatal(err)
	}
	if text != "第一章" {
		t.Fatalf("explicit encoding lost to the header: %q", text)
	}

	// 头里声明的编码解不动时不报错，继续往下探测：
	// 一个能读出来的错误编码比一个报错的正确编码有用。
	text, err = Decode([]byte("plain ascii"), &http.Response{Header: http.Header{"Content-Type": {"text/html; charset=no-such-encoding"}}}, "")
	if err != nil {
		t.Fatalf("an unusable declared charset must fall through, not fail: %v", err)
	}
	if text != "plain ascii" {
		t.Fatalf("text = %q", text)
	}

	if !LooksGarbled("\uFFFD\uFFFD\uFFFD正常") {
		t.Fatal("replacement characters must be reported as garbled")
	}
	if LooksGarbled("一段正常的中文") {
		t.Fatal("clean text must not be reported as garbled")
	}
}

// ---------------------------------------------------------------------------
// URL 归一化与稳定标识
// ---------------------------------------------------------------------------

func TestRemoteIDIsStableAcrossDomainsAndQueryOrder(t *testing.T) {
	cases := []struct{ left, right string }{
		// 换域名（镜像、防封）不该让整本书的进度失效。
		{"https://a.example.test/book/1/2.html", "https://b.example.test/book/1/2.html"},
		// query 顺序不同必须是同一个身份，否则目录同步时每章都出现两遍。
		{"https://a.example.test/api?t=2&t=1", "https://a.example.test/api?t=1&t=2"},
		// 锚点不改变服务端返回的内容。
		{"https://a.example.test/book/1/2.html#top", "https://a.example.test/book/1/2.html"},
		// 大小写变体。
		{"HTTPS://A.Example.Test/Book/1", "https://a.example.test/Book/1"},
	}
	for _, tc := range cases {
		if RemoteID(tc.left) != RemoteID(tc.right) {
			t.Fatalf("RemoteID(%q) = %q but RemoteID(%q) = %q; these must be the same identity",
				tc.left, RemoteID(tc.left), tc.right, RemoteID(tc.right))
		}
	}
	// 不同的章必须是不同的身份。
	if RemoteID("https://a.example.test/book/1/2.html") == RemoteID("https://a.example.test/book/1/3.html") {
		t.Fatal("different chapters collapsed into one identity")
	}
	// API 源常见形状：所有章节都是根路径上的 ?id=123。path 为空时必须退回 query，
	// 否则整本书每一章的 remote_id 都是空串，会在 UNIQUE 上互相撞。
	if got := RemoteID("https://a.example.test/?id=7"); got != "?id=7" {
		t.Fatalf("query-only remote id = %q, want ?id=7", got)
	}
	// 有路径时路径也要进身份，否则同一接口下的不同资源会撞成一个。
	if RemoteID("https://a.example.test/api?id=7") != "/api?id=7" {
		t.Fatalf("path+query remote id = %q, want /api?id=7", RemoteID("https://a.example.test/api?id=7"))
	}
}

func TestSameTargetSurvivesSignatureParams(t *testing.T) {
	base := "https://example.test/book/1/"
	// Legado 在这里做的是绝对化之后的裸字符串相等（BookContent.kt:86-89），
	// 只做绝对化不做归一，所以带不同签名参数的下一章地址拦不住。
	if !SameTarget(base, "2.html#bottom", "https://example.test/book/1/2.html") {
		t.Fatal("the next-chapter guard missed a fragment-only difference")
	}
	if SameTarget(base, "2.html", "3.html") {
		t.Fatal("different chapters must not compare equal")
	}
	if SameTarget(base, "", "2.html") || SameTarget(base, "2.html", "") {
		t.Fatal("an empty side must not compare equal to anything")
	}
}

func TestAbsolutizeRejectsNonFetchableSchemes(t *testing.T) {
	base := "https://example.test/book/1/"
	for _, raw := range []string{"javascript:void(0)", "data:text/html,x", "#top", ""} {
		if got := Absolutize(base, raw); got != "" {
			t.Fatalf("Absolutize(%q) = %q, want empty; this is not a fetchable address", raw, got)
		}
	}
	if got := Absolutize(base, "../2/index.html"); got != "https://example.test/book/2/index.html" {
		t.Fatalf("relative resolution gave %q", got)
	}
	// 没有可用基准时，绝对地址仍然要能用。
	if got := Absolutize("", "https://cdn.example.test/a.jpg"); got != "https://cdn.example.test/a.jpg" {
		t.Fatalf("absolute address without a base gave %q", got)
	}
}

// ---------------------------------------------------------------------------
// 文档校验
// ---------------------------------------------------------------------------

func TestParseDocumentRejectsUnsupportedFieldsByName(t *testing.T) {
	// 严格解码是"不支持的字段要显式报错"这条要求的实现：一份为别的引擎写的
	// 文档带着 js / webView 之类的键时，要报出**具体是哪个键**，
	// 而不是当它不存在——静默忽略在界面上表现为"这个源搜不出东西"，
	// 与"源真的没有这本书"无法区分（reports/02:135）。
	for _, key := range []string{"js", "webView", "webJs", "preUpdateJs", "loginCheckJs", "imageDecode"} {
		raw := `{"version":3,"kind":"novel","` + key + `":"x","operations":{"content":{"format":"html","text":{"query":"#c"}}}}`
		_, err := ParseDocument(raw, "novel")
		if err == nil {
			t.Fatalf("document with %q was accepted; unsupported capabilities must be rejected by name", key)
		}
		if !strings.Contains(err.Error(), key) {
			t.Fatalf("rejection for %q did not name the field: %v", key, err)
		}
	}
}

func TestParseDocumentValidation(t *testing.T) {
	valid := `{"version":3,"kind":"novel","operations":{
		"search":{"format":"html","list":{"query":"li"},"fields":{"title":{"query":"a"}}},
		"content":{"format":"html","text":{"query":"#content"}}}}`
	if _, err := ParseDocument(valid, "novel"); err != nil {
		t.Fatalf("a valid document was rejected: %v", err)
	}

	cases := []struct{ name, raw string }{
		{"版本不对", `{"version":2,"kind":"novel","operations":{"content":{"format":"html","text":{"query":"#c"}}}}`},
		{"kind 与源不一致", `{"version":3,"kind":"comic","operations":{"content":{"format":"html","text":{"query":"#c"}}}}`},
		{"小说缺 content", `{"version":3,"kind":"novel","operations":{"search":{"format":"json","list":{"path":"a"}}}}`},
		{"漫画缺 pages", `{"version":3,"kind":"comic","operations":{"content":{"format":"html","text":{"query":"#c"}}}}`},
		{"没有任何操作", `{"version":3,"kind":"novel","operations":{}}`},
		{"format 缺失", `{"version":3,"kind":"novel","operations":{"content":{"text":{"query":"#c"}}}}`},
		{"format 不认识", `{"version":3,"kind":"novel","operations":{"content":{"format":"xml","text":{"query":"#c"}}}}`},
		{"method 不认识", `{"version":3,"kind":"novel","operations":{"content":{"format":"html","method":"PUT","text":{"query":"#c"}}}}`},
		{"字段名拼错", `{"version":3,"kind":"novel","operations":{"content":{"format":"html","text":{"query":"#c"},"fields":{"titel":{"query":"a"}}}}}`},
		{"选择器两种形态都写", `{"version":3,"kind":"novel","operations":{"content":{"format":"html","text":{"query":"#c","path":"a"}}}}`},
		{"选择器一种都没写", `{"version":3,"kind":"novel","operations":{"content":{"format":"html","text":{"regex":{"pattern":"a"}}}}}`},
		{"两个组合算子并存", `{"version":3,"kind":"novel","operations":{"content":{"format":"html","text":{"concat":[{"query":"a"}],"fallback":[{"query":"b"}]}}}}`},
		{"空 index", `{"version":3,"kind":"novel","operations":{"content":{"format":"html","text":{"query":"a","index":{"indices":[]}}}}}`},
		{"正则编译不过", `{"version":3,"kind":"novel","operations":{"content":{"format":"html","text":{"query":"a","regex":{"pattern":"("}}}}}`},
		{"header 带换行", "{\"version\":3,\"kind\":\"novel\",\"headers\":{\"X-A\":\"b\\r\\nEvil: 1\"},\"operations\":{\"content\":{\"format\":\"html\",\"text\":{\"query\":\"#c\"}}}}"},
	}
	for _, tc := range cases {
		if _, err := ParseDocument(tc.raw, "novel"); err == nil {
			t.Fatalf("%s: document was accepted, want rejection", tc.name)
		}
	}
}

func TestRegexUsesRE2AndSaysSo(t *testing.T) {
	// Go 的 regexp 是 RE2，不支持回溯引用与零宽断言。从别的引擎抄来的规则会
	// 在这里编译失败，所以报错要指明是语法不被支持，而不是让用户以为
	// 是自己转义写错了。
	_, err := compileReplace(&Replace{Pattern: `(?<=第)一章`})
	if err == nil || !strings.Contains(err.Error(), "RE2") {
		t.Fatalf("lookbehind gave %v, want an error naming RE2", err)
	}
	_, err = compileReplace(&Replace{Pattern: `(a)\1`})
	if err == nil || !strings.Contains(err.Error(), "RE2") {
		t.Fatalf("backreference gave %v, want an error naming RE2", err)
	}
}

// ---------------------------------------------------------------------------
// 端到端：目录分页与正文分页
// ---------------------------------------------------------------------------

func TestTocFollowsPaginationAndAssignsOrder(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if strings.HasSuffix(r.URL.Path, "index_2.html") {
			_, _ = w.Write([]byte(tocHTMLPage2))
			return
		}
		_, _ = w.Write([]byte(tocHTML))
	}))
	defer server.Close()

	doc := &Document{Version: DocumentVersion, Kind: "novel", Operations: Operations{
		Toc: &Operation{
			Format: "html",
			Root:   &Selector{Query: "div.box"},
			List:   &Selector{Query: "ul.chapterlist > li"},
			Fields: map[string]Selector{
				FieldTitle:      {Query: "a", Attr: attrOwnText},
				FieldChapterURL: {Query: "a", Attr: "href"},
			},
			Next: &Selector{Query: "a.next", Attr: "href"},
		},
	}}
	pipeline := NewPipeline(doc, NewFetcher(), Limits{})
	item := &Item{RemoteID: "/book/1/", Title: "示例", URL: server.URL + "/book/1/", TocURL: server.URL + "/book/1/"}

	chapters, err := pipeline.Toc(context.Background(), server.URL, item)
	if err != nil {
		t.Fatal(err)
	}
	if len(chapters) != 4 {
		t.Fatalf("got %d chapters, want 4 across two pages", len(chapters))
	}
	for i, chapter := range chapters {
		// ord 由我们算，不用远端下标（Mihon 直接用下标并假设源从新到旧返回，
		// 且不做方向检测，SyncChaptersWithSource.kt:195-196）。
		if chapter.Ord != i {
			t.Fatalf("chapter %d has ord %d", i, chapter.Ord)
		}
	}
	if chapters[3].Title != "第四章 起雾" {
		t.Fatalf("page 2 was not merged in order: %+v", chapters[3])
	}
}

func TestTocStopsAtThePageLimit(t *testing.T) {
	// 一个永远指向下一页的源。Legado 的循环**没有数值上限**，只靠 URL 去重
	// 与协程取消终止（BookChapterList.kt:68-86），这种源在那边会一直跟下去。
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html><body><ul class="l"><li><a href="/c.html">章</a></li></ul>
			<a class="next" href="/index_` + string(rune('a'+requests)) + `.html">下一页</a></body></html>`))
	}))
	defer server.Close()

	doc := &Document{Version: DocumentVersion, Kind: "novel", Operations: Operations{
		Toc: &Operation{Format: "html", List: &Selector{Query: "ul.l > li"},
			Fields: map[string]Selector{FieldTitle: {Query: "a"}, FieldChapterURL: {Query: "a", Attr: "href"}},
			Next:   &Selector{Query: "a.next", Attr: "href"}},
	}}
	pipeline := NewPipeline(doc, NewFetcher(), Limits{MaxTocPages: 3})
	_, err := pipeline.Toc(context.Background(), server.URL, &Item{Title: "x", TocURL: server.URL + "/"})
	if err != nil {
		t.Fatal(err)
	}
	if requests > 3 {
		t.Fatalf("made %d requests with a 3-page limit", requests)
	}
}

func TestTocEmptyIsAnErrorNotAnEmptyBook(t *testing.T) {
	// 空目录不是"这本书没有章节"，是这一次没解析出东西。Legado 专门为它抛
	// TocEmptyException（BookChapterList.kt:116-118）：一次规则失效与一本
	// 真的完结无章的书在界面上必须能分开。
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><body><div>本站正在维护</div></body></html>`))
	}))
	defer server.Close()
	doc := &Document{Version: DocumentVersion, Kind: "novel", Operations: Operations{
		Toc: &Operation{Format: "html", List: &Selector{Query: "ul.l > li"},
			Fields: map[string]Selector{FieldTitle: {Query: "a"}, FieldChapterURL: {Query: "a", Attr: "href"}}},
	}}
	pipeline := NewPipeline(doc, NewFetcher(), Limits{})
	_, err := pipeline.Toc(context.Background(), server.URL, &Item{Title: "x", TocURL: server.URL + "/"})
	if err == nil {
		t.Fatal("an empty toc must fail, not report a book with no chapters")
	}
}

func TestContentExcludesTheNextChapter(t *testing.T) {
	// 正文页的"下一页"链接指向下一章：必须被拦住，否则一章的正文里
	// 会混进下一章的内容，而用户看到的是一段莫名其妙的接续。
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		switch r.URL.Path {
		case "/1.html":
			_, _ = w.Write([]byte(`<html><body><div id="c"><p>第一章第一段</p></div>
				<a class="next" href="/1_2.html">下一页</a></body></html>`))
		case "/1_2.html":
			_, _ = w.Write([]byte(`<html><body><div id="c"><p>第一章第二段</p></div>
				<a class="next" href="/2.html">下一页</a></body></html>`))
		case "/2.html":
			_, _ = w.Write([]byte(`<html><body><div id="c"><p>第二章不该出现在这里</p></div></body></html>`))
		}
	}))
	defer server.Close()

	doc := &Document{Version: DocumentVersion, Kind: "novel", Operations: Operations{
		Content: &Operation{Format: "html",
			Text: &Selector{Query: "div#c", Attr: attrText},
			Next: &Selector{Query: "a.next", Attr: "href"}},
	}}
	pipeline := NewPipeline(doc, NewFetcher(), Limits{})
	content, err := pipeline.Content(context.Background(), server.URL,
		&Chapter{Title: "第一章", URL: server.URL + "/1.html"}, server.URL+"/2.html")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(content.Paragraphs, "|")
	if joined != "第一章第一段|第一章第二段" {
		t.Fatalf("content = %q, want the two pages of chapter one only", joined)
	}
	if strings.Contains(joined, "第二章") {
		t.Fatal("the next chapter was swallowed into this one")
	}
}

func TestContentEmptyIsAnError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><body><div id="c"></div></body></html>`))
	}))
	defer server.Close()
	doc := &Document{Version: DocumentVersion, Kind: "novel", Operations: Operations{
		Content: &Operation{Format: "html", Text: &Selector{Query: "div#c", Attr: attrText}},
	}}
	pipeline := NewPipeline(doc, NewFetcher(), Limits{})
	_, err := pipeline.Content(context.Background(), server.URL,
		&Chapter{Title: "第一章", URL: server.URL + "/1.html"}, "")
	if err == nil {
		t.Fatal("empty chapter text must fail; caching it as blank would hide the real cause")
	}
}

func TestVolumeChapterIsNotAnError(t *testing.T) {
	// 卷标题行没有正文，这不是错误。Legado 同样豁免它的判空
	// （BookContent.kt:149：isVolume 时不抛 ContentEmptyException）。
	pipeline := NewPipeline(&Document{Version: DocumentVersion, Kind: "novel", Operations: Operations{
		Content: &Operation{Format: "html", Text: &Selector{Query: "div#c"}},
	}}, NewFetcher(), Limits{})
	content, err := pipeline.Content(context.Background(), "https://example.test",
		&Chapter{Title: "第一卷", IsVolume: true, URL: "https://example.test/v"}, "")
	if err != nil {
		t.Fatalf("a volume row must not fail: %v", err)
	}
	if len(content.Paragraphs) != 0 {
		t.Fatalf("a volume row produced text: %q", content.Paragraphs)
	}
}

// ---------------------------------------------------------------------------
// 请求构造
// ---------------------------------------------------------------------------

func TestBuildURLScrubsInheritedParams(t *testing.T) {
	op := &Operation{Format: "json", ParamNames: ParamNames{}}
	// base 里遗留的 page 与 wd 不能活到这一步来：一个已经取到的目录地址
	// 仍然可能需要追加 page，而继承来的旧值会让第一页取到第九页的内容。
	got, err := BuildURL("https://example.test/api", op, Target{
		URL:  "https://example.test/book/1/?pg=9&wd=旧词",
		Vars: map[string]string{VarPage: "2", VarKeyword: "新词"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "pg=9") || strings.Contains(got, "%E6%97%A7%E8%AF%8D") {
		t.Fatalf("inherited params survived: %q", got)
	}
	if !strings.Contains(got, "pg=2") || !strings.Contains(got, "wd=") {
		t.Fatalf("declared params were not applied: %q", got)
	}
}

func TestBuildURLRejectsUnexpandedPlaceholders(t *testing.T) {
	op := &Operation{Format: "json", Path: "/search/{id}"}
	_, err := BuildURL("https://example.test", op, Target{Vars: map[string]string{}})
	// 漏掉的变量必须报错，而且要在 url.Parse 之前报：解析会把 { } 转义成
	// %7B %7D，之后就再也认不出这是一个没取到值的占位符了。
	// 换成空串更糟——地址会打到源站列表页，返回一堆无关结果；
	// 留着转义后的 %7Bid%7D 也糟——请求发出去然后 404，看不出是配置漏了变量。
	if !apperror.Is(err, apperror.Validation) {
		t.Fatalf("err = %v, want VALIDATION", err)
	}
	if err == nil || !strings.Contains(err.Error(), "id") {
		t.Fatalf("the error must name the missing variable: %v", err)
	}
	// 传上了就正常拼出来。
	got, err := BuildURL("https://example.test", op, Target{Vars: map[string]string{VarID: "42"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "42") || strings.Contains(got, "{") {
		t.Fatalf("url = %q, want the variable substituted", got)
	}
}

func TestApplyHeadersKeepsFullRefererAndIgnoresAcceptEncoding(t *testing.T) {
	request, _ := http.NewRequest(http.MethodGet, "https://example.test/a.jpg", nil)
	doc := &Document{Headers: map[string]string{
		// Referer 不能被截成 origin：真实漫画 CDN 校验的往往是完整页面地址，
		// 而现有 HLS 代理的 RefererOrigin 正是把它截成了 scheme://host/
		// （hls.go:110-116）。见 ADR 0016 与 pending-decisions 第 16 条。
		"Referer":         "https://example.test/book/1/2.html?page=3",
		"Accept-Encoding": "br",
	}}
	applyHeaders(request, doc, &Operation{Headers: map[string]string{"X-Op": "1"}})
	if got := request.Header.Get("Referer"); got != "https://example.test/book/1/2.html?page=3" {
		t.Fatalf("Referer = %q, want the full page address", got)
	}
	if got := request.Header.Get("Accept-Encoding"); got == "br" {
		t.Fatal("Accept-Encoding must not be overridable: declaring it disables Go's transparent decompression")
	}
	if got := request.Header.Get("X-Op"); got != "1" {
		t.Fatalf("operation-level header missing: %q", got)
	}
	if request.Header.Get("User-Agent") == "" {
		t.Fatal("a default User-Agent must always be present")
	}
}

func TestRedirectIsRevalidated(t *testing.T) {
	// 每一跳都要重新过地址校验：只在最初那一跳校验等于没校验，
	// 一个 302 就能把请求从公网地址带进内网。
	hops := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hops++
		if hops == 1 {
			http.Redirect(w, r, "/second", http.StatusFound)
			return
		}
		_, _ = w.Write([]byte(`<html><body><div id="c"><p>正文</p></div></body></html>`))
	}))
	defer server.Close()

	doc := &Document{Version: DocumentVersion, Kind: "novel", Operations: Operations{
		Content: &Operation{Format: "html", Text: &Selector{Query: "div#c", Attr: attrText}},
	}}
	pipeline := NewPipeline(doc, NewFetcher(), Limits{})
	content, err := pipeline.Content(context.Background(), server.URL,
		&Chapter{Title: "x", URL: server.URL + "/first"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(content.Paragraphs) != 1 || content.Paragraphs[0] != "正文" {
		t.Fatalf("content = %q after %d hops", content.Paragraphs, hops)
	}
}

func TestChapterAccessNormalizes(t *testing.T) {
	// 拼错的访问条件按 unknown 处理：不能让一个拼错的字符串变成界面上
	// 一个谁也不认的徽章。
	page := `<html><body><ul><li><a href="/1.html">第一章</a><i>bogus</i></li></ul></body></html>`
	op := &Operation{Format: "html", List: &Selector{Query: "ul > li"}, Fields: map[string]Selector{
		FieldTitle: {Query: "a"}, FieldChapterURL: {Query: "a", Attr: "href"}, FieldAccess: {Query: "i"},
	}}
	chapters, _, err := ExtractChapters(&Document{Version: DocumentVersion, Kind: "novel"}, op,
		&Response{Text: page, FinalURL: "https://example.test/"})
	if err != nil {
		t.Fatal(err)
	}
	if chapters[0].Access != model.ChapterAccessUnknown {
		t.Fatalf("access = %q, want unknown", chapters[0].Access)
	}
}
