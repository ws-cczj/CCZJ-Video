// Package provider 解释小说与漫画源的声明式文档，把一次 HTTP 响应变成领域数据。
//
// 这个包存在的理由与它的边界都写在 ADR 0016 里，这里只记三条最容易被后来人改坏的：
//
//  1. **配置是结构化的，不是规则字符串。** Legado 需要一个 14KB 的引号/括号感知
//     切分器（RuleAnalyzer.kt:91-298），因为它把 &&、||、%%、@、## 全压进一个字符串，
//     而这些记号也可能合法地出现在 CSS 属性选择器、JSONPath 过滤表达式或正则里。
//     我们的选择器是一个不透明的字符串值，组合关系由 JSON 结构表达，所以这里
//     **永远不要**对选择器做任何字符串切分。
//
//  2. **不实现 JS 求值，也不为第三方源开 WebView。** 因此失去的能力是明确可枚举的
//     （reports/06 §2.2）。编译内置适配器可拥有专用协议/字体转换，声明文档不执行脚本。
//     文档里出现未支持字段时必须在导入阶段报出具名错误，
//     不能静默忽略：一个被静默忽略的规则在界面上表现为"这个源搜不出东西"，
//     与"源真的没有这本书"无法区分。严格解码（DisallowUnknownFields）负责这件事。
//
//  3. **空结果只代表一次成功的"没有匹配"。** 解析不出东西与源返回了登录页是两件事，
//     必须走不同的错误码（AGENTS.md），不能用空列表把后者盖掉。
package provider

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// DocumentVersion 是这个包认的文档版本。视频侧的 collect.StrategyConfig 是 v2，
// 两种文档共用 sources.strategy_config 一列，靠 version 与 sources.kind 一起区分，
// 谁也不会去解析另一种。
const DocumentVersion = 3

// Document 是一个小说或漫画源的完整声明。
type Document struct {
	Version int `json:"version"`
	// Adapter selects a compiled protocol implementation. It is never a script
	// URL, plugin name or executable supplied by the source document.
	Adapter string `json:"adapter,omitempty"`
	// Kind 必须与 sources.kind 一致。两处都存是为了让一份被复制粘贴到错误模式下的
	// 文档在导入时就被拒，而不是在运行时解析出一堆语义不对的字段。
	Kind string `json:"kind"`

	// Encoding 是响应解码的显式声明，空值表示自动探测。
	//
	// 这一级是 Legado 缺的：它的 URL option `charset` 只影响请求参数编码，
	// 完全不参与响应解码（AnalyzeUrl.kt:242,287-298），GBK 站能不能读对全看
	// 自动探测。我们的源是用户在表单里加的，给一个编码下拉是自然的，
	// 而探测失败时用户能自己救回来。
	Encoding string `json:"encoding,omitempty"`

	// Headers 是这个源所有请求都带的头。图片代理会拿同一份去发实际资源请求：
	// reports/03:97 要求 source headers 一路保留到资源请求，不能在拿到最终
	// 图片地址后丢掉。
	Headers map[string]string `json:"headers,omitempty"`

	// Reverse 声明远端目录是"从新到旧"返回的。为真时我们在写入前翻正，
	// ord 一律是"第几章"。
	//
	// 显式声明而不是自动检测：Mihon 直接拿远端下标当 sourceOrder，并在注释里
	// 要求源"MUST return the chapters from most to less recent"，同时不做任何
	// 方向检测（SyncChaptersWithSource.kt:195-196）——倒序源会让它的语义整个反转。
	Reverse bool `json:"reverse,omitempty"`

	// CheckKeyword 是"测试这个源通不通"用的关键词，源管理页那个按钮拿它发一次
	// 搜索并检查有没有结果。设计取自 Legado 的 ruleSearch.checkKeyWord。
	CheckKeyword string `json:"check_keyword,omitempty"`

	Operations Operations `json:"operations"`
}

// Operations 是四种模式各自需要的操作集合。
//
// 小说用 Search / Detail / Toc / Content；漫画用 Search / Detail / Toc / Pages，
// 以及可选的 Image（两步解析的第二步：章节页只给得出解析页地址时，
// 再请求一次才拿到真实图片 URL）。
type Operations struct {
	Search  *Operation `json:"search,omitempty"`
	Detail  *Operation `json:"detail,omitempty"`
	Toc     *Operation `json:"toc,omitempty"`
	Content *Operation `json:"content,omitempty"`
	Pages   *Operation `json:"pages,omitempty"`
	Image   *Operation `json:"image,omitempty"`
}

