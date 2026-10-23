package library

import (
	"context"
	"strings"
	"sync"
	"unicode"

	"cczjVideo/app/db"
	"cczjVideo/app/library/quality"
	"cczjVideo/app/model"
	"github.com/mozillazg/go-pinyin"
)

var phoneticTitles struct {
	sync.Once
	byKind map[model.SourceKind]map[string][]string
}

func phoneticKey(value string) string {
	var out strings.Builder
	for _, r := range strings.ToLower(value) {
		if r >= 'a' && r <= 'z' || unicode.IsDigit(r) {
			out.WriteRune(r)
		}
	}
	return out.String()
}

func titlePinyin(title string) string {
	args := pinyin.NewArgs()
	args.Fallback = func(r rune, _ pinyin.Args) []string { return []string{string(r)} }
	return phoneticKey(strings.Join(pinyin.LazyPinyin(title, args), ""))
}

// Resolve full pinyin against known titles, never guess arbitrary syllables as
// Chinese. Ambiguous spellings remain unchanged. Mode indexes are built once;
// local titles cover works discovered after the bundled reference corpus.
func resolveSearchKeyword(ctx context.Context, kind model.SourceKind, query string) (string, error) {
	for _, r := range query {
		if unicode.Is(unicode.Han, r) {
			return query, nil
		}
	}
	key := phoneticKey(query)
	if len(key) < 4 {
		return query, nil
	}
	phoneticTitles.Do(func() {
		phoneticTitles.byKind = make(map[model.SourceKind]map[string][]string)
		for _, kind := range []model.SourceKind{model.SourceKindNovel, model.SourceKindComic} {
			index := make(map[string][]string)
			corpus, err := quality.AuditCorpus(string(kind))
			if err == nil {
				for _, work := range corpus.Works {
					for _, title := range append([]string{work.Title}, work.Aliases...) {
						if k := titlePinyin(title); k != "" {
							index[k] = append(index[k], work.Title)
						}
					}
				}
			}
			phoneticTitles.byKind[kind] = index
		}
	})
	if err := ctx.Err(); err != nil {
		return "", err
	}
	candidates := map[string]bool{}
	for _, title := range phoneticTitles.byKind[kind][key] {
		candidates[title] = true
	}
	if len(candidates) == 1 {
		for title := range candidates {
			return title, nil
		}
	}
	// Stream only titles, with request cancellation, rather than allocating full
	// metadata for the entire local library. Work is linear and memory is bounded
	// by the number of matching titles.
	rows, err := db.DB().QueryxContext(ctx, `SELECT DISTINCT title FROM library_items WHERE kind=? AND lifecycle_state='active'`, kind)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		var title string
		if err := rows.Scan(&title); err != nil {
			return "", err
		}
		if titlePinyin(title) == key {
			candidates[title] = true
		}
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if len(candidates) == 1 {
		for title := range candidates {
			return title, nil
		}
	}
	return query, nil
}
