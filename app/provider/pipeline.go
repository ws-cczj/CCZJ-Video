package provider

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"cczjVideo/app/apperror"
	"cczjVideo/app/model"
)

// Pipeline 把一份源文档、一个执行器与一组上限绑在一起，提供"取到底"的操作。
//
// 分页循环放在这一层而不是编排层，是因为它们是**协议的一部分**：什么时候算取完、
// 怎么去重、怎么不把下一章吞进本章，都由源文档的 next 选择器与这里的上限共同决定。
// Legado 的对应物是 WebBook + BookContent + BookChapterList 三个类。
type Pipeline struct {
	Doc     *Document
	Fetcher *Fetcher
	Limits  Limits
}

// NewPipeline 组一条管线。limits 里为 0 的项取 ADR 0016 的默认值。
func NewPipeline(doc *Document, fetcher *Fetcher, limits Limits) *Pipeline {
	resolved := limits.orDefault()
	return &Pipeline{Doc: doc, Fetcher: fetcher, Limits: resolved}
}

// ItemPage 是一页搜索/浏览结果。
type ItemPage struct {
	Items []*Item
	// NextURL 是下一页地址，空表示没有下一页。
	//
	// 刻意用"下一页地址"而不是"page+1"：reports/00:52 要求分页 token 由 provider
	// 自己解释，不统一假设 page=1。有的源用 page、有的用 offset、有的用游标，
	// 有的干脆只在页面里给一个"下一页"链接。
	NextURL string
	// HasNext 单独给出，因为"NextURL 为空"与"没有下一页"在某些源上不是一回事：
	// 有的源最后一页仍然给出一个指向自己的链接。去重集合会拦下那种自指，
	// 但界面需要一个不含歧义的布尔值来决定要不要显示"加载更多"。
	HasNext bool
	// Garbled 报告这一页的解码结果像是编码错了。
	Garbled bool
}

// Search 取一页搜索结果。
//
// 不跟随分页：搜索的翻页由界面驱动，用户翻一页取一页。Legado 也是这样
// （SearchModel 逐页发，各源的分页状态独立保存，reports/00:131）。
func (p *Pipeline) Search(ctx context.Context, apiURL, keyword string, page int, nextURL string) (*ItemPage, error) {
	if p.Doc.Adapter == "zhaicool-web" {
		return p.zhaicoolSearch(ctx, keyword, page)
	}
	if p.Doc.Adapter == "bh3-web" {
		return p.bh3Search(ctx, keyword, page)
	}
	if p.Doc.Adapter == "terra-web" {
		return p.terraSearch(ctx, keyword, page)
	}
	if p.Doc.Adapter == "fanqie-web" {
		return p.fanqieSearch(ctx, keyword, page, nextURL)
	}
	op := p.Doc.Operations.Search
	if op == nil {
		return nil, apperror.New(apperror.Unsupported, "这个源没有声明搜索操作")
	}
	target := Target{
		URL:           nextURL,
		Vars:          map[string]string{VarKeyword: keyword, VarPage: fmt.Sprint(page)},
		PreserveQuery: nextURL != "",
	}
	resp, err := p.Fetcher.Fetch(ctx, p.Doc, op, apiURL, target)
	if err != nil {
		return nil, err
	}
	items, next, err := ExtractItems(p.Doc, op, resp)
	if err != nil {
		return nil, err
	}
	return &ItemPage{Items: items, NextURL: next, HasNext: next != "", Garbled: resp.Garbled}, nil
}