// Operation 是一次请求加一次提取。
type Operation struct {
	// Path 是相对源 api_url 的路径，可含 {name} 占位符。空表示直接用 api_url。
	Path string `json:"path,omitempty"`
	// Method 只认 GET 与 POST，空值按 GET。
	Method string `json:"method,omitempty"`
	// Params 是固定 query；ParamNames 声明各变量在 query 里叫什么名字，
	// 形状与视频侧 collect.ParamConfig 一致，为的是让两种源文档读起来是同一种语言。
	Params     map[string]string `json:"params,omitempty"`
	ParamNames ParamNames        `json:"param_names,omitempty"`
	// Body 是 POST 正文，可含 {name} 占位符。
	Body string `json:"body,omitempty"`
	// Headers 只作用于这一个操作，与 Document.Headers 合并，同名字段这里覆盖。
	Headers map[string]string `json:"headers,omitempty"`

	// Format 是响应形态，json 或 html。必填：靠嗅探判断会把"源返回了 HTML 错误页"
	// 与"这是个 HTML 源"混成一件事。
	Format string `json:"format"`
	// Root 先把响应缩到一个子树，后续所有选择器相对它求值。
	// 对应 Legado 的 ruleBookInfo.init（BookInfo.kt:58-64）：详情页里目录区块与
	// 书籍信息混在一起时，先切出子树能让后面的选择器简单一个量级。
	Root *Selector `json:"root,omitempty"`
	// List 取条目数组。搜索与目录用它；详情与正文不用。
	List *Selector `json:"list,omitempty"`
	// Fields 是"标准字段名 → 怎么取"。字段名见 StandardFields。
	Fields map[string]Selector `json:"fields,omitempty"`
	// Text 取正文，只有 Content 操作用。
	Text *Selector `json:"text,omitempty"`
	// Next 取下一页地址，目录分页与正文分页共用同一个形状。
	// 两者是**各自独立的循环**，各有各的去重集合与上限（reports/06 §4.1）。
	Next *Selector `json:"next,omitempty"`
}

// ParamNames 声明各变量在 query 里的名字。缺省值取 MAC-CMS 的习惯名，
// 与 collect.ParamConfig 保持一致，这样一份视频源文档与一份书源文档读起来是同一种语言。
type ParamNames struct {
	Keyword string `json:"keyword,omitempty"` // 默认 wd
	Page    string `json:"page,omitempty"`    // 默认 pg
	ID      string `json:"id,omitempty"`      // 默认 ids
	Limit   string `json:"limit,omitempty"`
}

