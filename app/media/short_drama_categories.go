package media

import (
	"cczjVideo/app/apperror"
	"cczjVideo/app/collect"
	"cczjVideo/app/db"
	"cczjVideo/app/model"
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

type ShortDramaCategory struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	ParentID  string `json:"parent_id"`
	Automatic bool   `json:"automatic"`
	Manual    bool   `json:"manual"`
}

func (s *Service) ShortDramaCategories(ctx context.Context, sourceKey string) ([]ShortDramaCategory, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	src, err := db.GetSourceByKeyContext(ctx, sourceKey)
	if err != nil {
		return nil, err
	}
	if src.GetKind() != model.SourceKindVideo {
		return nil, apperror.New(apperror.Unsupported, "短剧只支持视频源")
	}
	strategy := collect.CreateStrategyFromSource(src)
	if strategy == nil {
		return nil, apperror.New(apperror.Unsupported, "来源策略不可用")
	}
	rows, err := db.ExportSourceTypesContext(ctx, sourceKey)
	if err != nil {
		return nil, err
	}
	names, _, err := sourceShortDramaTypes(ctx, src, strategy, rows, true)
	if err != nil {
		return nil, err
	}
	if len(names) > db.MaxSourceCategories {
		return nil, apperror.New(apperror.Corrupt, "来源分类数量超出范围")
	}
	rows, err = db.ExportSourceTypesContext(ctx, sourceKey)
	if err != nil {
		return nil, err
	}
	automatic, err := db.GetShortDramaSourceTypesContext(ctx, sourceKey, false)
	if err != nil {
		return nil, err
	}
	auto := map[string]bool{}
	for _, row := range automatic {
		auto[row.SourceTypeID] = true
	}
	result := make([]ShortDramaCategory, 0, len(names))
	for _, row := range rows {
		if strings.TrimSpace(row.SourceTypeID) == "" || strings.TrimSpace(row.TypeName) == "" {
			continue
		}
		if _, current := names[row.SourceTypeID]; !current {
			continue
		}
		parent := row.ParentTypeID
		if parent == "0" {
			parent = ""
		}
		result = append(result, ShortDramaCategory{ID: row.SourceTypeID, Name: row.TypeName, ParentID: parent, Automatic: auto[row.SourceTypeID], Manual: row.ManualShortDrama != nil && *row.ManualShortDrama})
	}
	return result, nil
}

func (s *Service) SetShortDramaCategories(ctx context.Context, sourceKey string, ids []string) error {
	if len(ids) > db.MaxSourceCategories {
		return apperror.New(apperror.Validation, "选择的分类数量超出范围")
	}
	src, err := db.GetSourceByKeyContext(ctx, sourceKey)
	if err != nil {
		return err
	}
	if src.GetKind() != model.SourceKindVideo {
		return apperror.New(apperror.Unsupported, "短剧只支持视频源")
	}
	unique := make([]string, 0, len(ids))
	seen := map[string]bool{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || len(id) > 120 {
			return apperror.New(apperror.Validation, "分类编号无效")
		}
		if !seen[id] {
			seen[id] = true
			unique = append(unique, id)
		}
	}
	if err = db.SetManualShortDramaTypesContext(ctx, sourceKey, unique); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return apperror.Wrap(apperror.NotFound, err, "来源或分类已变化，请刷新后重试")
		}
		return apperror.Wrap(apperror.Storage, err, "保存短剧分类失败")
	}
	return nil
}
