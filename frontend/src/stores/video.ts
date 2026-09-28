import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { GetTypes, GetYearsAndAreas } from '../api/app'
import * as AppMod from '../api/app'
import type { Video, Episode, PlayLine, VType, VideoDetailResponse } from '../types'
import { normalizePlayLines } from '../utils/playLines'
import { useErrorStore } from './error'
import { tr } from '../locales'
import { useGeneration } from '../composables/useGeneration'
import { readDetailCache, writeDetailCache } from './detailCache'

/**
 * 删除通知。带序号而不是「一个 vodId + 清空」：列表页不止一个（首页与搜索页都被
 * KeepAlive 常驻），先把 vodId 置空的那个 watcher 会让后面的页面永远收不到这次删除。
 */
export interface DeletionNotice {
  seq: number
  sourceKey: string
  vodId: string
}

/**
 * 跨视图共享的视频状态：详情页/播放页共用的当前影片，以及按源缓存的筛选项。
 *
 * 列表状态（videos/total/cursor/loading）不在这里 —— 它属于发起它的那个视图，
 * 见 composables/useVideoList.ts。
 */
export const useVideoStore = defineStore('video', () => {
  const currentVideo = ref<Video | null>(null)
  const lines = ref<PlayLine[]>([])
  const activeLineIndex = ref(0)
  const detailLoading = ref(false)
  const types = ref<VType[]>([])
  const years = ref<string[]>([])
  const areas = ref<string[]>([])

  /**
   * 界面看到的「选集列表」始终是当前那条线路的集表。
   *
   * maccms 用 $$$ 并列多条线路，旧实现只取了第一条就丢掉其余，慢的 CDN 只能硬卡。
   * 后端已经把 vod_play_url 拆成 lines，这里按 activeLineIndex 派生，切线路不需要重新请求详情。
   */
  const episodes = computed<Episode[]>(() => lines.value[activeLineIndex.value]?.episodes ?? [])

  const refreshTrigger = ref(0)
  const lastDeletion = ref<DeletionNotice | null>(null)

  // 筛选项各自计数：首页并行加载类型与年地区，共用序号会让先发起的那个误判自己过期。
  const detailGen = useGeneration()
  const typesGen = useGeneration()
  const areasGen = useGeneration()
  let deletionSeq = 0
  let typesCachedFor = ''
  let areasCachedFor = ''
  let detailKey = ''

  const errorStore = useErrorStore()

  function notifyDeletion(sourceKey: string, vodId: string): void {
    deletionSeq += 1
    lastDeletion.value = { seq: deletionSeq, sourceKey, vodId }
  }

  /** 数据被清空或重新采集后广播刷新，同时作废按源缓存的筛选项。 */
  function notifyRefresh(): void {
    dropFilterMeta()
    refreshTrigger.value++
  }

  /** 由统一缓存失效层调用：源改过或清过之后，类型与年地区必须重新取。 */
  function dropFilterMeta(sourceKey = ''): void {
    if (!sourceKey || typesCachedFor === sourceKey) typesCachedFor = ''
    if (!sourceKey || areasCachedFor === sourceKey) areasCachedFor = ''
  }

  /** 详情身份：与详情缓存键同构，用于判断「还是不是同一部影片」。 */
  function detailIdentity(sourceKey: string, vodId: string, globalId: number): string {
    return `${sourceKey}:${vodId || `global:${globalId}`}`
  }

  /**
   * 载入详情并决定线路指针。
   *
   * 从详情页跳播放页会再取一次同一部影片的详情，重新归零就把用户刚选的线路丢了；
   * 换影片才回到首条 —— 上一部选的第 3 条在这部里可能不存在，也可能正好是最慢的。
   */
  function applyDetail(resp: VideoDetailResponse | null | undefined, key = ''): void {
    const next = normalizePlayLines(resp || {})
    lines.value = next
    const keep = key !== '' && key === detailKey ? activeLineIndex.value : 0
    activeLineIndex.value = Math.min(Math.max(keep, 0), Math.max(next.length - 1, 0))
    detailKey = key
  }

  /** 切换播放线路（按 lines 下标，不是后端给的线路序号）。 */
  function setActiveLine(index: number): void {
    if (index < 0 || index >= lines.value.length || index === activeLineIndex.value) return
    activeLineIndex.value = index
  }

  // Returns true only when a healthy browser-cache entry was used. A remote
  // result, including the backend's catalog fallback on an error, never
  // masquerades as a cache hit.
  async function loadDetail(sourceKey: string, vodId: string, refresh = false, globalId = 0): Promise<boolean> {
    const my = detailGen.begin()
    const key = detailIdentity(sourceKey, vodId, globalId)
    detailLoading.value = true
    try {
      if (!refresh) {
        const cached = await readDetailCache(sourceKey, vodId, globalId)
        if (!detailGen.isCurrent(my)) return false
        if (cached?.video) {
          currentVideo.value = cached.video
          applyDetail(cached, key)
          return true
        }
      }
      const resp = (await (AppMod as any).GetVideoDetail({ source_key: sourceKey, vod_id: vodId, global_id: globalId, refresh })) as VideoDetailResponse
      if (!detailGen.isCurrent(my)) return false
      currentVideo.value = resp?.video ?? null
      applyDetail(resp, key)
      if (resp?.video && !resp.error) {
        await writeDetailCache(sourceKey, vodId, resp, globalId)
      }
      if (resp?.error) errorStore.fromError(tr('errors.detailTempUnavailable'), new Error(resp.error.message), 'videoStore.loadDetail')
      return false
    } catch (e: any) {
      if (!detailGen.isCurrent(my)) return false
      const msg = e?.message || ''
      currentVideo.value = null
      applyDetail(null, key)
      if (!msg.includes('video not found') && !msg.includes('sql: no rows')) {
        errorStore.fromError(tr('errors.loadDetailFailed'), e, 'videoStore.loadDetail')
      }
      return false
    } finally {
      if (detailGen.isCurrent(my)) detailLoading.value = false
    }
  }

  /** 后台刷新详情（不设置 loading，用于已有本地数据后异步更新） */
  async function refreshDetail(sourceKey: string, vodId: string, globalId = 0): Promise<boolean> {
    const my = detailGen.begin()
    try {
      const resp = (await (AppMod as any).GetVideoDetail({ source_key: sourceKey, vod_id: vodId, global_id: globalId, refresh: true })) as VideoDetailResponse
      if (!detailGen.isCurrent(my)) return false
      if (resp?.video) {
        currentVideo.value = resp.video
        // 后台刷新只在真拿到了集表时才覆盖用户当前看到的线路，否则空响应会把界面清空。
        if (normalizePlayLines(resp).length > 0) applyDetail(resp, detailIdentity(sourceKey, vodId, globalId))
        if (!resp.error) {
          await writeDetailCache(sourceKey, vodId, resp, globalId)
          return true
        }
      }
      return false
    } catch {
      return false
    }
  }

  async function loadTypes(sourceKey: string): Promise<void> {
    if (!sourceKey || typesCachedFor === sourceKey) return
    const my = typesGen.begin()
    try {
      const raw = await GetTypes({ source_key: sourceKey })
      if (!typesGen.isCurrent(my)) return
      let arr: any[] = []
      if (Array.isArray(raw)) arr = raw
      else if (raw && typeof raw === 'object') {
        if (Array.isArray((raw as any).list)) arr = (raw as any).list
        else if ((raw as any).type_id && (raw as any).name) arr = [raw]
      }
      types.value = arr.map((t: any) => ({ type_id: t?.type_id ?? '', name: t?.name ?? '' }))
      typesCachedFor = sourceKey
    } catch (e: any) {
      if (!typesGen.isCurrent(my)) return
      errorStore.fromError(tr('errors.loadTypesFailed'), e, 'videoStore.loadTypes')
      types.value = []
    }
  }

  async function loadYearsAndAreas(sourceKey: string): Promise<void> {
    if (!sourceKey || areasCachedFor === sourceKey) return
    const my = areasGen.begin()
    try {
      const resp = (await GetYearsAndAreas(sourceKey)) as any
      if (!areasGen.isCurrent(my)) return
      years.value = Array.isArray(resp?.years) ? resp.years : []
      areas.value = Array.isArray(resp?.areas) ? resp.areas : []
      areasCachedFor = sourceKey
    } catch (e: any) {
      if (!areasGen.isCurrent(my)) return
      errorStore.fromError(tr('errors.loadYearsAreasFailed'), e, 'videoStore.loadYearsAndAreas')
      years.value = []
      areas.value = []
    }
  }

  return {
    currentVideo,
    lines,
    activeLineIndex,
    episodes,
    detailLoading,
    types,
    years,
    areas,
    lastDeletion,
    refreshTrigger,
    setActiveLine,
    loadDetail,
    refreshDetail,
    loadTypes,
    loadYearsAndAreas,
    dropFilterMeta,
    notifyDeletion,
    notifyRefresh,
  }
})