// Selector 是"从一个响应里取出一个或一组值"的最小单元。
//
// 它要么是**叶子**（Path 或 Query，加上取值方式与可选的下标、正则；两者都不写
// 而只写 Attr 表示"就取当前这个节点"），要么是**组合**（Concat 或 Fallback，
// 各自装一组子 Selector）。两种形态互斥，Validate 会拒绝同时出现或全不出现的写法。
//
// 两个组合算子的语义取自 Legado 的 && 与 ||，但**语法不取**：
//
//   - Concat：依次执行各子选择器。用在**节点级**（list / root）时结果追加合并；
//     用在**取值级**（fields / text / next）时结果按 Separator 拼成**一个**字符串。
//     两种语义都是"拼接"的自然读法，而取值级那一种是真实需要的：
//     「<span class="num">第1话</span><span class="name">起风</span>」要合成
//     一个标题，而不是两个值里取第一个。
//   - Fallback：按序执行，**第一个返回非空结果的胜出并停止**，后续不再执行。
//     这不是"默认值"，也不是并集。源站改版时用得上：先试新选择器，
//     选不中再试旧的。
//
// Legado 的第三个算子 %%（按下标交错合并）**没有采纳**。实现时才发现它在结构化
// 格式里没有落点：用在 list 位置会交错出 [标题0,链接0,标题1,链接1,…]，
// 而 list 的每一项要能独立取出全部字段，于是产出一堆半成品条目；
// 用在取值位置则只有第一个值会被取走，剩下的被丢掉。它需要的是"配对"而不是
// "交错"，而那是一个我们没有频率证据支撑的能力（reports/05 任务 A 的
// 频率统计还没回来）。等有证据说明真实源用得上，再按"配对"的语义加，
// 而不是先放一个半工作的形态进来。
type Selector struct {
	// Path 是 JSON 侧的点分路径，与 collect.lookupPath 同一套语义：
	// 对象按键、数组按十进制下标，不做通配。
	Path string `json:"path,omitempty"`
	// Query 是 HTML 侧的 CSS 选择器。**不得对它做任何字符串切分**：
	// 一个合法的 CSS 选择器里可以出现引号、方括号、圆括号甚至 @。
	Query string `json:"query,omitempty"`
	// Attr 是取值方式：text（规范化文本，默认）、ownText（仅自身文本，不含子元素）、
	// html（内部 HTML，先剥掉 script 与 style）、all（原样 HTML），
	// 或任意属性名（href / src / content / data-x …）。
	Attr string `json:"attr,omitempty"`
	// Index 从选中的节点里挑出子集。空表示全要。
	Index *IndexSpec `json:"index,omitempty"`
	// Regex 对取出的每个字符串做一次替换。
	Regex *Replace `json:"regex,omitempty"`
	// Separator 只在 Concat 的取值级用到：把各子结果拼成一个字符串时的连接符。
	Separator string `json:"separator,omitempty"`

	Concat   []Selector `json:"concat,omitempty"`
	Fallback []Selector `json:"fallback,omitempty"`
	// Chain evaluates each selector relative to the preceding selected nodes.
	Chain []Selector `json:"chain,omitempty"`
}

// IndexSpec 挑节点子集。
//
// 只支持明确下标与排除，不支持区间与步长。Legado 那边有 [1:3]、[s:e:step]、[-1:0]
// 反转等一整套（AnalyzeByJSoup.kt:282-511），但真实源里用得到的几乎只有
// "取第 N 个"与"跳过开头那几个"——区间与步长在没有频率证据支撑前属于长尾，
// 需要时再加，加了也不算破坏兼容（新字段而已）。
//
// 负下标从尾部计，-1 是最后一个。
type IndexSpec struct {
	Indices []int `json:"indices"`
	// Exclude 为真时反过来：排除这些下标，其余全要。
	Exclude bool `json:"exclude,omitempty"`
}

// Replace 是一次正则替换。
//
// 刻意不复制 Legado 的 `###`（replaceFirst）语义：它的实现是先 find 第一个匹配，
// 然后**只返回那一处匹配子串的一次替换结果，其余整段丢弃**，无匹配时返回空串
// （AnalyzeRule.kt:441-452）。那不是"只替换第一处、保留其余原文"，是一个陷阱。
// 我们要 First 就是字面意思：只替换第一处，其余原文保留。
type Replace struct {
	Pattern     string `json:"pattern"`
	Replacement string `json:"replacement,omitempty"`
	First       bool   `json:"first,omitempty"`
}

// 标准字段名。提取层的输出键只用这些，源文档里的 Fields 也只认这些：
// 界面上显示什么、进度按什么关联，都不该因为换了一个源就换一套字段名。
const (
	FieldID         = "id"          // 远端稳定标识；缺失时用条目自身的 URL 规范化结果
	FieldTitle      = "title"       //
	FieldAuthor     = "author"      //
	FieldCover      = "cover"       //
	FieldSummary    = "summary"     //
	FieldGenres     = "genres"      //
	FieldStatus     = "status"      //
	FieldLatest     = "latest"      // 最新章节名
	FieldWords      = "words"       // 字数
	FieldUpdatedAt  = "updated_at"  // 源给的原文，不在这一层换算单位
	FieldURL        = "url"         // 详情页地址
	FieldTocURL     = "toc_url"     // 目录页地址；缺失时用详情页地址
	FieldChapterURL = "chapter_url" //
	FieldVolume     = "volume"      //
	FieldIsVolume   = "is_volume"   //
	FieldAccess     = "access"      //
	FieldUploadedAt = "uploaded_at" //
	FieldPageURL    = "page_url"    // 漫画：解析页地址
	FieldImageURL   = "image_url"   // 漫画：图片最终地址
	FieldNext       = "next"        // 下一页地址
)

