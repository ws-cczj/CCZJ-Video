package provider

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	"github.com/andybalholm/cascadia"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// 与 ADR 0016 的参数表一致。Legado 那边**没有**这两个上限：它的正文与目录分页
// 循环只靠 URL 去重和"命中下一章就 break"终止，grep maxPage|MAX_PAGE|contentLimit
// 在 model/webBook/ 下零命中。一条写错的 next 规则在那边可以无限跟页，
// 我们要有硬边界。
const (
	// maxChapterContentBytes 是单章正文的字节上限。
	maxChapterContentBytes = 4 << 20
	// maxTocPages 是目录分页最多跟随的页数。50 页足够覆盖上万章的源，
	// 超了基本可以断定是规则写错而不是书真有那么长。
	maxTocPages = 50
	// maxContentPages 是章内正文分页最多跟随的页数。
	maxContentPages = 20
	// maxPageConcurrency 是一页里同时列出多个分页地址时的并发抓取度。
	// Legado 的 threadCount 默认 16（AppConfig.kt:234-235），对一个桌面应用
	// 的单章抓取太激进；我们视频侧采集是页间隔 30s 串行（collect/engine.go:34），
	// 阅读是交互式的要快，但也不能打爆源站。
	maxPageConcurrency = 4
)

// Limits 把上面那几个上限暴露给编排层，让目录与正文两个循环各自持有自己的计数。
//
// 两个循环是**完全独立**的：各有各的去重集合、各有各的页数计数。Legado 也是这样
// （BookContent.kt:57 与 BookChapterList.kt:49 各持一个 nextUrlList），
// 唯一共享的是协程取消。合并成一个循环会让"目录第 3 页"和"正文第 3 页"互相挤占配额。
type Limits struct {
	MaxTocPages     int
	MaxContentPages int
	MaxContentBytes int
	Concurrency     int
}

// DefaultLimits 返回 ADR 0016 定下的默认值。
func DefaultLimits() Limits {
	return Limits{
		MaxTocPages:     maxTocPages,
		MaxContentPages: maxContentPages,
		MaxContentBytes: maxChapterContentBytes,
		Concurrency:     maxPageConcurrency,
	}
}

func (l Limits) orDefault() Limits {
	out := DefaultLimits()
	if l.MaxTocPages > 0 {
		out.MaxTocPages = l.MaxTocPages
	}
	if l.MaxContentPages > 0 {
		out.MaxContentPages = l.MaxContentPages
	}
	if l.MaxContentBytes > 0 {
		out.MaxContentBytes = l.MaxContentBytes
	}
	if l.Concurrency > 0 {
		out.Concurrency = l.Concurrency
	}
	return out
}

// item 是提取过程中流动的中间形态。
//
// node 非 nil 表示它还能被继续选择（一个 JSON 子树、一个 HTML 元素）；
// 否则 text 就是已经取出的终值。两种形态共用一套组合算子逻辑，
// 这样"拼接两个列表"与"两个候选标题取先有值的那个"是同一段代码。
type item struct {
	node any
	text string
}

func (i item) empty() bool {
	if i.node != nil {
		return false
	}
	return strings.TrimSpace(i.text) == ""
}

// backend 是一种响应形态的提取实现。JSON 与 HTML 各一份，
// 组合算子与下标、正则的处理都在 backend 之外，两边共用。
type backend interface {
	// parse 把解码后的文本解析成根节点。
	parse(text string) (any, error)
	// selectNodes 在一个节点上执行叶子选择，返回子节点列表（尚未取值）。
	selectNodes(root any, sel *Selector) ([]any, error)
	// valueOf 按 Attr 把一个节点取成字符串。
	valueOf(node any, attr string) (string, error)
}

// Extractor 在一份响应文本上求值选择器。
type Extractor struct {
	backend backend
	root    any
}

