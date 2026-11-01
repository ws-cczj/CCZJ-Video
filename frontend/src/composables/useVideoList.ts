import { computed, onActivated, onDeactivated, onScopeDispose, reactive, ref, watch } from 'vue'
import { SearchVideos } from '../api/app'
import { GetVideoPage, GetShortDramaPage, SHORT_DRAMA_FRESH_MS } from '../api/video'
import { normalizeApiError } from '../api/errors'
import type { Video } from '../types'
import { useErrorStore } from '../stores/error'
import { useVideoStore } from '../stores/video'
import { tr } from '../locales'
import { useGeneration } from './useGeneration'
import { createRequestScope } from '../api/requestScope'
import { recordDiagnosticEvent } from '../diagnostics/recording'

export interface VideoFilter {
  type_id: string | number
  year: string
  area: string
  keyword: string
  recent_days?: number
  sort?: string // '' = 默认; 'rating' = 按评分; 'hot' = 按热度
  short_drama?: boolean
}

export interface VideoListOptions {
  remoteShortDrama?: boolean
  /** A retained child scene can suspend reads without discarding its page. */
  enabled?: () => boolean
  /**
   * 列表之外的派生结构（首页的轮播、推荐分组）在同一视频被删除时的清理钩子。
   * 每个新的删除通知都会调用一次，跨源也会，所以视图要先比对 sourceKey 再动手。
   * 列表自身的移除由本 composable 负责，视图不需要再接删除通知。
   */
  onDeleted?: (vodId: string, sourceKey: string) => void
}

/**
 * 一个视图自己的视频列表状态。
 *
 * 原先这些状态放在 videoStore 里全局共享，而首页与搜索页都被 KeepAlive 常驻：任一页
 * 发起的列表请求都会改写同一个数组，于是搜索结果成了首页网格的内容，翻页游标也会串到
 * 另一个页面上。
 *
 * 返回 reactive 对象而不是裸 ref，视图里 `list.videos` 在脚本和模板中写法一致；
 * 解构会丢掉这份解包，必须整体持有。
 */
