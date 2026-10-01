<script setup lang="ts">
defineOptions({ name: 'Search' })
import { ref, onMounted, onUnmounted, onActivated, onDeactivated, computed, watch, nextTick } from 'vue'
import { storeToRefs } from 'pinia'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { GetRecentHistory, GetVideoList, SearchSource, ImportSourceVideos, normalizeApiError } from '../api/app'
import { onBackendEvent } from '../api/events'
import { useSourceStore } from '../stores/source'
import { useVideoStore } from '../stores/video'
import { useLayoutStore } from '../stores/layout'
import { useVideoList } from '../composables/useVideoList'
import { usePosterCacheStore } from '../stores/posterCache'
import VideoCard from '../components/VideoCard.vue'
import RemoteImage from '../components/RemoteImage.vue'
import Icon from '../components/Icon.vue'
import { Button, Tag, Select as SelectDropdown, Spinner as LoadingSpinner, Empty as EmptyState } from '../components/ui'
import { getDetailPath } from '../utils'
import { readStorage, readStorageBoolean, removeStorage, writeStorage } from '../platform/storage'
import type { Video, HistoryItem } from '../types'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const sourceStore = useSourceStore()
const videoStore = useVideoStore()
const posterCache = usePosterCacheStore()
// 本页自己的列表状态：与首页各自独立，互不覆盖结果与翻页游标。
const list = useVideoList({ onDeleted })

const sourceOptions = computed(() =>
  sourceStore.sources
    .filter((s) => s.source_key && s.source_key.length > 0)
    .map((s) => ({ value: String(s.source_key), label: s.name })),
)

const currentSearchSource = ref('')
const keyword = ref('')
const searchHistory = ref<string[]>(readStorage<string[]>('search_history', []))
const sourceSearchMode = ref(readStorageBoolean('source_search_mode'))
const hasKeyword = computed(() => keyword.value.trim().length > 0)
let searchPageWasDeactivated = false
// Mount assigns currentSearchSource from the stored/URL source; the watcher below
// is for user-driven switches, so that initial assignment must not fire it too.
let suppressSourceWatch = false

// videoStore.refreshTrigger 快照，用于判断返回本页时是否需要重搜
let lastSearchRefreshTrigger = 0

function clearSearchResults(): void {
  list.reset()
}

// 是否进行过搜索
const hasSearched = ref(false)

// 推荐相关
const recommendLoading = ref(false)

// 推荐项：区分历史来源和视频来源，以便用不同方式解析名称/封面
interface RecommendItem {
  vod_id: string
  vod_name?: string
  vod_pic?: string
  source_key: string
  isHistory?: boolean
  // 对应的 history 项（如果来自历史记录）
  history?: HistoryItem
  // 对应的 video 对象（如果来自 GetVideoList）
  video?: Video
}

interface RecommendGroup {
  typeName: string
  items: RecommendItem[]
}

const recommendGroups = ref<RecommendGroup[]>([])
const loadingRecommend = ref<Set<string>>(new Set())

// 列数与密度走 layout store：这一页在 KeepAlive 下不会重挂，跟着同一份状态
// 才能在设置页改完立刻看见。
const layoutStore = useLayoutStore()
const { gridStyle } = storeToRefs(layoutStore)

onMounted(async () => {
  suppressSourceWatch = true
  await layoutStore.load()
  await sourceStore.loadSources()

  currentSearchSource.value = sourceStore.currentSourceKey

  // 从 URL 查询参数读取来源与关键词（用于标签跳转搜索）
  const urlSource = route.query.source
  if (urlSource && typeof urlSource === 'string' && urlSource) {
    const exists = sourceStore.sources.find(s => String(s.source_key) === urlSource)
    if (exists) currentSearchSource.value = urlSource
  }

  const urlKw = route.query.keyword
  if (urlKw && typeof urlKw === 'string' && urlKw.trim()) {
    keyword.value = urlKw.trim()
    await nextTick()
    if (currentSearchSource.value) {
      doSearch()
    }
  } else if (currentSearchSource.value) {
    await loadRecommendations()
  }
  // Release after the pre-flush watcher jobs from the assignments above have run.
  await nextTick()
  suppressSourceWatch = false
})

watch(
  () => currentSearchSource.value,
  async (key) => {
    if (suppressSourceWatch) return
    clearSearchResults()
    hasSearched.value = false
    if (key) {
      await loadRecommendations()
    }
  }
)

// The page is kept alive across navigation. Only re-run the search when the
// catalog's visibility rules actually changed (toggled in the type-management
// view) or data was refreshed — otherwise keep the cached result list so
// returning to this tab does not re-hit the remote API and drop scroll state.
onActivated(async () => {
  if (!searchPageWasDeactivated) return
  searchPageWasDeactivated = false
  if (!currentSearchSource.value) return
  if (keyword.value.trim() && hasSearched.value) {
    if (videoStore.refreshTrigger !== lastSearchRefreshTrigger) doSearch()
  } else {
    await loadRecommendations()
  }
})

onDeactivated(() => {
  searchPageWasDeactivated = true
})

watch(keyword, (value) => {
  if (value.trim()) return
  clearSearchResults()
  hasSearched.value = false
  clearSourceResults()
  searchProgress.value = { stage: '', message: '', current: 0, total: 0 }
})

