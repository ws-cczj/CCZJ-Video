package handler

import (
	"cczjVideo/app/db"
	"cczjVideo/app/model"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

func GetAllSources() ([]*model.Source, error) {
	return db.GetAllSources()
}

func GetEnabledSources() ([]*model.Source, error) {
	return db.GetEnabledSources()
}

var nonAlpha = regexp.MustCompile(`[^a-z0-9]`)

func deriveKey(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	host := u.Hostname()
	host = strings.TrimPrefix(host, "api.")
	parts := strings.Split(host, ".")
	if len(parts) >= 2 && (parts[len(parts)-1] == "com" || parts[len(parts)-1] == "cn" || parts[len(parts)-1] == "me" || parts[len(parts)-1] == "cc") {
		return nonAlpha.ReplaceAllString(parts[len(parts)-2], "_")
	}
	return nonAlpha.ReplaceAllString(parts[0], "_")
}

func AddSource(s *model.Source) error {
	if s.ApiUrl == "" {
		return fmt.Errorf("api_url is required")
	}
	if _, err := url.Parse(s.ApiUrl); err != nil {
		return fmt.Errorf("invalid api_url: %w", err)
	}
	if s.SourceKey == "" {
		s.SourceKey = deriveKey(s.ApiUrl)
	}
	if s.SourceKey == "" {
		return fmt.Errorf("cannot derive source_key from api_url")
	}
	if s.Name == "" {
		s.Name = s.SourceKey
	}

	existing, _ := db.GetSourceByKey(s.SourceKey)
	if existing != nil {
		return fmt.Errorf("来源 %s 已存在", s.SourceKey)
	}
	return db.AddSource(s)
}

func UpdateSource(s *model.Source) error {
	return db.UpdateSource(s)
}

func DeleteSource(key string) error {
	return db.DeleteSource(key)
}

func GetSourceStats() ([]model.SourceStat, error) {
	return db.GetSourceStats()
}

// ======================== 数据源详情 / 操作 ========================

// SourceTableSummary 描述该源下某一张表的信息
type SourceTableSummary struct {
	TableName string           `json:"table_name"`
	Role      string           `json:"role"` // video / episode / type
	RowCount  int              `json:"row_count"`
	Columns   []db.TableColumn `json:"columns"`
}

// SourceDetail 返回该源的"字段 + 示例"，用于设置页面展示
type SourceDetail struct {
	SourceKey string                `json:"source_key"`
	Name      string                `json:"name"`
	ApiUrl    string                `json:"api_url"`
	Tables    []SourceTableSummary  `json:"tables"`
	Samples   []*model.Video        `json:"sample_videos"`
	Episodes  []*model.Episode      `json:"sample_episodes"`
}

func GetSourceDetail(sourceKey string) (*SourceDetail, error) {
	src, err := db.GetSourceByKey(sourceKey)
	if err != nil {
		return nil, fmt.Errorf("source not found: %s", sourceKey)
	}

	tables := []SourceTableSummary{
		{TableName: db.VideoTableName(sourceKey), Role: "video"},
		{TableName: db.EpisodeTableName(sourceKey), Role: "episode"},
		{TableName: db.TypeTableName(sourceKey), Role: "type"},
	}
	for i := range tables {
		t := &tables[i]
		cols, cErr := db.GetTableColumns(t.TableName)
		if cErr == nil {
			t.Columns = cols
		}
		if db.TableExists(t.TableName) {
			var cnt int
			_ = db.DB().Get(&cnt, fmt.Sprintf(`SELECT COUNT(1) FROM %s`, t.TableName))
			t.RowCount = cnt
		}
	}

	samples, _ := db.GetSampleVideos(sourceKey, 5)
	episodes, _ := db.GetSampleEpisodes(sourceKey, 10)

	return &SourceDetail{
		SourceKey: src.SourceKey,
		Name:      src.Name,
		ApiUrl:    src.ApiUrl,
		Tables:    tables,
		Samples:   samples,
		Episodes:  episodes,
	}, nil
}

// TruncateSourceData 仅清空该源的视频/剧集/分类数据，保留 source 元信息
func TruncateSourceData(sourceKey string) (bool, error) {
	if err := db.TruncateSource(sourceKey); err != nil {
		return false, err
	}
	return true, nil
}

// RecreateSourceTables 删除并重建该源的三张表（数据全部丢失）
func RecreateSourceTables(sourceKey string) (bool, error) {
	if err := db.RecreateSourceTables(sourceKey); err != nil {
		return false, err
	}
	return true, nil
}

// DeleteSourceVideo 精确删除该源下的某一条 vod_id
func DeleteSourceVideo(sourceKey string, vodId string) (bool, error) {
	if err := db.DeleteByVodId(sourceKey, vodId); err != nil {
		return false, err
	}
	return true, nil
}
