package provider

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"cczjVideo/app/apperror"
	"cczjVideo/app/model"
)

// Item 是一部作品（小说的书 / 漫画的作品）从一次响应里提取出来的结果。
type Item struct {
	RemoteID string
	Title    string
	Author   string
	Cover    string
	Summary  string
	Genres   string
	Status   string
	Latest   string
	Words    string
	// UpdatedAt 是源给的原文，不换算单位（见 model.LibraryItem.RemoteUpdatedAt）。
	UpdatedAt string
	// URL 是详情页地址，TocURL 是目录页地址。两个都要留着：Legado 的详情渲染
	// 可以复用已存的 infoHtml，所以"目录页与详情页是同一个地址"是常见情况
	// （WebBook.kt:152-269），这时候少发一次请求。
	URL    string
	TocURL string
}

// Catalog-backed sources share title/author filtering. Source-specific aliases
// stay with their adapters; a metadata hit is not proof of readable content.
func filterCatalogItems(items []*Item, keyword, alias string) []*Item {
	query := strings.ToLower(strings.TrimSpace(keyword))
	var out []*Item
	for _, item := range items {
		if query == "" || strings.Contains(strings.ToLower(alias), query) || strings.Contains(strings.ToLower(item.Title), query) || strings.Contains(strings.ToLower(item.Author), query) {
			out = append(out, item)
		}
	}
	return out
}

// Chapter 是目录里的一章。
type Chapter struct {
	RemoteID string
	Title    string
	Volume   string
	URL      string
	// Ord 是展示序，从 0 开始，已按 Reverse 翻正。
	Ord      int
	IsVolume bool
	Access   model.ChapterAccess
	// UploadedAt 是源给的原文。
	UploadedAt string
}

// TextContent 是一章正文。
type TextContent struct {
	// Paragraphs 是按段落切好的正文。切分只认换行，不做任何排版加工：
	// 缩进、行距、分页都是阅读视图的事（ADR 0016）。Legado 在清洗阶段就给
	// 每行补两个全角空格（BookContent.kt:139-144），那是把排版烧进了数据，
	// 换个字号或换个阅读模式就改不回来了。
	Paragraphs []string
	// NextURL 是章内下一页地址，空表示本章已经取完。
	NextURL string
	// Truncated 报告正文因为超过上限被截断。它是**显式信号**：
	// 界面要能说"这一章可能不完整"，而不是让用户以为书就这么长。
	Truncated bool
	// Garbled 报告解码结果像是编码错了，FontObfuscated 报告像是被字体码位混淆过。
	// 两条都只是提示，不阻止显示——判错了至少用户还能看到内容。
	Garbled        bool
	FontObfuscated bool
}

// Page 是漫画的一页。
type Page struct {
	Index int
	// PageURL 是解析页地址，ImageURL 是图片最终地址。
	// 两者可以只有其一：源直接给图片地址时不需要第二步解析（reports/03:95）。
	PageURL  string
	ImageURL string
	// Referer 是取到这一页时所在页面的地址。它必须一路带到实际图片请求上：
	// reports/03:97 明确指出下载器不能在拿到 imageUrl 之后丢掉这些上下文。
	Referer string
}

// ---------------------------------------------------------------------------
// 响应体的第一道判定
// ---------------------------------------------------------------------------

// 这一组判据是自己定的，两个参照项目都没有可抄的实现：Legado 只有源自带的
// loginCheckJs 钩子（判据由书源 JS 决定），Mihon 只有 Cloudflare 挑战拦截
// （靠响应头 cf-mitigated: challenge + Server: cloudflare，不看状态码）。
// 详见 reports/06 §4.4。
var (
	htmlDoctypeRe = regexp.MustCompile(`(?i)^\s*(<!doctype\s+html|<html[\s>]|<head[\s>]|<body[\s>])`)
	loginFormRe   = regexp.MustCompile(`(?i)<input[^>]+type\s*=\s*["']?password`)
	loginTitleRe  = regexp.MustCompile(`(?i)<title[^>]*>[^<]*(登录|登入|登陆|sign\s*in|log\s*in|login)[^<]*</title>`)
	captchaRe     = regexp.MustCompile(`(?i)(captcha|验证码|人机验证|滑动验证|安全验证)`)
)

