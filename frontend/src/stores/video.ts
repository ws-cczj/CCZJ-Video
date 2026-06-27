import { defineStore } from 'pinia'
import { ref } from 'vue'
import { GetVideoList, SearchVideos, GetTypes, GetYearsAndAreas, GetRecommend } from '../../bindings/cczjVideo/app'
import * as AppMod from '../../bindings/cczjVideo/app'
import type { Video, Episode, VType, VideoDetailResponse } from '../types'
import { useErrorStore } from './error'

export interface VideoFilter {
  type_id: string | number
  year: string
  area: string
  keyword: string
  sort?: string // '' = 默认; 'rating' = 按评分; 'hot' = 按热度
}

export const useVideoStore = defineStore('video', () => {
  const videos = ref<Video[]>([])
  const currentVideo = ref<Video | null>(null)
  const episodes = ref<Episode[]>([])
  const types = ref<VType[]>([])
  const years = ref<string[]>([])
  const areas = ref<string[]>([])
  const total = ref(0)
  const page = ref(1)
  const loading = ref(false)

  // 删除通知：当某页面删除视频后，其他列表页监听此值来移除该视频
  const deletedVodId = ref<string | null>(null)
  const deletedSourceKey = ref<string | null>(null)
  function notifyDeletion(sourceKey: string, vodId: string): void {
    deletedSourceKey.value = sourceKey
    deletedVodId.value = vodId
  }
  function clearDeletionNotify(): void {
    deletedSourceKey.value = null
    deletedVodId.value = null
  }

  // 全局刷新通知：当数据被清空或重新采集后，通知所有页面刷新数据
  const refreshTrigger = ref(0)
  function notifyRefresh(): void {
    refreshTrigger.value++
  }

  const errorStore = useErrorStore()

  async function loadVideos(sourceKey: string, filter: VideoFilter, p = 1, pageSize = 50): Promise<void> {
    loading.value = true
    try {
      // 内部对象：强制类型断言避免 handler.VideoListReq 新字段（year/area/keyword/sort）造成 TS 报错
      const req = {
        source_key: sourceKey,
        type_id: filter.type_id === undefined || filter.type_id === null ? '' : String(filter.type_id),
        year: filter.year ?? '',
        area: filter.area ?? '',
        keyword: filter.keyword ?? '',
        sort: filter.sort ?? '',
        page: p,
        page_size: pageSize,
      } as any
      const resp = (await GetVideoList(req)) as any
      const list: Video[] = Array.isArray(resp?.videos) ? resp.videos : []
      const ttl: number = typeof resp?.total === 'number' ? resp.total : list.length
      if (p === 1) {
        // 先加载完新数据，再一次性替换，避免短暂空白引起布局跳动
        videos.value = list
      } else {
        videos.value.push(...list)
      }
      total.value = ttl
      page.value = p
    } catch (e: any) {
      errorStore.fromError('加载视频列表失败', e, 'videoStore.loadVideos')
    } finally {
      loading.value = false
    }
  }

  async function loadDetail(sourceKey: string, vodId: string, refresh = false): Promise<void> {
    loading.value = true
    try {
      const resp = (await (AppMod as any).GetVideoDetail({ source_key: sourceKey, vod_id: vodId, refresh })) as VideoDetailResponse
      currentVideo.value = resp?.video ?? null
      episodes.value = Array.isArray(resp?.episodes) ? resp.episodes : []
    } catch (e: any) {
      const msg = e?.message || ''
      if (msg.includes('video not found') || msg.includes('sql: no rows')) {
        currentVideo.value = null
        episodes.value = []
      } else {
        errorStore.fromError('加载视频详情失败', e, 'videoStore.loadDetail')
        currentVideo.value = null
        episodes.value = []
      }
    } finally {
      loading.value = false
    }
  }

  /** 后台刷新详情（不设置 loading，用于已有本地数据后异步更新） */
  async function refreshDetail(sourceKey: string, vodId: string): Promise<boolean> {
    try {
      const resp = (await (AppMod as any).GetVideoDetail({ source_key: sourceKey, vod_id: vodId, refresh: true })) as VideoDetailResponse
      if (resp?.video) {
        currentVideo.value = resp.video
        if (Array.isArray(resp.episodes) && resp.episodes.length > 0) {
          episodes.value = resp.episodes
        }
        return true
      }
      return false
    } catch {
      return false
    }
  }

  async function search(sourceKey: string, keyword: string, p = 1): Promise<void> {
    loading.value = true
    try {
      const resp = (await SearchVideos({
        source_key: sourceKey,
        keyword,
        page: p,
        page_size: 50,
      })) as any
      const list: Video[] = Array.isArray(resp?.videos) ? resp.videos : []
      const ttl: number = typeof resp?.total === 'number' ? resp.total : list.length
      if (p === 1) {
        videos.value = list
      } else {
        videos.value.push(...list)
      }
      total.value = ttl
      page.value = p
    } catch (e: any) {
      errorStore.fromError('搜索失败', e, 'videoStore.search')
    } finally {
      loading.value = false
    }
  }

  async function loadTypes(sourceKey: string): Promise<void> {
    try {
      const raw = await GetTypes({ source_key: sourceKey })
      let arr: any[] = []
      if (Array.isArray(raw)) arr = raw
      else if (raw && typeof raw === 'object') {
        if (Array.isArray((raw as any).list)) arr = (raw as any).list
        else if ((raw as any).type_id && (raw as any).name) arr = [raw]
      }
      types.value = arr.map((t: any) => ({ type_id: t?.type_id ?? '', name: t?.name ?? '' }))
    } catch (e: any) {
      errorStore.fromError('加载分类失败', e, 'videoStore.loadTypes')
      types.value = []
    }
  }

  async function loadYearsAndAreas(sourceKey: string): Promise<void> {
    try {
      const resp = (await GetYearsAndAreas(sourceKey)) as any
      years.value = Array.isArray(resp?.years) ? resp.years : []
      areas.value = Array.isArray(resp?.areas) ? resp.areas : []
    } catch (e: any) {
      errorStore.fromError('加载年份/地区选项失败', e, 'videoStore.loadYearsAndAreas')
      years.value = []
      areas.value = []
    }
  }

  async function loadRecommend(sourceKey: string, excludeIds: string[], limit = 12): Promise<Video[]> {
    try {
      const list = (await GetRecommend({
        source_key: sourceKey,
        limit,
        exclude_ids: excludeIds,
      })) as any
      return Array.isArray(list) ? list : []
    } catch (e: any) {
      errorStore.fromError('加载推荐失败', e, 'videoStore.loadRecommend')
      return []
    }
  }

  return {
    videos,
    currentVideo,
    episodes,
    types,
    years,
    areas,
    total,
    page,
    loading,
    loadVideos,
    loadDetail,
    refreshDetail,
    search,
    loadTypes,
    loadYearsAndAreas,
    loadRecommend,
    deletedVodId,
    deletedSourceKey,
    notifyDeletion,
    clearDeletionNotify,
    refreshTrigger,
    notifyRefresh,
  }
})