// 删除通知：列表本身（videos / localVideos / total）由 useVideoList 按自己的
// sourceKey 移除，这里只清理本页派生出来的结构。通知跨源也会到，先按当前源过滤。
function onDeleted(vodId: string, sourceKey: string): void {
  if (sourceKey !== currentSearchSource.value) return
  // 源站搜索结果
  const si = sourceSearchResults.value.findIndex(v => String(v.vod_id) === vodId)
  if (si >= 0) {
    sourceSearchResults.value.splice(si, 1)
    sourceSearchTotal.value = Math.max(0, sourceSearchTotal.value - 1)
  }
  // 推荐分组
  for (const g of recommendGroups.value) {
    const gi = g.items.findIndex(i => String(i.vod_id) === vodId)
    if (gi >= 0) g.items.splice(gi, 1)
  }
}

async function loadRecommendations(): Promise<void> {
  if (!currentSearchSource.value) return
  recommendLoading.value = true
  recommendGroups.value = []

  try {
    const history = (await GetRecentHistory(30)) as HistoryItem[] | null | undefined
    const sameSourceHistory = Array.isArray(history)
      ? history.filter(h => h.source_key === currentSearchSource.value)
      : []

    // 尝试获取最新视频（用于"猜你喜欢"）
    let freshVideos: Video[] = []
    try {
      const resp = (await GetVideoList({
        source_key: currentSearchSource.value,
        type_id: '0',
        year: '',
        area: '',
        keyword: '',
        page: 1,
        page_size: 12,
        sort: '',
        recent_days: 0,
        cursor: '',
      })) as { videos: Video[]; total: number }
      freshVideos = Array.isArray(resp?.videos) ? resp.videos : []
    } catch {
      // 忽略
    }

    // 构建分组
    const groups: RecommendGroup[] = []

    if (sameSourceHistory.length > 0) {
      // 按整部去重，只保留最近观看的那一集
      const seenVod = new Set<string>()
      const dedupedHistory: HistoryItem[] = []
      for (const h of sameSourceHistory) {
        const id = String(h.vod_id ?? '')
        if (!id || seenVod.has(id)) continue
        seenVod.add(id)
        dedupedHistory.push(h)
        if (dedupedHistory.length >= 6) break
      }
      // 把历史记录转换为推荐项（优先用缓存补齐名称/封面）
      const historyItems: RecommendItem[] = dedupedHistory
        .map((h, i) => {
          const cached = posterCache.get(h.source_key, h.vod_id)
          return {
            vod_id: h.vod_id,
            vod_name: h.vod_name || cached?.vod_name,
            vod_pic: cached?.vod_pic,
            source_key: h.source_key,
            isHistory: true,
            history: h,
          }
        })
      groups.push({ typeName: t('home.continueWatching'), items: historyItems })

      // 过滤掉已看过的视频作为"猜你喜欢"
      const seenIds = new Set(sameSourceHistory.map(h => h.vod_id))
      const unknownVideos = freshVideos.filter(v => !seenIds.has(String(v.vod_id)))
      if (unknownVideos.length > 0) {
        groups.push({
          typeName: t('home.recommended'),
          items: unknownVideos.slice(0, 6).map(v => ({
            vod_id: String(v.vod_id || ''),
            vod_name: v.vod_name,
            vod_pic: v.vod_pic,
            source_key: currentSearchSource.value!,
            video: v,
          })),
        })
      }
    } else {
      // 没有历史记录：只显示最新上线
      groups.push({
        typeName: t('home.latest'),
        items: freshVideos.slice(0, 12).map(v => ({
          vod_id: String(v.vod_id || ''),
          vod_name: v.vod_name,
          vod_pic: v.vod_pic,
          source_key: currentSearchSource.value!,
          video: v,
        })),
      })
    }

    recommendGroups.value = groups

    // 异步补充历史项的名称/封面（如果缺失）
    hydrateHistoryRecommendations()
  } catch {
    // 忽略
  } finally {
    recommendLoading.value = false
  }
}

/**
 * 异步补充历史推荐项缺失的名称/封面（通过 GetVideoDetail + 缓存）
 */
async function hydrateHistoryRecommendations(): Promise<void> {
  const toLoad: RecommendItem[] = []
  for (const g of recommendGroups.value) {
    for (const item of g.items) {
      if (!item.isHistory) continue
      if (item.vod_name && item.vod_pic) continue
      const key = `${item.source_key}:${item.vod_id}`
      if (loadingRecommend.value.has(key)) continue
      toLoad.push(item)
    }
  }
  if (toLoad.length === 0) return

  // ⭐ 并行加载所有缺失项，加快补充速度
  await Promise.all(toLoad.map(async (item) => {
    const key = `${item.source_key}:${item.vod_id}`
    loadingRecommend.value.add(key)
    try {
      const entry = await posterCache.ensureLoaded(item.source_key, item.vod_id)
      if (entry) {
        if (entry.vod_name) item.vod_name = entry.vod_name
        if (entry.vod_pic) item.vod_pic = entry.vod_pic
      }
    } catch {
      // 忽略
    } finally {
      loadingRecommend.value.delete(key)
    }
  }))
}

function resolveName(item: RecommendItem): string {
  if (item.vod_name) return item.vod_name
  if (item.video?.vod_name) return item.video.vod_name
  if (item.isHistory && item.history?.vod_name) return item.history.vod_name
  const cached = posterCache.get(item.source_key, item.vod_id)
  return cached?.vod_name || t('search.video')
}

function resolvePic(item: RecommendItem): string {
  if (item.vod_pic) return item.vod_pic
  if (item.video?.vod_pic) return item.video.vod_pic
  const cached = posterCache.get(item.source_key, item.vod_id)
  return cached?.vod_pic || ''
}

