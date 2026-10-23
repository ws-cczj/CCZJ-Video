package provider

import (
	"bytes"
	"fmt"
	"io"
	"mime"
	"net/http"
	"regexp"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
	"golang.org/x/text/transform"
)

// 响应解码的探测顺序，与 Legado 实测出的那条链一致
// （help/http/OkHttpUtils.kt:79-95 → utils/EncodingDetect.kt:18-58）：
//
//	源文档显式声明 → HTTP Content-Type 的 charset → HTML <meta> → UTF-8
//
// 与 Legado 有两处刻意的不同：
//
//  1. 显式声明**排在最前**并且真的参与响应解码。Legado 的 URL option `charset`
//     只影响请求参数编码，完全不参与响应解码，所以那边 GBK 站能不能读对全看探测；
//     我们让用户能在加源时直接指定，探测失败时有一条自救路径。
//  2. **不做统计探测**。Legado 兜底是 icu4j 的 CharsetDetector，Go 生态没有等价物，
//     为一个长尾场景引入一套统计模型不划算。探测不到就按 UTF-8 解，
//     解出来若含大量替换字符则在诊断里报"这个源可能需要显式指定编码"，
//     而不是猜一个可能更错的。
const (
	encodingAuto = "auto"
	encodingUTF8 = "utf-8"
)

var encodings = map[string]encoding.Encoding{
	"gbk":       simplifiedchinese.GBK,
	"gb18030":   simplifiedchinese.GB18030,
	"gb2312":    simplifiedchinese.HZGB2312,
	"big5":      traditionalchinese.Big5,
	"shift_jis": japanese.ShiftJIS,
	"euc-jp":    japanese.EUCJP,
	"euc-kr":    korean.EUCKR,
}

// legacyGBKRune recognizes the historical GBK private-use block FE50..FEA0.
// Sources may retain these Unicode PUA characters in otherwise UTF-8 text.
// This is opt-in after a source's own font alphabet, never a global PUA guess.
// Mapping evidence: https://lists.w3.org/Archives/Public/www-archive/2013Dec/0010.html
func legacyGBKRune(r rune) (rune, bool) {
	if r < 0xe815 || r > 0xe864 {
		return 0, false
	}
	trail := 0x50 + int(r-0xe815)
	if trail >= 0x7f {
		trail++
	}
	decoded, err := encodings["gb18030"].NewDecoder().Bytes([]byte{0xfe, byte(trail)})
	if err != nil {
		return 0, false
	}
	canonical, size := utf8.DecodeRune(decoded)
	return canonical, size == len(decoded) && canonical != utf8.RuneError && !(canonical >= 0xe000 && canonical <= 0xf8ff)
}

// IsSupportedEncoding 报告一个编码名能不能用。auto 与 utf-8 是特殊值，
// 其余按 encodings 表的键匹配（大小写与常见别名归一后）。
func IsSupportedEncoding(name string) bool {
	key := normalizeEncodingName(name)
	if key == encodingAuto || key == encodingUTF8 {
		return true
	}
	_, ok := encodings[key]
	return ok
}

// SupportedEncodings 返回可选的编码名，加源界面的下拉拿它。
func SupportedEncodings() []string {
	names := []string{encodingAuto, encodingUTF8}
	for key := range encodings {
		names = append(names, key)
	}
	// map 迭代顺序随机，而这个列表要直接进界面，抖动的顺序会被当成 bug 报上来。
	sortStrings(names)
	return names
}

func sortStrings(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}

// normalizeEncodingName 把常见写法收敛成 encodings 表的键。
//
// 源文档是用户手填的，"GBK"、"gb-2312"、"x-mac-cyrillic" 这种写法都会出现；
// 这里只做**等价别名**归一，不做猜测——认不出来就原样返回，
// 让 IsSupportedEncoding 去拒绝，而不是悄悄换成一个相近的编码。
func normalizeEncodingName(name string) string {
	key := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(name), "_", "-"))
	switch key {
	case "", encodingAuto:
		return encodingAuto
	case "utf8", "utf-8", "unicode-1-1-utf-8":
		return encodingUTF8
	case "gb", "gbk", "cp936", "ms936", "windows-936":
		return "gbk"
	case "gb18030", "gb-18030":
		return "gb18030"
	case "gb2312", "gb-2312", "hz-gb-2312", "hz":
		return "gb2312"
	case "big5", "big-5", "cp950":
		return "big5"
	case "shift_jis", "shift-jis", "sjis", "cp932", "windows-31j":
		return "shift_jis"
	case "euc-jp", "eucjp":
		return "euc-jp"
	case "euc-kr", "euckr", "cp949":
		return "euc-kr"
	}
	return key
}

// maxEncodingProbeBytes 限制为找 <meta charset> 而解析的字节数。
// charset 声明必然在 <head> 里，而 <head> 在文档开头；不需要把整份 HTML 解析一遍。
const maxEncodingProbeBytes = 4096

var (
	metaCharsetRe        = regexp.MustCompile(`(?is)<meta[^>]+charset\s*=\s*["']?\s*([a-z0-9_\-]+)`)
	metaHTTPEquivRe      = regexp.MustCompile(`(?is)<meta[^>]+http-equiv\s*=\s*["']?content-type["']?[^>]+content\s*=\s*["'][^"']*charset=([a-z0-9_\-]+)`)
	contentTypeCharsetRe = regexp.MustCompile(`(?i)charset=([a-z0-9_\-]+)`)
)

