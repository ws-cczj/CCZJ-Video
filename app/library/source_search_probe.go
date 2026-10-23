package library

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"cczjVideo/app/provider"
)

// legacySearchProbe extracts only a validated static search operation. A source
// can expose searchable metadata even when its content script is unsupported;
// that never makes it an importable/full-reading adapter.
func legacySearchProbe(raw map[string]json.RawMessage, kind string) (*provider.Document, error) {
	doc := &provider.Document{Version: 3, Kind: kind, CheckKeyword: "三国"}
	if kind == "comic" {
		doc.CheckKeyword = "斗罗"
	}
	if header := stringValue(raw["header"]); header != "" {
		if err := json.Unmarshal([]byte(header), &doc.Headers); err != nil {
			return nil, fmt.Errorf("非静态请求头")
		}
	}
	address := stringValue(raw["searchUrl"])
	if address == "" || strings.Contains(address, "<js>") || strings.Contains(address, "@js:") || strings.Contains(address, ",") {
		return nil, fmt.Errorf("非静态搜索地址")
	}
	address = strings.ReplaceAll(strings.ReplaceAll(address, "{{key}}", "{keyword}"), "{{page}}", "{page}")
	if !strings.Contains(address, "{keyword}") {
		return nil, fmt.Errorf("固定书单没有关键词搜索能力")
	}
	if strings.Contains(address, "{{") || strings.Contains(address, "}}") {
		return nil, fmt.Errorf("未知搜索模板")
	}
	u, err := url.Parse(address)
	if err != nil {
		return nil, err
	}
	params := u.Query()
	u.RawQuery = ""
	op := &provider.Operation{Path: u.String(), Format: "html", Fields: map[string]provider.Selector{}, Params: map[string]string{}}
	for key, values := range params {
		if len(values) != 1 {
			return nil, fmt.Errorf("重复参数")
		}
		switch values[0] {
		case "{keyword}":
			op.ParamNames.Keyword = key
		case "{page}":
			op.ParamNames.Page = key
		default:
			op.Params[key] = values[0]
		}
	}
	var rules map[string]json.RawMessage
	if err = json.Unmarshal(raw["ruleSearch"], &rules); err != nil {
		return nil, err
	}
	list, err := searchProbeSelector(stringValue(rules["bookList"]))
	if err != nil {
		return nil, err
	}
	op.List = &list
	if list.Path != "" {
		op.Format = "json"
	}
	for from, to := range map[string]string{"name": "title", "bookUrl": "url", "author": "author", "coverUrl": "cover", "intro": "summary", "kind": "genres"} {
		v := stringValue(rules[from])
		if v == "" {
			continue
		}
		sel, e := searchProbeSelector(v)
		if e != nil {
			if to == "title" || to == "url" {
				return nil, e
			}
			continue
		}
		if op.Format == "json" && sel.Path == "" {
			if to == "title" || to == "url" {
				return nil, fmt.Errorf("混合搜索格式")
			}
			continue
		}
		op.Fields[to] = sel
	}
	if _, ok := op.Fields["title"]; !ok {
		return nil, fmt.Errorf("缺少静态标题规则")
	}
	if _, ok := op.Fields["url"]; !ok {
		return nil, fmt.Errorf("缺少静态作品地址规则")
	}
	doc.Operations.Search = op
	if err = provider.ValidateSearchDocument(doc); err != nil {
		return nil, err
	}
	return doc, nil
}

var simpleJSONSearchPath = regexp.MustCompile(`^\$\.[A-Za-z0-9_]+(?:\.[A-Za-z0-9_]+)*(?:\[\*\])?$`)

func searchProbeSelector(s string) (provider.Selector, error) {
	if simpleJSONSearchPath.MatchString(s) {
		return provider.Selector{Path: strings.TrimSuffix(strings.TrimPrefix(s, "$."), "[*]")}, nil
	}
	return legadoSelector(s)
}
