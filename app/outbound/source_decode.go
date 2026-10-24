package outbound

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

var errSourceChallenge = errors.New("source verification page")
var errSourceFormat = errors.New("unsupported proxy source format")

type candidate struct {
	URL     string
	score   float64
	latency float64
}

func decodeCandidates(raw []byte) ([]candidate, error) {
	return decodeSource(SourceConfig{Format: "auto", Protocol: "http"}, raw)
}

func decodeSource(source SourceConfig, raw []byte) ([]candidate, error) {
	if len(raw) > 2<<20 {
		return nil, fmt.Errorf("proxy list exceeds budget")
	}
	raw = bytes.TrimSpace(bytes.TrimPrefix(raw, []byte{0xef, 0xbb, 0xbf}))
	format := source.Format
	if format == "auto" || format == "" {
		switch {
		case bytes.HasPrefix(raw, []byte("<")):
			format = "html"
		case bytes.HasPrefix(raw, []byte("[")), bytes.HasPrefix(raw, []byte("{")):
			format = "json"
		default:
			format = "text"
		}
	}
	rows := []candidate{}
	seen := map[string]bool{}
	add := func(value, protocol, country string, score, latency float64) {
		if len(rows) >= 20000 || source.Country != "" && source.Country != country {
			return
		}
		value = strings.TrimSpace(value)
		if !strings.Contains(value, "://") {
			if protocol == "" {
				protocol = source.Protocol
			}
			if protocol == "" {
				protocol = "http"
			}
			// A list's HTTPS label describes CONNECT capability. Only an explicit
			// https:// endpoint requests TLS on the client-to-proxy connection.
			protocol = strings.ToLower(protocol)
			if protocol == "https" {
				protocol = "http"
			}
			if protocol != "http" && protocol != "socks5" {
				return
			}
			value = protocol + "://" + value
		}
		if u, err := parseEndpoint(value); err == nil && !seen[u.String()] {
			seen[u.String()] = true
			rows = append(rows, candidate{URL: u.String(), score: score, latency: latency})
		}
	}
	var err error
	switch format {
	case "text":
		if bytes.HasPrefix(raw, []byte("<")) || bytes.HasPrefix(raw, []byte("{")) {
			return nil, errSourceFormat
		}
		scanner := bufio.NewScanner(bytes.NewReader(raw))
		scanner.Buffer(make([]byte, 1024), 4096)
		for n := 0; scanner.Scan() && n < 20000; n++ {
			add(scanner.Text(), "", "", 0, 0)
		}
		err = scanner.Err()
		if len(raw) > 0 && len(rows) == 0 {
			err = errSourceFormat
		}
	case "json":
		err = decodeJSONSource(raw, add)
	case "html":
		err = decodeHTMLSource(raw, add)
	default:
		err = errSourceFormat
	}
	if err != nil {
		return nil, err
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].score != rows[j].score {
			return rows[i].score > rows[j].score
		}
		return rows[i].latency < rows[j].latency
	})
	return rows, nil
}

type addCandidate func(string, string, string, float64, float64)

func decodeJSONSource(raw []byte, add addCandidate) error {
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&value) != nil || !json.Valid(raw) {
		return errSourceFormat
	}
	if envelope, ok := value.(map[string]any); ok {
		if code, present := envelope["code"]; present && number(code) != 200 {
			return errSourceFormat
		}
		switch {
		case envelope["items"] != nil:
			value = envelope["items"]
		case envelope["data"] != nil:
			value = envelope["data"]
		case envelope["proxies"] != nil:
			value = envelope["proxies"]
		default:
			return errSourceFormat
		}
		if inner, ok := value.(map[string]any); ok {
			value = inner["proxies"]
		}
	}
	records, ok := value.([]any)
	if !ok || len(records) > 20000 {
		return errSourceFormat
	}
	for _, record := range records {
		if value, ok := record.(string); ok {
			add(value, "", "", 0, 0)
			continue
		}
		r, ok := record.(map[string]any)
		if !ok {
			return errSourceFormat
		}
		if r["ip"] == nil && r["host"] == nil && r["proxy"] == nil {
			return errSourceFormat
		}
		if str(r["username"]) != "" || str(r["password"]) != "" {
			continue
		}
		country := strings.ToUpper(str(r["country_code"]))
		if country == "" {
			country = str(r["country"])
		}
		if geo, ok := r["geolocation"].(map[string]any); ok {
			if c, ok := geo["country"].(map[string]any); ok {
				country = str(c["iso_code"])
			}
		}
		protocols := []string{str(r["protocol"])}
		if ps, ok := r["protocols"].([]any); ok {
			protocols = nil
			for _, v := range ps {
				protocols = append(protocols, str(v))
			}
		}
		host := str(r["ip"])
		if host == "" {
			host = str(r["host"])
		}
		port := number(r["port"])
		value := str(r["proxy"])
		if value == "" && host != "" && port == float64(int(port)) && port >= 1 && port <= 65535 {
			value = net.JoinHostPort(host, strconv.Itoa(int(port)))
		}
		if value == "" {
			continue
		}
		score := number(r["uptime_percent"])
		if score == 0 {
			score = number(r["uptime"])
		}
		for _, protocol := range protocols {
			// Explicit false capability is different from absent metadata. Unknown
			// HTTP capabilities still have to pass our CONNECT probe before use.
			if protocol == "http" && (r["ssl"] == false || r["https"] == false) {
				continue
			}
			add(value, protocol, strings.ToUpper(country), score, number(r["latency_ms"]))
		}
	}
	return nil
}
func str(v any) string { s, _ := v.(string); return s }
func number(v any) float64 {
	switch x := v.(type) {
	case json.Number:
		n, _ := x.Float64()
		return n
	case string:
		n, _ := strconv.ParseFloat(x, 64)
		return n
	}
	return 0
}