// Detail 取一部作品的完整元信息。
//
// target 是详情页地址。它通常来自搜索结果里的 url 字段，而不是 api_url 拼出来的：
// 真实源的详情页地址形态由站点决定（Legado 的 Book.bookUrl、Mihon 的 manga.url）。
func (p *Pipeline) Detail(ctx context.Context, apiURL string, detailURL string) (*Item, error) {
	if p.Doc.Adapter == "zhaicool-web" {
		compiled, err := p.zhaicoolPipeline(detailURL)
		if err != nil {
			return nil, err
		}
		return compiled.Detail(ctx, apiURL, detailURL)
	}
	if p.Doc.Adapter == "bh3-web" {
		return p.bh3Detail(ctx, detailURL)
	}
	if p.Doc.Adapter == "terra-web" {
		return p.terraDetail(ctx, detailURL)
	}
	if p.Doc.Adapter == "fanqie-web" {
		return p.fanqieDetail(ctx, detailURL)
	}
	op := p.Doc.Operations.Detail
	if op == nil {
		return nil, apperror.New(apperror.Unsupported, "这个源没有声明详情操作")
	}
	if strings.TrimSpace(detailURL) == "" {
		return nil, apperror.New(apperror.Validation, "缺少详情页地址")
	}
	resp, err := p.Fetcher.Fetch(ctx, p.Doc, op, apiURL, Target{URL: detailURL})
	if err != nil {
		return nil, err
	}
	item, err := ExtractDetail(p.Doc, op, resp)
	if err != nil {
		return nil, err
	}
	if item.RemoteID == "" {
		item.RemoteID = RemoteID(detailURL)
	}
	return item, nil
}

// Toc 取完整目录，跟随分页直到取完或触到上限。
//
// 三处与 Legado 的实现刻意不同（依据都在 reports/06 §4）：
//
//   - **有页数上限**。Legado 的 while 循环没有数值上限，只靠 URL 去重与协程取消终止
//     （BookChapterList.kt:68-86），一条写错的 nextTocUrl 规则可以无限跟页。
//   - **去重集合存规范化后的 URL**，不是原始字符串。Legado 用 ArrayList.contains
//     比原始串，线性查找且"同一页的两个写法"会被当成两页。
//   - **空目录是错误**，不是"这本书没有章节"。Legado 也为它单独抛 TocEmptyException
//     （BookChapterList.kt:116-118），因为一次规则失效与一本真的完结无章的书
//     在界面上必须能分开。
func (p *Pipeline) Toc(ctx context.Context, apiURL string, item *Item) ([]*Chapter, error) {
	if p.Doc.Adapter == "zhaicool-web" {
		compiled, err := p.zhaicoolPipeline(item.URL)
		if err != nil {
			return nil, err
		}
		return compiled.Toc(ctx, apiURL, item)
	}
	if p.Doc.Adapter == "bh3-web" {
		return p.bh3Toc(ctx, item)
	}
	if p.Doc.Adapter == "terra-web" {
		return p.terraToc(ctx, item)
	}
	if p.Doc.Adapter == "fanqie-web" {
		return p.fanqieToc(ctx, item)
	}
	op := p.Doc.Operations.Toc
	if op == nil {
		return nil, apperror.New(apperror.Unsupported, "这个源没有声明目录操作")
	}
	startURL := item.TocURL
	if strings.TrimSpace(startURL) == "" {
		startURL = item.URL
	}
	if strings.TrimSpace(startURL) == "" {
		return nil, apperror.New(apperror.Validation, "这部作品没有目录页地址")
	}

	var (
		chapters []*Chapter
		visited  = map[string]bool{startURL: true}
		current  = startURL
	)
	for page := 0; page < p.Limits.MaxTocPages; page++ {
		if err := ctx.Err(); err != nil {
			return nil, apperror.Wrap(apperror.Cancelled, err, "目录加载已取消")
		}
		resp, err := p.Fetcher.Fetch(ctx, p.Doc, op, apiURL, Target{
			URL:  current,
			Vars: map[string]string{VarPage: fmt.Sprint(page + 1)},
		})
		if err != nil {
			return nil, err
		}
		batch, nexts, err := ExtractChapters(p.Doc, op, resp)
		if err != nil {
			return nil, err
		}
		chapters = append(chapters, batch...)

		next := p.pickUnvisited(nexts, visited)
		if next == "" {
			break
		}
		current = next
	}

	if len(chapters) == 0 {
		return nil, apperror.New(apperror.Corrupt,
			"目录解析出来是空的；可能是规则选错了元素，也可能这个源需要登录")
	}
	return p.normalizeChapters(chapters), nil
}

// pickUnvisited 从一页给出的若干个"下一页"地址里挑第一个没访问过的。
//
// 一页给出多个地址是真实形态（页底一次性列出全部页码）。Legado 在这种情况下
// 转并发抓取（BookChapterList.kt:90-114），但**仍然要按顺序合并**；
// 我们这里选择串行跟随第一个未访问的地址，理由是同一次目录刷新里
// 并发打源站十几请求的代价（被限流）高于串行多花的那几秒，
// 而目录刷新不是用户在盯着转圈的操作。
func (p *Pipeline) pickUnvisited(candidates []string, visited map[string]bool) string {
	for _, candidate := range candidates {
		if candidate == "" || visited[candidate] {
			continue
		}
		visited[candidate] = true
		return candidate
	}
	return ""
}

