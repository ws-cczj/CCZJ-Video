package main

import (
	"context"
	"fmt"
	"time"

	"cczjVideo/app/apperror"
	"cczjVideo/app/library"
	"cczjVideo/app/library/quality"
	"cczjVideo/app/model"
	"cczjVideo/app/provider"
)

type routedHit struct {
	Item     *library.Item    `json:"item"`
	Identity quality.Identity `json:"identity"`
}

type routedReading struct {
	ItemID   int                      `json:"itemId"`
	Item     *library.Item            `json:"detailItem,omitempty"`
	Identity quality.Identity         `json:"detailIdentity"`
	Catalog  *quality.CatalogEvidence `json:"catalog,omitempty"`
	Samples  []quality.Sample         `json:"samples"`
	Failure  *quality.Failure         `json:"failure,omitempty"`
}

type routedWork struct {
	Work           quality.Work             `json:"work"`
	Millis         int64                    `json:"millis"`
	Status         string                   `json:"status"`
	PendingSources int                      `json:"pendingSources"`
	Pages          []*library.SearchOutcome `json:"searchPages"`
	Hits           []routedHit              `json:"hits"`
	Reading        *routedReading           `json:"reading,omitempty"`
	Failure        *quality.Failure         `json:"failure,omitempty"`
}

type routedReport struct {
	Scope             string       `json:"scope"`
	Version           string       `json:"version"`
	IdentityAlgorithm string       `json:"identityAlgorithm"`
	CorpusHash        string       `json:"corpusHash"`
	StartedAt         string       `json:"startedAt"`
	Completed         bool         `json:"completed"`
	Works             []routedWork `json:"works"`
}