// StandardFields 是全部合法字段名，Validate 拿它拒绝拼错的键。
//
// 拼错的字段名如果不拒，表现是"这个源能搜到书但没有作者"，
// 用户会以为是源的问题而不是自己写错了配置。
var StandardFields = map[string]bool{
	FieldID: true, FieldTitle: true, FieldAuthor: true, FieldCover: true,
	FieldSummary: true, FieldGenres: true, FieldStatus: true, FieldLatest: true,
	FieldWords: true, FieldUpdatedAt: true, FieldURL: true, FieldTocURL: true,
	FieldChapterURL: true, FieldVolume: true, FieldIsVolume: true, FieldAccess: true,
	FieldUploadedAt: true, FieldPageURL: true, FieldImageURL: true, FieldNext: true,
}

// 这些上限的用途与 collect 里那两条同源（response.go:47-50）：源文档是用户输入，
// 一份 10MB 的文档不该被走完，也不该被切成一百万段。
const (
	maxDocumentBytes    = 256 << 10
	maxSelectorPathLen  = 512
	maxSelectorQueryLen = 1024
	maxRegexPatternLen  = 1024
	maxFields           = 64
	maxCombineChildren  = 16
	maxSelectorDepth    = 8
	maxHeaders          = 16
	maxHeaderNameLen    = 128
	maxHeaderValueLen   = 2048
)

// ParseDocument 严格解析一份源文档。
//
// 严格解码（DisallowUnknownFields）是"不支持的字段要显式报错"这条要求的实现：
// 一份为别的引擎写的文档里带着 js / webView / preUpdateJs 之类的键时，
// 这里会报出**具体是哪个键**，而不是把它当不存在。
func ParseDocument(raw string, kind string) (*Document, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, fmt.Errorf("源文档为空")
	}
	if len(trimmed) > maxDocumentBytes {
		return nil, fmt.Errorf("源文档超过 %d 字节上限", maxDocumentBytes)
	}
	decoder := json.NewDecoder(bytes.NewReader([]byte(trimmed)))
	decoder.DisallowUnknownFields()
	var doc Document
	if err := decoder.Decode(&doc); err != nil {
		return nil, fmt.Errorf("源文档不是本应用认的格式：%w", err)
	}
	if err := doc.Validate(kind); err != nil {
		return nil, err
	}
	return &doc, nil
}

// Validate 检查文档自洽，并返回一份归一化后的副本语义（就地补齐缺省值）。
func (d *Document) Validate(kind string) error {
	if d == nil {
		return fmt.Errorf("源文档缺失")
	}
	if d.Version != DocumentVersion {
		return fmt.Errorf("源文档版本是 %d，本应用只认 %d", d.Version, DocumentVersion)
	}
	if strings.TrimSpace(d.Kind) == "" {
		d.Kind = kind
	}
	if d.Kind != kind {
		return fmt.Errorf("源文档声明的 kind 是 %q，与这个源的模式 %q 不一致", d.Kind, kind)
	}
	if err := validateHeaders(d.Headers); err != nil {
		return err
	}
	if d.Encoding != "" && !IsSupportedEncoding(d.Encoding) {
		return fmt.Errorf("encoding %q 不支持；可选 %s", d.Encoding, strings.Join(SupportedEncodings(), " / "))
	}
	if d.Adapter != "" {
		supported := ((d.Adapter == "fanqie-web" || d.Adapter == "zhaicool-web") && d.Kind == "novel") || ((d.Adapter == "bh3-web" || d.Adapter == "terra-web") && d.Kind == "comic")
		if !supported {
			return fmt.Errorf("不支持的内置适配器 %q 或模式 %q", d.Adapter, d.Kind)
		}
		if d.Operations != (Operations{}) {
			return fmt.Errorf("内置适配器不接受额外声明式操作")
		}
		return nil
	}

	ops := map[string]*Operation{
		"search": d.Operations.Search, "detail": d.Operations.Detail, "toc": d.Operations.Toc,
		"content": d.Operations.Content, "pages": d.Operations.Pages, "image": d.Operations.Image,
	}
	present := 0
	for name, op := range ops {
		if op == nil {
			continue
		}
		present++
		if err := op.Validate(name, d.Kind); err != nil {
			return fmt.Errorf("操作 %s：%w", name, err)
		}
	}
	if present == 0 {
		return fmt.Errorf("源文档没有声明任何操作")
	}
	// 小说必须能取到正文，漫画必须能取到图片列表；缺了就不是"这个源不支持"，
	// 而是这份文档写漏了，导入时就该拦住。
	if d.Kind == "novel" && d.Operations.Content == nil {
		return fmt.Errorf("小说源必须声明 content 操作")
	}
	if d.Kind == "comic" && d.Operations.Pages == nil {
		return fmt.Errorf("漫画源必须声明 pages 操作")
	}
	return nil
}