export function useVideoList(options: VideoListOptions = {}) {
  const videos = ref<Video[]>([])
  const localVideos = ref<Video[]>([])
  const total = ref(0)
  const page = ref(1)
  const nextCursor = ref('')
  const loading = ref(false)
  const error = ref('')
  const refreshing = ref(false), refreshFailed = ref(false)
  let refreshedAt = 0
  const interrupted = ref(false)
  let active = true
  const sourceKey = ref('')
  const remoteSource = ref(''), remoteCategory = ref(''), remoteName = ref('')
  const remoteEmpty = ref(false)

  let seenDeletionSeq = 0
  const gen = useGeneration()
  const requests = createRequestScope()
  let loadDeadline: ReturnType<typeof setTimeout> | undefined
  function cancelRequests(): void { clearTimeout(loadDeadline); gen.invalidate(); requests.cancel(); loading.value = false; refreshing.value = false }
  onActivated(() => { active = true })
  onDeactivated(() => { active = false; interrupted.value = loading.value || refreshing.value; cancelRequests() })
  onScopeDispose(cancelRequests)
  watch(() => options.enabled?.() ?? true, enabled => {
    if (!enabled) { interrupted.value = loading.value || refreshing.value; cancelRequests() }
  }, {flush:'sync'})

  const errorStore = useErrorStore()
  const videoStore = useVideoStore()

  function settle(my: number): boolean {
    const stale = !gen.isCurrent(my)
    if (!stale) {loading.value = false;refreshing.value=false}
    return stale
  }

  function applyPage(list: Video[], ttl: number, p: number): void {
    if (p === 1) videos.value = list
    else videos.value.push(...list)
    total.value = ttl
    page.value = p
  }

  async function load(key: string, filter: VideoFilter, p = 1, pageSize = 50): Promise<void> {
    if (!key || !active || options.enabled?.() === false) return
    if (p > 1 && (!nextCursor.value || key !== sourceKey.value)) return
    const retainedPaging=!!options.remoteShortDrama&&p===1&&sourceKey.value===key&&videos.value.length>0&&page.value>1
    if (p === 1&&!retainedPaging) nextCursor.value = ''
    clearTimeout(loadDeadline)
    requests.cancel()
    interrupted.value = false
    const my = gen.begin()
    loading.value = true
    error.value = ''
    // Remote discovery has a 25s backend budget; also bound IPC completion.
    const deadline = options.remoteShortDrama ? setTimeout(() => {
      if (!gen.isCurrent(my)) return
      cancelRequests()
      if(videos.value.length){refreshFailed.value=true;void recordDiagnosticEvent('short_drama_catalog_failed',{code:'TIMEOUT'})}
      else error.value = tr('shortDrama.timeout')
    }, 30000) : undefined
    loadDeadline = deadline
    if (sourceKey.value !== key) clearRemote()
    sourceKey.value = key
    try {
      const req = {
        source_key: key,
        type_id: filter.type_id === undefined || filter.type_id === null ? '' : String(filter.type_id),
        year: filter.year ?? '',
        area: filter.area ?? '',
        keyword: filter.keyword ?? '',
        sort: filter.sort ?? '',
        recent_days: filter.recent_days ?? 0,
        short_drama: filter.short_drama ?? false,
        cursor: p > 1 ? nextCursor.value : '',
        page: p,
        page_size: pageSize,
      } as any
      const remoteEnabled = options.remoteShortDrama && filter.short_drama && (p===1||nextCursor.value.startsWith('remote:'))
      let resp = remoteEnabled
        ? { videos: [] as Video[], total: 0, next_cursor: '' }
        : await requests.run(() => GetVideoPage(req))
      if (!gen.isCurrent(my)) return
      if (remoteEnabled) {
        if(p===1)remoteEmpty.value=false
        // Show the selected source's persisted catalog while refreshing its
        // provider page. Remote pagination still owns the committed cursor.
        if (p === 1) {
          try {
            const cached = await requests.run(() => GetVideoPage(req))
            if (!gen.isCurrent(my)) return
            if (cached.videos.length&&!videos.value.length){applyPage(cached.videos,cached.total,p);nextCursor.value=cached.next_cursor}
          } catch (cause) {
            if (!gen.isCurrent(my)) return
            const failure = normalizeApiError(cause)
            if (failure.code === 'CANCELLED') throw cause
            void recordDiagnosticEvent('short_drama_catalog_failed', { code: failure.code })
          }
        }
        if(videos.value.length){loading.value=false;refreshing.value=true}
        const remote = await requests.run(() => GetShortDramaPage(key, p, filter.keyword, p===1?'':remoteSource.value, p===1?'':remoteCategory.value, filter.recent_days))
        if (!gen.isCurrent(my)) return
        if(!retainedPaging){remoteSource.value = remote.source_key;remoteCategory.value = remote.category_id}
        refreshedAt=performance.now();refreshFailed.value=false
        remoteName.value = remote.source_name
        remoteEmpty.value = p===1 && !filter.keyword && !filter.recent_days && !!remote.category_id && !remote.has_more && remote.total===0 && !remote.videos.length
        if(remoteEmpty.value)void recordDiagnosticEvent('short_drama_provider_empty',{count:0})
        const merged=new Map(videos.value.map(item=>[String(item.vod_id),item]))
        if(p===1){for(const item of remote.videos)merged.set(String(item.vod_id),item)}
        const incoming=p===1?Array.from(merged.values()):remote.videos.filter(item=>!merged.has(String(item.vod_id)))
        resp = { videos: incoming, total: Math.max(remote.total,incoming.length,videos.value.length), next_cursor: retainedPaging?nextCursor.value:remote.has_more ? 'remote:' + String(p + 1) : '' }
      }
      if (settle(my)) return
      const list: Video[] = Array.isArray(resp?.videos) ? resp.videos : []
      nextCursor.value = typeof resp?.next_cursor === 'string' ? resp.next_cursor : ''
      if(retainedPaging&&remoteEnabled){videos.value=list;total.value=resp.total}
      else applyPage(list, typeof resp?.total === 'number' ? resp.total : list.length, p)
    } catch (e: any) {
      if (settle(my)) return
      if(options.remoteShortDrama&&p>1&&normalizeApiError(e).code==='VALIDATION'){
        // A changed source/type boundary cannot reuse the previous page cursor.
        reset();await load(key,filter,1,pageSize);return
      }
      if(options.remoteShortDrama&&p===1&&videos.value.length){
        refreshFailed.value=true
        void recordDiagnosticEvent('short_drama_catalog_failed',{code:normalizeApiError(e).code})
        return
      }
      if (options.remoteShortDrama && p === 1) clearRemote()
      error.value = normalizeApiError(e).message
      errorStore.fromError(tr('errors.loadVideosFailed'), e, 'videoList.load')
    } finally { clearTimeout(deadline) }
  }

  async function search(key: string, keyword: string, p = 1, pageSize = 50): Promise<void> {
    if (!key || !active || options.enabled?.() === false) return
    requests.cancel()
    interrupted.value = false
    const my = gen.begin()
    loading.value = true
    error.value = ''
    sourceKey.value = key
    try {
      const resp = (await requests.run(() => SearchVideos({
        source_key: key,
        keyword,
        page: p,
        page_size: pageSize,
      }))) as any
      if (settle(my)) return
      const list: Video[] = Array.isArray(resp?.videos) ? resp.videos : []
      if (p === 1) localVideos.value = Array.isArray(resp?.local_videos) ? resp.local_videos : []
      applyPage(list, typeof resp?.total === 'number' ? resp.total : list.length, p)
      // 翻页后新到达的远端项可能与首屏的本地补搜重复，统一按 vod_id 去重
      const remoteIds = new Set(videos.value.map(v => String(v.vod_id ?? '')))
      localVideos.value = localVideos.value.filter(v => !remoteIds.has(String(v.vod_id ?? '')))
    } catch (e: any) {
      if (settle(my)) return
      error.value = normalizeApiError(e).message
      errorStore.fromError(tr('errors.searchFailed'), e, 'videoList.search')
    }
  }

  function remove(vodId: string): void {
    const idx = videos.value.findIndex(v => String(v.vod_id) === vodId)
    if (idx >= 0) {
      videos.value.splice(idx, 1)
      total.value = Math.max(0, total.value - 1)
    }
    const li = localVideos.value.findIndex(v => String(v.vod_id) === vodId)
    if (li >= 0) localVideos.value.splice(li, 1)
  }

  function clearRemote(): void {
    remoteSource.value = ''; remoteCategory.value = ''; remoteName.value = ''
    remoteEmpty.value = false;refreshedAt=0;refreshFailed.value=false
  }

  function reset(keepRemote = false): void {
    cancelRequests()
    if (!keepRemote) clearRemote()
    interrupted.value = false
    videos.value = []
    localVideos.value = []
    total.value = 0
    page.value = 1
    nextCursor.value = ''
    loading.value = false
    error.value = ''
  }

  const stopWatchingDeletion = watch(() => videoStore.lastDeletion, (notice) => {
    if (!notice || notice.seq === seenDeletionSeq) return
    seenDeletionSeq = notice.seq
    // 列表只在确实装着该源数据时摘除；派生结构可能来自没走过 load/search 的入口
    // （只用过「源站搜索」的搜索页里 sourceKey 仍是空），所以通知照常发出，
    // 由视图按自己当前的源判断。
    if (notice.sourceKey === (remoteSource.value || sourceKey.value)) remove(notice.vodId)
    options.onDeleted?.(notice.vodId, notice.sourceKey)
  })
  onScopeDispose(stopWatchingDeletion)

  return reactive({
    videos,
    localVideos,
    total,
    page,
    nextCursor,
    loading,
    refreshing,refreshFailed,needsRefresh:()=>!refreshedAt||performance.now()-refreshedAt>SHORT_DRAMA_FRESH_MS,
    error,
    interrupted,
    sourceKey,
    remoteSource,
    remoteName,
    remoteEmpty,
    hasNext: computed(() => videos.value.length < total.value),
    load,
    search,
    remove,
    reset,
  })
}
