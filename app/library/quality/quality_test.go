package quality

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"cczjVideo/app/apperror"
	"cczjVideo/app/provider"
)

func TestIdentityDoesNotMergeAdaptationsSequelsOrHomonyms(t *testing.T) {
	w := Work{Title: "斗罗大陆", Aliases: []string{"鬥羅大陸"}, Authors: []string{"唐家三少"}, References: []Reference{{URL: "https://original.example/book/1", Role: "metadata"}}}
	for _, tc := range []struct{ title, author, want string }{
		{"斗罗大陆", "唐家三少", "verified"}, {"鬥羅大陸", "作者：唐家三少", "verified"},
		{"斗罗大陆 II 绝世唐门", "唐家三少", "mismatch"}, {"斗罗大陆（漫画版）", "唐家三少", "mismatch"},
		{"斗罗大陆", "另一个人", "mismatch"}, {"斗罗大陆", "", "provisional"},
	} {
		if got := Match(w, &provider.Item{Title: tc.title, Author: tc.author}).Status; got != tc.want {
			t.Fatalf("%s/%s: got %s want %s", tc.title, tc.author, got, tc.want)
		}
	}
}

func TestIdentityUnderstandsAuthorCreditsWithoutMatchingTranslators(t *testing.T) {
	w := Work{Title: "无人生还", Authors: []string{"阿加莎·克里斯蒂"}}
	for _, tc := range []struct{ author, want string }{
		{"（英）阿加莎·克里斯蒂 著；夏阳 译", "verified"},
		{"作者：【英】阿加莎·克里斯蒂 著", "verified"},
		{"【英】别人 著,阿加莎·克里斯蒂 译", "mismatch"},
		{"阿加莎·克里斯蒂研究会", "mismatch"},
		{"（同人）阿加莎·克里斯蒂", "mismatch"},
	} {
		if got := Match(w, &provider.Item{Title: w.Title, Author: tc.author}).Status; got != tc.want {
			t.Errorf("%q: got %s, want %s", tc.author, got, tc.want)
		}
	}
}
func TestCatalogRequiresEditionAnchorsAndSeparatesVolumes(t *testing.T) {
	w := Work{Catalog: &Catalog{Count: 4, Unit: "chapter", Scope: "complete", Status: "completed", VerifiedAt: "2026-10-21", Anchors: []Anchor{{Number: 1, Title: "第一章 起风"}, {Number: 3, Title: "第三章 远行"}}}}
	chapters := []*provider.Chapter{{Title: "第一章 起风", URL: "/1"}, {Title: "第二章 落雨", URL: "/2"}, {Title: "第三章 远行", URL: "/3"}}
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	e := AnalyzeCatalog(w, chapters, now)
	if !e.ReferenceComparable || e.CountRelation != "less" || e.AnchorMatches != 2 {
		t.Fatal(e)
	}
	chapters[2].Title = "第三章 不同版本"
	if e = AnalyzeCatalog(w, chapters, now); e.ReferenceComparable || e.CountRelation != "unknown" {
		t.Fatal("compared different edition", e)
	}
	chapters = []*provider.Chapter{{Title: "第3章 结局", URL: "/3"}, {Title: "第2章 雨", URL: "/2"}, {Title: "第1章 风", URL: "/1"}}
	if e = AnalyzeCatalog(Work{}, chapters, now); !hasIssue(e.Issues, "reversed_chapter_order") {
		t.Fatal(e)
	}
	chapters = []*provider.Chapter{{Title: "第2章 结局", Volume: "上", URL: "/2"}, {Title: "第1章 新卷", Volume: "下", URL: "/n1"}}
	if e = AnalyzeCatalog(Work{}, chapters, now); hasIssue(e.Issues, "reversed_chapter_order") {
		t.Fatal("volume numbering reset is valid", e)
	}
	w.Catalog.Status = "ongoing"
	if e = AnalyzeCatalog(w, chapters, now.AddDate(0, 1, 0)); !hasIssue(e.Issues, "stale_reference") {
		t.Fatal(e)
	}
	if n, u, ok := chapterNumber("[第10话] 灰海豚女王"); !ok || n != 10 || u != "episode" {
		t.Fatal(n, u, ok)
	}
	chapters = []*provider.Chapter{{Title: "第33章 雨", URL: "/33"}, {Title: "第三十四节另种命名", URL: "/34"}, {Title: "卷末说明", URL: "/end"}, {Title: "第44章 风", URL: "/44"}}
	if e = AnalyzeCatalog(Work{}, chapters, now); hasIssue(e.Issues, "numbering_gap_review") {
		t.Fatal("invented adjacency across an unparsed entry", e)
	}
	chapters = []*provider.Chapter{{Title: "第88章 雨", URL: "/88"}, {Title: "第1章 新卷", URL: "/1"}, {Title: "第2章 风", URL: "/2"}}
	if e = AnalyzeCatalog(Work{}, chapters, now); hasIssue(e.Issues, "mixed_chapter_order") || !hasIssue(e.Issues, "numbering_decrease_review") {
		t.Fatal("unmarked volume reset must remain a review, not a confirmed disorder", e)
	}
}
func TestTextFlagsPromotionWithoutTreatingStoryDialogueAsNavigation(t *testing.T) {
	body := &provider.TextContent{Paragraphs: []string{"他笑道：下一章再说。", "作者的话：感谢大家支持。", "最新网址：www.example.net", "下一章"}}
	e := AnalyzeText(body)
	if !hasIssue(e.Issues, "promotion_in_text") || !hasIssue(e.Issues, "navigation_in_text") {
		t.Fatal(e)
	}
	e = AnalyzeText(&provider.TextContent{Paragraphs: []string{"他笑道：下一章再说。", "完结撒花，感谢阅读。"}})
	if len(e.Issues) != 0 {
		t.Fatal("short postscript or dialogue is not an ad", e)
	}
}
func hasIssue(rows []Issue, code string) bool {
	for _, r := range rows {
		if r.Code == code {
			return true
		}
	}
	return false
}

