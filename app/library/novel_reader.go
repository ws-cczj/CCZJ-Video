package library

import (
	"context"
	"time"

	"cczjVideo/app/apperror"
	"cczjVideo/app/db"
	"cczjVideo/app/model"
)

type ReadingLocation struct {
	ItemID    int    `json:"itemId"`
	ChapterID int    `json:"chapterId"`
	RemoteID  string `json:"remoteId"`
	Ord       int    `json:"ord"`
	First     bool   `json:"first"`
}

// ReadingContext is a bounded chapter window, not an ItemDetail. The
// complete directory is requested explicitly by the directory panel only.
type ReadingContext struct {
	Item          *Item    `json:"item"`
	Chapter       *Chapter `json:"chapter"`
	Previous      *Chapter `json:"previous"`
	Next          *Chapter `json:"next"`
	Index         int      `json:"index"`
	Count         int      `json:"count"`
	CacheIdentity string   `json:"cacheIdentity"`
}

// Named compatibility DTOs retain existing generated novel binding classes.
type NovelReadingLocation ReadingLocation
type NovelReadingContext ReadingContext

func chapterDTO(row *model.LibraryChapter) *Chapter {
	if row == nil {
		return nil
	}
	return &Chapter{ID: row.Id, RemoteID: row.RemoteID, Ord: row.Ord, Title: row.Title, Volume: row.Volume, IsVolume: row.IsVolume, Access: string(row.Access), Available: row.LifecycleState == "active"}
}

func (s *Service) NovelContext(ctx context.Context, location NovelReadingLocation) (*NovelReadingContext, error) {
	view, err := s.readingContext(ctx, model.SourceKindNovel, ReadingLocation(location))
	return (*NovelReadingContext)(view), err
}

// ComicContext shares bounded storage/navigation rules while keeping mode explicit.
func (s *Service) ComicContext(ctx context.Context, location ReadingLocation) (*ReadingContext, error) {
	return s.readingContext(ctx, model.SourceKindComic, location)
}

func (s *Service) readingContext(ctx context.Context, kind model.SourceKind, location ReadingLocation) (*ReadingContext, error) {
	release := s.priority.begin()
	defer release()
	row, err := db.GetLibraryItem(location.ItemID)
	if err != nil {
		return nil, apperror.Wrap(apperror.Storage, err, "读取作品失败")
	}
	if model.SourceKind(row.Kind) != kind {
		return nil, apperror.New(apperror.Validation, "作品与当前阅读模式不匹配")
	}
	window, err := db.ReadLibraryWindow(ctx, row.Id, location.ChapterID, location.RemoteID, location.Ord, location.First)
	if err != nil {
		return nil, apperror.Wrap(apperror.Storage, err, "读取章节位置失败")
	}
	// Only bootstrap a genuinely empty catalog. Missing/retired identities must
	// never silently restart the book at chapter one.
	if window.Count == 0 && row.LifecycleState == "active" {
		resolved, err := s.resolve(row.SourceKey, kind)
		if err != nil {
			return nil, err
		}
		if err = s.refreshDetailAndToc(ctx, resolved, row); err != nil {
			return nil, err
		}
		window, err = db.ReadLibraryWindow(ctx, row.Id, location.ChapterID, location.RemoteID, location.Ord, location.First)
		if err != nil {
			return nil, apperror.Wrap(apperror.Storage, err, "读取章节位置失败")
		}
		row, err = db.GetLibraryItem(row.Id)
		if err != nil {
			return nil, apperror.Wrap(apperror.Storage, err, "读取作品失败")
		}
	}
	result := &ReadingContext{Item: s.toDTO(kind, row, map[string]string{}), Chapter: chapterDTO(window.Current), Previous: chapterDTO(window.Previous), Next: chapterDTO(window.Next), Index: window.Index, Count: window.Count}
	if window.Current != nil {
		resolved, err := s.resolve(row.SourceKey, kind)
		if err == nil {
			result.CacheIdentity = s.textCacheKey(resolved, &chapterRow{itemRemoteID: row.RemoteID, remoteID: window.Current.RemoteID})
		}
	}
	return result, nil
}

// RefreshNovelContext performs the same near-end/cooldown policy as the legacy
// directory endpoint but never serializes the whole catalog to the reader.
func (s *Service) RefreshNovelContext(ctx context.Context, location NovelReadingLocation) (*NovelReadingContext, error) {
	view, err := s.refreshReadingContext(ctx, model.SourceKindNovel, ReadingLocation(location))
	return (*NovelReadingContext)(view), err
}

func (s *Service) RefreshComicContext(ctx context.Context, location ReadingLocation) (*ReadingContext, error) {
	return s.refreshReadingContext(ctx, model.SourceKindComic, location)
}

func (s *Service) refreshReadingContext(ctx context.Context, kind model.SourceKind, location ReadingLocation) (*ReadingContext, error) {
	window, err := db.ReadLibraryWindow(ctx, location.ItemID, location.ChapterID, location.RemoteID, location.Ord, location.First)
	if err != nil {
		return nil, apperror.Wrap(apperror.Storage, err, "读取章节位置失败")
	}
	if window.Current != nil && window.Current.LifecycleState == "active" && window.Remaining < 3 && s.claimReadingRefresh(location.ItemID) {
		row, err := db.GetLibraryItem(location.ItemID)
		if err != nil {
			return nil, apperror.Wrap(apperror.Storage, err, "读取作品失败")
		}
		if model.SourceKind(row.Kind) != kind {
			return nil, apperror.New(apperror.Validation, "作品与当前阅读模式不匹配")
		}
		resolved, err := s.resolve(row.SourceKey, kind)
		if err != nil {
			return nil, err
		}
		if err = s.refreshDetailAndToc(ctx, resolved, row); err != nil {
			return nil, err
		}
	}
	return s.readingContext(ctx, kind, location)
}

func (s *Service) claimReadingRefresh(itemID int) bool {
	s.tocMu.Lock()
	defer s.tocMu.Unlock()
	if last := s.tocAttempts[itemID]; !last.IsZero() && time.Since(last) < 10*time.Minute {
		return false
	}
	s.recordTocAttempt(itemID)
	return true
}
