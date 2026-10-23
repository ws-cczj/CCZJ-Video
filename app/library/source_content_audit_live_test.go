package library

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"cczjVideo/app/model"
	"cczjVideo/app/provider"
	"cczjVideo/app/proxy"
	_ "golang.org/x/image/webp"
)

// Opt-in evidence collection, not a claim that every upstream passed. Records
// actual failures and short content excerpts, never complete chapters or images.
func TestSourceContentQualityAuditLive(t *testing.T) {
	path := os.Getenv("CCZJ_CONTENT_AUDIT_REPORT")
	if path == "" {
		t.Skip("set CCZJ_CONTENT_AUDIT_REPORT for live source-content audit")
	}
	s := setupLibraryTest(t)
	defer s.Close(context.Background())
	var candidates []*SourceCandidate
	for _, kind := range []model.SourceKind{model.SourceKindNovel, model.SourceKindComic} {
		preview, err := s.PreviewSources(context.Background(), kind, "community", "")
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range preview.Candidates {
			if row.Compatible {
				candidates = append(candidates, row)
			}
		}
	}
	var mu sync.Mutex
	results := []map[string]any{}
	jobs := make(chan *SourceCandidate)
	var workers sync.WaitGroup
	for i := 0; i < 3; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for row := range jobs {
				ctx, cancel := context.WithTimeout(context.Background(), 110*time.Second)
				record := auditSourceContent(ctx, s, row)
				cancel()
				mu.Lock()
				results = append(results, record)
				raw, _ := json.MarshalIndent(results, "", "  ")
				if err := os.WriteFile(path, raw, 0600); err != nil {
					t.Error(err)
				}
				fmt.Printf("AUDIT %s: %v, works=%v\n", row.Name, record["status"], record["worksSampled"])
				mu.Unlock()
			}
		}()
	}
	for _, row := range candidates {
		jobs <- row
	}
	close(jobs)
	workers.Wait()
}