var combinedAddress = regexp.MustCompile(`(?:[0-9]{1,3}\.){3}[0-9]{1,3}:[0-9]{1,5}`)

func decodeHTMLSource(raw []byte, add addCandidate) error {
	lower := bytes.ToLower(raw)
	if bytes.Contains(lower, []byte("aliyun_waf")) || bytes.Contains(lower, []byte("captcha")) && !bytes.Contains(lower, []byte("<table")) {
		return errSourceChallenge
	}
	doc, err := html.Parse(bytes.NewReader(raw))
	if err != nil {
		return errSourceFormat
	}
	found := false
	walkHTML(doc, func(table *html.Node) {
		if table.Type != html.ElementNode || table.Data != "table" {
			return
		}
		headers := map[string]int{}
		walkHTML(table, func(tr *html.Node) {
			if tr.Type != html.ElementNode || tr.Data != "tr" {
				return
			}
			cells := []*html.Node{}
			for c := tr.FirstChild; c != nil; c = c.NextSibling {
				if c.Type == html.ElementNode && (c.Data == "td" || c.Data == "th") {
					cells = append(cells, c)
				}
			}
			if len(cells) == 0 {
				return
			}
			if len(headers) == 0 {
				for i, c := range cells {
					key := strings.ToLower(strings.Join(strings.Fields(htmlText(c)), ""))
					switch key {
					case "ip", "ip地址":
						headers["ip"] = i
					case "port", "端口", "端口号":
						headers["port"] = i
					case "类型", "协议", "protocol":
						headers["protocol"] = i
					case "代理", "proxy":
						headers["combined"] = i
					case "国家", "地区", "位置", "属地位置", "country":
						headers["country"] = i
					case "用户名", "username":
						headers["username"] = i
					case "密码", "password":
						headers["password"] = i
					}
				}
				_, ip := headers["ip"]
				_, port := headers["port"]
				_, combined := headers["combined"]
				if ip && port || combined {
					found = true
				} else {
					headers = map[string]int{}
				}
				return
			}
			get := func(key string) string {
				i, ok := headers[key]
				if !ok || i >= len(cells) {
					return ""
				}
				return strings.TrimSpace(htmlText(cells[i]))
			}
			for _, key := range []string{"username", "password"} {
				if i, ok := headers[key]; ok && i < len(cells) && !noAuthCell(cells[i]) {
					return
				}
			}
			country := get("country")
			if strings.Contains(country, "中国") {
				country = "CN"
			}
			country = strings.ToUpper(country)
			protocol := strings.ToLower(get("protocol"))
			value := ""
			if _, ok := headers["combined"]; ok {
				combined := strings.ToLower(strings.Join(strings.Fields(get("combined")), ""))
				value = combinedAddress.FindString(combined)
				for _, p := range []string{"socks5", "socks4", "https", "http"} {
					if strings.HasPrefix(combined, p) {
						protocol = p
						break
					}
				}
			} else {
				value = net.JoinHostPort(get("ip"), get("port"))
			}
			add(value, protocol, country, 0, 0)
		})
	})
	if !found {
		return errSourceFormat
	}
	return nil
}

func walkHTML(root *html.Node, visit func(*html.Node)) {
	var walk func(*html.Node, int)
	walk = func(n *html.Node, depth int) {
		if depth > 128 {
			return
		}
		visit(n)
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c, depth+1)
		}
	}
	walk(root, 0)
}
func htmlText(n *html.Node) string {
	var b strings.Builder
	walkHTML(n, func(c *html.Node) {
		if c.Type == html.TextNode {
			b.WriteString(c.Data)
		}
	})
	return b.String()
}
func noAuthCell(n *html.Node) bool {
	value := strings.ToLower(strings.TrimSpace(htmlText(n)))
	image, noNeed := false, false
	walkHTML(n, func(c *html.Node) {
		if c.Data == "img" {
			image = true
		}
		for _, a := range c.Attr {
			if a.Key == "data-text" && strings.EqualFold(a.Val, "no need") {
				noNeed = true
			}
		}
	})
	if image {
		return noNeed
	}
	return value == "" || value == "-" || value == "no need" || value == "无需"
}
