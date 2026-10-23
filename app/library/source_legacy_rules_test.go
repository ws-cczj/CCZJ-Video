package library

import (
	"cczjVideo/app/provider"
	"encoding/json"
	"testing"
)

func TestFixedBookListIsNotAKeywordSearch(t *testing.T) {
	raw := `{"bookSourceUrl":"https://example.org","searchUrl":"/author/123/","ruleSearch":{"bookList":".books a","name":"text","bookUrl":"href"},"ruleBookInfo":{"name":"h1@text"},"ruleToc":{"chapterList":".chapters a","chapterName":"text","chapterUrl":"href"},"ruleContent":{"content":"#content@text"}}`
	var fields map[string]json.RawMessage
	_ = json.Unmarshal([]byte(raw), &fields)
	if _, err := convertLegado(fields, "novel"); err == nil {
		t.Fatal("fixed author list accepted as a full keyword-search source")
	}
	if _, err := legacySearchProbe(fields, "novel"); err == nil {
		t.Fatal("fixed list accepted as a keyword-search probe")
	}
}

func TestConvertedLegacySnapshotDoesNotRestoreFixedSearchLists(t *testing.T) {
	payload, _ := json.Marshal([]map[string]any{{"name": "fixed", "kind": "novel", "format": "legado", "project": "Legado", "apiUrl": "https://example.org", "document": json.RawMessage(novelDocument())}})
	rows, err := parseSourceCatalog(payload, "novel", "test")
	if err != nil || len(rows) != 1 || rows[0].Compatible {
		t.Fatal("old preconverted snapshot bypassed keyword-search validation", rows, err)
	}
}

func TestLegadoNestedRulesPreserveIndicesAndAllContentParagraphs(t *testing.T) {
	raw := `{"bookSourceUrl":"https://example.org","searchUrl":"/search?q={{key}}","ruleSearch":{"bookList":"class.books@tag.li","name":"tag.a.0@text##《|》","bookUrl":"tag.a.0@href"},"ruleBookInfo":{"name":"tag.h1@text"},"ruleToc":{"chapterList":"class.chapters@tag.a","chapterName":"text","chapterUrl":"href"},"ruleContent":{"content":"id.content@tag.p@text","replaceRegex":"广告"}}`
	var fields map[string]json.RawMessage
	_ = json.Unmarshal([]byte(raw), &fields)
	encoded, err := convertLegado(fields, "novel")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := provider.ParseDocument(encoded, "novel")
	if err != nil {
		t.Fatal(err)
	}
	items, _, err := provider.ExtractItems(doc, doc.Operations.Search, &provider.Response{StatusCode: 200, ContentType: "text/html", FinalURL: "https://example.org/search", Text: `<ul class="books"><li><a href="/book/1">《测试》</a><a href="/ad">广告</a></li></ul>`})
	if err != nil || len(items) != 1 || items[0].Title != "测试" || items[0].URL != "https://example.org/book/1" {
		t.Fatalf("items %+v %v", items, err)
	}
	body, _, err := provider.ExtractText(doc, doc.Operations.Content, &provider.Response{StatusCode: 200, ContentType: "text/html", FinalURL: "https://example.org/chapter", Text: `<div id="content"><p>第一段广告</p><p>第二段</p></div>`})
	if err != nil || body != "第一段\n第二段" {
		t.Fatalf("body %q %v", body, err)
	}
}

func TestLegadoComicImageRulesCanImportWithoutExecutingScripts(t *testing.T) {
	raw := `{"bookSourceUrl":"https://example.org","searchUrl":"/search?q={{key}}","ruleSearch":{"bookList":".books a","name":"text","bookUrl":"href"},"ruleBookInfo":{"name":"h1@text"},"ruleToc":{"chapterList":".chapters a","chapterName":"text","chapterUrl":"href"},"ruleContent":{"content":".pages@tag.img@data-src"}}`
	var fields map[string]json.RawMessage
	_ = json.Unmarshal([]byte(raw), &fields)
	encoded, err := convertLegado(fields, "comic")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := provider.ParseDocument(encoded, "comic")
	if err != nil {
		t.Fatal(err)
	}
	pages, _, err := provider.ExtractPages(doc, doc.Operations.Pages, &provider.Response{StatusCode: 200, ContentType: "text/html", FinalURL: "https://example.org/chapter", Text: `<div class="pages"><img data-src="/one.jpg"><img data-src="/two.jpg"></div>`})
	if err != nil || len(pages) != 2 || pages[1].ImageURL != "https://example.org/two.jpg" {
		t.Fatalf("pages %+v %v", pages, err)
	}
}