// guardFormat 在解析之前先确认响应体是不是配置声明的那种形态。
//
// 这一条对应视频采集器已有的非 JSON 预检（collect/fetcher.go:268-276）：
// 源站限流或被墙时会返回 200 + HTML，直接按 JSON 解析会得到一句看不懂的错误。
// 分成两条不同的错误码是有意义的——"源返回了错误页"该重试或换源，
// "配置写错了 format"该改配置，界面给的处置不一样。
func guardFormat(op *Operation, resp *Response) error {
	body := strings.TrimSpace(resp.Text)
	if body == "" {
		return apperror.New(apperror.Corrupt, "源站返回了空响应")
	}
	if op.Format == "json" {
		if body[0] != '{' && body[0] != '[' {
			if looksLikeLogin(body) {
				return apperror.New(apperror.Unsupported,
					"源站返回的是一个登录/验证页而不是数据；这个源可能需要登录，本项目不支持登录态")
			}
			return apperror.New(apperror.Corrupt,
				"源站返回的不是 JSON（配置声明的是 json）；多半是错误页、限流页或规则里的地址写错了")
		}
		return nil
	}
	// html 侧反过来判：一个 JSON 源被配成 html 会解析出一棵几乎空的树，
	// 结果是"搜不到任何东西"，比报错难查得多。
	if body[0] == '{' || body[0] == '[' {
		return apperror.New(apperror.Corrupt,
			"源站返回的是 JSON，但配置声明的是 html；把 format 改成 json")
	}
	return nil
}

func looksLikeLogin(body string) bool {
	probe := body
	if len(probe) > 8192 {
		probe = probe[:8192]
	}
	return loginFormRe.MatchString(probe) || loginTitleRe.MatchString(probe) || captchaRe.MatchString(probe)
}

// guardTextPage 在正文提取之后再看一眼：拿到的是不是根本不像正文。
//
// 判据是"很短 + 有登录/验证特征"。单独一条短不足以判死——有的章本来就短，
// 所以短只降级成提示，短且有特征才报错。
func guardTextPage(resp *Response, text string) error {
	if looksLikeLogin(resp.Text) && len([]rune(text)) < 200 {
		return apperror.New(apperror.Unsupported,
			"这一章取回来的像是登录/验证页而不是正文；这个源可能需要登录，本项目不支持登录态")
	}
	return nil
}

// ---------------------------------------------------------------------------
// 单页提取
// ---------------------------------------------------------------------------

// ExtractItems 从一页搜索/浏览结果里取出作品列表与下一页地址。
func ExtractItems(doc *Document, op *Operation, resp *Response) ([]*Item, string, error) {
	if err := guardFormat(op, resp); err != nil {
		return nil, "", err
	}
	extractor, err := NewExtractor(op.Format, resp.Text)
	if err != nil {
		return nil, "", apperror.Wrap(apperror.Corrupt, err, "解析响应失败")
	}
	scope, err := rootScope(extractor, op)
	if err != nil {
		return nil, "", err
	}
	nodes, err := extractor.Nodes(scope, op.List)
	if err != nil {
		return nil, "", apperror.Wrap(apperror.Corrupt, err, "取作品列表失败")
	}
	items := make([]*Item, 0, len(nodes))
	for _, node := range nodes {
		item, err := extractItem(extractor, node, op, resp.FinalURL)
		if err != nil {
			return nil, "", err
		}
		// 没有标题的条目直接丢弃而不是留一条空标题的幽灵：Legado 也这么做
		// （BookList.kt:220-222，name 为空则整条结果被丢弃）。
		if item == nil {
			continue
		}
		// 没有稳定身份的条目也丢弃：存进去会在
		// UNIQUE(kind,source_key,remote_id) 上与下一条互撞，
		// 而界面上它是一个点进去什么也做不了的行。
		if item.RemoteID == "" {
			continue
		}
		items = append(items, item)
	}
	next, err := extractor.String(scope, op.Next)
	if err != nil {
		return nil, "", apperror.Wrap(apperror.Corrupt, err, "取下一页地址失败")
	}
	return items, Absolutize(resp.FinalURL, next), nil
}

// ExtractDetail 从一个详情页取出作品元信息。
func ExtractDetail(doc *Document, op *Operation, resp *Response) (*Item, error) {
	if err := guardFormat(op, resp); err != nil {
		return nil, err
	}
	extractor, err := NewExtractor(op.Format, resp.Text)
	if err != nil {
		return nil, apperror.Wrap(apperror.Corrupt, err, "解析响应失败")
	}
	scope, err := rootScope(extractor, op)
	if err != nil {
		return nil, err
	}
	item, err := extractItem(extractor, scope, op, resp.FinalURL)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, apperror.New(apperror.Corrupt, "详情页没有解析出书名；规则可能选错了元素")
	}
	// 详情页本身就是一个可用的目录入口：toc_url 缺失时退回当前地址。
	// Legado 在这种情况下会复用已经下载的 HTML，不再发一次请求
	// （BookInfo.kt:148-155）。
	if item.TocURL == "" {
		item.TocURL = resp.FinalURL
	}
	if item.URL == "" {
		item.URL = resp.FinalURL
	}
	// 补完地址之后再算身份：详情页常常不声明 url 字段（当前这一页就是详情页），
	// 在 extractItem 里判会误杀这种完全正常的配置。
	if item.RemoteID == "" {
		item.RemoteID = RemoteID(item.URL)
	}
	if item.RemoteID == "" {
		return nil, apperror.New(apperror.Corrupt,
			"详情页解析不出稳定标识（既没有 id 字段也没有可用地址）；这一条没法入库，否则会与下一条撞在同一个身份上")
	}
	return item, nil
}