function isPicLoading(item: RecommendItem): boolean {
  const key = `${item.source_key}:${item.vod_id}`
  return loadingRecommend.value.has(key)
}

const PAGE_SIZE = 50

const searchCurrentPage = ref(1)
const searchTotalPages = computed(() =>
  list.total > 0 ? Math.ceil(list.total / PAGE_SIZE) : 1
)
const searchPageRange = computed(() => {
  const total = searchTotalPages.value
  const cur = searchCurrentPage.value
  const pages: number[] = []
  const delta = 2
  const start = Math.max(1, cur - delta)
  const end = Math.min(total, cur + delta)
  // -1/-2 keep the two ellipses distinguishable: both pushed into one list would
  // collide as v-for keys whenever the range has a leading and a trailing gap.
  if (start > 1) { pages.push(1); if (start > 2) pages.push(-1) }
  for (let i = start; i <= end; i++) pages.push(i)
  if (end < total) { if (end < total - 1) pages.push(-2); pages.push(total) }
  return pages
})

function goSearchPage(p: number): void {
  if (p < 1 || p > searchTotalPages.value || p === searchCurrentPage.value) return
  searchCurrentPage.value = p
  list.search(currentSearchSource.value!, keyword.value.trim(), p)
}

function toggleSourceSearchMode(): void {
  sourceSearchMode.value = !sourceSearchMode.value
  writeStorage('source_search_mode', sourceSearchMode.value)
  if (sourceSearchMode.value) {
    clearSearchResults()
  } else {
    clearSourceResults()
  }
}

function doSearch(): void {
  const kw = keyword.value.trim()
  if (!kw || !currentSearchSource.value) {
    hasSearched.value = false
    return
  }
  if (!searchHistory.value.includes(kw)) {
    searchHistory.value.unshift(kw)
    if (searchHistory.value.length > 10) searchHistory.value.pop()
    writeStorage('search_history', searchHistory.value)
  }
  hasSearched.value = true
  lastSearchRefreshTrigger = videoStore.refreshTrigger
  searchCurrentPage.value = 1
  clearSourceResults()
  if (sourceSearchMode.value) {
    clearSearchResults()
    doSourceSearch(1)
  } else {
    list.reset()
    list.search(currentSearchSource.value, kw)
  }
}

function goDetail(item: RecommendItem): void {
  posterCache.recordClick(item.source_key, item.vod_id)
  router.push(getDetailPath(item.source_key, { vod_id: item.vod_id }))
}

/** 历史标签点击/回车/空格共用：模板里重复三段内联赋值不利于键盘与鼠标行为一致 */
function applyHistoryKeyword(h: string): void {
  keyword.value = h
  doSearch()
}

function goDetailVideo(v: Video): void {
  router.push(getDetailPath(currentSearchSource.value, v))
}

function loadMore(): void {
  if (
    currentSearchSource.value &&
    list.videos.length < list.total
  ) {
    list.search(
      currentSearchSource.value,
      keyword.value.trim(),
      list.page + 1
    )
  }
}

function clearHistory(): void {
  searchHistory.value = []
  removeStorage('search_history')
}

function removeHistoryItem(kw: string): void {
  const idx = searchHistory.value.indexOf(kw)
  if (idx < 0) return
  searchHistory.value.splice(idx, 1)
  if (searchHistory.value.length > 0) {
    writeStorage('search_history', searchHistory.value)
  } else {
    removeStorage('search_history')
  }
}

// ========= 源站搜索（当站内无结果时，直接调用源站 API） =========
// 结果不入库，仅展示；用户可勾选视频后一键入库
const sourceSearching = ref(false)
const sourceSearchResults = ref<Video[]>([])
const sourceSearchTotal = ref(0)
const sourceSearchPage = ref(1)
const sourceSearchPageCount = ref(1)
const sourceSearchPageSize = ref(50)
const hasSourceSearched = ref(false)

// 用户挑选的源站视频（vod_id 集合）
const selectedSourceVodIds = ref<Set<string>>(new Set())
// 已入库的 vod_id 集合（用于在卡片上标记"已入库"状态）
const importedSourceVodIds = ref<Set<string>>(new Set())

// 入库中状态 & 消息
const importing = ref(false)
const importMessage = ref<{ type: 'success' | 'error' | ''; text: string }>({ type: '', text: '' })
let importMessageTimer: ReturnType<typeof setTimeout> | null = null

// 搜索进度状态
const searchProgress = ref({ stage: '', message: '', current: 0, total: 0 })
let searchProgressListener: (() => void) | null = null
let sourceSearchGeneration = 0

onMounted(() => {
  searchProgressListener = onBackendEvent<any>('search:progress', (data) => {
    searchProgress.value = {
      stage: data.stage || '',
      message: data.message || '',
      current: data.current || 0,
      total: data.total || 0,
    }
  })
})

onUnmounted(() => {
  if (searchProgressListener) {
    searchProgressListener()
    searchProgressListener = null
  }
  if (importMessageTimer) {
    clearTimeout(importMessageTimer)
    importMessageTimer = null
  }
})

// 计算分页页码范围（用于分页控件展示）
const sourceSearchPageRange = computed(() => {
  const total = sourceSearchPageCount.value
  const cur = sourceSearchPage.value
  const pages: number[] = []
  if (total <= 1) return pages
  const delta = 2
  const start = Math.max(1, cur - delta)
  const end = Math.min(total, cur + delta)
  if (start > 1) { pages.push(1); if (start > 2) pages.push(-1) }
  for (let i = start; i <= end; i++) pages.push(i)
  if (end < total) { if (end < total - 1) pages.push(-2); pages.push(total) }
  return pages
})

