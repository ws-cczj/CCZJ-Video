package library

import (
	"context"
	"encoding/json"
	"time"

	"cczjVideo/app/apperror"
	"cczjVideo/app/db"
	"cczjVideo/app/library/comic"
	"cczjVideo/app/library/matching"
	"cczjVideo/app/library/novel"
	"cczjVideo/app/model"
)

type ChapterMatch struct {
	Kind         string     `json:"kind"`
	Identity     string     `json:"identity"`
	Status       string     `json:"status"`
	ChapterID    int        `json:"chapterId"`
	TargetItemID int        `json:"targetItemId"`
	Targets      []*Chapter `json:"targets"`
	Evidence     string     `json:"evidence"`
	Matched      int        `json:"matched"`
	Ambiguous    int        `json:"ambiguous"`
	Unmatched    int        `json:"unmatched"`
	CheckedAt    int64      `json:"checkedAt"`
}

func (s *Service) SourceAlternatives(ctx context.Context, kind model.SourceKind, itemID int, cursor string) (*SearchOutcome, error) {
	item, err := db.GetLibraryItem(itemID)
	if err != nil {
		return nil, err
	}
	if item.Kind != string(kind) {
		return nil, apperror.New(apperror.Validation, "不能跨模式查找来源")
	}
	out, err := s.SearchPageStream(ctx, kind, item.Title, cursor, nil)
	if err != nil {
		return nil, err
	}
	out.Total = 0
	for _, result := range out.Results {
		rows := result.Items[:0]
		for _, candidate := range result.Items {
			if candidate.ID != itemID && matching.Identity(item.Title, item.Author, candidate.Title, candidate.Author) != "mismatch" {
				rows = append(rows, candidate)
			}
		}
		result.Items = rows
		out.Total += len(rows)
	}
	return out, nil
}

// MatchChapter never changes the shelf or progress. The mode parser produces a
// verified mapping or explicit review candidates; the reader chooses a target.
func (s *Service) MatchChapter(ctx context.Context, kind model.SourceKind, fromItem, chapterID, toItem int) (*ChapterMatch, error) {
	release := s.priority.begin()
	defer release()
	a, err := db.GetLibraryItem(fromItem)
	if err != nil {
		return nil, err
	}
	b, err := db.GetLibraryItem(toItem)
	if err != nil {
		return nil, err
	}
	if a.Kind != string(kind) || b.Kind != string(kind) || (kind != model.SourceKindNovel && kind != model.SourceKindComic) {
		return nil, apperror.New(apperror.Validation, "章节匹配仅限同一种阅读模式")
	}
	identity := matching.Identity(a.Title, a.Author, b.Title, b.Author)
	if identity == "mismatch" {
		return nil, apperror.New(apperror.Conflict, "作品标题或作者不同，不能自动匹配章节")
	}
	fromBefore, err := db.GetSourceByKeyContext(ctx, a.SourceKey)
	if err != nil {
		return nil, err
	}
	toBefore, err := db.GetSourceByKeyContext(ctx, b.SourceKey)
	if err != nil {
		return nil, err
	}
	fromRevision, toRevision := sourceRevision(fromBefore), sourceRevision(toBefore)
	left, err := s.Detail(ctx, fromItem, false)
	if err != nil {
		return nil, err
	}
	target, err := s.Detail(ctx, toItem, false)
	if err != nil {
		return nil, err
	}
	if len(target.Chapters) == 0 {
		target, err = s.RefreshToc(ctx, toItem)
		if err != nil {
			return nil, err
		}
	}
	// Detail can refresh metadata: validate again before any mapping is exposed.
	identity = matching.Identity(left.Item.Title, left.Item.Author, target.Item.Title, target.Item.Author)
	if identity == "mismatch" {
		return nil, apperror.New(apperror.Conflict, "详情核验后作品身份不符")
	}
	convert := func(chapters []*Chapter) []matching.Chapter {
		out := make([]matching.Chapter, 0, len(chapters))
		for _, c := range chapters {
			if c != nil {
				out = append(out, matching.Chapter{ID: c.ID, Title: c.Title, Volume: c.Volume, IsVolume: c.IsVolume, Available: c.Available})
			}
		}
		return out
	}
	var result matching.Result
	if kind == model.SourceKindComic {
		result = comic.Align(identity, convert(left.Chapters), convert(target.Chapters))
	} else {
		result = novel.Align(identity, convert(left.Chapters), convert(target.Chapters))
	}
	out := &ChapterMatch{Kind: string(kind), Identity: identity, Status: "unmatched", ChapterID: chapterID, TargetItemID: toItem, Targets: []*Chapter{}, Matched: result.Matched, Ambiguous: result.Ambiguous, Unmatched: result.Unmatched, CheckedAt: time.Now().Unix()}
	byID := map[int]*Chapter{}
	for _, c := range target.Chapters {
		if c != nil {
			byID[c.ID] = c
		}
	}
	found := chapterID == 0
	if chapterID == 0 {
		out.Evidence = "当前阅读位置无法确认，请在新来源目录选择"
	}
	for _, pair := range result.Pairs {
		if pair.ChapterID == chapterID {
			found = true
			out.Status = pair.Status
			out.Evidence = pair.Evidence
			for _, id := range pair.TargetIDs {
				if c := byID[id]; c != nil {
					out.Targets = append(out.Targets, c)
				}
			}
			break
		}
	}
	if !found {
		return nil, apperror.New(apperror.Validation, "当前章节不属于原作品目录")
	}
	fromSource, err := db.GetSourceByKey(a.SourceKey)
	if err != nil {
		return nil, err
	}
	toSource, err := db.GetSourceByKey(b.SourceKey)
	if err != nil {
		return nil, err
	}
	if sourceRevision(fromSource) != fromRevision || sourceRevision(toSource) != toRevision {
		return nil, apperror.New(apperror.Conflict, "来源规则已变化，请重新核对章节")
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	if err := db.SaveReadingAlignment(ctx, kind, fromItem, toItem, sourceRevision(fromSource), sourceRevision(toSource), string(raw)); err != nil {
		return nil, apperror.Wrap(apperror.Storage, err, "保存章节对齐证据失败")
	}
	return out, nil
}
