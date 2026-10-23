package library

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"cczjVideo/app/apperror"
	"cczjVideo/app/db"
)

type SourceQuality struct {
	Status    string       `json:"status"`
	Score     int          `json:"score"`
	CheckedAt int64        `json:"checkedAt"`
	Millis    int64        `json:"millis"`
	Samples   int          `json:"samples"`
	Successes int          `json:"successes"`
	Stages    []*TestStage `json:"stages"`
	Evidence  string       `json:"evidence"`
}

const sourceQualitySetting = "reading.source-quality.v1"

func (s *Service) loadSourceQualities() (map[string]*SourceQuality, error) {
	s.qualityMu.Lock()
	defer s.qualityMu.Unlock()
	return readSourceQualities()
}

func readSourceQualities() (map[string]*SourceQuality, error) {
	raw, err := db.GetSetting(sourceQualitySetting)
	if err != nil {
		return nil, apperror.Wrap(apperror.Storage, err, "读取源检测记录失败")
	}
	rows := map[string]*SourceQuality{}
	if raw != "" {
		if len(raw) > 4<<20 {
			return nil, apperror.New(apperror.Corrupt, "源检测记录超出容量限制")
		}
		if err = json.Unmarshal([]byte(raw), &rows); err != nil {
			return nil, apperror.Wrap(apperror.Corrupt, err, "源检测记录已损坏")
		}
		for id, row := range rows {
			if len(id) != 64 || row == nil || row.CheckedAt <= 0 || row.Samples < 1 || row.Successes < 0 || row.Successes > row.Samples || row.Score < 0 || row.Score > 100 || row.Millis < 0 || len(row.Stages) > 8 || (row.Status != "healthy" && row.Status != "partial" && row.Status != "failed") {
				return nil, apperror.New(apperror.Corrupt, "源检测记录格式不合法")
			}
			for _, stage := range row.Stages {
				if stage == nil || stage.Millis < 0 || (stage.Status != "ok" && stage.Status != "skipped" && stage.Status != "failed") {
					return nil, apperror.New(apperror.Corrupt, "源检测步骤格式不合法")
				}
			}
		}
	}
	return rows, nil
}

func (s *Service) saveSourceQuality(id string, outcome *TestOutcome) (*SourceQuality, error) {
	s.qualityMu.Lock()
	defer s.qualityMu.Unlock()
	rows, err := readSourceQualities()
	if err != nil {
		return nil, err
	}
	quality := &SourceQuality{Status: "failed", CheckedAt: time.Now().Unix(), Samples: 1, Stages: outcome.Stages, Evidence: "local"}
	if prior := rows[id]; prior != nil {
		if strings.HasPrefix(prior.Evidence, "benchmark:") {
			// A successful quick check does not replace fresh multi-work evidence
			// or extend its age. A failure can invalidate it, using quick-run units.
			if outcome.OK && time.Since(time.Unix(prior.CheckedAt, 0)) <= 24*time.Hour {
				return prior, nil
			}
		} else {
			quality.Samples = prior.Samples + 1
			quality.Successes = prior.Successes
		}
	}
	completed, total := 0, 0
	searchOK := false
	for _, stage := range outcome.Stages {
		if stage.Name == StageConfig {
			continue
		}
		total++
		quality.Millis += stage.Millis
		if stage.Status == "ok" {
			completed++
			if stage.Name == StageSearch {
				searchOK = true
			}
		}
	}
	if searchOK {
		quality.Status = "partial"
	}
	if outcome.OK {
		quality.Status = "healthy"
		quality.Successes++
	}
	// Completeness dominates; historical success and latency break ties. An
	// untested source has no score. A single successful sample is not "stable".
	if total > 0 {
		quality.Score = completed * 80 / total
	}
	quality.Score += quality.Successes * 10 / quality.Samples
	if quality.Millis < 1000 {
		quality.Score += 10
	} else if quality.Millis < 5000 {
		quality.Score += 6
	} else if quality.Millis < 15000 {
		quality.Score += 3
	}
	if quality.Status != "healthy" && quality.Score > 79 {
		quality.Score = 79
	}
	if len(rows) >= 512 && rows[id] == nil {
		var oldest string
		var stamp int64
		for key, row := range rows {
			if stamp == 0 || row.CheckedAt < stamp {
				oldest, stamp = key, row.CheckedAt
			}
		}
		delete(rows, oldest)
	}
	rows[id] = quality
	encoded, err := json.Marshal(rows)
	if err != nil {
		return nil, fmt.Errorf("encode source quality: %w", err)
	}
	if err = db.SetSetting(sourceQualitySetting, string(encoded)); err != nil {
		return nil, apperror.Wrap(apperror.Storage, err, "保存源检测记录失败，请重试")
	}
	return quality, nil
}