// 当前页是否全选
const isAllCurrentPageSelected = computed(() => {
  if (sourceSearchResults.value.length === 0) return false
  return sourceSearchResults.value.every(v =>
    selectedSourceVodIds.value.has(String(v.vod_id ?? '')) ||
    importedSourceVodIds.value.has(String(v.vod_id ?? ''))
  )
})

async function doSourceSearch(page: number = 1): Promise<void> {
  const kw = keyword.value.trim()
  if (!kw || !currentSearchSource.value) return
  const sourceKey = currentSearchSource.value
  const generation = ++sourceSearchGeneration
  clearSearchResults()
  sourceSearching.value = true
  hasSourceSearched.value = true
  // 切换到新搜索或新页时清空当前结果
  sourceSearchResults.value = []
  selectedSourceVodIds.value = new Set()
  sourceSearchPage.value = page
  importMessage.value = { type: '', text: '' }
  searchProgress.value = { stage: '', message: '', current: 0, total: 0 }
  try {
    const resp = (await SearchSource(sourceKey, kw, page, sourceSearchPageSize.value)) as any
    if (generation !== sourceSearchGeneration || keyword.value.trim() !== kw || currentSearchSource.value !== sourceKey) return
    // 仅更新分页元数据
    sourceSearchTotal.value = resp?.total || sourceSearchResults.value.length
    const pc = resp?.page_count || 0
    if (pc > 0) {
      sourceSearchPageCount.value = pc
    } else if (sourceSearchTotal.value > 0) {
      sourceSearchPageCount.value = Math.ceil(sourceSearchTotal.value / sourceSearchPageSize.value)
    } else {
      sourceSearchPageCount.value = 1
    }
    if (Array.isArray(resp?.videos)) {
      const remoteVideos = resp.videos as Video[]
      sourceSearchResults.value = remoteVideos
      // 后端已按本地目录标注 in_catalog，回填后跨会话也能显示"已入库"
      markImported(remoteVideos.filter(v => v.in_catalog).map(v => String(v.vod_id ?? '')).filter(Boolean))
    }
  } catch (e) {
    if (generation === sourceSearchGeneration) console.warn(t('search.sourceSearchFailed'), e)
  } finally {
    if (generation === sourceSearchGeneration) {
      sourceSearching.value = false
      searchProgress.value = { stage: '', message: '', current: 0, total: 0 }
    }
  }
}

function goSourceSearchPage(p: number): void {
  if (p < 1 || p > sourceSearchPageCount.value || p === sourceSearchPage.value) return
  doSourceSearch(p)
}

function toggleSelectVideo(vodId: string | number | undefined): void {
  const id = String(vodId ?? '')
  if (!id) return
  const newSet = new Set(selectedSourceVodIds.value)
  if (newSet.has(id)) newSet.delete(id)
  else newSet.add(id)
  selectedSourceVodIds.value = newSet
}

function isVideoSelected(vodId: string | number | undefined): boolean {
  return selectedSourceVodIds.value.has(String(vodId ?? ''))
}

function isVideoImported(vodId: string | number | undefined): boolean {
  return importedSourceVodIds.value.has(String(vodId ?? ''))
}

// 标记一批 vod_id 为已入库：既服务于后端 in_catalog 回填，也服务于本地导入成功
function markImported(ids: string[]): void {
  if (ids.length === 0) return
  const next = new Set(importedSourceVodIds.value)
  for (const id of ids) next.add(id)
  importedSourceVodIds.value = next
}

function toggleSelectAllCurrentPage(): void {
  if (isAllCurrentPageSelected.value) {
    // 取消全选当前页（不影响已入库的）
    const newSet = new Set(selectedSourceVodIds.value)
    for (const v of sourceSearchResults.value) {
      newSet.delete(String(v.vod_id ?? ''))
    }
    selectedSourceVodIds.value = newSet
  } else {
    // 全选当前页（跳过已入库的）
    const newSet = new Set(selectedSourceVodIds.value)
    for (const v of sourceSearchResults.value) {
      const id = String(v.vod_id ?? '')
      if (id && !importedSourceVodIds.value.has(id)) {
        newSet.add(id)
      }
    }
    selectedSourceVodIds.value = newSet
  }
}

function clearSourceResults(): void {
  sourceSearchGeneration++
  sourceSearching.value = false
  sourceSearchResults.value = []
  sourceSearchTotal.value = 0
  sourceSearchPage.value = 1
  sourceSearchPageCount.value = 1
  hasSourceSearched.value = false
  selectedSourceVodIds.value = new Set()
  importedSourceVodIds.value = new Set()
  importMessage.value = { type: '', text: '' }
}

// 一键入库当前页所有视频
async function importCurrentPage(): Promise<void> {
  if (importing.value) return
  // 仅入库未入库的视频
  const toImport = sourceSearchResults.value.filter(v =>
    !importedSourceVodIds.value.has(String(v.vod_id ?? ''))
  )
  if (toImport.length === 0) {
    showImportMessage('info', t('search.alreadyImportedAll'))
    return
  }
  await doImport(toImport)
}

// 入库用户挑选的视频
async function importSelected(): Promise<void> {
  if (importing.value || selectedSourceVodIds.value.size === 0) return
  const selected = sourceSearchResults.value.filter(v => {
    const id = String(v.vod_id ?? '')
    return id && selectedSourceVodIds.value.has(id) && !importedSourceVodIds.value.has(id)
  })
  if (selected.length === 0) {
    showImportMessage('info', t('search.alreadyImportedAll'))
    return
  }
  await doImport(selected)
}