// normalizeChapters 翻正方向、去重、重排 ord。
//
// ord 一律由我们算，不用远端下标：Mihon 的 sourceOrder 直接取远端下标，
// 并在注释里要求源"MUST return the chapters from most to less recent"，
// 同时不做任何方向检测（SyncChaptersWithSource.kt:195-196）——倒序源会让它整个反转。
// 我们的方向由源文档显式声明（Document.Reverse）。
func (p *Pipeline) normalizeChapters(chapters []*Chapter) []*Chapter {
	if p.Doc.Reverse {
		reversed := make([]*Chapter, 0, len(chapters))
		for i := len(chapters) - 1; i >= 0; i-- {
			reversed = append(reversed, chapters[i])
		}
		chapters = reversed
	}
	// 去重键只用 remote_id。Legado 那边用 LinkedHashSet<BookChapter>，
	// 而 BookChapter 是 data class、equals 覆盖全部字段（BookChapterList.kt:123-125）：
	// 任何一个字段有差异就去重失败，这是个脆的判据。
	seen := make(map[string]bool, len(chapters))
	out := make([]*Chapter, 0, len(chapters))
	for _, chapter := range chapters {
		if chapter == nil || chapter.RemoteID == "" || seen[chapter.RemoteID] {
			continue
		}
		seen[chapter.RemoteID] = true
		chapter.Ord = len(out)
		out = append(out, chapter)
	}
	return out
}

// ExtractChapters 从一页目录里取出章节与"下一页"地址（可能多个）。
func ExtractChapters(doc *Document, op *Operation, resp *Response) ([]*Chapter, []string, error) {
	if err := guardFormat(op, resp); err != nil {
		return nil, nil, err
	}
	extractor, err := NewExtractor(op.Format, resp.Text)
	if err != nil {
		return nil, nil, apperror.Wrap(apperror.Corrupt, err, "解析目录页失败")
	}
	scope, err := rootScope(extractor, op)
	if err != nil {
		return nil, nil, err
	}
	nodes, err := extractor.Nodes(scope, op.List)
	if err != nil {
		return nil, nil, apperror.Wrap(apperror.Corrupt, err, "取章节列表失败")
	}
	chapters := make([]*Chapter, 0, len(nodes))
	for _, node := range nodes {
		chapter := &Chapter{Access: model.ChapterAccessUnknown}
		for _, binding := range []struct {
			name   string
			target *string
		}{
			{FieldTitle, &chapter.Title}, {FieldChapterURL, &chapter.URL},
			{FieldVolume, &chapter.Volume}, {FieldUploadedAt, &chapter.UploadedAt},
			{FieldID, &chapter.RemoteID},
		} {
			value, err := field(extractor, node, op, binding.name, resp.FinalURL)
			if err != nil {
				return nil, nil, err
			}
			*binding.target = value
		}
		// 没有标题的行不是章节：目录页里的卷分隔、广告位、"返回顶部"都会
		// 被一个宽泛的选择器选中，靠"没有标题"把它们筛掉。
		// Legado 也是丢弃空标题的条目（BookChapterList.kt:245-255）。
		if chapter.Title == "" {
			continue
		}
		if flag, err := field(extractor, node, op, FieldIsVolume, resp.FinalURL); err != nil {
			return nil, nil, err
		} else {
			chapter.IsVolume = isTruthy(flag)
		}
		if access, err := field(extractor, node, op, FieldAccess, resp.FinalURL); err != nil {
			return nil, nil, err
		} else {
			chapter.Access = model.NormalizeAccess(model.ChapterAccess(strings.ToLower(strings.TrimSpace(access))))
		}
		if chapter.RemoteID == "" {
			chapter.RemoteID = RemoteID(chapter.URL)
		}
		if chapter.RemoteID == "" {
			// 既没有显式 id 也没有 URL：用"标题 + 页内序号"当身份。
			// Legado 对卷章节也是这么兜底的（BookChapterList.kt:230-244）。
			// 这个身份不稳（改标题就变），但比丢掉整章好，
			// 而且卷标题行本来就没有正文可取。
			chapter.RemoteID = "title:" + strings.ToLower(strings.Join(strings.Fields(chapter.Title), " "))
		}
		chapters = append(chapters, chapter)
	}
	nexts, err := nextURLs(extractor, scope, op, resp.FinalURL)
	if err != nil {
		return nil, nil, err
	}
	return chapters, nexts, nil
}

