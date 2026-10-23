package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"cczjVideo/app/apperror"
	"cczjVideo/app/model"
)

func fanqieTestPage(chapter map[string]any, font string) string {
	raw, _ := json.Marshal(map[string]any{"reader": map[string]any{"chapterData": chapter}})
	return `<style>src:url(https://cdn.example/awesome-font/c/` + font + `.woff2)</style><script>window.__INITIAL_STATE__ = ` + string(raw) + `; throw new Error("never execute");</script>`
}

func TestFanqieClassicChapterLegacyGBKCharacterIsNotFontObfuscation(t *testing.T) {
	// Public Journey to the West chapter 2, item 7345722494013148201.
	// The chapter's webfont does not contain E82F; it is a legacy GBK PUA
	// character in the original text, rather than an encoded webfont glyph.
	chapter := map[string]any{"itemId": "7345722494013148201", "content": "<p>悟空笑道：师父果有些滴\ue82f。</p>", "needPay": 0, "isChapterLock": false}
	text, err := decodeFanqieContent(fanqieTestPage(chapter, "dc027189e0ba4cd"), "7345722494013148201", 4<<20)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(text.Paragraphs, ""); got != "悟空笑道：师父果有些滴㳠。" {
		t.Fatalf("wrong legacy character: %q", got)
	}
}

func TestFanqieLegacyCharactersDoNotDisableUnknownFontAndGlyphChecks(t *testing.T) {
	for _, row := range []struct{ content, font string }{
		{"<p>\ue82f</p>", "0000000"},
		{"<p>\ue82f\uf000</p>", "dc027189e0ba4cd"},
	} {
		chapter := map[string]any{"itemId": "123", "content": row.content}
		if _, err := decodeFanqieContent(fanqieTestPage(chapter, row.font), "123", 4<<20); !apperror.Is(err, apperror.Unsupported) {
			t.Fatalf("unknown font/glyph accepted: %v", err)
		}
	}
	chapter := map[string]any{"itemId": "123", "content": "<p>\ue423\ue82f</p>"}
	text, err := decodeFanqieContent(fanqieTestPage(chapter, "dc027189e0ba4cd"), "123", 4<<20)
	if err != nil || strings.Join(text.Paragraphs, "") != "大㳠" {
		t.Fatalf("mixed alphabets: %v %v", text, err)
	}
}

func TestFanqiePublicTextDecodesKnownFontAndRejectsUnknownAndLockedChapters(t *testing.T) {
	chapter := map[string]any{"itemId": "123", "content": "<p>\ue423乾皇朝，疆域亿\ue466\ue44d。</p>", "needPay": 0, "isChapterLock": false}
	text, err := decodeFanqieContent(fanqieTestPage(chapter, "dc027189e0ba4cd"), "123", 4<<20)
	if err != nil || strings.Join(text.Paragraphs, "") != "大乾皇朝，疆域亿万里。" {
		t.Fatal(text, err)
	}
	if _, err = decodeFanqieContent(fanqieTestPage(chapter, "0000000"), "123", 4<<20); !apperror.Is(err, apperror.Unsupported) {
		t.Fatal("unknown font accepted", err)
	}
	chapter["isChapterLock"] = true
	if _, err = decodeFanqieContent(fanqieTestPage(chapter, "dc027189e0ba4cd"), "123", 4<<20); !apperror.Is(err, apperror.PermissionDenied) {
		t.Fatal("locked chapter accepted", err)
	}
	chapter["isChapterLock"] = false
	chapter["content"] = "<p>\uf000</p>"
	if _, err = decodeFanqieContent(fanqieTestPage(chapter, "dc027189e0ba4cd"), "123", 4<<20); !apperror.Is(err, apperror.Unsupported) {
		t.Fatal("unknown glyph accepted", err)
	}
}

type fanqieTransport func(*http.Request) (*http.Response, error)

func (f fanqieTransport) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func TestFanqieSearchPreservesLargeIDsFiltersAudioAndContinuesWithExactOffset(t *testing.T) {
	fetcher := NewFetcher()
	fetcher.client.Transport = fanqieTransport(func(request *http.Request) (*http.Response, error) {
		if request.URL.Query().Get("offset") != "20" || request.URL.Query().Get("q") != "book" {
			t.Fatal(request.URL)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"code":0,"data":{"has_more":true,"offset":30,"ret_data":[{"book_id":"9007199254740993123","title":"<em>book</em>","genre":"6"},{"book_id":"2","title":"audio","genre":"4"}]}}`))}, nil
	})
	doc, err := ParseDocument(`{"version":3,"kind":"novel","adapter":"fanqie-web"}`, "novel")
	if err != nil {
		t.Fatal(err)
	}
	pipeline := NewPipeline(doc, fetcher, DefaultLimits())
	result, err := pipeline.Search(context.Background(), "https://fanqienovel.com", "book", 3, "")
	if err != nil || len(result.Items) != 1 || result.Items[0].RemoteID != "9007199254740993123" || result.Items[0].Title != "book" || !strings.Contains(result.NextURL, "offset=30") {
		t.Fatal(result, err)
	}
	if _, err = pipeline.Content(context.Background(), "", &Chapter{URL: "https://evil.example/reader/123", Access: model.ChapterAccessAvailable}, ""); !apperror.Is(err, apperror.Validation) {
		t.Fatal("nonofficial chapter accepted", err)
	}
}
