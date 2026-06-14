package douban

import (
	"sync"

	"cczjVideo/app/applog"
	"cczjVideo/app/db"
)

type Updater struct {
	batchSize int
	mu        sync.Mutex
	enabled   bool
	running   bool
}

func NewUpdater() *Updater {
	return &Updater{
		batchSize: 3,
		enabled:   true,
	}
}

func (u *Updater) Enable(enable bool) {
	u.mu.Lock()
	u.enabled = enable
	u.mu.Unlock()
	applog.Info("[Douban] Updater %s", map[bool]string{true: "enabled", false: "disabled"}[enable])
}

func (u *Updater) IsRunning() bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.running
}

func (u *Updater) UpdateBatch() (int, error) {
	u.mu.Lock()
	if !u.enabled || u.running {
		if !u.enabled {
			applog.Info("[Douban] UpdateBatch skipped: updater is disabled")
		} else {
			applog.Info("[Douban] UpdateBatch skipped: already running")
		}
		u.mu.Unlock()
		return 0, nil
	}
	u.running = true
	u.mu.Unlock()

	defer func() {
		u.mu.Lock()
		u.running = false
		u.mu.Unlock()
	}()

	applog.Info("[Douban] Starting batch update, batch size: %d", u.batchSize)
	totalUpdated := 0

	// 步骤1: 为 subject_id 为空的记录搜索豆瓣
	missingID, err := db.GetDoubanInfoMissingSubjectID(u.batchSize)
	if err != nil {
		applog.Error("[Douban] ERROR: failed to get records missing subject_id: %v", err)
	} else {
		applog.Info("[Douban] Found %d records missing subject_id", len(missingID))
		for i, row := range missingID {
			if row == nil || row.VodName == "" {
				continue
			}
			applog.Info("[Douban] [%d/%d] Filling subject_id for: %s", i+1, len(missingID), row.VodName)
			if err := u.fillSubjectID(row); err != nil {
				applog.Warn("[Douban] [%d/%d] FAILED to fill subject_id for '%s': %v", i+1, len(missingID), row.VodName, err)
				continue
			}
			totalUpdated++
			applog.Info("[Douban] [%d/%d] SUCCESS filled subject_id for '%s'", i+1, len(missingID), row.VodName)
		}
	}

	// 步骤2: 为有 subject_id 但信息不完整的记录解析豆瓣详情页
	incomplete, err := db.GetIncompleteDoubanInfo(u.batchSize)
	if err != nil {
		applog.Error("[Douban] ERROR: failed to get incomplete records: %v", err)
	} else {
		applog.Info("[Douban] Found %d incomplete records (has subject_id but missing details)", len(incomplete))
		for i, row := range incomplete {
			if row == nil || row.SubjectID == "" {
				continue
			}
			applog.Info("[Douban] [%d/%d] Filling detail for subject %s (%s)", i+1, len(incomplete), row.SubjectID, row.VodName)
			if err := u.fillDetail(row); err != nil {
				applog.Warn("[Douban] [%d/%d] FAILED to fill detail for subject '%s': %v", i+1, len(incomplete), row.SubjectID, err)
				continue
			}
			totalUpdated++
			applog.Info("[Douban] [%d/%d] SUCCESS filled detail for subject '%s'", i+1, len(incomplete), row.SubjectID)
		}
	}

	applog.Info("[Douban] Batch update completed: %d records updated", totalUpdated)
	return totalUpdated, nil
}

func (u *Updater) fillSubjectID(row *db.DoubanInfoRow) error {
	subjectID, err := SearchSubjectID(row.VodName)
	if err != nil {
		return err
	}

	applog.Debug("[Douban] Upserting subject_id '%s' for '%s' (global_id=%d)", subjectID, row.VodName, row.GlobalID)
	updated := *row
	updated.SubjectID = subjectID
	return db.UpsertDoubanInfo(&updated)
}