// NewExtractor 按 format 解析一份响应文本。format 必须是 "json" 或 "html"，
// 由 Operation.Validate 在导入阶段保证。
//
// 不嗅探：一个 JSON 源返回了 HTML 错误页时，嗅探会把它当成"这是个 HTML 源"
// 然后解析出一堆空字段，用户看到的是"这个源搜不出东西"。按声明的 format 解析
// 失败则报错，才分得清"源坏了"和"源没有这本书"。
func NewExtractor(format, text string) (*Extractor, error) {
	var b backend
	switch format {
	case "json":
		b = jsonBackend{}
	case "html":
		b = htmlBackend{}
	default:
		return nil, fmt.Errorf("未知的响应格式 %q", format)
	}
	root, err := b.parse(text)
	if err != nil {
		return nil, err
	}
	return &Extractor{backend: b, root: root}, nil
}

// Root 返回根节点，供"先把响应缩到一个子树"的预处理使用。
func (e *Extractor) Root() any { return e.root }

// Nodes 在一个节点上求值一个**节点级**选择器（List / Root），返回子节点。
//
// 节点级选择器不该带 Attr 与 Regex——那两个是取值动作，放在这里会让
// "选出一组节点"与"选出一组字符串"变成同一件事，调用方拿到手不知道是哪种。
// Operation.Validate 已经拦住了这种写法，这里再兜一次。
func (e *Extractor) Nodes(scope any, sel *Selector) ([]any, error) {
	if sel == nil {
		return nil, nil
	}
	if scope == nil {
		scope = e.root
	}
	items, err := e.eval(scope, sel, false)
	if err != nil {
		return nil, err
	}
	nodes := make([]any, 0, len(items))
	for _, it := range items {
		if it.node != nil {
			nodes = append(nodes, it.node)
		}
	}
	return nodes, nil
}

// Strings 在一个节点上求值一个**取值级**选择器（Fields / Text / Next），返回字符串。
func (e *Extractor) Strings(scope any, sel *Selector) ([]string, error) {
	if sel == nil {
		return nil, nil
	}
	if scope == nil {
		scope = e.root
	}
	items, err := e.eval(scope, sel, true)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(items))
	for _, it := range items {
		if it.node != nil {
			// 一个取值级选择器落在对象/数组上，说明配置指错了层级。
			// 报出来比静默返回空字符串好：空字符串在界面上是"这个源没有作者"，
			// 而真正的问题是路径写错了。
			return nil, fmt.Errorf("选择器指向的是一个%s而不是标量值", e.kindName(it.node))
		}
		if strings.TrimSpace(it.text) != "" {
			out = append(out, it.text)
		}
	}
	return out, nil
}

// String 取第一个非空值。没有就返回空串，不报错——"这一章没有卷名"是正常情况。
func (e *Extractor) String(scope any, sel *Selector) (string, error) {
	values, err := e.Strings(scope, sel)
	if err != nil {
		return "", err
	}
	if len(values) == 0 {
		return "", nil
	}
	return values[0], nil
}

func (e *Extractor) kindName(node any) string {
	if _, ok := e.backend.(jsonBackend); ok {
		return jsonKindName(node)
	}
	if element, ok := node.(*html.Node); ok {
		return "HTML <" + element.Data + "> 元素"
	}
	return "复合值"
}

