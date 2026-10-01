// Package handler 播放线路测速入口。
//
// 一条影片常用 $$$ 并列多条线路，而源站给出的顺序跟真实快慢毫无关系：默认拿到哪条
// 就放哪条，慢的要卡半天。这里按详情解析出的线路各取一次样本（命中播放列表时下钻到
// 它的分片），按吞吐排序，界面据此标出最快的一条并允许手动切换。
package handler

import (
	"cczjVideo/app/apperror"
	"cczjVideo/app/detail"
	"cczjVideo/app/model"
	"cczjVideo/app/proxy"
	"context"
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"time"

	applog "cczjVideo/app/applog"
)

// lineSpeedTotalBudget 给整轮测速兜底：单条线路自带 8 秒超时，再加一次 Range 重试，
// 并发跑完也不该超过这个数。超时后未回的是哪条就按不可用记账，不让按钮永远转圈。
const lineSpeedTotalBudget = 24 * time.Second

// PlayLineSpeedReq 定位一次测速。播放地址不在请求里：由服务端按同一份详情解析，
// 免得前端把地址传回来再解析一遍，两边口径还可能不一致。
type PlayLineSpeedReq struct {
	SourceKey string `json:"source_key"`
	GlobalID  int64  `json:"global_id"`
	VodId     string `json:"vod_id"`
	EpNum     int    `json:"ep_num"`
}

func (r *PlayLineSpeedReq) UnmarshalJSON(data []byte) error {
	var raw struct {
		SourceKey string          `json:"source_key"`
		GlobalID  json.RawMessage `json:"global_id"`
		VodId     json.RawMessage `json:"vod_id"`
		EpNum     json.RawMessage `json:"ep_num"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	r.SourceKey = raw.SourceKey
	if v, err := parseInt64(raw.GlobalID, 0); err == nil {
		r.GlobalID = v
	}
	r.VodId = normalizeStringId(raw.VodId)
	if v, err := parseInt(raw.EpNum, 0); err == nil {
		r.EpNum = v
	}
	return nil
}

// PlayLineSpeedItem 是一条线路的测速结果。Error 只写日志，不下发给界面。
type PlayLineSpeedItem struct {
	Index       int     `json:"index"`
	Name        string  `json:"name"`
	EpNum       int     `json:"ep_num"`
	OK          bool    `json:"ok"`
	LatencyMS   int64   `json:"latency_ms"`
	BytesPerSec float64 `json:"bytes_per_sec"`
}

// PlayLineSpeedResp 的 Items 已按「可用优先 → 吞吐高优先 → 延迟低优先」排好，
// 界面可以直接照序渲染；BestIndex 为 -1 表示没有任何一条线路可用。
type PlayLineSpeedResp struct {
	Items     []*PlayLineSpeedItem `json:"items"`
	BestIndex int                  `json:"best_index"`
}

// SpeedTestPlayLines 并发探测一个视频的全部播放线路。
//
// ctx 是应用生命周期上下文：退出时这一轮最多 24 秒的并发探测必须能立刻断开，
// 否则窗口已经关了，测速还在给源站发请求。
func SpeedTestPlayLines(ctx context.Context, req PlayLineSpeedReq) (*PlayLineSpeedResp, error) {
	if req.SourceKey == "" || (req.GlobalID <= 0 && req.VodId == "") {
		return nil, apperror.New(apperror.Validation, "source_key and global_id are required")
	}
	result, err := resolveDetail(ctx, req)
	if err != nil {
		return nil, err
	}
	lines := usableLines(result.Lines)
	if len(lines) == 0 {
		return nil, apperror.New(apperror.NotFound, "该视频没有可测速的播放线路")
	}

	probeCtx, cancel := context.WithTimeout(ctx, lineSpeedTotalBudget)
	defer cancel()

	items := make([]*PlayLineSpeedItem, len(lines))
	var wg sync.WaitGroup
	for i, line := range lines {
		episode := pickProbeEpisode(line, req.EpNum)
		if episode == nil {
			items[i] = &PlayLineSpeedItem{Index: line.Index, Name: line.Name, EpNum: req.EpNum}
			continue
		}
		wg.Add(1)
		go func(i int, line *model.PlayLine, episode *model.Episode) {
			defer wg.Done()
			items[i] = probeLine(probeCtx, line, episode)
		}(i, line, episode)
	}
	wg.Wait()

	return rankLineSpeed(items), nil
}

func resolveDetail(ctx context.Context, req PlayLineSpeedReq) (*detail.Result, error) {
	if req.GlobalID > 0 {
		return detail.Default.GetByGlobalContext(ctx, req.SourceKey, req.GlobalID)
	}
	return detail.Default.GetContext(ctx, req.SourceKey, req.VodId)
}

// usableLines 丢掉没有线路序号或集表为空的解析残留，测速不该为它们发请求。
func usableLines(lines []*model.PlayLine) []*model.PlayLine {
	out := make([]*model.PlayLine, 0, len(lines))
	for _, line := range lines {
		if line == nil || line.Index < 0 || len(line.Episodes) == 0 {
			continue
		}
		out = append(out, line)
	}
	return out
}

// pickProbeEpisode 选一条线路里实际要探测的那一集：优先用户正在看的集数，这样测出来的
// 就是「切过去能不能立刻放」；该线路没有这一集时退回首集。
func pickProbeEpisode(line *model.PlayLine, epNum int) *model.Episode {
	if epNum > 0 {
		for _, ep := range line.Episodes {
			if ep != nil && ep.EpNum == epNum && strings.TrimSpace(ep.EpUrl) != "" {
				return ep
			}
		}
	}
	for _, ep := range line.Episodes {
		if ep != nil && strings.TrimSpace(ep.EpUrl) != "" {
			return ep
		}
	}
	return nil
}

func probeLine(ctx context.Context, line *model.PlayLine, episode *model.Episode) *PlayLineSpeedItem {
	item := &PlayLineSpeedItem{Index: line.Index, Name: line.Name, EpNum: episode.EpNum}
	sample := proxy.ProbePlayURL(ctx, episode.EpUrl)
	item.OK = sample.OK
	item.LatencyMS = sample.LatencyMS
	item.BytesPerSec = sample.BytesPerSec
	if !sample.OK {
		applog.Warn("[SpeedTestPlayLines] 线路 %d(%s) 探测失败: %s", line.Index, line.Name, sample.Error)
	}
	return item
}

func rankLineSpeed(items []*PlayLineSpeedItem) *PlayLineSpeedResp {
	sort.SliceStable(items, func(a, b int) bool {
		left, right := items[a], items[b]
		if left.OK != right.OK {
			return left.OK
		}
		if !left.OK {
			return left.Index < right.Index
		}
		// BytesPerSec 为 0 的可用线路是「下载快过时钟精度、量不出来」的那类，不是慢线路，
		// 所以排在前而不是按最小的速度垫底（见 proxy.finishSample）。
		if (left.BytesPerSec > 0) != (right.BytesPerSec > 0) {
			return right.BytesPerSec > 0
		}
		if left.BytesPerSec != right.BytesPerSec {
			return left.BytesPerSec > right.BytesPerSec
		}
		return left.LatencyMS < right.LatencyMS
	})
	resp := &PlayLineSpeedResp{Items: items, BestIndex: -1}
	if len(items) > 0 && items[0].OK {
		resp.BestIndex = items[0].Index
	}
	return resp
}