func (u *Updater) fillDetail(row *db.DoubanInfoRow) error {
	info, err := ParseDetail(row.SubjectID)
	if err != nil {
		return err
	}

	updated := *row
	updatedFields := 0

	if info.Rating != "" && updated.Rating == "" {
		updated.Rating = info.Rating
		updatedFields++
	}
	if info.Votes != "" && updated.Votes == "" {
		updated.Votes = info.Votes
		updatedFields++
	}
	if info.Director != "" && updated.Director == "" {
		updated.Director = info.Director
		updatedFields++
	}
	if info.Writer != "" && updated.Writer == "" {
		updated.Writer = info.Writer
		updatedFields++
	}
	if info.Actor != "" && updated.Actor == "" {
		updated.Actor = info.Actor
		updatedFields++
	}
	if info.Genre != "" && updated.Genre == "" {
		updated.Genre = info.Genre
		updatedFields++
	}
	if info.Country != "" && updated.Country == "" {
		updated.Country = info.Country
		updatedFields++
	}
	if info.Language != "" && updated.Language == "" {
		updated.Language = info.Language
		updatedFields++
	}
	if info.ReleaseDate != "" && updated.ReleaseDate == "" {
		updated.ReleaseDate = info.ReleaseDate
		updatedFields++
	}
	if info.SeasonCount != "" && updated.SeasonCount == "" {
		updated.SeasonCount = info.SeasonCount
		updatedFields++
	}
	if info.EpisodeCount != "" && updated.EpisodeCount == "" {
		updated.EpisodeCount = info.EpisodeCount
		updatedFields++
	}
	if info.Duration != "" && updated.Duration == "" {
		updated.Duration = info.Duration
		updatedFields++
	}
	if info.Aka != "" && updated.Aka == "" {
		updated.Aka = info.Aka
		updatedFields++
	}
	if info.IMDb != "" && updated.Imdb == "" {
		updated.Imdb = info.IMDb
		updatedFields++
	}
	if info.PosterURL != "" && updated.PosterURL == "" {
		updated.PosterURL = info.PosterURL
		updatedFields++
	}

	applog.Debug("[Douban] Upserting %d fields for subject '%s' (global_id=%d)", updatedFields, row.SubjectID, row.GlobalID)
	return db.UpsertDoubanInfo(&updated)
}

func (u *Updater) UpdateSingleByKeyword(keyword string) (*DoubanInfo, error) {
	applog.Info("[Douban] Manual update for keyword: %s", keyword)
	info, err := FetchDoubanInfo(keyword)
	if err != nil {
		applog.Error("[Douban] Manual update FAILED for '%s': %v", keyword, err)
		return nil, err
	}

	if info != nil {
		// 获取或创建 global_video 记录
		globalID, err := db.GetOrCreateGlobalID(keyword)
		if err != nil {
			applog.Error("[Douban] Failed to get/create global_video for '%s': %v", keyword, err)
		} else {
			row := &db.DoubanInfoRow{
				GlobalID:     int(globalID),
				SubjectID:    info.SubjectID,
				Rating:       info.Rating,
				Votes:        info.Votes,
				Director:     info.Director,
				Writer:       info.Writer,
				Actor:        info.Actor,
				Genre:        info.Genre,
				Country:      info.Country,
				Language:     info.Language,
				ReleaseDate:  info.ReleaseDate,
				SeasonCount:  info.SeasonCount,
				EpisodeCount: info.EpisodeCount,
				Duration:     info.Duration,
				Aka:          info.Aka,
				Imdb:         info.IMDb,
				PosterURL:    info.PosterURL,
			}
			if err := db.UpsertDoubanInfo(row); err != nil {
				applog.Error("[Douban] Failed to upsert manual update for '%s': %v", keyword, err)
			} else {
				applog.Info("[Douban] Manual update SUCCESS for '%s': subject_id=%s, rating=%s", keyword, info.SubjectID, info.Rating)
			}
		}
	}

	return info, nil
}