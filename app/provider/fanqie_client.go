package provider

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"cczjVideo/app/apperror"
)

const fanqieDHPrime = "2410312426921032588552076022197566074856950548502459942654116941958108831682612228890093858261341614673227141477904012196503648957050582631942730706805009223062734745341073406696246014589361659774041027169249453200378729434170325843778659198143763193776859869524088940195577346119843545301547043747207749969763750084308926339295559968882457872412993810129130294592999947926365264059284647209730384947211681434464714438488520940127459844288859336526896320919633919"

func (p *Pipeline) fanqieClientContent(ctx context.Context, id, title string) (*TextContent, error) {
	if !fanqieNumber.MatchString(id) {
		return nil, apperror.New(apperror.Validation, "番茄章节标识无效")
	}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	prime, ok := new(big.Int).SetString(fanqieDHPrime, 10)
	if !ok {
		return nil, apperror.New(apperror.Internal, "正文协议初始化失败")
	}
	secret := make([]byte, 32)
	iv := make([]byte, 16)
	if _, err := rand.Read(secret); err != nil {
		return nil, err
	}
	defer clear(secret)
	if _, err := rand.Read(iv); err != nil {
		return nil, err
	}
	exponent := new(big.Int).SetBytes(secret)
	defer exponent.SetInt64(0)
	public := new(big.Int).Exp(big.NewInt(2), exponent, prime).Bytes()
	if public[0]&128 != 0 {
		public = append([]byte{0}, public...)
	}
	requestKey, _ := base64.StdEncoding.DecodeString("rCXGfd2POMGzeiNIgo4iLg==")
	encrypted, err := fanqieCBC(public, requestKey, iv)
	if err != nil {
		return nil, err
	}
	params := url.Values{"aid": {"13"}, "app_name": {"novelapp"}, "channel": {"0"}, "device_platform": {"android"}, "device_type": {"25053RT47C"}, "item_id": {id}, "os_version": {"10"}, "support_image": {"1"}, "version_code": {"66.9"}, "version_name": {"6.6.9"}, "device_id": {"0"}}
	query := params.Encode()
	headers, err := fanqieClientSign(query, time.Now().Unix())
	if err != nil {
		return nil, apperror.Wrap(apperror.Internal, err, "正文请求初始化失败")
	}
	headers["y"] = base64.StdEncoding.EncodeToString(append(iv, encrypted...))
	headers["User-Agent"] = "com.ss.android.article.news/13400 (Linux; U; Android 10; zh_CN; tb8788p1_64_bsp; Build/QQ3A.200805.001; Cronet/TTNetVersion:fc4cebd3 2024-12-10 QuicVersion:d9628e3d 2024-10-11)"
	address := "https://novel.snssdk.com/api/novel/book/reader/content/v1?" + query
	response, err := p.Fetcher.Fetch(ctx, p.Doc, &Operation{Format: "json", Headers: headers}, address, Target{URL: address, PreserveQuery: true})
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, apperror.Wrap(apperror.Timeout, err, "番茄客户端正文请求超时")
		}
		return nil, err
	}
	var result struct {
		Code *int `json:"code"`
		Data struct {
			Content string `json:"content"`
			ItemID  string `json:"item_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(response.Text), &result); err != nil || result.Code == nil {
		return nil, apperror.New(apperror.Corrupt, "番茄客户端正文响应格式异常")
	}
	if *result.Code != 0 {
		return nil, apperror.New(apperror.Unavailable, "番茄客户端正文暂不可用")
	}
	if result.Data.ItemID != "" && result.Data.ItemID != id {
		return nil, apperror.New(apperror.Corrupt, "番茄客户端返回了其他章节")
	}
	y, err := base64.StdEncoding.DecodeString(response.protocolHeaders["y"])
	if err != nil || len(y) == 0 || len(y) > 193 {
		return nil, apperror.New(apperror.Corrupt, "番茄正文密钥协商响应异常")
	}
	peer := new(big.Int).SetBytes(y)
	upper := new(big.Int).Sub(prime, big.NewInt(1))
	if peer.Cmp(big.NewInt(1)) <= 0 || peer.Cmp(upper) >= 0 {
		return nil, apperror.New(apperror.Corrupt, "番茄正文密钥协商响应异常")
	}
	shared := new(big.Int).Exp(peer, exponent, prime)
	defer shared.SetInt64(0)
	keyBytes := shared.Bytes()
	defer clear(keyBytes)
	if len(keyBytes) < 32 {
		return nil, apperror.New(apperror.Corrupt, "番茄正文密钥协商响应异常")
	}
	encoded := response.protocolHeaders["c"]
	if encoded == "" || encoded == "1" {
		encoded = result.Data.Content
	}
	if len(encoded) > ((p.Limits.MaxContentBytes+32)*4/3)+8 {
		return nil, apperror.New(apperror.Corrupt, "番茄正文超过单章上限")
	}
	blob, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(blob) < 32 || (len(blob)-16)%16 != 0 {
		return nil, apperror.New(apperror.Corrupt, "番茄正文数据损坏")
	}
	block, err := aes.NewCipher(keyBytes[:32])
	if err != nil {
		return nil, err
	}
	plain := make([]byte, len(blob)-16)
	defer clear(plain)
	cipher.NewCBCDecrypter(block, blob[:16]).CryptBlocks(plain, blob[16:])
	padding := int(plain[len(plain)-1])
	if padding < 1 || padding > 16 || padding > len(plain) {
		return nil, apperror.New(apperror.Corrupt, "番茄正文解码失败")
	}
	for _, v := range plain[len(plain)-padding:] {
		if int(v) != padding {
			return nil, apperror.New(apperror.Corrupt, "番茄正文解码失败")
		}
	}
	plain = plain[:len(plain)-padding]
	if !utf8.Valid(plain) || len(plain) > p.Limits.MaxContentBytes {
		return nil, apperror.New(apperror.Corrupt, "番茄正文编码或长度异常")
	}
	extractor, err := NewExtractor("html", string(plain))
	if err != nil {
		return nil, apperror.New(apperror.Corrupt, "番茄正文结构异常")
	}
	text, err := extractor.String(extractor.Root(), &Selector{Attr: "text"})
	if err != nil {
		return nil, err
	}
	paragraphs := SplitParagraphs(text)
	if len(paragraphs) > 1 && strings.TrimSpace(title) != "" && strings.Join(strings.Fields(paragraphs[0]), "") == strings.Join(strings.Fields(title), "") {
		paragraphs = paragraphs[1:]
	}
	if len(paragraphs) == 0 || LooksGarbled(text) || strings.TrimSpace(text) == "" {
		return nil, apperror.New(apperror.Corrupt, "番茄正文为空或字符异常")
	}
	return &TextContent{Paragraphs: paragraphs}, nil
}