// urlFields 是取值之后必须做绝对化的字段。
//
// 分成两类是必要的：源给出的地址大多是相对的，不绝对化就没法请求；
// 而文本字段一旦被"绝对化"，一个恰好长得像路径的标题会被 url.Parse 改坏。
var urlFields = map[string]bool{
	FieldCover: true, FieldURL: true, FieldTocURL: true, FieldChapterURL: true,
	FieldPageURL: true, FieldImageURL: true, FieldNext: true,
}

// rootScope 执行 root 预处理：先把响应缩到一个子树，后续所有选择器相对它求值。
//
// 对应 Legado 的 ruleBookInfo.init（BookInfo.kt:58-64）。详情页里目录区块与
// 书籍信息混在一起时，先切出子树能让后面的选择器简单一个量级，
// 也让"页面里别处还有一个同名 class"这种巧合不再干扰结果。
func rootScope(extractor *Extractor, op *Operation) (any, error) {
	if op.Root == nil {
		return extractor.Root(), nil
	}
	nodes, err := extractor.Nodes(extractor.Root(), op.Root)
	if err != nil {
		return nil, apperror.Wrap(apperror.Corrupt, err, "预处理（root）失败")
	}
	if len(nodes) == 0 {
		return nil, apperror.New(apperror.Corrupt,
			"预处理（root）没有选中任何元素；后续选择器都相对它求值，所以这一步选错等于整个操作都取不到东西")
	}
	return nodes[0], nil
}

// field 取出一个标准字段的值。地址类字段按 baseURL 绝对化。
//
// 两类失败必须分开处置：
//
//   - **响应里没有这个键**（ErrPathMissing）→ 返回空串，不报错。一本书没有作者、
//     一章没有卷名都是常事，为它中断整次解析，用户看到的是"这个源搜不出东西"。
//   - **配置指错了层级**（路径指到对象上、选择器语法错）→ 报错。这种错静默返回
//     空串的话，界面上永远是"这个源没有作者"，用户以为是源的问题、
//     实际是配置的问题，永远修不好。
func field(extractor *Extractor, scope any, op *Operation, name, baseURL string) (string, error) {
	selector, ok := op.Fields[name]
	if !ok {
		return "", nil
	}
	value, err := extractor.String(scope, &selector)
	if err != nil {
		if errors.Is(err, ErrPathMissing) {
			return "", nil
		}
		return "", fmt.Errorf("字段 %s：%w", name, err)
	}
	value = strings.TrimSpace(value)
	if urlFields[name] {
		return Absolutize(baseURL, value), nil
	}
	return value, nil
}

func extractItem(extractor *Extractor, scope any, op *Operation, baseURL string) (*Item, error) {
	item := &Item{}
	for _, binding := range []struct {
		name   string
		target *string
	}{
		{FieldTitle, &item.Title}, {FieldAuthor, &item.Author}, {FieldCover, &item.Cover},
		{FieldSummary, &item.Summary}, {FieldGenres, &item.Genres}, {FieldStatus, &item.Status},
		{FieldLatest, &item.Latest}, {FieldWords, &item.Words}, {FieldUpdatedAt, &item.UpdatedAt},
		{FieldURL, &item.URL}, {FieldTocURL, &item.TocURL}, {FieldID, &item.RemoteID},
	} {
		value, err := field(extractor, scope, op, binding.name, baseURL)
		if err != nil {
			return nil, err
		}
		*binding.target = value
	}
	// 没有标题的条目直接丢弃，而不是留一条空标题的幽灵：Legado 也这么做
	// （BookList.kt:220-222，name 为空则整条结果被丢弃）。
	if item.Title == "" {
		return nil, nil
	}
	// 没有显式 id 时退回用详情地址算身份。**这里不判空**：详情页的 url 字段
	// 常常没有声明（当前这一页就是详情页），要等 ExtractDetail 用 FinalURL
	// 补上之后才判得出有没有身份。在这一层提前返回 nil，会让"详情页规则没写
	// url 字段"这种完全正常的配置解析不出任何东西。
	if item.RemoteID == "" {
		item.RemoteID = RemoteID(item.URL)
	}
	return item, nil
}