// eval 是唯一的求值入口，terminal 决定叶子是"选出节点"还是"取出字符串"。
//
// 组合算子在两种模式下语义一致（Concat 除外：节点级追加、取值级拼成一个字符串），
// 这是把 JSON 与 HTML 收到同一套代码里的关键。
// 判"空"的规则由 item.empty() 统一定义。
func (e *Extractor) eval(scope any, sel *Selector, terminal bool) ([]item, error) {
	if sel == nil {
		return nil, nil
	}
	switch {
	case len(sel.Chain) > 0:
		current := []item{{node: scope}}
		for i := range sel.Chain {
			var next []item
			for _, parent := range current {
				if parent.node == nil {
					return nil, fmt.Errorf("chain 中间步骤必须返回节点")
				}
				batch, err := e.eval(parent.node, &sel.Chain[i], terminal && i == len(sel.Chain)-1)
				if err != nil {
					return nil, err
				}
				next = append(next, batch...)
				if len(next) > 100000 {
					return nil, fmt.Errorf("chain 结果超过 100000 个节点")
				}
			}
			current = next
		}
		return current, nil
	case len(sel.Concat) > 0:
		// 节点级：结果追加合并（合并两个同形状的容器，比如正文分散在两个 div 里）。
		if !terminal {
			out := make([]item, 0, len(sel.Concat))
			for i := range sel.Concat {
				child, err := e.eval(scope, &sel.Concat[i], false)
				if err != nil {
					return nil, err
				}
				out = append(out, child...)
			}
			return out, nil
		}
		// 取值级：拼成**一个**字符串。这一种是真实需要的——
		// 「<span class="num">第1话</span><span class="name">起风</span>」
		// 要合成一个标题，而不是两个值里取第一个。
		parts := make([]string, 0, len(sel.Concat))
		for i := range sel.Concat {
			child, err := e.eval(scope, &sel.Concat[i], true)
			if err != nil {
				return nil, err
			}
			for _, it := range child {
				if it.node != nil {
					return nil, fmt.Errorf("concat 的取值级里出现了一个未取值的节点")
				}
				if strings.TrimSpace(it.text) != "" {
					parts = append(parts, it.text)
				}
			}
		}
		return []item{{text: strings.Join(parts, sel.Separator)}}, nil

	case len(sel.Fallback) > 0:
		// 第一个返回非空结果的子选择器胜出并停止，后续不再执行。
		// 这不是"默认值"也不是并集：Legado 的 || 就是这个语义
		// （AnalyzeByJSoup.kt:104-107,146-148,174-177）。
		// "不再执行"是有意义的——后一个候选可能是个更贵的选择器。
		for i := range sel.Fallback {
			child, err := e.eval(scope, &sel.Fallback[i], terminal)
			if err != nil {
				return nil, err
			}
			if hasContent(child) {
				return child, nil
			}
		}
		return nil, nil
	}

	nodes, err := e.backend.selectNodes(scope, sel)
	if err != nil {
		return nil, err
	}
	nodes = applyIndex(nodes, sel.Index)
	if !terminal {
		out := make([]item, 0, len(nodes))
		for _, node := range nodes {
			out = append(out, item{node: node})
		}
		return out, nil
	}
	attr := strings.TrimSpace(sel.Attr)
	out := make([]item, 0, len(nodes))
	for _, node := range nodes {
		text, err := e.backend.valueOf(node, attr)
		if err != nil {
			return nil, err
		}
		if sel.Regex != nil {
			text, err = applyReplace(text, sel.Regex)
			if err != nil {
				return nil, err
			}
		}
		out = append(out, item{text: text})
	}
	return out, nil
}

func hasContent(items []item) bool {
	for _, it := range items {
		if !it.empty() {
			return true
		}
	}
	return false
}

// applyIndex 从选中的节点里挑出子集。负下标从尾部计，-1 是最后一个。
//
// 越界下标静默丢弃而不是报错：一个"跳过开头两个广告位"的规则在某个只有
// 一条结果的页面上不该整页失败。
func applyIndex(nodes []any, spec *IndexSpec) []any {
	if spec == nil || len(spec.Indices) == 0 || len(nodes) == 0 {
		return nodes
	}
	total := len(nodes)
	picked := make(map[int]bool, len(spec.Indices))
	for _, raw := range spec.Indices {
		index := raw
		if index < 0 {
			index += total
		}
		if index < 0 || index >= total {
			continue
		}
		picked[index] = true
	}
	out := make([]any, 0, len(picked))
	for i, node := range nodes {
		if picked[i] != spec.Exclude {
			out = append(out, node)
		}
	}
	return out
}

var replaceCache compiledRules // pattern -> *regexp.Regexp or error

