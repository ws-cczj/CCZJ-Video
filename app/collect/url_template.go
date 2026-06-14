package collect

import (
	"fmt"
	"strings"
)

// TemplateURL represents a parameterized URL like https://{host}.ppqrrs.com/{prefix}/{path}/video/index.m3u8
type TemplateURL struct {
	Template string            `json:"template"`
	Vars     map[string]string `json:"vars"`
}

// NewTemplate extracts template vars from a concrete URL using a template pattern.
// template: "https://{host}.ppqrrs.com/{prefix}/{path}/video/index.m3u8"
func NewTemplate(template, concrete string) (*TemplateURL, error) {
	parts := tokenize(concrete)
	tplParts := tokenize(template)

	if len(parts) != len(tplParts) {
		return nil, fmt.Errorf("url parts mismatch: template has %d, concrete has %d", len(tplParts), len(parts))
	}

	vars := make(map[string]string)
	for i, tp := range tplParts {
		if isVar(tp) {
			vars[tp] = parts[i]
		} else if tp != parts[i] {
			return nil, fmt.Errorf("mismatch at part %d: expected %q got %q", i, tp, parts[i])
		}
	}
	return &TemplateURL{Template: template, Vars: vars}, nil
}

// Build constructs the full URL from template + vars.
func (t *TemplateURL) Build() string {
	result := t.Template
	for k, v := range t.Vars {
		result = strings.ReplaceAll(result, "{"+k+"}", v)
	}
	return result
}

// EncodeVars serializes vars into a compact string: "host=v5|prefix=wjv5|path=202602/22/wbDBBVpVt993"
func (t *TemplateURL) EncodeVars() string {
	parts := make([]string, 0, len(t.Vars))
	for k, v := range t.Vars {
		parts = append(parts, k+"="+v)
	}
	return strings.Join(parts, "|")
}

// DecodeVars parses encoded vars back into a map.
func DecodeVars(template, encoded string) map[string]string {
	vars := make(map[string]string)
	for _, pair := range strings.Split(encoded, "|") {
		kv := strings.SplitN(pair, "=", 2)
		if len(kv) == 2 {
			vars[kv[0]] = kv[1]
		}
	}
	return vars
}

// BuildURL constructs a full URL from template and encoded vars.
func BuildURL(template, encoded string) string {
	vars := DecodeVars(template, encoded)
	result := template
	for k, v := range vars {
		result = strings.ReplaceAll(result, "{"+k+"}", v)
	}
	return result
}

func tokenize(url string) []string {
	var parts []string
	current := ""
	inVar := false
	for i := 0; i < len(url); i++ {
		ch := url[i]
		if ch == '{' {
			if current != "" {
				parts = append(parts, current)
				current = ""
			}
			inVar = true
		}
		if inVar {
			current += string(ch)
			if ch == '}' {
				parts = append(parts, current)
				current = ""
				inVar = false
			}
		} else {
			if ch == '/' || ch == '.' || ch == ':' || ch == '?' || ch == '&' || ch == '=' {
				if current != "" {
					parts = append(parts, current)
					current = ""
				}
				parts = append(parts, string(ch))
			} else {
				current += string(ch)
			}
		}
	}
	if current != "" {
		parts = append(parts, current)
	}
	return parts
}

func isVar(s string) bool {
	return len(s) > 2 && s[0] == '{' && s[len(s)-1] == '}'
}
