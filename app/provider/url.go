package provider

import (
	"net/url"
	"sort"
	"strings"
)

// Absolutize 把一个可能是相对的地址按 base 解析成绝对地址。
//
// base 应当是**重定向之后的最终 URL**，不是发起请求前的那个：Legado 两个基准并存
// （redirectUrl 用于解析规则里取出的相对地址，baseUrl 是请求前的 URL，
// AnalyzeRule.kt:105-115,319-325），而规则里取出的地址是相对**它所在的那一页**的，
// 那一页可能就是重定向后的地址。
//
// 返回空串表示这个值不该被当地址用：javascript: 伪协议、data: URL、
// 以及解析失败的情况。调用方据此判断"源没有给出下一步地址"，
// 而不是把一个假地址存进库里再去请求它。
func Absolutize(base, raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	lowered := strings.ToLower(value)
	// 这三类不是可请求的地址：javascript: 是页面脚本、data: 是内联内容、
	// "#" 开头是同一页内的锚点。Legado 对 javascript: 也返回空
	// （utils/NetworkUtils.kt:160-189）。
	if strings.HasPrefix(lowered, "javascript:") || strings.HasPrefix(lowered, "data:") {
		return ""
	}
	if strings.HasPrefix(value, "#") {
		return ""
	}
	baseURL, err := url.Parse(strings.TrimSpace(base))
	if err != nil || baseURL.Scheme == "" || baseURL.Host == "" {
		// 没有可用的基准：只有当它本身就是绝对地址时才还有救。
		if absolute, parseErr := url.Parse(value); parseErr == nil && absolute.Scheme != "" && absolute.Host != "" {
			return normalizeScheme(absolute)
		}
		return ""
	}
	resolved, err := baseURL.Parse(value)
	if err != nil {
		return ""
	}
	if resolved.Scheme != "http" && resolved.Scheme != "https" {
		return ""
	}
	return normalizeScheme(resolved)
}

func normalizeScheme(u *url.URL) string {
	// url.URL.String() 已经会补 "//" 与默认端口省略，这里只统一小写 scheme 与 host：
	// 同一个地址的大小写变体在去重集合里必须是同一个键。
	out := *u
	out.Scheme = strings.ToLower(out.Scheme)
	out.Host = strings.ToLower(out.Host)
	return out.String()
}

// RemoteID 从绝对地址算出章节/作品的稳定标识。
//
// 形状取自 Mihon 的 setUrlWithoutDomain（HttpSource.kt:428-463）——去域名的
// path+query+fragment——但多走两步：
//
//   - **剥掉 fragment**。锚点不改变服务端返回的内容，留着它会让
//     "同一章的两个锚点"变成两章。
//   - **query 键排序**。同一个源在不同请求里参数顺序可能不同，
//     不排序会让同一章算出两个 remote_id，目录同步时每章都出现两遍。
//
// 域名被剥掉是有意的：源站换域名（镜像、防封）不该让整本书的进度失效。
// 代价是 path 一变就认不出来——那一层由 db.ReplaceLibraryChapters 的
// 标题+序兜底接住（ADR 0016）。
//
// path 为空时（API 源常见，所有章节都是 ?id=123 这种形状）退回用排序后的 query，
// 否则整本书的每一章 remote_id 都是空串，会在 UNIQUE(item_id, remote_id) 上互相撞。
func RemoteID(absoluteURL string) string {
	trimmed := strings.TrimSpace(absoluteURL)
	if trimmed == "" {
		return ""
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		// 解析不了就原样当标识用：它至少是稳定的，而丢掉它会让这一章无法入库。
		return trimmed
	}
	path := parsed.Path
	query := sortedQuery(parsed)
	switch {
	case path != "" && path != "/" && query != "":
		return path + "?" + query
	case path != "" && path != "/":
		return path
	case query != "":
		return "?" + query
	default:
		// 只有域名、没有路径也没有参数：整本书共用一个地址的源（单页应用）
		// 会走到这里。退回完整地址，让上层至少能把它当一个身份存下来。
		return strings.ToLower(parsed.Scheme) + "://" + strings.ToLower(parsed.Host)
	}
}

// sortedQuery 把 query 按键排序后重新编码。
//
// 同名多值保留全部并按值排序，这样 ?t=2&t=1 与 ?t=1&t=2 得到同一个结果。
func sortedQuery(u *url.URL) string {
	values := u.Query()
	if len(values) == 0 {
		return ""
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := url.Values{}
	for _, key := range keys {
		list := append([]string(nil), values[key]...)
		sort.Strings(list)
		out[key] = list
	}
	return out.Encode()
}

// SameTarget 报告两个地址是不是指向同一个资源，用于"别把下一章吞进本章正文"。
//
// Legado 在这里做的是绝对化之后的**裸字符串相等**（BookContent.kt:86-89），
// 只做绝对化、不做归一，所以下一章地址带了不同的签名参数就拦不住。
// 我们比的是 remote_id：剥掉 fragment、query 排序，两处归一之后相同即视为同一目标。
//
// 仍然拦不住"签名参数本身在变"的情况——那种源必须靠显式的 id 字段来标识章节。
// 这是已知的边界，不是可以靠比较方式修掉的。
func SameTarget(base, left, right string) bool {
	l := RemoteID(Absolutize(base, left))
	r := RemoteID(Absolutize(base, right))
	if l == "" || r == "" {
		return false
	}
	return l == r
}

// HostOf 取出一个地址的主机名，代理与诊断用它分组统计。
func HostOf(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	return strings.ToLower(parsed.Hostname())
}
