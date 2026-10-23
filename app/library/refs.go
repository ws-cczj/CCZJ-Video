package library

import (
	"context"

	"cczjVideo/app/apperror"
	"cczjVideo/app/db"
	"cczjVideo/app/model"
)

func (s *Service) ResolveRefs(ctx context.Context, refs []db.LibraryRef) ([]*Item, error) {
	if len(refs) > 256 {
		return nil, apperror.New(apperror.Validation, "书架校验批次过大")
	}
	for _, ref := range refs {
		if (ref.Kind != "novel" && ref.Kind != "comic") || ref.SourceKey == "" || ref.RemoteID == "" {
			return nil, apperror.New(apperror.Validation, "作品身份无效")
		}
	}
	rows, err := db.GetLibraryItemsByRefs(ctx, refs)
	if err != nil {
		return nil, apperror.Wrap(apperror.Storage, err, "读取书架作品失败")
	}
	names := make(map[string]string)
	kinds := make(map[string]bool)
	for _, row := range rows {
		names[row.SourceKey] = ""
		kinds[row.Kind] = true
	}
	for kind := range kinds {
		sources, err := db.GetSourcesByKind(model.SourceKind(kind))
		if err != nil {
			return nil, apperror.Wrap(apperror.Storage, err, "读取书架来源失败")
		}
		for _, source := range sources {
			names[source.SourceKey] = source.Name
		}
	}
	indexed := make(map[string]*Item, len(rows))
	for _, row := range rows {
		indexed[row.Kind+"\x00"+row.SourceKey+"\x00"+row.RemoteID] = s.toDTO(model.SourceKind(row.Kind), row, names)
	}
	items := make([]*Item, len(refs))
	for i, ref := range refs {
		items[i] = indexed[ref.Kind+"\x00"+ref.SourceKey+"\x00"+ref.RemoteID]
	}
	return items, nil
}
