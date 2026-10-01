package service

import (
	"cczjVideo/app/apperror"
	"cczjVideo/app/db"
	"cczjVideo/app/douban"
)

// ======================== Douban ========================

// DoubanSearchReq 豆瓣搜索请求
type DoubanSearchReq struct {
	Keyword string `json:"keyword"`
}

// DoubanSearchResp 豆瓣搜索响应
type DoubanSearchResp struct {
	SubjectID string `json:"subject_id"`
	URL       string `json:"url"`
}

// DoubanSearch 搜索豆瓣获取 subject_id
func (a *App) DoubanSearch(req DoubanSearchReq) (*DoubanSearchResp, error) {
	if req.Keyword == "" {
		return nil, apperror.New(apperror.Validation, "关键词不能为空")
	}
	// GlobalID 留空：这是用户在界面上主动发起的搜索，不该被某条记录的 24 小时
	// 「查无此片」冷却挡住；反过来说它的失败结果也不该写进任何一行的冷却。
	id, err := douban.SearchSubjectID(req.Keyword, douban.SearchMeta{VodName: req.Keyword})
	if err != nil {
		return nil, err
	}
	return &DoubanSearchResp{
		SubjectID: id,
		URL:       "https://movie.douban.com/subject/" + id + "/",
	}, nil
}

// DoubanDetailReq 豆瓣详情请求
type DoubanDetailReq struct {
	SubjectID string `json:"subject_id"`
}

// DoubanDetailResp 豆瓣详情响应
type DoubanDetailResp struct {
	*douban.DoubanInfo
}

// DoubanDetail 获取豆瓣详情信息
func (a *App) DoubanDetail(req DoubanDetailReq) (*DoubanDetailResp, error) {
	if req.SubjectID == "" {
		return nil, apperror.New(apperror.Validation, "subject_id 不能为空")
	}
	info, err := douban.ParseDetail(req.SubjectID)
	if err != nil {
		return nil, err
	}
	return &DoubanDetailResp{DoubanInfo: info}, nil
}

// DoubanUpdateVideoReq 更新单个视频的豆瓣信息请求
type DoubanUpdateVideoReq struct {
	Keyword string `json:"keyword"`
}

// DoubanUpdateVideo 手动为某个视频补全豆瓣信息（搜索+解析详情，存入全局表）
func (a *App) DoubanUpdateVideo(req DoubanUpdateVideoReq) (*douban.DoubanInfo, error) {
	if req.Keyword == "" {
		return nil, apperror.New(apperror.Validation, "关键词不能为空")
	}
	return a.schedulers.UpdateDoubanByKeyword(req.Keyword)
}

// DoubanChart 获取豆瓣热榜视频（立即返回，带匹配状态）
func (a *App) DoubanChart() ([]douban.ChartVideoItem, error) {
	return douban.GetChartVideos()
}

// DoubanChartResolve 用户点击时解析热榜视频（实时检查匹配状态）
func (a *App) DoubanChartResolve(subjectID string) (*douban.ChartVideoItem, error) {
	return douban.ResolveChartVideo(subjectID)
}

// DoubanTriggerNow 立即触发一次豆瓣信息补全
func (a *App) DoubanTriggerNow() (int, error) {
	return a.schedulers.TriggerDouban()
}

// DoubanStatus 返回豆瓣调度器状态
func (a *App) DoubanStatus() map[string]interface{} {
	result := map[string]interface{}{
		"running": false,
	}
	running, updating := a.schedulers.DoubanStatus()
	result["running"] = running
	result["updating"] = updating
	// 附加数据统计
	if all, err := db.GetAllDoubanInfo(); err == nil {
		result["total"] = len(all)
		completed := 0
		for _, r := range all {
			if r.SubjectID != "" {
				completed++
			}
		}
		result["completed"] = completed
		result["pending"] = len(all) - completed
	}
	return result
}

// DoubanGetAllReq 豆瓣数据请求（支持分页）
type DoubanGetAllReq struct {
	Page     int `json:"page"`
	PageSize int `json:"page_size"`
}

// DoubanGetAllResp 豆瓣数据响应（含分页信息）
type DoubanGetAllResp struct {
	Rows     []*db.DoubanInfoRow `json:"rows"`
	Total    int                 `json:"total"`
	Page     int                 `json:"page"`
	PageSize int                 `json:"page_size"`
}

// DoubanGetAll 获取全局豆瓣信息表中的记录（支持分页）
func (a *App) DoubanGetAll(req DoubanGetAllReq) (*DoubanGetAllResp, error) {
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 20
	}
	rows, total, err := db.GetAllDoubanInfoPaginated(req.Page, req.PageSize)
	if err != nil {
		return nil, err
	}
	return &DoubanGetAllResp{
		Rows:     rows,
		Total:    total,
		Page:     req.Page,
		PageSize: req.PageSize,
	}, nil
}

// ======================== 豆瓣评论 ========================

// DoubanCommentsReq 获取豆瓣评论请求
type DoubanCommentsReq struct {
	DoubanId string `json:"douban_id"`
	Page     int    `json:"page"`
	Sort     string `json:"sort"` // "new_score" | "time"
}

// GetDoubanComments 获取豆瓣评论（带 24h 缓存）
func (a *App) GetDoubanComments(req DoubanCommentsReq) (*douban.DoubanCommentsResp, error) {
	if req.DoubanId == "" {
		return nil, apperror.New(apperror.Validation, "douban_id 不能为空")
	}
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.Sort == "" {
		req.Sort = "new_score"
	}
	return douban.FetchComments(req.DoubanId, req.Page, req.Sort)
}