// The service owns all network scheduling. This runner stays sequential and
// reuses quality.Match/AnalyzeCatalog/AnalyzeText rather than inventing scores.
func verifyRoutedSearches(ctx context.Context, svc *library.Service, corpus quality.Corpus, kind string, maxWorks, maxRead int, timeout time.Duration, path string) error {
	index, err := quality.Compile(corpus)
	if err != nil {
		return err
	}
	report := routedReport{Scope: "production Go builtin routing; real upstream; disposable SQLite; not a native UI test", Version: quality.Version, IdentityAlgorithm: quality.IdentityAlgorithm, CorpusHash: index.Hash(), StartedAt: time.Now().Format(time.RFC3339), Works: []routedWork{}}
	read := 0
	for _, work := range corpus.Works {
		if work.Kind != kind {
			continue
		}
		if len(report.Works) >= maxWorks {
			break
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		call, cancel := context.WithTimeout(ctx, timeout)
		result := verifyRoutedWork(call, svc, work)
		if len(result.Hits) > 0 && read < maxRead {
			// Read only confirmed identities, never an arbitrary first result.
			for _, hit := range result.Hits {
				if hit.Identity.Status == "verified" {
					result.Reading = inspectRoutedReading(call, svc, work, hit.Item.ID)
					read++
					break
				}
			}
		}
		cancel()
		report.Works = append(report.Works, result)
		if err := writeReport(path, report); err != nil {
			return err
		}
		fmt.Printf("SEARCH %s: %s, hits=%d, pending=%d, %.2fs\n", work.Title, result.Status, len(result.Hits), result.PendingSources, float64(result.Millis)/1000)
	}
	report.Completed = ctx.Err() == nil
	if err := writeReport(path, report); err != nil {
		return err
	}
	return ctx.Err()
}

type routedSearcher interface {
	SearchPage(context.Context, model.SourceKind, string, string) (*library.SearchOutcome, error)
}

func verifyRoutedWork(ctx context.Context, svc routedSearcher, work quality.Work) routedWork {
	start := time.Now()
	out := routedWork{Work: work, Status: "absent", Pages: []*library.SearchOutcome{}, Hits: []routedHit{}}
	cursor := ""
	seen := map[int]bool{}
	// Match the frontend automatic expansion ceiling: three rounds, no new
	// round after 32 seconds, stopping on an exact title. Pending
	// sources remain explicit; a bounded miss is not proof of global absence.
	for round := 0; round < 3; round++ {
		page, err := svc.SearchPage(ctx, model.SourceKind(work.Kind), work.Title, cursor)
		if err != nil {
			out.Failure = routedFailure("search", err)
			break
		}
		out.Pages = append(out.Pages, page)
		out.PendingSources = page.PendingSources
		exact := false
		for _, result := range page.Results {
			for _, item := range result.Items {
				if quality.Normalize(item.Title) == quality.Normalize(work.Title) {
					exact = true
				}
				if seen[item.ID] {
					continue
				}
				seen[item.ID] = true
				identity := quality.Match(work, &provider.Item{Title: item.Title, Author: item.Author, URL: item.RemoteID})
				if identity.Status == "verified" || identity.Status == "provisional" {
					out.Hits = append(out.Hits, routedHit{item, identity})
				}
			}
		}
		cursor = page.NextCursor
		if exact || cursor == "" || page.PendingSources == 0 || time.Since(start) >= 32*time.Second {
			break
		}
	}
	for _, hit := range out.Hits {
		if hit.Identity.Status == "verified" {
			out.Status = "verified"
			break
		}
		out.Status = "provisional"
	}
	if out.Status == "absent" {
		if out.Failure != nil {
			out.Status = "failed"
		} else if out.PendingSources > 0 {
			out.Status = "pending"
		}
	}
	out.Millis = time.Since(start).Milliseconds()
	return out
}

func inspectRoutedReading(ctx context.Context, svc *library.Service, work quality.Work, id int) *routedReading {
	out := &routedReading{ItemID: id, Samples: []quality.Sample{}}
	detail, err := svc.Detail(ctx, id, false)
	if err == nil && len(detail.Chapters) == 0 {
		detail, err = svc.RefreshToc(ctx, id)
	}
	if err != nil {
		out.Failure = routedFailure("detail", err)
		return out
	}
	out.Item = detail.Item
	out.Identity = quality.Match(work, &provider.Item{Title: detail.Item.Title, Author: detail.Item.Author, URL: detail.Item.RemoteID})
	if out.Identity.Status != "verified" {
		return out
	}
	chapters := []*provider.Chapter{}
	available := []*library.Chapter{}
	for _, ch := range detail.Chapters {
		if !ch.Available {
			continue
		}
		chapters = append(chapters, &provider.Chapter{RemoteID: ch.RemoteID, Title: ch.Title, Volume: ch.Volume, IsVolume: ch.IsVolume})
		if !ch.IsVolume {
			available = append(available, ch)
		}
	}
	catalog := quality.AnalyzeCatalog(work, chapters, time.Now())
	out.Catalog = &catalog
	if len(available) == 0 {
		return out
	}
	seen := map[int]bool{}
	for _, position := range []int{0, len(available) / 2, len(available) - 1} {
		if seen[position] {
			continue
		}
		seen[position] = true
		ch := available[position]
		sample := quality.Sample{Position: position, Title: ch.Title, Access: ch.Access}
		if work.Kind == "novel" {
			text, err := svc.ChapterText(ctx, ch.ID)
			if err != nil {
				sample.Failure = routedFailure("text", err)
			} else {
				evidence := quality.AnalyzeText(&provider.TextContent{Paragraphs: text.Paragraphs})
				sample.Text = &evidence
				for _, warning := range text.Warnings {
					sample.Text.Issues = append(sample.Text.Issues, quality.Issue{Code: "reader_warning", Severity: "warning", Evidence: warning})
				}
			}
		} else {
			pages, err := svc.ChapterPages(ctx, ch.ID)
			if err != nil {
				sample.Failure = routedFailure("pages", err)
			} else {
				sample.Pages = len(pages.Pages)
			}
		}
		out.Samples = append(out.Samples, sample)
	}
	return out
}

func routedFailure(stage string, err error) *quality.Failure {
	public := apperror.Public(err)
	return &quality.Failure{Stage: stage, Code: string(public.Code), Message: public.Message}
}