// compileReplace 编译并缓存一条替换规则。
//
// 缓存是因为同一份源文档会被一章一章地反复使用，而正则编译不便宜。
// 键是 pattern 字符串，值可能是 error——编译失败也要缓存，
// 否则一份写错的配置会让每一章都重新编译一次再重新失败一次。
func compileReplace(spec *Replace) (*regexp.Regexp, error) {
	if cached, ok := replaceCache.Load(spec.Pattern); ok {
		if err, isErr := cached.(error); isErr {
			return nil, err
		}
		return cached.(*regexp.Regexp), nil
	}
	// Go 的 regexp 是 RE2，不支持回溯引用（\1）与零宽断言（?=）。
	// 从别的引擎抄来的规则会在这里编译失败，所以报错要指明是语法不被支持，
	// 而不是让用户以为是自己写错了转义。
	compiled, err := regexp.Compile(spec.Pattern)
	if err != nil {
		wrapped := fmt.Errorf("正则 %q 编译失败（本应用用 RE2，不支持回溯引用与零宽断言）: %w", spec.Pattern, err)
		replaceCache.Store(spec.Pattern, wrapped)
		return nil, wrapped
	}
	replaceCache.Store(spec.Pattern, compiled)
	return compiled, nil
}

func applyReplace(text string, spec *Replace) (string, error) {
	if spec == nil || text == "" {
		return text, nil
	}
	compiled, err := compileReplace(spec)
	if err != nil {
		return "", err
	}
	if !spec.First {
		return compiled.ReplaceAllString(text, spec.Replacement), nil
	}
	// First 是字面意思：只替换第一处，其余原文保留。
	//
	// 这里刻意不复制 Legado 的 `###` 语义——它的实现是先 find 第一个匹配，
	// 然后只返回那一处匹配子串的替换结果、**其余整段丢弃**，无匹配时返回空串
	// （AnalyzeRule.kt:441-452）。那不是"只替换第一处"，是个陷阱。
	//
	// 用 ExpandString 而不是 ReplaceAllStringFunc，为的是保留 $1 这类分组引用：
	// 后者不做展开，一条依赖分组的规则会静默产出字面量 "$1"。
	location := compiled.FindStringSubmatchIndex(text)
	if location == nil {
		return text, nil
	}
	expanded := compiled.ExpandString(nil, spec.Replacement, text, location)
	return text[:location[0]] + string(expanded) + text[location[1]:], nil
}

// ---------------------------------------------------------------------------
// JSON 后端
// ---------------------------------------------------------------------------

type jsonBackend struct{}

// 路径上限与 collect/response.go:47-50 那两条同源：源文档是用户输入，
// 一份 10MB 的路径不该被走完，也不该被切成一百万段。两处刻意保持同样的数值，
// 改一边就要改另一边。
const (
	maxPathLength   = 512
	maxPathSegments = 64
)

// ErrPathMissing 表示"路径本身合法，但这份响应里没有这个键/下标"。
//
// 它必须与"路径写错了"（语法错、指到了对象上）区分开，因为两者的处置相反：
// 一本书没有作者是常事，取不到就留空；而配置指错了层级要报错，
// 否则界面上永远是"这个源没有作者"，用户以为源的问题、实际是配置的问题，
// 永远修不好。collect/response.go:55-59 用 ErrPathNotFound 与 ErrPathType
// 分了同一刀。
var ErrPathMissing = errors.New("响应里没有这个路径")

func (jsonBackend) parse(text string) (any, error) {
	decoder := json.NewDecoder(strings.NewReader(text))
	// UseNumber 让 20 位的远端 ID 以原文活下来。转成 float64 会丢精度，
	// 而 reports/04:22 明确警告过"损失精度后再转字符串无法修复"。
	decoder.UseNumber()
	var doc any
	if err := decoder.Decode(&doc); err != nil {
		return nil, fmt.Errorf("响应不是合法 JSON：%w", err)
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, fmt.Errorf("JSON 文档后面还有多余内容")
	}
	return doc, nil
}

