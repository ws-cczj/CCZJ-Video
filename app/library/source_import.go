package library

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"cczjVideo/app/provider"
)

// Previously converted snapshots need the same keyword check as raw imports.
func legacyDocumentHasKeyword(doc *provider.Document) bool {
	if doc.Adapter != "" {
		return true
	}
	op := doc.Operations.Search
	if op == nil {
		return false
	}
	if op.ParamNames.Keyword != "" || strings.Contains(op.Path, "{keyword}") || strings.Contains(op.Body, "{keyword}") {
		return true
	}
	for _, value := range op.Params {
		if strings.Contains(value, "{keyword}") {
			return true
		}
	}
	return false
}

// Only a declared static subset is converted; unsupported execution stays explicit.
func convertLegado(raw map[string]json.RawMessage, kind string) (string, error) {
	if kind != "novel" && kind != "comic" {
		return "", fmt.Errorf("请选择小说或漫画规则")
	}
	for _, key := range []string{"loginCheckJs", "respondTime"} {
		if key != "respondTime" && stringValue(raw[key]) != "" {
			return "", fmt.Errorf("需要登录或脚本能力：%s", key)
		}
	}
	doc := provider.Document{Version: 3, Kind: kind, CheckKeyword: "三国"}
	if header := stringValue(raw["header"]); header != "" {
		if err := json.Unmarshal([]byte(header), &doc.Headers); err != nil {
			return "", fmt.Errorf("header 必须是静态 JSON 请求头")
		}
	}
	searchURL := stringValue(raw["searchUrl"])
	if strings.Contains(searchURL, ",") || strings.Contains(searchURL, "<js>") {
		return "", fmt.Errorf("搜索地址需要脚本或 Legado URL 选项，尚不能自动转换")
	}
	searchURL = strings.ReplaceAll(strings.ReplaceAll(searchURL, "{{key}}", "{keyword}"), "{{page}}", "{page}")
	if strings.ContainsAny(searchURL, "{}") && (strings.Contains(searchURL, "{{") || strings.Contains(searchURL, "}}")) {
		return "", fmt.Errorf("搜索地址含未支持的模板表达式")
	}
	if searchURL == "" {
		return "", fmt.Errorf("缺少 searchUrl")
	}
	if !strings.Contains(searchURL, "{keyword}") {
		return "", fmt.Errorf("固定书单没有关键词搜索能力")
	}
	parsedURL, err := url.Parse(searchURL)
	if err != nil {
		return "", fmt.Errorf("搜索地址不合法")
	}
	params := parsedURL.Query()
	parsedURL.RawQuery = ""
	if strings.Contains(parsedURL.Path, "{keyword}") {
		return "", fmt.Errorf("路径中的关键词模板暂不自动转换")
	}
	doc.Operations.Search = &provider.Operation{Path: parsedURL.String(), Format: "html", Fields: map[string]provider.Selector{}, Params: map[string]string{}}
	for key, values := range params {
		if len(values) != 1 {
			return "", fmt.Errorf("重复搜索参数暂不自动转换")
		}
		value := values[0]
		switch value {
		case "{keyword}":
			doc.Operations.Search.ParamNames.Keyword = key
		case "{page}":
			doc.Operations.Search.ParamNames.Page = key
		default:
			doc.Operations.Search.Params[key] = value
		}
	}
	for _, spec := range []struct {
		key    string
		op     **provider.Operation
		fields map[string]string
	}{
		{"ruleSearch", &doc.Operations.Search, map[string]string{"name": "title", "bookUrl": "url", "author": "author", "coverUrl": "cover", "intro": "summary", "kind": "genres", "lastChapter": "latest"}},
		{"ruleBookInfo", &doc.Operations.Detail, map[string]string{"name": "title", "author": "author", "coverUrl": "cover", "intro": "summary", "kind": "genres", "tocUrl": "toc_url", "lastChapter": "latest", "wordCount": "words"}},
		{"ruleToc", &doc.Operations.Toc, map[string]string{"chapterName": "title", "chapterUrl": "chapter_url", "isVolume": "is_volume"}},
		{"ruleContent", &doc.Operations.Content, map[string]string{}},
	} {
		var rules map[string]json.RawMessage
		if err := json.Unmarshal(raw[spec.key], &rules); err != nil {
			return "", fmt.Errorf("缺少或无效的 %s", spec.key)
		}
		for _, key := range []string{"webJs", "webView", "loginCheckJs", "sourceRegex", "imageDecode", "fontDecode", "contentJs", "nextContentJs"} {
			if value := rules[key]; len(value) > 0 && string(value) != "null" && string(value) != `""` && string(value) != "false" {
				return "", fmt.Errorf("%s.%s 需要脚本或专用适配", spec.key, key)
			}
		}
		if *spec.op == nil {
			*spec.op = &provider.Operation{Format: "html", Fields: map[string]provider.Selector{}}
		}
		op := *spec.op
		for from, to := range spec.fields {
			if value := stringValue(rules[from]); value != "" {
				sel, err := legadoSelector(value)
				if err != nil {
					return "", fmt.Errorf("%s.%s：%w", spec.key, from, err)
				}
				op.Fields[to] = sel
			}
		}
		for from, target := range map[string]**provider.Selector{"bookList": &op.List, "chapterList": &op.List, "nextTocUrl": &op.Next, "nextContentUrl": &op.Next, "content": &op.Text, "init": &op.Root} {
			if value := stringValue(rules[from]); value != "" {
				if from == "chapterList" && strings.HasPrefix(value, "-") {
					doc.Reverse = true
					value = strings.TrimPrefix(value, "-")
				}
				sel, err := legadoSelector(value)
				if err != nil {
					return "", fmt.Errorf("%s.%s：%w", spec.key, from, err)
				}
				*target = &sel
			}
		}
		if spec.key == "ruleContent" && op.Text != nil {
			leaf := op.Text
			if len(leaf.Chain) > 0 {
				leaf = &leaf.Chain[len(leaf.Chain)-1]
			}
			if kind == "comic" {
				if leaf.Attr != "src" && leaf.Attr != "data-src" && leaf.Attr != "data-original" {
					return "", fmt.Errorf("漫画正文需要明确的图片地址规则")
				}
				imageAttr := leaf.Attr
				leaf.Attr = ""
				op.List = op.Text
				op.Text = nil
				op.Fields = map[string]provider.Selector{"image_url": {Attr: imageAttr}}
				doc.Operations.Pages = op
				doc.Operations.Content = nil
			} else {
				if leaf.Attr == "html" {
					leaf.Attr = "text"
				}
				if replacement := stringValue(rules["replaceRegex"]); replacement != "" {
					parts := strings.Split(strings.TrimPrefix(replacement, "##"), "##")
					if len(parts) > 2 || leaf.Regex != nil {
						return "", fmt.Errorf("正文需要多个正则替换，暂不自动合并")
					}
					leaf.Regex = &provider.Replace{Pattern: parts[0]}
					if len(parts) == 2 {
						leaf.Regex.Replacement = parts[1]
					}
				}
				text := *op.Text
				op.Text = &provider.Selector{Concat: []provider.Selector{text}, Separator: "\n"}
			}
		}
	}
	encoded, err := json.Marshal(doc)
	if err != nil {
		return "", err
	}
	if _, err = provider.ParseDocument(string(encoded), kind); err != nil {
		return "", err
	}
	return string(encoded), nil
}