// Decode 把响应字节解成字符串。declared 是源文档里显式声明的编码，可为空。
func Decode(body []byte, resp *http.Response, declared string) (string, error) {
	body = stripBOM(body)
	if name := normalizeEncodingName(declared); name != encodingAuto {
		return decodeWith(body, name)
	}
	if resp != nil {
		if name := charsetFromContentType(resp.Header.Get("Content-Type")); name != "" {
			if text, err := decodeWith(body, name); err == nil {
				return text, nil
			}
			// 头里声明的编码解不动（源站写错了 charset 是常事）：不报错，
			// 继续往下探测。一个能读出来的错误编码比一个报错的正确编码有用。
		}
	}
	if name := charsetFromMeta(body); name != "" {
		if text, err := decodeWith(body, name); err == nil {
			return text, nil
		}
	}
	return string(body), nil
}

func decodeWith(body []byte, name string) (string, error) {
	key := normalizeEncodingName(name)
	if key == encodingUTF8 || key == encodingAuto {
		return string(body), nil
	}
	enc, ok := encodings[key]
	if !ok {
		return "", fmt.Errorf("不支持的编码 %q", name)
	}
	reader := transform.NewReader(bytes.NewReader(body), enc.NewDecoder())
	// UTF-8 的中文正文一章通常几 KB 到几十 KB，4 MiB 的上限与正文缓存的
	// 单章上限一致（ADR 0016），所以这里不会因为解码而放进比缓存更大的东西。
	decoded, err := io.ReadAll(io.LimitReader(reader, maxChapterContentBytes*2))
	if err != nil {
		return "", fmt.Errorf("按 %s 解码失败: %w", key, err)
	}
	return string(decoded), nil
}

func charsetFromContentType(contentType string) string {
	if contentType == "" {
		return ""
	}
	// 先用 mime 包正经解析一次：它认得引号、参数顺序与多参数。
	if _, params, err := mime.ParseMediaType(contentType); err == nil {
		if cs := strings.TrimSpace(params["charset"]); cs != "" {
			return normalizeEncodingName(cs)
		}
	}
	if m := contentTypeCharsetRe.FindStringSubmatch(contentType); m != nil {
		return normalizeEncodingName(m[1])
	}
	return ""
}

// charsetFromMeta 在文档开头找 <meta charset> 或 <meta http-equiv=content-type>。
//
// 用正则而不是解析 DOM：这一步发生在**还不知道编码是什么**的时候，
// 而 x/net/html 的解析器假定输入是 UTF-8 解码后的字符串。用 GBK 字节喂给它，
// <head> 之外的内容会解出一堆替换字符——虽然 <meta> 本身是 ASCII 所以还能找到，
// 但为此解析整份文档没有意义。Legado 那边同样是先字节级截取 <head> 再解析
// （utils/EncodingDetect.kt:18-53）。
func charsetFromMeta(body []byte) string {
	probe := body
	if len(probe) > maxEncodingProbeBytes {
		probe = probe[:maxEncodingProbeBytes]
	}
	// ASCII 范围内的字节在任何单字节兼容编码里都是它自己，所以直接按 latin1 看待是安全的。
	text := string(probe)
	if m := metaCharsetRe.FindStringSubmatch(text); m != nil {
		return normalizeEncodingName(m[1])
	}
	if m := metaHTTPEquivRe.FindStringSubmatch(text); m != nil {
		return normalizeEncodingName(m[1])
	}
	return ""
}

// stripBOM 去掉 UTF-8 BOM。Legado 在解码前也做这一步（OkHttpUtils.kt:80）：
// 留着它会让正文首段多出一个不可见字符，而 JSON 源会直接解析失败。
func stripBOM(body []byte) []byte {
	return bytes.TrimPrefix(body, []byte{0xEF, 0xBB, 0xBF})
}

// LooksGarbled 报告一段解出来的文本是不是"编码错了"的样子。
//
// 判据是替换字符 U+FFFD 的占比：按错误的编码解 UTF-8/GBK 字节，
// 解码器会大量产出 U+FFFD。这个信号只用来在诊断里提示"试试显式指定编码"，
// **不用来自动改判编码**——猜错的代价（把能读的正文换成另一种乱码）比不猜更高。
func LooksGarbled(text string) bool {
	if text == "" {
		return false
	}
	replacements := strings.Count(text, "\uFFFD")
	if replacements == 0 {
		return false
	}
	runes := len([]rune(text))
	if runes == 0 {
		return false
	}
	return float64(replacements)/float64(runes) > 0.02
}

// DetectFontObfuscation 报告一段正文是不是像被字体码位混淆过。
//
// 手法见 reports/06 §6：站点下发一份码位↔字形被打乱的 WebFont，浏览器渲染正常，
// 但抽出来的文本是**码点有效、字义错误的汉字**。不是方块、不是编码乱码，
// 用户看到的是一屏"正常汉字组成的胡话"，完全无法自查。
//
// Legado 靠 JS 宿主函数 java.queryTTF / java.replaceFont 解（JsExtensions.kt:802-910，
// 用字形轮廓指纹反查真实码位）。我们这轮不实现解密——它需要解析字体文件，
// 量级不小，而且在 Legado 那边也只被 JS 规则触达，属于长尾。
// 但**必须能报出来**，否则用户面对一屏胡话没有任何线索。
//
// 判据是私用区码位（U+E000–U+F8FF）的占比。字体混淆通常把常用字映射到私用区，
// 正常中文正文里私用区字符极罕见，所以这个信号的误报率很低。
// 它是**提示**不是判决：命中了只说"这个源可能用了字体混淆，本项目不支持"，
// 不阻止显示——万一判错了，用户至少还能看到内容。
func DetectFontObfuscation(text string) bool {
	if text == "" {
		return false
	}
	privateUse := 0
	total := 0
	for _, r := range text {
		total++
		if r >= 0xE000 && r <= 0xF8FF {
			privateUse++
		}
	}
	if total == 0 || privateUse == 0 {
		return false
	}
	return float64(privateUse)/float64(total) > 0.05
}