func (b jsonBackend) selectNodes(root any, sel *Selector) ([]any, error) {
	path := strings.TrimSpace(sel.Path)
	if path == "" {
		// 没有 path 的叶子表示"就是当前这个节点"，取值方式由 Attr 决定。
		// JSON 侧 Attr 是被忽略的（值就是标量本身），所以这一形态在 JSON 里
		// 等价于"把 root 自己当结果"。
		if strings.TrimSpace(sel.Query) != "" {
			return nil, fmt.Errorf("JSON 提取不认识 CSS 选择器 %q；这个操作的 format 是 json", sel.Query)
		}
		return []any{root}, nil
	}
	node, err := walkJSONPath(root, path)
	if err != nil {
		return nil, err
	}
	// 路径落在数组上就展开成元素列表，落在标量或对象上就是单元素列表。
	// 这样"list 指向一个数组"与"list 指向单个对象"是同一条规则写法。
	if list, ok := node.([]any); ok {
		return list, nil
	}
	return []any{node}, nil
}

func (jsonBackend) valueOf(node any, _ string) (string, error) {
	// JSON 侧没有 Attr 的概念：值就是标量本身。
	// 落在对象或数组上说明配置指错了层级，报出来而不是返回空串。
	switch value := node.(type) {
	case nil:
		return "", nil
	case string:
		return value, nil
	case bool:
		if value {
			return "true", nil
		}
		return "false", nil
	case json.Number:
		return value.String(), nil
	case float64:
		if value == float64(int64(value)) {
			return strconv.FormatInt(int64(value), 10), nil
		}
		return strconv.FormatFloat(value, 'g', -1, 64), nil
	case map[string]any:
		return "", fmt.Errorf("路径指向的是一个对象，取不出标量值")
	case []any:
		return "", fmt.Errorf("路径指向的是一个数组，取不出标量值")
	default:
		return "", fmt.Errorf("无法从 %T 取值", node)
	}
}

// walkJSONPath 走一条点分路径。对象按键、数组按十进制下标，不做通配。
//
// 语义与 collect.lookupPath（app/collect/response.go:142-170）刻意保持一致：
// 同一个项目里两种源文档的 path 写法必须是同一种语言，否则用户在视频源里
// 学会的 `data.list` 到了书源里就得换写法。区别只在错误类型——那边有
// ErrPathSyntax / ErrPathNotFound / ErrPathType 三种供调用方分支，
// 这边统一成一条带路径与段名的错误，因为提取层的调用方不需要按错误种类分流。
func walkJSONPath(doc any, path string) (any, error) {
	segments, err := splitJSONPath(path)
	if err != nil {
		return nil, err
	}
	node := doc
	for _, segment := range segments {
		switch current := node.(type) {
		case map[string]any:
			next, ok := current[segment]
			if !ok {
				return nil, fmt.Errorf("%w：%q 的第 %q 段不存在", ErrPathMissing, path, segment)
			}
			node = next
		case []any:
			index, err := strconv.Atoi(segment)
			if err != nil || index < 0 {
				return nil, fmt.Errorf("路径 %q 的第 %q 段不是合法的数组下标", path, segment)
			}
			if index >= len(current) {
				return nil, fmt.Errorf("%w：路径 %q 的第 %q 段越界，数组只有 %d 项", ErrPathMissing, path, segment, len(current))
			}
			node = current[index]
		default:
			return nil, fmt.Errorf("路径 %q 在第 %q 段之前已经走到了 %s，不能再往下取", path, segment, jsonKindName(node))
		}
	}
	return node, nil
}

func splitJSONPath(path string) ([]string, error) {
	if len(path) > maxPathLength {
		return nil, fmt.Errorf("路径超过 %d 字节上限", maxPathLength)
	}
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("路径为空")
	}
	segments := strings.Split(path, ".")
	if len(segments) > maxPathSegments {
		return nil, fmt.Errorf("路径超过 %d 段上限", maxPathSegments)
	}
	for i, segment := range segments {
		if segment == "" {
			return nil, fmt.Errorf("路径第 %d 段为空（连续的两个点？）", i)
		}
	}
	return segments, nil
}