// nextURLs 取出"下一页"地址，可能多于一个。
func nextURLs(extractor *Extractor, scope any, op *Operation, baseURL string) ([]string, error) {
	if op.Next == nil {
		return nil, nil
	}
	values, err := extractor.Strings(scope, op.Next)
	if err != nil {
		return nil, apperror.Wrap(apperror.Corrupt, err, "取下一页地址失败")
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if absolute := Absolutize(baseURL, value); absolute != "" {
			out = append(out, absolute)
		}
	}
	return out, nil
}

// isTruthy 判定一个源给出的布尔字段。
//
// 认的写法比较宽：源里可能是 "1"、"true"、"是"、"VIP"、"✓"，也可能是一个
// 只要存在就代表真的属性。空串与明确的否义词才算假。
func isTruthy(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "0", "false", "no", "n", "否", "无", "null", "nil":
		return false
	}
	return true
}

// Content 取一章正文，跟随章内分页。
//
// nextChapterURL 是下一章的地址，用来防止把下一章吞进本章。Legado 的做法是
// 绝对化之后做裸字符串相等比较（BookContent.kt:86-89），扛不住签名参数变化；
// 我们比的是归一化后的 remote_id（SameTarget）。
func (p *Pipeline) Content(ctx context.Context, apiURL string, chapter *Chapter, nextChapterURL string) (*TextContent, error) {
	if p.Doc.Adapter == "zhaicool-web" {
		if chapter == nil {
			return nil, apperror.New(apperror.Validation, "这一章没有可请求的地址")
		}
		compiled, err := p.zhaicoolPipeline(chapter.URL)
		if err != nil {
			return nil, err
		}
		return compiled.Content(ctx, apiURL, chapter, nextChapterURL)
	}
	if p.Doc.Adapter == "fanqie-web" {
		return p.fanqieContent(ctx, chapter)
	}
	op := p.Doc.Operations.Content
	if op == nil {
		return nil, apperror.New(apperror.Unsupported, "这个源没有声明正文操作")
	}
	if chapter == nil || strings.TrimSpace(chapter.URL) == "" {
		return nil, apperror.New(apperror.Validation, "这一章没有可请求的地址")
	}
	if chapter.IsVolume {
		// 卷标题行没有正文，这不是错误。Legado 同样豁免它的判空
		// （BookContent.kt:149：isVolume 时不抛 ContentEmptyException）。
		return &TextContent{}, nil
	}

	var (
		paragraphs []string
		visited    = map[string]bool{chapter.URL: true}
		current    = chapter.URL
		total      int
		truncated  bool
		garbled    bool
		obfuscated bool
		nextURL    string
	)
	for page := 0; page < p.Limits.MaxContentPages; page++ {
		if err := ctx.Err(); err != nil {
			return nil, apperror.Wrap(apperror.Cancelled, err, "正文加载已取消")
		}
		resp, err := p.Fetcher.Fetch(ctx, p.Doc, op, apiURL, Target{
			URL:  current,
			Vars: map[string]string{VarPage: fmt.Sprint(page + 1), VarChapterURL: chapter.URL},
		})
		if err != nil {
			return nil, err
		}
		text, nexts, err := ExtractText(p.Doc, op, resp)
		if err != nil {
			return nil, err
		}
		if err := guardTextPage(resp, text); err != nil {
			return nil, err
		}
		garbled = garbled || resp.Garbled
		obfuscated = obfuscated || DetectFontObfuscation(text)
		for _, line := range SplitParagraphs(text) {
			paragraphs = append(paragraphs, line)
			total += len(line)
		}
		if total > p.Limits.MaxContentBytes {
			// 触到上限就停，但要**明确报告**被截断了：让用户以为书就这么长，
			// 比报一个错更糟。
			truncated = true
			break
		}
		next := p.pickContentNext(nexts, visited, resp.FinalURL, nextChapterURL)
		if next == "" {
			break
		}
		nextURL = next
		current = next
	}

	if len(paragraphs) == 0 {
		// 正文为空是错误，不是"这一章没内容"。Legado 为它单独抛
		// ContentEmptyException（BookContent.kt:149-151）；把它当空正文缓存下来，
		// 用户下次进来还是空的，而真正的原因是规则失效或需要登录。
		return nil, apperror.New(apperror.Corrupt,
			"这一章没有解析出正文；可能是规则选错了元素、这一章需要登录，或者源站把它移走了")
	}
	return &TextContent{
		Paragraphs: paragraphs, NextURL: nextURL, Truncated: truncated,
		Garbled: garbled, FontObfuscated: obfuscated,
	}, nil
}

