package db

import (
	"cczjVideo/app/model"
	"fmt"
)

func GetAllSources() ([]*model.Source, error) {
	return ReadSources(instance)
}

func GetEnabledSources() ([]*model.Source, error) {
	var sources []*model.Source
	err := instance.Select(&sources, `SELECT * FROM sources WHERE enabled = 1 ORDER BY id`)
	return sources, err
}

func GetSourceByKey(key string) (*model.Source, error) {
	if err := model.ValidateSourceKey(key); err != nil {
		return nil, err
	}
	var s model.Source
	err := instance.Get(&s, `SELECT * FROM sources WHERE source_key = ?`, key)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func AddSource(s *model.Source) error {
	if s == nil {
		return fmt.Errorf("source is required")
	}
	if err := model.ValidateSourceKey(s.SourceKey); err != nil {
		return err
	}
	_, err := instance.NamedExec(`INSERT INTO sources (source_key, name, api_url, enabled, collect_limit, collect_hours, adv_config, schedule_config, strategy_config)
		VALUES (:source_key, :name, :api_url, :enabled, :collect_limit, :collect_hours, :adv_config, :schedule_config, :strategy_config)`, s)
	return err
}

func UpdateSource(s *model.Source) error {
	if s == nil {
		return fmt.Errorf("source is required")
	}
	if err := model.ValidateSourceKey(s.SourceKey); err != nil {
		return err
	}
	_, err := instance.NamedExec(`UPDATE sources SET name=:name, api_url=:api_url, enabled=:enabled,
		collect_limit=:collect_limit, collect_hours=:collect_hours,
		adv_config=:adv_config, schedule_config=:schedule_config, strategy_config=:strategy_config
		WHERE source_key=:source_key`, s)
	return err
}

func DeleteSource(key string) error {
	if err := model.ValidateSourceKey(key); err != nil {
		return err
	}
	tx, err := instance.Beginx()
	if err != nil {
		return fmt.Errorf("begin source deletion: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE source_videos SET lifecycle_state='deleted', updated_at=CURRENT_TIMESTAMP WHERE source_key=?`, key); err != nil {
		return fmt.Errorf("retire source catalog: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM sources WHERE source_key = ?`, key); err != nil {
		return fmt.Errorf("delete source record: %w", err)
	}
	if err := ResetCollectCursor(tx, key); err != nil {
		return err
	}
	if err := ResetSourceHealth(tx, key); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit source deletion: %w", err)
	}
	return nil
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
			VideoCount:   catalogCount(s.SourceKey),
			EpisodeCount: 0,
		})
	}
	return stats, nil
}

func catalogCount(sourceKey string) int {
	var count int
	_ = instance.Get(&count, `SELECT COUNT(*) FROM source_videos WHERE source_key=? AND lifecycle_state='active'`, sourceKey)
	return count
}