// ValidateSearchDocument validates a diagnostic-only static search document.
// It is not accepted by ParseDocument/import, which still requires content/pages.
func ValidateSearchDocument(d *Document) error {
	if d == nil || d.Version != DocumentVersion || (d.Kind != "novel" && d.Kind != "comic") || d.Adapter != "" {
		return fmt.Errorf("无效的静态搜索探测文档")
	}
	if err := validateHeaders(d.Headers); err != nil {
		return err
	}
	if d.Encoding != "" && !IsSupportedEncoding(d.Encoding) {
		return fmt.Errorf("未知编码")
	}
	return d.Operations.Search.Validate("search", d.Kind)
}

// Validate 检查一个操作。name 只用于报错时指认是哪一步。
func (o *Operation) Validate(name, kind string) error {
	if o == nil {
		return fmt.Errorf("操作缺失")
	}
	switch o.Format {
	case "json", "html":
	case "":
		return fmt.Errorf("format 必填，json 或 html；靠嗅探判断会把「源返回了 HTML 错误页」与「这是个 HTML 源」混成一件事")
	default:
		return fmt.Errorf("format %q 不支持，只认 json 与 html", o.Format)
	}
	switch strings.ToUpper(strings.TrimSpace(o.Method)) {
	case "", "GET", "POST":
	default:
		return fmt.Errorf("method %q 不支持，只认 GET 与 POST", o.Method)
	}
	if err := validateHeaders(o.Headers); err != nil {
		return err
	}
	if len(o.Fields) > maxFields {
		return fmt.Errorf("fields 有 %d 项，超过 %d 上限", len(o.Fields), maxFields)
	}
	for field, sel := range o.Fields {
		if !StandardFields[field] {
			return fmt.Errorf("字段名 %q 不是标准字段；拼错的字段名不会报错，只会让界面上那一格永远是空的", field)
		}
		if err := sel.Validate(1); err != nil {
			return fmt.Errorf("字段 %s：%w", field, err)
		}
	}
	for _, named := range []struct {
		label string
		sel   *Selector
	}{
		{"root", o.Root}, {"list", o.List}, {"text", o.Text}, {"next", o.Next},
	} {
		if named.sel == nil {
			continue
		}
		if err := named.sel.Validate(1); err != nil {
			return fmt.Errorf("%s：%w", named.label, err)
		}
	}
	// 正文操作必须能取出正文；目录操作必须有列表。
	switch name {
	case "content":
		if o.Text == nil {
			return fmt.Errorf("content 操作必须声明 text")
		}
	case "toc", "search", "pages":
		if o.List == nil {
			return fmt.Errorf("%s 操作必须声明 list", name)
		}
	}
	// 漫画的 pages 操作要能取出图片地址或解析页地址之一，否则两步解析的第一步无从下手。
	if name == "pages" && kind == "comic" {
		_, hasImage := o.Fields[FieldImageURL]
		_, hasPage := o.Fields[FieldPageURL]
		if !hasImage && !hasPage {
			return fmt.Errorf("pages 操作至少要声明 image_url 或 page_url 之一")
		}
	}
	return nil
}