// pickContentNext 挑章内下一页，同时排除下一章。
func (p *Pipeline) pickContentNext(candidates []string, visited map[string]bool, baseURL, nextChapterURL string) string {
	for _, candidate := range candidates {
		if candidate == "" || visited[candidate] {
			continue
		}
		// 这一条是"别把下一章吞进本章正文"的关键。Legado 用绝对化后的裸字符串
		// 相等来判（BookContent.kt:86-89），签名参数一变就拦不住；
		// 我们比归一化后的身份。
		if nextChapterURL != "" && SameTarget(baseURL, candidate, nextChapterURL) {
			return ""
		}
		visited[candidate] = true
		return candidate
	}
	return ""
}

// ExtractText 从一页里取出正文文本与"下一页"地址。
func ExtractText(doc *Document, op *Operation, resp *Response) (string, []string, error) {
	if err := guardFormat(op, resp); err != nil {
		return "", nil, err
	}
	extractor, err := NewExtractor(op.Format, resp.Text)
	if err != nil {
		return "", nil, apperror.Wrap(apperror.Corrupt, err, "解析正文页失败")
	}
	scope, err := rootScope(extractor, op)
	if err != nil {
		return "", nil, err
	}
	text, err := extractor.String(scope, op.Text)
	if err != nil {
		return "", nil, apperror.Wrap(apperror.Corrupt, err, "取正文失败")
	}
	nexts, err := nextURLs(extractor, scope, op, resp.FinalURL)
	if err != nil {
		return "", nil, err
	}
	return text, nexts, nil
}

// paragraphSplitRe 按一个或多个换行切段。
//
// 只认换行是因为换行是 HTML 提取阶段刻意保留的唯一结构信号：textOf 在块级元素
// 前后与 <br> 处补换行，其余空白已经被 normalizeWhitespace 压掉了。
// 再按标点或长度切一次就是在猜作者的意图，猜错的代价是段落断裂。
var paragraphSplitRe = regexp.MustCompile(`\n+`)

// SplitParagraphs 把正文文本切成段落。
//
// 这里**不做任何排版加工**：不加缩进、不补空行、不改标点。Legado 在清洗阶段
// 就给每行补两个全角空格（BookContent.kt:139-144），那是把排版烧进了数据——
// 换个字号、换个阅读模式、或者用户想复制一段原文，那个缩进都改不回来。
// 缩进属于阅读视图（ADR 0016）。
func SplitParagraphs(text string) []string {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	parts := paragraphSplitRe.Split(text, -1)
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		// 行内的多余空白仍然要压掉：HTML 源码里的缩进与换行不是内容，
		// 而 <br> 分隔的段落内部可能残留多个空格。
		trimmed := strings.Join(strings.Fields(part), " ")
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}
	return out
}

