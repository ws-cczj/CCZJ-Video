package media

import (
	"cczjVideo/app/apperror"
	"cczjVideo/app/applog"
	"cczjVideo/app/collect"
	"cczjVideo/app/db"
	"cczjVideo/app/model"
	"cczjVideo/app/operation"
	"cczjVideo/app/parallel"
	"cczjVideo/app/proxy"
	"context"
	"database/sql"
	"errors"
	"time"
)

type PlayLineQualityItem struct {
	Index int    `json:"index"`
	Name  string `json:"name"`
	EpNum int    `json:"ep_num"`
	proxy.PlaylistQuality
}

// ProbePlayLines previews a remote match without importing it into the user's
// catalogue. Existing catalogue entries continue to share the detail cache.
func (s *Service) ProbePlayLines(ctx context.Context, req PlayLineSpeedReq) ([]*PlayLineQualityItem, error) {
	if req.SourceKey == "" || req.VodId == "" {
		return nil, apperror.New(apperror.Validation, "source_key and vod_id are required")
	}
	ctx, cancel := context.WithTimeout(ctx, 24*time.Second)
	defer cancel()
	var lines []*model.PlayLine
	_, err := db.GetCatalogItemContext(ctx, req.SourceKey, req.VodId)
	if err == nil {
		result, detailErr := s.resolveDetail(ctx, req)
		if detailErr != nil {
			return nil, detailErr
		}
		lines = result.Lines
	} else if errors.Is(err, sql.ErrNoRows) {
		source, sourceErr := db.GetSourceByKeyContext(ctx, req.SourceKey)
		if sourceErr != nil {
			return nil, sourceErr
		}
		strategy := collect.CreateStrategyFromSource(source)
		if strategy == nil {
			return nil, apperror.New(apperror.Unavailable, "source strategy unavailable")
		}
		video, fetchErr := collect.FetchVideoDetailWithStrategy(ctx, strategy, strategy.BuildDetailUrl(req.VodId))
		if fetchErr != nil {
			return nil, fetchErr
		}
		lines = collect.ParsePlayLines(video.VodPlayUrl, video.VodPlayFrom, model.FlexibleString(req.VodId))
	} else {
		return nil, err
	}
	lines = usableLines(lines)
	if len(lines) > 32 {
		lines = lines[:32]
	}
	items := make([]*PlayLineQualityItem, len(lines))
	for i, line := range lines {
		items[i] = &PlayLineQualityItem{Index: line.Index, Name: line.Name, EpNum: req.EpNum}
	}
	err = parallel.Each(ctx, len(lines), 2, func(ctx context.Context, i int) error {
		ep := pickProbeEpisode(lines[i], req.EpNum)
		if ep == nil {
			return nil
		}
		quality, probeErr := proxy.ProbePlaylistQuality(ctx, ep.EpUrl)
		items[i].PlaylistQuality = quality
		items[i].EpNum = ep.EpNum
		fields := applog.Fields{"source_key": req.SourceKey, "vod_id": req.VodId, "line_index": lines[i].Index, "ep_num": ep.EpNum, "ok": quality.OK, "width": quality.Width, "height": quality.Height, "bandwidth": quality.Bandwidth}
		if probeErr != nil {
			fields["reason"] = applog.DiagnosticText(probeErr.Error(), 1024)
		}
		operation.Stage(ctx, "playlist_quality", fields)
		return ctx.Err()
	})
	if err != nil {
		return nil, err
	}
	return items, nil
}
