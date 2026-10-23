package library

import (
	"cczjVideo/app/apperror"
	"cczjVideo/app/db"
	"cczjVideo/app/provider"
	"context"
	"encoding/json"
	"strings"
	"time"
)

type ComicTrend struct {
	Title  string `json:"title"`
	Author string `json:"author"`
	Cover  string `json:"cover"`
	Genres string `json:"genres"`
	URL    string `json:"url"`
}
type ComicTrendPage struct {
	Board     string        `json:"board"`
	Items     []*ComicTrend `json:"items"`
	FetchedAt int64         `json:"fetchedAt"`
	FromCache bool          `json:"fromCache"`
	Stale     bool          `json:"stale"`
	Warning   string        `json:"warning"`
}

func (s *Service) TrendingComics(ctx context.Context, board string, refresh bool) (*ComicTrendPage, error) {
	if board != "china" && board != "japan" {
		return nil, apperror.New(apperror.Validation, "漫画榜单无效")
	}
	return s.trendReads.Do(ctx, board, func(ctx context.Context) (*ComicTrendPage, error) {
		release := s.priority.begin()
		defer release()
		key := "comic.trending.v1." + board
		raw, err := db.GetSetting(key)
		if err != nil {
			return nil, apperror.Wrap(apperror.Storage, err, "读取漫画榜单缓存失败")
		}
		var cached *ComicTrendPage
		if json.Unmarshal([]byte(raw), &cached) != nil || !validComicTrend(cached, board) {
			cached = nil
		}
		if cached != nil && !refresh && time.Since(time.Unix(cached.FetchedAt, 0)) < 6*time.Hour {
			cached.FromCache = true
			return s.proxyComicTrend(cached), nil
		}
		found, err := provider.ComicTrending(ctx, s.fetcher.ScopeMode("comic-trending", "comic"), board)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if cached == nil {
				return nil, err
			}
			cached.FromCache = true
			cached.Stale = true
			cached.Warning = "榜单暂时无法更新，显示上次保存的结果"
			return s.proxyComicTrend(cached), nil
		}
		page := &ComicTrendPage{Board: board, FetchedAt: time.Now().Unix(), Items: make([]*ComicTrend, 0, len(found))}
		for _, item := range found {
			page.Items = append(page.Items, &ComicTrend{Title: item.Title, Author: item.Author, Cover: item.Cover, Genres: item.Genres, URL: item.URL})
		}
		encoded, err := json.Marshal(page)
		if err != nil {
			return nil, err
		}
		if err = db.SetSetting(key, string(encoded)); err != nil {
			page.Warning = "榜单已更新，但暂时无法保存离线结果"
		}
		return s.proxyComicTrend(page), nil
	})
}

func validComicTrend(page *ComicTrendPage, board string) bool {
	if page == nil || page.Board != board || page.FetchedAt <= 0 || page.FetchedAt > time.Now().Add(time.Minute).Unix() || len(page.Items) < 3 || len(page.Items) > 24 {
		return false
	}
	for _, item := range page.Items {
		if item == nil || strings.TrimSpace(item.Title) == "" || !strings.HasPrefix(item.URL, "https://manga.bilibili.com/detail/mc") {
			return false
		}
	}
	return true
}
func (s *Service) proxyComicTrend(page *ComicTrendPage) *ComicTrendPage {
	copy := *page
	copy.Items = make([]*ComicTrend, 0, len(page.Items))
	for _, item := range page.Items {
		row := *item
		if row.Cover != "" && s.imageURL != nil {
			row.Cover = s.imageURL(row.Cover, "https://manga.bilibili.com/", "")
		}
		copy.Items = append(copy.Items, &row)
	}
	return &copy
}