// Pages 取一话的图片列表，跟随分页。
//
// 每一步都记下"这一页是从哪个地址取到的"，作为图片请求的 Referer：
// reports/03:97 要求 source headers 一路保留到实际资源请求，
// 而真实漫画 CDN 校验的往往就是那个章节页地址。
func (p *Pipeline) Pages(ctx context.Context, apiURL string, chapter *Chapter) ([]*Page, error) {
	if chapter == nil {
		return nil, apperror.New(apperror.Validation, "缺少漫画章节")
	}
	if p.Doc.Adapter == "bh3-web" {
		return p.bh3Pages(ctx, chapter)
	}
	if p.Doc.Adapter == "terra-web" {
		return p.terraPages(ctx, chapter)
	}
	op := p.Doc.Operations.Pages
	if op == nil {
		return nil, apperror.New(apperror.Unsupported, "这个源没有声明图片列表操作")
	}
	if chapter == nil || strings.TrimSpace(chapter.URL) == "" {
		return nil, apperror.New(apperror.Validation, "这一话没有可请求的地址")
	}

	var (
		pages   []*Page
		visited = map[string]bool{chapter.URL: true}
		current = chapter.URL
	)
	for page := 0; page < p.Limits.MaxContentPages; page++ {
		if err := ctx.Err(); err != nil {
			return nil, apperror.Wrap(apperror.Cancelled, err, "图片列表加载已取消")
		}
		resp, err := p.Fetcher.Fetch(ctx, p.Doc, op, apiURL, Target{
			URL:  current,
			Vars: map[string]string{VarPage: fmt.Sprint(page + 1), VarChapterURL: chapter.URL},
		})
		if err != nil {
			return nil, err
		}
		batch, nexts, err := ExtractPages(p.Doc, op, resp)
		if err != nil {
			return nil, err
		}
		pages = append(pages, batch...)
		// 一页给出多个"下一页"时并发抓取、按序合并。
		// Legado 在这种情况下也转并发（BookContent.kt:129 传 getNextPageUrl=false，
		// 用 mapAsync(threadCount) 且靠 channelFlow 的有序 await 保证输出顺序）。
		// 我们照做，但并发度取 Limits.Concurrency 而不是 16。
		extra := make([]string, 0, len(nexts))
		for _, candidate := range nexts {
			if candidate == "" || visited[candidate] {
				continue
			}
			visited[candidate] = true
			extra = append(extra, candidate)
		}
		if len(extra) == 0 {
			break
		}
		if len(extra) == 1 {
			current = extra[0]
			continue
		}
		batches, err := p.fetchPagesConcurrently(ctx, apiURL, op, extra)
		if err != nil {
			return nil, err
		}
		for _, batch := range batches {
			pages = append(pages, batch...)
		}
		// 并发分支是一次性列表，不再递归跟随：Legado 同样在这一支关掉
		// getNextPageUrl（BookContent.kt:129），否则会与串行分支重复跟页。
		break
	}

	if len(pages) == 0 {
		return nil, apperror.New(apperror.Corrupt,
			"这一话没有解析出任何图片；可能是规则选错了元素，也可能这个源需要登录")
	}
	// 重排 index：分页合并之后序号必须连续，否则阅读进度与预取窗口都会错位。
	// Mihon 的加载器也是"重新按列表顺序建立 index"（HttpPageLoader.kt:43-103）。
	for i, page := range pages {
		page.Index = i
	}
	return pages, nil
}

// fetchPagesConcurrently 并发抓取若干页并按输入顺序返回。
//
// 完成可以乱序，返回必须有序：正文与图片的顺序是内容的一部分，
// 乱序合并出来的章节读起来是错乱的。
func (p *Pipeline) fetchPagesConcurrently(ctx context.Context, apiURL string, op *Operation, urls []string) ([][]*Page, error) {
	concurrency := p.Limits.Concurrency
	if concurrency < 1 {
		concurrency = 1
	}
	if concurrency > len(urls) {
		concurrency = len(urls)
	}
	results := make([][]*Page, len(urls))
	errs := make([]error, len(urls))
	semaphore := make(chan struct{}, concurrency)
	var group sync.WaitGroup
	for i, target := range urls {
		group.Add(1)
		go func(index int, url string) {
			defer group.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			if ctx.Err() != nil {
				errs[index] = ctx.Err()
				return
			}
			resp, err := p.Fetcher.Fetch(ctx, p.Doc, op, apiURL, Target{URL: url})
			if err != nil {
				errs[index] = err
				return
			}
			batch, _, err := ExtractPages(p.Doc, op, resp)
			if err != nil {
				errs[index] = err
				return
			}
			results[index] = batch
		}(i, target)
	}
	group.Wait()
	for i, err := range errs {
		if err != nil {
			// 一页失败就整次失败：图片列表缺中间一页，读起来是断的，
			// 而"少一页但显示成功"比报错更难查。
			return nil, fmt.Errorf("第 %d 页图片列表：%w", i+1, err)
		}
	}
	return results, nil
}