func jsonKindName(node any) string {
	switch node.(type) {
	case nil:
		return "null"
	case map[string]any:
		return "对象"
	case []any:
		return "数组"
	case string:
		return "字符串"
	case bool:
		return "布尔值"
	case json.Number, float64, int, int64:
		return "数字"
	default:
		return fmt.Sprintf("%T", node)
	}
}

// ---------------------------------------------------------------------------
// HTML 后端
// ---------------------------------------------------------------------------

type htmlBackend struct{}

// selectorCache 缓存编译好的 CSS 选择器。同一份源文档会被一页一页地反复使用，
// 而 cascadia 的编译要走一遍词法与语法分析。
var selectorCache compiledRules // query -> cascadia.Selector or error

func compileSelector(query string) (cascadia.Selector, error) {
	if cached, ok := selectorCache.Load(query); ok {
		if err, isErr := cached.(error); isErr {
			return nil, err
		}
		return cached.(cascadia.Selector), nil
	}
	compiled, err := cascadia.Compile(query)
	if err != nil {
		wrapped := fmt.Errorf("CSS 选择器 %q 编译失败: %w", query, err)
		selectorCache.Store(query, wrapped)
		return nil, wrapped
	}
	selectorCache.Store(query, compiled)
	return compiled, nil
}

func (htmlBackend) parse(text string) (any, error) {
	// x/net/html 的解析器按 HTML5 的容错规则重建 DOM：未闭合标签、错误嵌套、
	// 裸文本都会被安置到一个合法位置。真实小说站的 HTML 大多是脏的，
	// 一个严格的解析器会在第一页就失败。
	root, err := html.Parse(strings.NewReader(text))
	if err != nil {
		return nil, fmt.Errorf("HTML 解析失败：%w", err)
	}
	return root, nil
}

func (htmlBackend) selectNodes(root any, sel *Selector) ([]any, error) {
	node, ok := root.(*html.Node)
	if !ok {
		return nil, fmt.Errorf("HTML 提取的起点不是一个元素")
	}
	query := strings.TrimSpace(sel.Query)
	if query == "" {
		// 没有 query 的叶子表示"就是当前这个节点"：root 已经把范围缩到正文容器时，
		// text 只需要说"取它的文本"，再写一层选择器是多余的。
		if strings.TrimSpace(sel.Path) != "" {
			return nil, fmt.Errorf("HTML 提取不认识 JSON 路径 %q；这个操作的 format 是 html", sel.Path)
		}
		return []any{node}, nil
	}
	selector, err := compileSelector(query)
	if err != nil {
		return nil, err
	}
	matches := selector.MatchAll(node)
	out := make([]any, 0, len(matches))
	for _, match := range matches {
		out = append(out, match)
	}
	return out, nil
}

// HTML 取值方式。前四个是关键字，其余一律当属性名。
const (
	attrText      = "text"
	attrOwnText   = "ownText"
	attrHTML      = "html"
	attrAll       = "all"
	attrInnerHTML = "innerHtml"
)

func (htmlBackend) valueOf(node any, attr string) (string, error) {
	element, ok := node.(*html.Node)
	if !ok {
		return "", fmt.Errorf("要取值的不是一个 HTML 元素")
	}
	name := strings.TrimSpace(attr)
	if name == "" {
		name = attrText
	}
	switch name {
	case attrText:
		return normalizeWhitespace(textOf(element)), nil
	case attrOwnText:
		return normalizeWhitespace(ownTextOf(element)), nil
	case attrHTML, attrInnerHTML:
		// 先剥掉 script 与 style：正文里混进来的埋点脚本会变成一屏 JS 代码，
		// 而它们在阅读视图里没有任何意义。Legado 的 @html 也这么做
		// （AnalyzeByJSoup.kt:260-267），@all 才保留原样。
		return innerHTMLWithout(element, atom.Script, atom.Style), nil
	case attrAll:
		return innerHTML(element), nil
	}
	// 其余一律当属性名：@href、@src、@content、@data-x 都是这一条分支，
	// 不是各自的关键字。这样源里出现一个我们不认识但有意义的属性时，
	// 它能直接工作，而不需要先改代码。
	for _, a := range element.Attr {
		if strings.EqualFold(a.Key, name) {
			return strings.TrimSpace(a.Val), nil
		}
	}
	return "", nil
}