// Validate 检查一个选择器：叶子与组合互斥，且组合不能无限嵌套。
func (s *Selector) Validate(depth int) error {
	if depth > maxSelectorDepth {
		return fmt.Errorf("选择器嵌套超过 %d 层", maxSelectorDepth)
	}
	leaves := 0
	if strings.TrimSpace(s.Path) != "" {
		leaves++
	}
	if strings.TrimSpace(s.Query) != "" {
		leaves++
	}
	composites := 0
	if len(s.Concat) > 0 {
		composites++
	}
	if len(s.Fallback) > 0 {
		composites++
	}
	if len(s.Chain) > 0 {
		composites++
	}
	// 第三种合法形态：没有 path 也没有 query，只有 attr——意思是"就取当前这个节点"。
	//
	// root 已经把范围缩到正文容器时，text 只需要说"取它的文本"，
	// 再写一层选择器是多余的。没有 attr 的空选择器仍然是错的——
	// 那既没说取哪个节点、也没说取什么。
	selfLeaf := leaves == 0 && composites == 0 && strings.TrimSpace(s.Attr) != ""
	if leaves+composites != 1 && !selfLeaf {
		return fmt.Errorf("选择器必须恰好声明一种形态：path 或 query（叶子），或 concat / fallback（组合）之一；只写 attr 表示\"就取当前节点\"。当前叶子 %d 个、组合 %d 个、attr %q", leaves, composites, s.Attr)
	}
	if len(s.Path) > maxSelectorPathLen {
		return fmt.Errorf("path 超过 %d 字节上限", maxSelectorPathLen)
	}
	if len(s.Query) > maxSelectorQueryLen {
		return fmt.Errorf("query 超过 %d 字节上限", maxSelectorQueryLen)
	}
	if s.Query != "" {
		if _, err := compileSelector(s.Query); err != nil {
			return fmt.Errorf("无效 CSS 规则: %w", err)
		}
	}
	if s.Regex != nil {
		if len(s.Regex.Pattern) > maxRegexPatternLen {
			return fmt.Errorf("regex.pattern 超过 %d 字节上限", maxRegexPatternLen)
		}
		if strings.TrimSpace(s.Regex.Pattern) == "" {
			return fmt.Errorf("regex.pattern 不能为空")
		}
		if _, err := compileReplace(s.Regex); err != nil {
			return err
		}
	}
	if s.Index != nil {
		if len(s.Index.Indices) == 0 {
			return fmt.Errorf("index.indices 不能为空：要全选就不要写 index")
		}
		if len(s.Index.Indices) > maxCombineChildren {
			return fmt.Errorf("index.indices 有 %d 项，超过 %d 上限", len(s.Index.Indices), maxCombineChildren)
		}
	}
	for _, group := range [][]Selector{s.Concat, s.Fallback, s.Chain} {
		if len(group) == 0 {
			continue
		}
		if len(group) > maxCombineChildren {
			return fmt.Errorf("组合算子有 %d 个子选择器，超过 %d 上限", len(group), maxCombineChildren)
		}
		for i := range group {
			if err := group[i].Validate(depth + 1); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateHeaders(headers map[string]string) error {
	if len(headers) > maxHeaders {
		return fmt.Errorf("headers 有 %d 项，超过 %d 上限", len(headers), maxHeaders)
	}
	for name, value := range headers {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			return fmt.Errorf("headers 里有一个空的头名")
		}
		if len(trimmed) > maxHeaderNameLen {
			return fmt.Errorf("header 名 %q 超过 %d 字节上限", trimmed, maxHeaderNameLen)
		}
		if len(value) > maxHeaderValueLen {
			return fmt.Errorf("header %q 的值超过 %d 字节上限", trimmed, maxHeaderValueLen)
		}
		// 换行会被 net/http 拒掉，但报错发生在请求时、离配置很远。
		// 在导入阶段就拦住，用户才知道是自己那个头写错了。
		if strings.ContainsAny(trimmed, ":\r\n") || strings.ContainsAny(value, "\r\n") {
			return fmt.Errorf("header %q 含非法字符", trimmed)
		}
	}
	return nil
}

// Operation 的 method 归一化。
func (o *Operation) normalizedMethod() string {
	if strings.EqualFold(strings.TrimSpace(o.Method), "post") {
		return "POST"
	}
	return "GET"
}

// keywordParam / pageParam / idParam / limitParam 返回各变量在 query 里的名字，
// 缺省取 MAC-CMS 的习惯名，与 collect.defaultConfig 一致。
func (p ParamNames) keywordParam() string { return orDefault(p.Keyword, "wd") }
func (p ParamNames) pageParam() string    { return orDefault(p.Page, "pg") }
func (p ParamNames) idParam() string      { return orDefault(p.ID, "ids") }
func (p ParamNames) limitParam() string   { return orDefault(p.Limit, "limit") }

func orDefault(value, fallback string) string {
	if trimmed := strings.TrimSpace(value); trimmed != "" {
		return trimmed
	}
	return fallback
}