async function doImport(videos: Video[]): Promise<void> {
  if (!currentSearchSource.value || videos.length === 0) return
  importing.value = true
  importMessage.value = { type: '', text: '' }
  try {
    // 使用 any 绕过前端 Video 类型与绑定生成 Video 类型之间的字段差异
    const count = (await ImportSourceVideos(currentSearchSource.value, videos as any)) as number
    // 标记已入库
    markImported(videos.map(v => String(v.vod_id ?? '')).filter(Boolean))
    // 清空选中（已入库的不需要再选）
    selectedSourceVodIds.value = new Set()
    showImportMessage('success', t('search.importSuccess', { count }))
  } catch (e: any) {
    const reason = normalizeApiError(e).message
    showImportMessage('error', reason ? `${t('search.importFailed')}: ${reason}` : t('search.importFailed'))
  } finally {
    importing.value = false
  }
}

function showImportMessage(type: 'success' | 'error' | 'info', text: string): void {
  // info 类型映射到 success 颜色（仅为提示）
  const msgType = type === 'info' ? 'success' : type
  importMessage.value = { type: msgType as 'success' | 'error', text }
  if (importMessageTimer) clearTimeout(importMessageTimer)
  importMessageTimer = setTimeout(() => {
    importMessage.value = { type: '', text: '' }
    importMessageTimer = null
  }, 3500)
}

// 源站搜索结果卡片点击：
// - 已入库 → 跳转详情
// - 未入库 → 切换选中状态
function onSourceVideoClick(v: Video): void {
  const id = String(v.vod_id ?? '')
  if (!id) return
  if (importedSourceVodIds.value.has(id)) {
    router.push(getDetailPath(currentSearchSource.value, v))
  } else {
    toggleSelectVideo(id)
  }
}
</script>