// ExtractPages 从一页里取出图片条目与"下一页"地址。
//
// 图片地址有两种形态，一个源可能只给其一：
//
//   - **image_url**：这一页直接给出了图片最终地址，一步到位。
//   - **page_url**：这一页只给出一个"解析页"地址，还要再请求一次才拿得到图片。
//     这就是 reports/03:95 说的两步解析，第二步由 Pipeline.ResolveImage 做。
//
// 两者都没有的条目被丢弃：一个既没有图片也没有解析页的条目，
// 阅读视图拿到手只能显示空白，而那看起来像"这一页加载失败"。
func ExtractPages(doc *Document, op *Operation, resp *Response) ([]*Page, []string, error) {
	if err := guardFormat(op, resp); err != nil {
		return nil, nil, err
	}
	extractor, err := NewExtractor(op.Format, resp.Text)
	if err != nil {
		return nil, nil, apperror.Wrap(apperror.Corrupt, err, "解析图片列表失败")
	}
	scope, err := rootScope(extractor, op)
	if err != nil {
		return nil, nil, err
	}
	nodes, err := extractor.Nodes(scope, op.List)
	if err != nil {
		return nil, nil, apperror.Wrap(apperror.Corrupt, err, "取图片列表失败")
	}
	pages := make([]*Page, 0, len(nodes))
	for _, node := range nodes {
		page := &Page{Referer: resp.FinalURL}
		for _, binding := range []struct {
			name   string
			target *string
		}{
			{FieldImageURL, &page.ImageURL}, {FieldPageURL, &page.PageURL},
		} {
			value, err := field(extractor, node, op, binding.name, resp.FinalURL)
			if err != nil {
				return nil, nil, err
			}
			*binding.target = value
		}
		if page.ImageURL == "" && page.PageURL == "" {
			continue
		}
		pages = append(pages, page)
	}
	nexts, err := nextURLs(extractor, scope, op, resp.FinalURL)
	if err != nil {
		return nil, nil, err
	}
	return pages, nexts, nil
}

// ResolveImage 做两步解析的第二步：解析页地址 → 图片最终地址。
//
// 源能直接给出 image_url 时不需要这一步（Pipeline.Pages 的调用方据此判断）。
// 拿到的地址要按**解析页的最终地址**做绝对化，而不是按章节页：
// 解析页自己可能也重定向过。
func (p *Pipeline) ResolveImage(ctx context.Context, apiURL string, page *Page) (string, error) {
	op := p.Doc.Operations.Image
	if op == nil {
		return "", apperror.New(apperror.Unsupported,
			"这一页只给出了解析页地址，但这个源没有声明 image 操作来做第二步解析")
	}
	if page == nil || strings.TrimSpace(page.PageURL) == "" {
		return "", apperror.New(apperror.Validation, "这一页没有解析页地址")
	}
	resp, err := p.Fetcher.Fetch(ctx, p.Doc, op, apiURL, Target{
		URL:  page.PageURL,
		Vars: map[string]string{VarPageURL: page.PageURL, VarChapterURL: page.Referer},
	})
	if err != nil {
		return "", err
	}
	if err := guardFormat(op, resp); err != nil {
		return "", err
	}
	extractor, err := NewExtractor(op.Format, resp.Text)
	if err != nil {
		return "", apperror.Wrap(apperror.Corrupt, err, "解析图片地址失败")
	}
	scope, err := rootScope(extractor, op)
	if err != nil {
		return "", err
	}
	// 第二步只有一个输出：图片最终地址。用 text 选择器承载它，
	// 因为 image_url 语义上属于"列表里的每一页"，而这里没有列表。
	selector := op.Text
	if selector == nil {
		if named, ok := op.Fields[FieldImageURL]; ok {
			selector = &named
		}
	}
	if selector == nil {
		return "", apperror.New(apperror.Validation,
			"image 操作既没有声明 text 也没有声明 image_url 字段，取不出图片地址")
	}
	value, err := extractor.String(scope, selector)
	if err != nil {
		return "", apperror.Wrap(apperror.Corrupt, err, "取图片地址失败")
	}
	resolved := Absolutize(resp.FinalURL, value)
	if resolved == "" {
		return "", apperror.New(apperror.Corrupt,
			"解析页里没有取出可用的图片地址；可能这一页需要登录，或者规则选错了元素")
	}
	return resolved, nil
}
