// 项目统一类型定义

// 来源（注：不与 wails 的 models.Source 冲突，前端单独用这个类型）
export interface SourceInfo {
  id?: number
  source_key?: string
  name: string
  api_url: string
  url_template?: string
  url_prefix?: string
  url_suffix?: string
  enabled?: boolean
  collect_limit?: number
  collect_hours?: number
  created_at?: string
}

// 通用 source 对象（用于字段不全的场景）
export type SourceLike = Partial<SourceInfo> & {
  name: string
  api_url: string
}

// 来源统计
export interface SourceStat {
  source_key: string
  name?: string
  video_count: number
  episode_count: number
}

// 类型筛选
export interface VType {
  type_id: string
  name: string
}

// 视频（列表项）
export interface Video {
  id?: number
  vod_id?: string
  vod_g_id?: string
  vod_name?: string
  global_id?: number
  vod_pic?: string
  vod_remarks?: string
  vod_area?: string
  vod_year?: string
  type_name?: string
  type_id?: string
  vod_actor?: string
  vod_director?: string
  vod_content?: string
  vod_play_url?: string
  vod_down_url?: string
  vod_time?: string
  vod_douban_id?: string
  vod_douban_score?: string
  vod_hits?: string
  vod_hits_day?: string
  vod_hits_week?: string
  vod_hits_month?: string
  vod_pubdate?: string
  vod_version?: string
  vod_state?: string
  vod_score?: string
  vod_score_all?: string
  vod_score_num?: string
  vod_isend?: string
  vod_play_from?: string
  vod_play_note?: string
  vod_letter?: string
  vod_tag?: string
  vod_sub?: string
  vod_en?: string
}

// 剧集
export interface Episode {
  ep_num: number
  ep_name?: string
  ep_url: string
  ep_down_url?: string
}

// 视频详情响应
export interface VideoDetailResponse {
  video: Video | null
  episodes: Episode[]
}

// 视频列表响应
export interface VideoListResponse {
  videos: Video[]
  total: number
}

// 收藏
export interface Favorite {
  id: number
  source_key: string
  vod_id: string
  vod_name?: string
  video?: Video
  created_at?: string
}

// 历史记录
export interface HistoryItem {
  global_id?: number
  source_key: string
  vod_id: string
  ep_num: number
  vod_name?: string
  position?: number
  created_at?: string
  updated_at?: string
}

// 采集事件数据
export interface CollectProgressEvent {
  current: number
  total: number
}

export interface CollectLogEvent {
  message: string
}

export interface CollectDoneEvent {
  error?: string
}

// 视频列表请求
export interface VideoListRequest {
  source_key: string
  type_id: string
  page: number
  page_size: number
}

// 视频详情请求
export interface VideoDetailRequest {
  source_key: string
  vod_id: string
}

// 搜索请求
export interface SearchRequest {
  source_key: string
  keyword: string
  page: number
  page_size: number
}
