package library

import (
	"context"
	"errors"

	"cczjVideo/app/apperror"
	"cczjVideo/app/db"
	"cczjVideo/app/model"
)

type CatalogPage struct {
	Items      []*Item  `json:"items"`
	Genres     []string `json:"genres"`
	NextCursor string   `json:"nextCursor"`
}

func (s *Service) CatalogPage(ctx context.Context, q db.LibraryPageQuery) (*CatalogPage, error) {
	page, err := db.PageLibraryItems(ctx, q)
	if err != nil {
		if errors.Is(err, db.ErrInvalidCatalogQuery) {
			return nil, apperror.Wrap(apperror.Validation, err, "书库分页参数无效，请重新加载")
		}
		return nil, apperror.Wrap(apperror.Storage, err, "读取书库分页失败")
	}
	sources, err := db.GetSourcesByKind(model.SourceKind(q.Kind))
	if err != nil {
		return nil, apperror.Wrap(apperror.Storage, err, "读取来源失败")
	}
	names := make(map[string]string)
	for _, row := range page.Items {
		names[row.SourceKey] = ""
	}
	for _, source := range sources {
		names[source.SourceKey] = source.Name
	}
	items := make([]*Item, 0, len(page.Items))
	for _, row := range page.Items {
		items = append(items, s.toDTO(model.SourceKind(q.Kind), row, names))
	}
	return &CatalogPage{Items: items, Genres: page.Genres, NextCursor: page.NextCursor}, nil
}
