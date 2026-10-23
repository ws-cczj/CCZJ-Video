package db

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"cczjVideo/app/model"
	"github.com/jmoiron/sqlx"
)

var ErrInvalidCatalogQuery = errors.New("invalid catalog query")

const libraryKindIDIndex = `CREATE INDEX IF NOT EXISTS idx_li_kind_id ON library_items(kind,id)`
const libraryKindTitleIndex = `CREATE INDEX IF NOT EXISTS idx_li_kind_title_id ON library_items(kind,title COLLATE NOCASE,id)`

func migrateLibraryPaging(tx *sqlx.Tx) error {
	for _, statement := range []string{libraryKindIDIndex, libraryKindTitleIndex} {
		if _, err := tx.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}

type LibraryPageQuery struct {
	Kind           string `json:"kind"`
	Genre          string `json:"genre"`
	Complete       bool   `json:"complete"`
	Sort           string `json:"sort"`
	IncludeRetired bool   `json:"includeRetired"`
	Cursor         string `json:"cursor"`
	Limit          int    `json:"limit"`
}
type LibraryItemPage struct {
	Items      []*model.LibraryItem
	Genres     []string
	NextCursor string
}
type libraryPageCursor struct {
	Version                  int
	Kind, Genre, Sort        string
	Complete, IncludeRetired bool
	ID, MaxID                int
	Title                    string
}

var completedReadingStatus = regexp.MustCompile(`^(completed?|finished|done|ended)$`)

func readingGenres(raw string) []string {
	return strings.FieldsFunc(raw, func(r rune) bool { return unicode.IsSpace(r) || strings.ContainsRune(",，、;；/|", r) })
}

// PageLibraryItems scans compact filter projections and materializes metadata
// only for the requested page. Cursors are query-bound keysets with an ID ceiling.
func PageLibraryItems(ctx context.Context, q LibraryPageQuery) (*LibraryItemPage, error) {
	if q.Kind != "novel" && q.Kind != "comic" {
		return nil, fmt.Errorf("%w: invalid reading kind", ErrInvalidCatalogQuery)
	}
	if q.Sort != "" && q.Sort != "title" {
		return nil, fmt.Errorf("%w: invalid reading sort", ErrInvalidCatalogQuery)
	}
	if q.Limit < 1 || q.Limit > 100 {
		q.Limit = 60
	}
	if len(q.Genre) > 256 || len(q.Cursor) > 8192 {
		return nil, fmt.Errorf("%w: invalid catalog query size", ErrInvalidCatalogQuery)
	}
	cursor := libraryPageCursor{Version: 1, Kind: q.Kind, Genre: q.Genre, Sort: q.Sort, Complete: q.Complete, IncludeRetired: q.IncludeRetired}
	if q.Cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(q.Cursor)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid catalog cursor", ErrInvalidCatalogQuery)
		}
		if err := json.Unmarshal(raw, &cursor); err != nil {
			return nil, fmt.Errorf("%w: invalid catalog cursor", ErrInvalidCatalogQuery)
		}
		if cursor.Version != 1 || cursor.Kind != q.Kind || cursor.Genre != q.Genre || cursor.Sort != q.Sort || cursor.Complete != q.Complete || cursor.IncludeRetired != q.IncludeRetired || cursor.ID < 1 || cursor.MaxID < cursor.ID {
			return nil, fmt.Errorf("%w: catalog cursor does not match query", ErrInvalidCatalogQuery)
		}
	} else {
		if err := instance.GetContext(ctx, &cursor.MaxID, `SELECT COALESCE(MAX(id),0) FROM library_items WHERE kind=?`, q.Kind); err != nil {
			return nil, err
		}
	}
	where := `kind=? AND id<=?`
	args := []any{q.Kind, cursor.MaxID}
	if !q.IncludeRetired {
		where += ` AND lifecycle_state='active'`
	}
	// Facets cover the whole catalog, independently of the loaded page/filter.
	var genreRows []string
	facetWhere := `kind=?`
	if !q.IncludeRetired {
		facetWhere += ` AND lifecycle_state='active'`
	}
	if err := instance.SelectContext(ctx, &genreRows, `SELECT DISTINCT genres FROM library_items WHERE `+facetWhere, q.Kind); err != nil {
		return nil, err
	}
	genreSet := make(map[string]bool)
	for _, raw := range genreRows {
		for _, genre := range readingGenres(raw) {
			genreSet[genre] = true
		}
	}
	genres := make([]string, 0, len(genreSet))
	for genre := range genreSet {
		genres = append(genres, genre)
	}
	sort.Strings(genres)
	order := `id DESC`
	if q.Sort == "title" {
		order = `title COLLATE NOCASE ASC,id ASC`
	}
	if q.Cursor != "" {
		if q.Sort == "title" {
			where += ` AND (title COLLATE NOCASE > ? OR (title COLLATE NOCASE = ? AND id>?))`
			args = append(args, cursor.Title, cursor.Title, cursor.ID)
		} else {
			where += ` AND id<?`
			args = append(args, cursor.ID)
		}
	}
	rows, err := instance.QueryxContext(ctx, `SELECT id,title,genres,status FROM library_items WHERE `+where+` ORDER BY `+order, args...)
	if err != nil {
		return nil, err
	}
	type projected struct {
		ID                    int `db:"id"`
		Title, Genres, Status string
	}
	selected := make([]projected, 0, q.Limit+1)
	for rows.Next() {
		var item projected
		if err := rows.StructScan(&item); err != nil {
			rows.Close()
			return nil, err
		}
		if q.Complete && !strings.Contains(item.Status, "完") && !completedReadingStatus.MatchString(strings.ToLower(strings.TrimSpace(item.Status))) {
			continue
		}
		if q.Genre != "" {
			matches := false
			for _, genre := range readingGenres(item.Genres) {
				if genre == q.Genre {
					matches = true
					break
				}
			}
			if !matches {
				continue
			}
		}
		selected = append(selected, item)
		if len(selected) > q.Limit {
			break
		}
	}
	readErr := rows.Err()
	rows.Close()
	if readErr != nil {
		return nil, readErr
	}
	result := &LibraryItemPage{Items: make([]*model.LibraryItem, 0, len(selected)), Genres: genres}
	if len(selected) > q.Limit {
		selected = selected[:q.Limit]
		last := selected[len(selected)-1]
		cursor.ID, cursor.Title = last.ID, last.Title
		raw, _ := json.Marshal(cursor)
		result.NextCursor = base64.RawURLEncoding.EncodeToString(raw)
	}
	if len(selected) == 0 {
		return result, nil
	}
	ids := make([]int, len(selected))
	for i, item := range selected {
		ids[i] = item.ID
	}
	query, values, err := sqlx.In(`SELECT * FROM library_items WHERE id IN (?)`, ids)
	if err != nil {
		return nil, err
	}
	var materialized []*model.LibraryItem
	if err := instance.SelectContext(ctx, &materialized, query, values...); err != nil {
		return nil, err
	}
	indexed := make(map[int]*model.LibraryItem, len(materialized))
	for _, item := range materialized {
		indexed[item.Id] = item
	}
	for _, item := range selected {
		if row := indexed[item.ID]; row != nil {
			result.Items = append(result.Items, row)
		}
	}
	return result, nil
}