func stringValue(raw json.RawMessage) string {
	var s string
	_ = json.Unmarshal(raw, &s)
	return strings.TrimSpace(s)
}

var legadoSimpleIndex = regexp.MustCompile(`^(class|id|tag)\.([\w-]+)(?:\.(-?\d+))?$`)

func legadoSelector(rule string) (provider.Selector, error) {
	rule = strings.TrimSpace(rule)
	if rule == "" {
		return provider.Selector{}, fmt.Errorf("空选择器")
	}
	for _, marker := range []string{"<js>", "@js:", "{{", "@xpath:", "//", "&&", "||", "%%", "@json:", "$.", "@put:", "@get:"} {
		if strings.Contains(rule, marker) {
			return provider.Selector{}, fmt.Errorf("需要未支持的规则能力 %s", marker)
		}
	}
	rule = strings.TrimPrefix(rule, "@css:")
	regexParts := strings.Split(rule, "##")
	if len(regexParts) > 3 {
		return provider.Selector{}, fmt.Errorf("不支持多段正则提取")
	}
	rule = regexParts[0]
	pieces := splitLegadoChain(rule)
	var chain []provider.Selector
	for _, piece := range pieces {
		piece = strings.TrimSpace(piece)
		if piece == "" {
			return provider.Selector{}, fmt.Errorf("规则中存在空步骤")
		}
		if len(chain) > 0 && (piece == "text" || piece == "textNodes" || piece == "ownText" || piece == "html" || piece == "href" || piece == "src" || piece == "content" || piece == "value" || piece == "title" || piece == "alt" || strings.HasPrefix(piece, "data-")) {
			chain[len(chain)-1].Attr = piece
			continue
		}
		part := provider.Selector{Query: piece}
		if m := legadoSimpleIndex.FindStringSubmatch(piece); m != nil {
			switch m[1] {
			case "class":
				part.Query = "." + m[2]
			case "id":
				part.Query = "#" + m[2]
			case "tag":
				part.Query = m[2]
			}
			if m[3] != "" {
				n, _ := strconv.Atoi(m[3])
				part.Index = &provider.IndexSpec{Indices: []int{n}}
			}
		}
		chain = append(chain, part)
	}
	if len(chain) > 1 {
		last := &chain[len(chain)-1]
		if last.Attr == "textNodes" {
			last.Attr = "text"
		}
		if len(regexParts) > 1 {
			last.Regex = &provider.Replace{Pattern: regexParts[1]}
			if len(regexParts) == 3 {
				last.Regex.Replacement = regexParts[2]
			}
		}
		return provider.Selector{Chain: chain}, nil
	}
	sel := chain[0]
	if len(regexParts) > 1 {
		sel.Regex = &provider.Replace{Pattern: regexParts[1]}
		if len(regexParts) == 3 {
			sel.Regex.Replacement = regexParts[2]
		}
	}
	switch sel.Query {
	case "text", "textNodes", "ownText", "html", "all", "href", "src":
		sel.Attr = sel.Query
		sel.Query = ""
	}
	if sel.Attr == "textNodes" {
		sel.Attr = "text"
	}
	if strings.HasPrefix(sel.Query, ".") || strings.HasPrefix(sel.Query, "#") {
		return sel, nil
	}
	if m := legadoSimpleIndex.FindStringSubmatch(sel.Query); m != nil {
		switch m[1] {
		case "class":
			sel.Query = "." + m[2]
		case "id":
			sel.Query = "#" + m[2]
		case "tag":
			sel.Query = m[2]
		}
		if m[3] != "" {
			index, _ := strconv.Atoi(m[3])
			sel.Index = &provider.IndexSpec{Indices: []int{index}}
		}
	}
	return sel, nil
}

// @ inside CSS attributes/quoted strings is part of the selector, not a step.
func splitLegadoChain(rule string) []string {
	var parts []string
	start, depth := 0, 0
	var quote byte
	for i := 0; i < len(rule); i++ {
		ch := rule[i]
		if ch == '\\' {
			i++
			continue
		}
		if quote != 0 {
			if ch == quote {
				quote = 0
			}
			continue
		}
		if ch == '\'' || ch == '"' {
			quote = ch
			continue
		}
		if ch == '[' || ch == '(' {
			depth++
		} else if ch == ']' || ch == ')' {
			depth--
		}
		if ch == '@' && depth == 0 {
			parts = append(parts, rule[start:i])
			start = i + 1
		}
	}
	return append(parts, rule[start:])
}

func validCatalogOrigin(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return ""
	}
	parsed.User = nil
	parsed.Fragment = ""
	return parsed.String()
}