func testCorpus() Corpus {
	c := Corpus{Version: Version, UpdatedAt: "2026-10-21"}
	for i, tag := range []string{"历史", "悬疑", "科幻"} {
		c.Works = append(c.Works, Work{ID: fmt.Sprint(i), Kind: "novel", Title: fmt.Sprintf("作品%d", i), Authors: []string{"作者"}, Tags: []string{tag}, Language: "zh", Layout: "text", Edition: "原著", Selection: "测试夹具", References: []Reference{{URL: fmt.Sprintf("https://example.org/book/%d", i), CheckedAt: c.UpdatedAt, Role: "metadata"}}})
	}
	return c
}

type fakeReader struct {
	wrongAuthor, duplicate, restricted, block bool
	calls                                     int
}

func (f *fakeReader) Search(ctx context.Context, base, q string, page int, cursor string) (*provider.ItemPage, error) {
	f.calls++
	if f.block {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if q == "不存在" {
		return &provider.ItemPage{}, nil
	}
	author := "作者"
	if f.wrongAuthor {
		author = "同名异书作者"
	}
	return &provider.ItemPage{Items: []*provider.Item{{Title: q, Author: author, URL: "https://mirror.example/" + q}}}, nil
}
func (f *fakeReader) Detail(ctx context.Context, base, u string) (*provider.Item, error) {
	return &provider.Item{Title: strings.TrimPrefix(u, "https://mirror.example/"), Author: "作者", URL: u, Cover: "https://mirror.example/cover", Summary: "简介"}, nil
}
func (f *fakeReader) Toc(ctx context.Context, base string, item *provider.Item) ([]*provider.Chapter, error) {
	return []*provider.Chapter{{Title: "第1章 起风", URL: item.URL + "/1"}, {Title: "第2章 落雨", URL: item.URL + "/2"}, {Title: "第3章 远行", URL: item.URL + "/3"}}, nil
}
func (f *fakeReader) Content(ctx context.Context, base string, c *provider.Chapter, next string) (*provider.TextContent, error) {
	if f.restricted && strings.HasSuffix(c.URL, "/2") {
		return nil, apperror.New(apperror.PermissionDenied, "需要订阅")
	}
	text := strings.Repeat(c.Title+"正文", 30)
	if f.duplicate {
		text = strings.Repeat("相同正文", 40)
	}
	return &provider.TextContent{Paragraphs: []string{text}}, nil
}
func (f *fakeReader) Pages(context.Context, string, *provider.Chapter) ([]*provider.Page, error) {
	return nil, nil
}
func (f *fakeReader) ResolveImage(context.Context, string, *provider.Page) (string, error) {
	return "", nil
}

func TestRunnerPreservesCoverageAndDetectsRepeatedBodiesBeyondFirstChapter(t *testing.T) {
	c := testCorpus()
	c.Works = append(c.Works, Work{ID: "absent", Kind: "novel", Title: "不存在", Authors: []string{"作者"}, Tags: []string{"传统文学"}, Language: "zh", Layout: "text", Edition: "原著", Selection: "测试夹具", References: c.Works[0].References})
	opts := Options{MaxQueries: 4, MaxMatches: 5}
	good, err := Evaluate(context.Background(), &fakeReader{}, Source{Kind: "novel"}, c, opts, nil)
	if err != nil {
		t.Fatal(err)
	}
	if good.Summary.Queries != 4 || good.Summary.Matched != 3 || good.Summary.Readable != 9 || good.Summary.Score != 84 || good.Summary.Confidence != "limited" {
		t.Fatal(good.Summary)
	}
	bad, err := Evaluate(context.Background(), &fakeReader{duplicate: true}, Source{Kind: "novel"}, c, opts, nil)
	if err != nil {
		t.Fatal(err)
	}
	if bad.Summary.Score >= good.Summary.Score || bad.Summary.Status != "partial" {
		t.Fatal(bad.Summary, good.Summary)
	}
	if !hasIssue(bad.Works[0].Samples[1].Text.Issues, "repeated_chapter_body") {
		t.Fatal("first-chapter-only regression was missed")
	}
	wrong, err := Evaluate(context.Background(), &fakeReader{wrongAuthor: true}, Source{Kind: "novel"}, c, opts, nil)
	if err != nil {
		t.Fatal(err)
	}
	if wrong.Summary.Status != "inconclusive" || wrong.Summary.Attempts != 0 {
		t.Fatal("read a homonym", wrong.Summary)
	}
}
func TestRestrictedChaptersAndCancellationAreNotDamagedText(t *testing.T) {
	r, err := Evaluate(context.Background(), &fakeReader{restricted: true}, Source{Kind: "novel"}, testCorpus(), Options{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.Summary.Restricted != 3 || r.Summary.Readable != 6 || r.Summary.Status != "partial" {
		t.Fatal(r.Summary)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err = Evaluate(ctx, &fakeReader{block: true}, Source{Kind: "novel"}, testCorpus(), Options{}, nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}
func TestCorpusRuntimeValidationAndBundledCoverage(t *testing.T) {
	c, err := DefaultCorpus()
	if err != nil {
		t.Fatal(err)
	}
	kinds, tags := map[string]int{}, map[string]bool{}
	for _, w := range c.Works {
		kinds[w.Kind]++
		for _, tag := range w.Tags {
			tags[tag] = true
		}
	}
	if kinds["novel"] < 20 || kinds["comic"] < 15 || len(tags) < 12 {
		t.Fatal("lost genre coverage", kinds, tags)
	}
	c.Works[0].Catalog = &Catalog{Unit: "episode", Scope: "arbitrary", Status: "unknown", URL: "https://example.org", VerifiedAt: "2026-10-21"}
	raw, _ := json.Marshal(c)
	if _, err = ParseCorpus(raw); err == nil {
		t.Fatal("accepted invalid reference scope")
	}
	raw, _ = json.Marshal(testCorpus())
	if _, err = ParseCorpus(append(raw, []byte(` {}`)...)); err == nil {
		t.Fatal("accepted trailing JSON")
	}
}