func auditSourceContent(ctx context.Context, s *Service, row *SourceCandidate) map[string]any {
	started := time.Now()
	record := map[string]any{"id": row.ID, "name": row.Name, "kind": row.Kind, "apiUrl": row.ApiURL, "checkedAt": started.Unix(), "status": "failed", "worksSampled": 0}
	doc, err := provider.ParseDocument(row.document, row.Kind)
	if err != nil {
		record["error"] = err.Error()
		return record
	}
	p := provider.NewPipeline(doc, s.fetcher.ScopeMode("audit_"+row.ID, row.Kind), s.limits)
	resolved := &resolvedSource{source: &model.Source{SourceKey: row.ID, ApiUrl: row.ApiURL, Kind: row.Kind}, document: doc, pipeline: p}
	baseline := s.testResolvedSource(ctx, row.ID, resolved)
	record["baseline"] = baseline
	if baseline.OK {
		record["status"] = "sample_chain_passed"
	}
	searchOK := false
	for _, stage := range baseline.Stages {
		if stage.Name == StageSearch && stage.Status == "ok" {
			searchOK = true
		}
	}
	if !searchOK {
		record["millis"] = time.Since(started).Milliseconds()
		return record
	}
	keyword := doc.CheckKeyword
	if keyword == "" {
		keyword = "的"
	}
	record["keyword"] = keyword
	first, err := p.Search(ctx, row.ApiURL, keyword, 1, "")
	if err != nil {
		record["error"] = err.Error()
		return record
	}
	searchSummary := []map[string]any{}
	for i, item := range first.Items {
		if i >= 6 {
			break
		}
		searchSummary = append(searchSummary, map[string]any{"title": item.Title, "author": item.Author, "url": item.URL, "coverPresent": item.Cover != "", "summaryRunes": utf8.RuneCountInString(item.Summary)})
	}
	record["searchSample"] = searchSummary
	// A changed query distinguishes a genuine search route from a fixed book/list.
	other := "斗罗"
	if row.Kind == "comic" {
		other = "神之塔"
	}
	if keyword == other {
		other = "三国"
	}
	second, searchErr := p.Search(ctx, row.ApiURL, other, 1, "")
	if searchErr == nil {
		ids := func(items []*provider.Item) string {
			var ids []string
			for i, item := range items {
				if i >= 5 {
					break
				}
				ids = append(ids, item.RemoteID)
			}
			return strings.Join(ids, "|")
		}
		record["alternateSearch"] = map[string]any{"keyword": other, "count": len(second.Items), "sameLeadingIDs": ids(first.Items) == ids(second.Items)}
	} else {
		record["alternateSearchError"] = searchErr.Error()
	}
	works := []map[string]any{}
	for index, item := range first.Items {
		if index >= 2 || ctx.Err() != nil {
			break
		}
		work := map[string]any{"searchTitle": item.Title, "url": item.URL}
		works = append(works, work)
		book, detailErr := p.Detail(ctx, row.ApiURL, item.URL)
		if detailErr != nil {
			work["detailError"] = detailErr.Error()
			book = item
		}
		work["title"], work["author"], work["summaryRunes"], work["coverPresent"] = book.Title, book.Author, utf8.RuneCountInString(book.Summary), book.Cover != ""
		chapters, tocErr := p.Toc(ctx, row.ApiURL, book)
		if tocErr != nil {
			work["tocError"] = tocErr.Error()
			continue
		}
		usable := []*provider.Chapter{}
		for _, chapter := range chapters {
			if !chapter.IsVolume {
				usable = append(usable, chapter)
			}
		}
		work["chapters"] = len(usable)
		if len(usable) == 0 {
			continue
		}
		work["firstChapter"], work["lastChapter"] = usable[0].Title, usable[len(usable)-1].Title
		samples := []map[string]any{}
		positions := []int{0, len(usable) / 2, len(usable) - 1}
		seen := map[int]bool{}
		for _, pos := range positions {
			if seen[pos] || ctx.Err() != nil {
				continue
			}
			seen[pos] = true
			chapter := usable[pos]
			sample := map[string]any{"position": pos, "title": chapter.Title, "access": chapter.Access}
			samples = append(samples, sample)
			if row.Kind == "novel" {
				next := ""
				if pos+1 < len(usable) {
					next = usable[pos+1].URL
				}
				body, bodyErr := p.Content(ctx, row.ApiURL, chapter, next)
				if bodyErr != nil {
					sample["error"] = bodyErr.Error()
					continue
				}
				text := strings.Join(body.Paragraphs, "\n")
				sample["runes"], sample["paragraphs"], sample["garbled"], sample["obfuscated"], sample["truncated"] = utf8.RuneCountInString(text), len(body.Paragraphs), body.Garbled, body.FontObfuscated, body.Truncated
				markers := []string{}
				for _, word := range []string{"请收藏", "最新网址", "加入书签", "手机用户", "上一章", "下一章", "下载APP", "订阅本章", "购买本章"} {
					if strings.Contains(text, word) {
						markers = append(markers, word)
					}
				}
				sample["navigationOrAdMarkers"] = markers
				runes := []rune(text)
				if len(runes) > 65 {
					runes = runes[:65]
				}
				sample["excerpt"] = string(runes)
			} else {
				pages, pagesErr := p.Pages(ctx, row.ApiURL, chapter)
				if pagesErr != nil {
					sample["error"] = pagesErr.Error()
					continue
				}
				sample["pages"] = len(pages)
				if len(pages) == 0 {
					continue
				}
				// First/middle/last image of one sampled chapter, first image of
				// the other chapters, validates resolution beyond a successful URL.
				indices := []int{0}
				if index == 0 && pos == 0 {
					indices = []int{0, len(pages) / 2, len(pages) - 1}
				}
				images := []map[string]any{}
				for _, pi := range indices {
					page := pages[pi]
					address := page.ImageURL
					if address == "" {
						address, err = p.ResolveImage(ctx, row.ApiURL, page)
						if err != nil {
							images = append(images, map[string]any{"index": pi, "error": err.Error()})
							continue
						}
					}
					check := auditImageConfig(ctx, address, page.Referer, doc.Headers)
					check["index"] = pi
					images = append(images, check)
				}
				sample["images"] = images
			}
		}
		work["chapterSamples"] = samples
	}
	record["works"], record["worksSampled"], record["millis"] = works, len(works), time.Since(started).Milliseconds()
	return record
}

func auditImageConfig(ctx context.Context, address, referer string, headers map[string]string) map[string]any {
	out := map[string]any{}
	request, err := http.NewRequestWithContext(ctx, "GET", address, nil)
	if err != nil {
		out["error"] = err.Error()
		return out
	}
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	if referer != "" {
		request.Header.Set("Referer", referer)
	}
	transport := &http.Transport{DialContext: proxy.SafeDialContext}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 20 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		out["error"] = err.Error()
		return out
	}
	defer response.Body.Close()
	out["httpStatus"], out["mime"] = response.StatusCode, response.Header.Get("Content-Type")
	if response.StatusCode != 200 {
		return out
	}
	config, format, err := image.DecodeConfig(io.LimitReader(response.Body, 8<<20))
	if err != nil {
		out["error"] = err.Error()
		return out
	}
	out["width"], out["height"], out["format"] = config.Width, config.Height, format
	return out
}