// textOf 取一个元素及其后代的规范化文本，等价于 Jsoup 的 Element.text()。
func textOf(node *html.Node) string {
	var builder strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			builder.WriteString(n.Data)
			builder.WriteByte(' ')
			return
		}
		block := false
		if n.Type == html.ElementNode {
			// 这两类里的文本不是给人读的，Jsoup 的 text() 同样跳过它们。
			if n.DataAtom == atom.Script || n.DataAtom == atom.Style {
				return
			}
			// <br> 是一个换行，不是一个容器：它没有子节点，进去再出来会丢掉这一次换行。
			if n.DataAtom == atom.Br {
				builder.WriteByte('\n')
				return
			}
			// 块级元素前后各补一个换行：正文的段落边界靠它保留，
			// 否则一整章会连成一行，前端再按什么切都不对。
			block = isBlockLevel(n.DataAtom)
			if block {
				builder.WriteByte('\n')
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
		if block {
			builder.WriteByte('\n')
		}
	}
	walk(node)
	return builder.String()
}

// ownTextOf 只取元素自己的直接文本节点，不含子元素。
// 对应 Legado 的 @ownText（AnalyzeByJSoup.kt:253-258）：
// 「<a>第一章 <span>VIP</span></a>」里只要「第一章」。
func ownTextOf(node *html.Node) string {
	var builder strings.Builder
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == html.TextNode {
			builder.WriteString(child.Data)
			builder.WriteByte(' ')
		}
	}
	return builder.String()
}

func isBlockLevel(a atom.Atom) bool {
	switch a {
	case atom.P, atom.Div, atom.Br, atom.Li, atom.Ul, atom.Ol, atom.Dd, atom.Dt,
		atom.Tr, atom.Table, atom.Section, atom.Article, atom.Header, atom.Footer,
		atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6, atom.Blockquote, atom.Pre:
		return true
	}
	return false
}

// normalizeWhitespace 把连续空白压成一个空格，但**保留换行**：
// 正文的段落边界是换行，压掉它就等于把整章连成一行。
// 行内多余的空白（HTML 源码里的缩进与换行）才是要压掉的那部分。
func normalizeWhitespace(text string) string {
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.Join(strings.Fields(line), " ")
		out = append(out, trimmed)
	}
	// 连续空行压成一个：HTML 里块级元素前后各补一次换行会产生空行，
	// 留着会让正文每隔一段就多一个空段落。
	collapsed := make([]string, 0, len(out))
	previousBlank := false
	for _, line := range out {
		blank := line == ""
		if blank && previousBlank {
			continue
		}
		collapsed = append(collapsed, line)
		previousBlank = blank
	}
	return strings.TrimSpace(strings.Join(collapsed, "\n"))
}

func innerHTML(node *html.Node) string {
	var buffer bytes.Buffer
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if err := html.Render(&buffer, child); err != nil {
			return ""
		}
	}
	return buffer.String()
}

func innerHTMLWithout(node *html.Node, skip ...atom.Atom) string {
	var buffer bytes.Buffer
	// 循环变量不能叫 atom：那会遮蔽 atom 包，让同一段代码里第二次用到
	// atom.Atom 类型的地方变成一个变量引用。
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == html.ElementNode {
			skipped := false
			for _, excluded := range skip {
				if child.DataAtom == excluded {
					skipped = true
					break
				}
			}
			if skipped {
				continue
			}
		}
		if err := html.Render(&buffer, child); err != nil {
			return ""
		}
	}
	return buffer.String()
}
