package db

import (
	"cczjVideo/app/model"
	"fmt"
)

func GetAllSources() ([]*model.Source, error) {
	var sources []*model.Source
	err := instance.Select(&sources, `SELECT * FROM sources ORDER BY id`)
	return sources, err
}

func GetEnabledSources() ([]*model.Source, error) {
	var sources []*model.Source
	err := instance.Select(&sources, `SELECT * FROM sources WHERE enabled = 1 ORDER BY id`)
	return sources, err
}

func GetSourceByKey(key string) (*model.Source, error) {
	var s model.Source
	err := instance.Get(&s, `SELECT * FROM sources WHERE source_key = ?`, key)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func AddSource(s *model.Source) error {
	_, err := instance.NamedExec(`INSERT INTO sources (source_key, name, api_url, url_template, url_prefix, url_suffix, collect_limit, collect_hours, adv_config, schedule_config)
		VALUES (:source_key, :name, :api_url, :url_template, :url_prefix, :url_suffix, :collect_limit, :collect_hours, :adv_config, :schedule_config)`, s)
	return err
}

func UpdateSource(s *model.Source) error {
	_, err := instance.NamedExec(`UPDATE sources SET name=:name, api_url=:api_url,
		url_template=:url_template, url_prefix=:url_prefix, url_suffix=:url_suffix, enabled=:enabled,
		collect_limit=:collect_limit, collect_hours=:collect_hours,
		adv_config=:adv_config, schedule_config=:schedule_config
		WHERE source_key=:source_key`, s)
	return err
}

func DeleteSource(key string) error {
	// ⭐ 先删除关联的 v_* 和 e_* 数据表，避免残留数据
	vTbl := VideoTableName(key)
	eTbl := EpisodeTableName(key)
	_, _ = instance.Exec(fmt.Sprintf(`DROP TABLE IF EXISTS %s`, vTbl))
	_, _ = instance.Exec(fmt.Sprintf(`DROP TABLE IF EXISTS %s`, eTbl))
	// 删除源记录
	_, err := instance.Exec(`DELETE FROM sources WHERE source_key = ?`, key)
	return err
}

func GetSourceStats() ([]model.SourceStat, error) {
	sources, err := GetAllSources()
	if err != nil {
		return nil, err
	}
	var stats []model.SourceStat
	for _, s := range sources {
		stats = append(stats, model.SourceStat{
			SourceKey:    s.SourceKey,
			Name:         s.Name,
			VideoCount:   GetVideoCount(s.SourceKey),
			EpisodeCount: GetEpisodeCount(s.SourceKey),
		})
	}
	return stats, nil
}