<template>
  <div class="search-page">
    <!-- 搜索栏 -->
    <div class="search-bar cczj-flex cczj-gap-2 cczj-mb-4 cczj-p-2">
      <div class="source-picker cczj-relative cczj-flex cczj-items-center">
        <SelectDropdown
          v-model="currentSearchSource"
          :options="sourceOptions"
          :disabled="sourceStore.sources.length === 0"
          :placeholder="t('search.selectSource')"
        />
        <Icon name="source" :size="14" class="pick-icon" />
      </div>

      <!-- 激活态描边由 scoped `.source-search-toggle.cczj-bg-primary`（var(--accent)）提供。 -->
      <button
        class="source-search-toggle cczj-flex cczj-items-center cczj-gap-1 cczj-px-2 cczj-py-1 cczj-border cczj-border-gray-300 cczj-rounded cczj-text-sm cczj-cursor-pointer cczj-transition-colors"
        :class="{ 'cczj-bg-primary cczj-text-white': sourceSearchMode }"
        @click="toggleSourceSearchMode"
      >
        <span class="checkbox-icon">{{ sourceSearchMode ? '✓' : '○' }}</span>
        <span>{{ t('search.sourceSearch') }}</span>
      </button>

      <div class="input-wrap cczj-flex-1 cczj-relative cczj-flex cczj-items-center">
        <Icon name="search" :size="16" class="input-icon" />
        <input
          v-model="keyword"
          @keyup.enter="doSearch"
          :placeholder="t('search.inputPlaceholder')"
          class="search-input cczj-flex-1"
        />
        <Button
          v-if="keyword"
          variant="text"
          size="sm"
          icon
          class="clear-x"
          @click="keyword = ''"
          :aria-label="t('search.clear')"
        >
          <Icon name="close" :size="12" />
        </Button>
      </div>

      <Button variant="primary" @click="doSearch" class="cczj-flex cczj-items-center cczj-gap-1">
        <Icon name="search" :size="14" />
        <span>{{ t('search.search') }}</span>
      </Button>

      
    </div>

    <!-- 历史搜索 -->
    <div v-if="searchHistory.length > 0 && !hasSearched" class="history-tags cczj-flex cczj-flex-wrap cczj-gap-2 cczj-mb-4">
      <span class="history-label cczj-flex cczj-items-center cczj-gap-1">
        <Icon name="clock" :size="12" />
        {{ t('search.historySearch') }}
      </span>
      <div
        v-for="h in searchHistory"
        :key="h"
        class="tag-wrap cczj-flex cczj-items-center cczj-gap-1"
      >
        <Tag
          class="tag-btn cczj-cursor-pointer"
          role="button"
          tabindex="0"
          @click="applyHistoryKeyword(h)"
          @keydown.enter.prevent="applyHistoryKeyword(h)"
          @keydown.space.prevent="applyHistoryKeyword(h)"
        >{{ h }}</Tag>
        <Button variant="text" size="sm" icon class="tag-remove" :title="t('search.deleteHistory')" @click.stop="removeHistoryItem(h)">
          <Icon name="close" :size="10" />
        </Button>
      </div>
      <Button variant="text" size="sm" @click="clearHistory">{{ t('search.clearAll') }}</Button>
    </div>

    <!-- ============ 推荐区域（未搜索时显示） ============ -->
    <section v-if="!hasSearched && (recommendGroups.length > 0 || recommendLoading)" class="recommend-section cczj-mb-4">
      <div class="recommend-header cczj-flex cczj-items-center cczj-gap-2 cczj-mb-3">
        <h3 class="cczj-flex cczj-items-center cczj-gap-2">
          <Icon name="play" :size="14" />
          <span>{{ t('search.recommendForYou') }}</span>
        </h3>
      </div>

      <div v-if="recommendLoading" class="recommend-loading cczj-text-center cczj-py-4">
        <LoadingSpinner size="sm" :label="t('search.loadingRecommendations')" />
      </div>

      <div v-else class="recommend-groups cczj-flex cczj-flex-col cczj-gap-4">
        <div v-for="(group, groupIdx) in recommendGroups" :key="group.typeName" class="recommend-group cczj-motion-reveal" :style="{ '--cczj-motion-delay': `${groupIdx * 100}ms` }">
          <div class="group-label-row cczj-flex cczj-items-center cczj-gap-2 cczj-mb-2">
            <span class="dot"></span>
            <span>{{ group.typeName }}</span>
          </div>
          <div class="recommend-row cczj-grid cczj-gap-3">
            <div
              v-for="(item, idx) in group.items"
              :key="`rec-${group.typeName}-${item.vod_id}-${idx}`"
              class="rec-card cczj-cursor-pointer cczj-rounded cczj-motion-reveal-scale"
              role="button"
              tabindex="0"
              @click="goDetail(item)"
              @keydown.enter.prevent="goDetail(item)"
              @keydown.space.prevent="goDetail(item)"
              :style="{ '--cczj-motion-delay': `${idx * 50}ms` }"
            >
              <div class="rec-poster cczj-relative cczj-rounded cczj-overflow-hidden">
                <RemoteImage
                  v-if="resolvePic(item)"
                  :src="resolvePic(item)"
                  :alt="resolveName(item)"
                  loading="lazy"
                  class="cczj-w-full"
                />
                <div v-else-if="isPicLoading(item)" class="rec-poster-loading cczj-flex cczj-items-center cczj-justify-center">
                  <LoadingSpinner size="sm" />
                </div>
                <div v-else class="rec-poster-placeholder cczj-flex cczj-items-center cczj-justify-center">
                  <Icon name="film" :size="22" />
                </div>
                <div class="rec-overlay cczj-absolute cczj-inset-0 cczj-flex cczj-items-center cczj-justify-center">
                  <Icon name="play" :size="16" />
                </div>
              </div>
              <div class="rec-title cczj-truncate cczj-mt-1">{{ resolveName(item) }}</div>
            </div>
          </div>
        </div>
      </div>
    </section>

    <!-- 搜索结果 -->
    <div v-if="hasKeyword && hasSearched && !sourceSearchMode && !hasSourceSearched && list.loading && list.videos.length === 0" class="cczj-text-center cczj-py-8">
      <LoadingSpinner :label="t('search.searching')" />
    </div>

    <div
      v-else-if="hasKeyword && hasSearched && !sourceSearchMode && !hasSourceSearched && list.videos.length > 0"
      class="search-results cczj-mb-4"
    >
      <div class="results-header cczj-flex cczj-items-center cczj-justify-between cczj-gap-2 cczj-mb-3">
        <span class="results-label cczj-flex cczj-items-center cczj-gap-1">
          <Icon name="search" :size="12" />
          {{ t('search.searchResults') }}
        </span>
        <span class="results-count cczj-text-sm cczj-text-muted">{{ t('search.totalItems', { count: list.total }) }}</span>
      </div>
      <div class="video-grid cczj-grid" :style="gridStyle">
        <VideoCard
          v-for="v in list.videos"
          :key="`${v.vod_g_id ?? v.vod_id ?? v.id}`"
          :video="v"
          :in-catalog="v.in_catalog"
          :catalog-label="t('search.imported')"
          @click="goDetailVideo(v)"
        />
      </div>
    </div>

    <div
      v-if="hasKeyword && hasSearched && !sourceSearchMode && !hasSourceSearched && list.videos.length > 0 && searchTotalPages > 1"
      class="search-pagination cczj-flex cczj-items-center cczj-justify-center cczj-gap-2 cczj-my-4"
    >
      <button
        class="page-btn cczj-cursor-pointer cczj-rounded"
        :disabled="searchCurrentPage <= 1"
        @click="goSearchPage(searchCurrentPage - 1)"
      >
        <Icon name="back" :size="12" />
      </button>
      <template v-for="p in searchPageRange" :key="p">
        <span v-if="p < 0" class="page-ellipsis">…</span>
        <button
          v-else
          class="page-btn cczj-cursor-pointer cczj-rounded"
          :class="{ active: p === searchCurrentPage }"
          @click="goSearchPage(p)"
        >{{ p }}</button>
      </template>
      <button
        class="page-btn cczj-cursor-pointer cczj-rounded"
        :disabled="searchCurrentPage >= searchTotalPages"
        @click="goSearchPage(searchCurrentPage + 1)"
      >
        <Icon name="chevron-right" :size="12" />
      </button>
      <span class="page-info cczj-text-sm cczj-text-muted">{{ searchCurrentPage }} / {{ searchTotalPages }} {{ t('search.page') }} · {{ t('search.totalItems', { count: list.total }) }}</span>
    </div>

    <!-- 本地库补搜：源站关键词匹配不到、但本地目录标题包含关键词 -->
    <div
      v-if="hasKeyword && hasSearched && !sourceSearchMode && !hasSourceSearched && !list.loading && list.localVideos.length > 0"
      class="local-matches cczj-mb-4"
    >
      <div class="results-header cczj-flex cczj-items-center cczj-justify-between cczj-gap-2 cczj-mb-3">
        <span class="results-label cczj-flex cczj-items-center cczj-gap-1">
          <Icon name="database" :size="12" />
          {{ t('search.localMatches') }}
        </span>
        <span class="results-count cczj-text-sm cczj-text-muted">{{ t('search.totalItems', { count: list.localVideos.length }) }}</span>
      </div>
      <div class="video-grid cczj-grid" :style="gridStyle">
        <VideoCard
          v-for="v in list.localVideos"
          :key="`local-${v.vod_g_id ?? v.vod_id ?? v.id}`"
          :video="v"
          :in-catalog="true"
          :catalog-label="t('search.imported')"
          @click="goDetailVideo(v)"
        />
      </div>
    </div>

    <!-- 源站搜索进度（仅在搜索中且尚未收到任何结果时独占展示） -->
    <div v-if="hasKeyword && sourceSearching && sourceSearchResults.length === 0" class="source-search-progress-section cczj-mb-6">
      <div class="search-progress-card">
        <!-- 步骤指示器 -->
        <div class="sp-steps">
          <div class="sp-step" :class="{ active: searchProgress.stage === 'fetching_list' || !searchProgress.stage, done: searchProgress.stage === 'fetching_details' }">
            <div class="sp-step-dot">
              <svg v-if="searchProgress.stage === 'fetching_details'" class="sp-check" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3"><path d="M5 13l4 4L19 7"/></svg>
              <div v-else class="sp-pulse"></div>
            </div>
            <span class="sp-step-label">{{ t('search.fetchingList') }}</span>
          </div>
          <div class="sp-step-line" :class="{ filled: searchProgress.stage === 'fetching_details' }"></div>
          <div class="sp-step" :class="{ active: searchProgress.stage === 'fetching_details' }">
            <div class="sp-step-dot">
              <div v-if="searchProgress.stage === 'fetching_details'" class="sp-pulse"></div>
              <svg v-else class="sp-check" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3" style="opacity:0.3"><path d="M5 13l4 4L19 7"/></svg>
            </div>
            <span class="sp-step-label">{{ t('search.fetchingDetails') }}</span>
          </div>
        </div>
        <!-- 进度条 -->
        <div class="sp-bar-wrap">
          <div class="sp-bar">
            <div class="sp-bar-fill" :style="{ width: searchProgress.total > 0 ? `${(searchProgress.current / searchProgress.total) * 100}%` : (searchProgress.stage === 'fetching_list' ? '30%' : '70%') }"></div>
            <div class="sp-bar-shimmer"></div>
          </div>
          <span v-if="searchProgress.total > 0" class="sp-bar-pct">{{ searchProgress.current }}/{{ searchProgress.total }}</span>
        </div>
        <!-- 状态消息 -->
        <p class="sp-msg">{{ searchProgress.message || t('search.searchingFromSource') }}</p>
      </div>
    </div>

    <!-- 搜索中顶部紧凑进度条（已开始出现结果后，进度条移到结果区顶部） -->
    <div v-if="hasKeyword && sourceSearching && sourceSearchResults.length > 0" class="source-inline-progress cczj-mb-3">
      <div class="sip-bar">
        <div class="sip-bar-fill" :style="{ width: searchProgress.total > 0 ? `${(searchProgress.current / searchProgress.total) * 100}%` : '70%' }"></div>
      </div>
      <span class="sip-msg">{{ searchProgress.message || t('search.searchingFromSource') }}</span>
    </div>

    <div
      v-if="
        hasSearched &&
        hasKeyword &&
        !list.loading &&
        list.videos.length === 0 &&
        list.localVideos.length === 0 &&
        sourceStore.currentSourceKey &&
        !sourceSearching &&
        !hasSourceSearched
      "
    >
      <EmptyState
        icon="🔍"
        :title="t('search.noResultsTitle')"
        :description="t('search.noResultsHint')"
      >
        <div class="source-search-wrap cczj-flex cczj-flex-col cczj-gap-3 cczj-items-center cczj-mt-4">
          <Button
            variant="primary"
            @click="doSourceSearch(1)"
            class="cczj-flex cczj-items-center cczj-gap-2"
          >
            <Icon name="search" :size="12" />
            <span>{{ t('search.searchFromSource', { keyword: keyword }) }}</span>
          </Button>
        </div>
      </EmptyState>
    </div>

    <!-- 源站搜索结果（渐进式展示：搜索中也会显示已到达的结果） -->
    <div
      v-if="hasKeyword && hasSourceSearched && sourceSearchResults.length > 0"
      class="source-search-results cczj-mb-4"
    >
      <div class="results-header source-search-header cczj-flex cczj-items-center cczj-justify-between cczj-gap-2 cczj-mb-3">
        <span class="results-label cczj-flex cczj-items-center cczj-gap-1">
          <Icon name="globe" :size="12" />
          {{ t('search.sourceSearchResults') }}
          <span v-if="sourceSearching" class="cczj-text-muted cczj-text-xs cczj-ml-1">{{ t('search.loadingProgressive') }}</span>
        </span>
        <span class="results-count cczj-text-sm cczj-text-muted">
          {{ t('search.totalItems', { count: sourceSearchTotal }) }}
          <span v-if="sourceSearchPageCount > 1" class="cczj-ml-2">· {{ t('search.pageInfo', { page: sourceSearchPage, total: sourceSearchPageCount }) }}</span>
        </span>
      </div>

      <!-- 操作工具栏：全选 / 入库按钮 / 已选计数 -->
      <div class="source-toolbar cczj-flex cczj-items-center cczj-justify-between cczj-gap-2 cczj-mb-3 cczj-flex-wrap">
        <div class="cczj-flex cczj-items-center cczj-gap-2">
          <label class="select-all-check cczj-flex cczj-items-center cczj-gap-1 cczj-cursor-pointer cczj-text-sm">
            <input
              type="checkbox"
              :checked="isAllCurrentPageSelected"
              :disabled="sourceSearchResults.length === 0 || importing"
              @change="toggleSelectAllCurrentPage"
            />
            <span>{{ t('search.selectAllCurrentPage') }}</span>
          </label>
          <span v-if="selectedSourceVodIds.size > 0" class="cczj-text-sm cczj-text-muted">
            {{ t('search.selectedCount', { count: selectedSourceVodIds.size }) }}
          </span>
        </div>
        <div class="cczj-flex cczj-items-center cczj-gap-2">
          <Button
            variant="secondary"
            size="sm"
            :disabled="importing || selectedSourceVodIds.size === 0"
            @click="importSelected"
            class="cczj-flex cczj-items-center cczj-gap-1"
          >
            <Icon name="download" :size="12" />
            <span>{{ t('search.importSelected') }}</span>
          </Button>
          <Button
            variant="primary"
            size="sm"
            :disabled="importing || sourceSearchResults.length === 0"
            @click="importCurrentPage"
            class="cczj-flex cczj-items-center cczj-gap-1"
          >
            <Icon name="download" :size="12" />
            <span>{{ importing ? t('search.importing') : t('search.importCurrentPage') }}</span>
          </Button>
        </div>
      </div>

      <!-- 入库结果消息 -->
      <div v-if="importMessage.text" class="import-message cczj-mb-3" :class="importMessage.type">
        <Icon :name="importMessage.type === 'success' ? 'check' : 'close'" :size="12" />
        <span>{{ importMessage.text }}</span>
      </div>

      <div class="video-grid cczj-grid" :style="gridStyle">
        <div
          v-for="v in sourceSearchResults"
          :key="`src-${v.vod_g_id ?? v.vod_id ?? v.id}`"
          class="source-video-card-wrap cczj-relative"
          :class="{ selected: isVideoSelected(v.vod_id), imported: isVideoImported(v.vod_id) }"
        >
          <!-- 选中复选框（已入库的不显示） -->
          <div
            v-if="!isVideoImported(v.vod_id)"
            class="select-checkbox cczj-absolute"
            :class="{ checked: isVideoSelected(v.vod_id) }"
            role="button"
            tabindex="0"
            :aria-pressed="isVideoSelected(v.vod_id) ? 'true' : 'false'"
            :aria-label="v.vod_name ? t('search.selectVideoNamed', { name: v.vod_name }) : t('search.selectVideo')"
            @click.stop="toggleSelectVideo(v.vod_id)"
            @keydown.enter.stop.prevent="toggleSelectVideo(v.vod_id)"
            @keydown.space.stop.prevent="toggleSelectVideo(v.vod_id)"
          >
            <svg v-if="isVideoSelected(v.vod_id)" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3">
              <path d="M5 13l4 4L19 7"/>
            </svg>
          </div>
          <!-- 已入库徽章由 VideoCard 在海报角上渲染，避免与更新状态角标重叠 -->
          <VideoCard
            :video="v"
            :in-catalog="isVideoImported(v.vod_id)"
            :catalog-label="t('search.imported')"
            @click="onSourceVideoClick(v)"
          />
        </div>
      </div>

      <!-- 分页控件 -->
      <div
        v-if="!sourceSearching && sourceSearchPageCount > 1"
        class="source-search-pagination cczj-flex cczj-items-center cczj-justify-center cczj-gap-2 cczj-my-4"
      >
        <button
          class="page-btn cczj-cursor-pointer cczj-rounded"
          :disabled="sourceSearchPage <= 1"
          @click="goSourceSearchPage(sourceSearchPage - 1)"
        >
          <Icon name="back" :size="12" />
        </button>
        <template v-for="p in sourceSearchPageRange" :key="p">
          <span v-if="p < 0" class="page-ellipsis">…</span>
          <button
            v-else
            class="page-btn cczj-cursor-pointer cczj-rounded"
            :class="{ active: p === sourceSearchPage }"
            @click="goSourceSearchPage(p)"
          >{{ p }}</button>
        </template>
        <button
          class="page-btn cczj-cursor-pointer cczj-rounded"
          :disabled="sourceSearchPage >= sourceSearchPageCount"
          @click="goSourceSearchPage(sourceSearchPage + 1)"
        >
          <Icon name="chevron-right" :size="12" />
        </button>
        <span class="page-info cczj-text-sm cczj-text-muted">{{ t('search.pageInfo', { page: sourceSearchPage, total: sourceSearchPageCount }) }} · {{ t('search.totalItems', { count: sourceSearchTotal }) }}</span>
      </div>
    </div>

    <div
      v-if="hasKeyword && hasSourceSearched && !sourceSearching && sourceSearchResults.length === 0"
      class="cczj-mb-4"
    >
      <EmptyState
        icon="📡"
        :title="t('search.noSourceResults')"
        :description="t('search.noSourceResultsHint')"
      />
    </div>

    <div v-if="!currentSearchSource && !list.loading" class="cczj-mb-4">
      <EmptyState
        icon="📡"
        :title="t('search.selectSourceFirst')"
        :description="t('search.selectSourceHint')"
      >
        <Button variant="primary" @click="router.push('/sources')">
          {{ t('search.manageSources') }}
        </Button>
      </EmptyState>
    </div>
  </div>
</template>

<style scoped src="../styles/views/search.css"></style>
