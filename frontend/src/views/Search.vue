<script setup lang="ts">
defineOptions({ name: 'Search' })
import { ref, onMounted, onUnmounted, computed, watch, nextTick } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { GetSetting, GetRecentHistory, GetVideoList, SearchSource } from '../../bindings/cczjVideo/app'
import { Events } from '@wailsio/runtime'
import { useSourceStore } from '../stores/source'
import { useVideoStore } from '../stores/video'
import { usePosterCacheStore } from '../stores/posterCache'
import VideoCard from '../components/VideoCard.vue'
import Icon from '../components/Icon.vue'
import { Button, Tag, Select as SelectDropdown, Spinner as LoadingSpinner, Empty as EmptyState } from '../components/ui'
import { getDetailPath } from '../utils'
import type { Video, HistoryItem } from '../types'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const sourceStore = useSourceStore()
const videoStore = useVideoStore()
const posterCache = usePosterCacheStore()

const sourceOptions = computed(() =>
  sourceStore.sources
    .filter((s) => s.source_key && s.source_key.length > 0)
    .map((s) => ({ value: String(s.source_key), label: s.name })),
)

const currentSearchSource = ref('')
const keyword = ref('')
const searchHistory = ref<string[]>(
  JSON.parse(localStorage.getItem('search_history') || '[]')
)

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

const gridColumns = ref(5)
const layoutDensity = ref<'comfortable' | 'compact' | 'spacious'>('comfortable')
const gridStyle = computed(() => {
  const density = layoutDensity.value
  const gap = density === 'compact' ? '10px' : density === 'spacious' ? '20px' : '16px'
  const minWidth = density === 'compact' ? '120px' : density === 'spacious' ? '180px' : '150px'
  
  return {
    display: 'grid',
    gridTemplateColumns: `repeat(${gridColumns.value}, minmax(${minWidth}, 1fr))`,
    gap: gap
  }
})

onMounted(async () => {
  try {
    const col = await GetSetting('grid_columns')
    if (col) gridColumns.value = parseInt(col as string, 10) || 5
    const den = await GetSetting('layout_density')
    if (den === 'compact' || den === 'spacious') layoutDensity.value = den as any
  } catch {
    // 忽略
  }
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
})

watch(
  () => currentSearchSource.value,
  async (key) => {
    videoStore.videos.length = 0
    videoStore.total = 0
    hasSearched.value = false
    if (key) {
      await loadRecommendations()
    }
  }
)

// 删除通知：从详情页删除视频后，自动移除本地列表中的对应项
watch(() => videoStore.deletedVodId, (vodId) => {
  if (!vodId || videoStore.deletedSourceKey !== currentSearchSource.value) return
  const idx = videoStore.videos.findIndex(v => String(v.vod_id) === vodId)
  if (idx >= 0) {
    videoStore.videos.splice(idx, 1)
    videoStore.total = Math.max(0, videoStore.total - 1)
  }
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
  videoStore.clearDeletionNotify()
})

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
  videoStore.total > 0 ? Math.ceil(videoStore.total / PAGE_SIZE) : 1
)
const searchPageRange = computed(() => {
  const total = searchTotalPages.value
  const cur = searchCurrentPage.value
  const pages: number[] = []
  const delta = 2
  const start = Math.max(1, cur - delta)
  const end = Math.min(total, cur + delta)
  if (start > 1) { pages.push(1); if (start > 2) pages.push(-1) }
  for (let i = start; i <= end; i++) pages.push(i)
  if (end < total) { if (end < total - 1) pages.push(-1); pages.push(total) }
  return pages
})

function goSearchPage(p: number): void {
  if (p < 1 || p > searchTotalPages.value || p === searchCurrentPage.value) return
  searchCurrentPage.value = p
  videoStore.search(currentSearchSource.value!, keyword.value.trim(), p)
}

function doSearch(): void {
  const kw = keyword.value.trim()
  if (!kw || !currentSearchSource.value) return
  if (!searchHistory.value.includes(kw)) {
    searchHistory.value.unshift(kw)
    if (searchHistory.value.length > 10) searchHistory.value.pop()
    localStorage.setItem('search_history', JSON.stringify(searchHistory.value))
  }
  hasSearched.value = true
  searchCurrentPage.value = 1
  clearSourceResults()
  videoStore.search(currentSearchSource.value, kw)
}

function goDetail(item: RecommendItem): void {
  posterCache.recordClick(item.source_key, item.vod_id)
  router.push(getDetailPath(item.source_key, { vod_id: item.vod_id }))
}

function goDetailVideo(v: Video): void {
  router.push(getDetailPath(currentSearchSource.value, v))
}

function loadMore(): void {
  if (
    currentSearchSource.value &&
    videoStore.videos.length < videoStore.total
  ) {
    videoStore.search(
      currentSearchSource.value,
      keyword.value.trim(),
      videoStore.page + 1
    )
  }
}

function clearHistory(): void {
  searchHistory.value = []
  localStorage.removeItem('search_history')
}

function removeHistoryItem(kw: string): void {
  const idx = searchHistory.value.indexOf(kw)
  if (idx < 0) return
  searchHistory.value.splice(idx, 1)
  if (searchHistory.value.length > 0) {
    localStorage.setItem('search_history', JSON.stringify(searchHistory.value))
  } else {
    localStorage.removeItem('search_history')
  }
}

// ========= 源站搜索（当站内无结果时，直接调用源站 API） =========
const sourceSearching = ref(false)
const sourceSearchResults = ref<Video[]>([])
const sourceSearchTotal = ref(0)
const hasSourceSearched = ref(false)

// 搜索进度状态
const searchProgress = ref({ stage: '', message: '', current: 0, total: 0 })
let searchProgressListener: (() => void) | null = null

onMounted(() => {
  searchProgressListener = Events.On('search:progress', (event) => {
    const data = event.data
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
})

async function doSourceSearch(): Promise<void> {
  const kw = keyword.value.trim()
  if (!kw || !currentSearchSource.value) return
  sourceSearching.value = true
  hasSourceSearched.value = true
  sourceSearchResults.value = []
  searchProgress.value = { stage: '', message: '', current: 0, total: 0 }
  try {
    const resp = (await SearchSource(currentSearchSource.value, kw, 50)) as any
    sourceSearchTotal.value = resp?.total || 0
    sourceSearchResults.value = (resp?.videos as Video[]) || []
  } catch (e) {
    console.warn(t('search.sourceSearchFailed'), e)
  } finally {
    sourceSearching.value = false
    searchProgress.value = { stage: '', message: '', current: 0, total: 0 }
  }
}

function clearSourceResults(): void {
  sourceSearchResults.value = []
  sourceSearchTotal.value = 0
  hasSourceSearched.value = false
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
        <Tag class="tag-btn cczj-cursor-pointer" @click="keyword = h; doSearch()">{{ h }}</Tag>
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
        <div v-for="(group, groupIdx) in recommendGroups" :key="group.typeName" class="recommend-group" v-motion :initial="{ opacity: 0, y: 20 }" :visible="{ opacity: 1, y: 0, transition: { duration: 400, delay: groupIdx * 100, ease: 'easeOut' } }">
          <div class="group-label-row cczj-flex cczj-items-center cczj-gap-2 cczj-mb-2">
            <span class="dot"></span>
            <span>{{ group.typeName }}</span>
          </div>
          <div class="recommend-row cczj-grid cczj-gap-3">
            <div
              v-for="(item, idx) in group.items"
              :key="`rec-${group.typeName}-${item.vod_id}-${idx}`"
              class="rec-card cczj-cursor-pointer cczj-rounded"
              @click="goDetail(item)"
              v-motion
              :initial="{ opacity: 0, scale: 0.9 }"
              :visible="{ opacity: 1, scale: 1, transition: { duration: 300, delay: idx * 50, ease: 'easeOut' } }"
              :hovered="{ scale: 1.05, transition: { duration: 200 } }"
            >
              <div class="rec-poster cczj-relative cczj-rounded cczj-overflow-hidden">
                <img
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
    <div v-if="hasSearched && videoStore.loading && videoStore.videos.length === 0" class="cczj-text-center cczj-py-8">
      <LoadingSpinner :label="t('search.searching')" />
    </div>

    <div
      v-else-if="hasSearched && videoStore.videos.length > 0"
      class="search-results cczj-mb-4"
    >
      <div class="results-header cczj-flex cczj-items-center cczj-justify-between cczj-gap-2 cczj-mb-3">
        <span class="results-label cczj-flex cczj-items-center cczj-gap-1">
          <Icon name="search" :size="12" />
          {{ t('search.searchResults') }}
        </span>
        <span class="results-count cczj-text-sm cczj-text-muted">{{ t('search.totalItems', { count: videoStore.total }) }}</span>
      </div>
      <div class="video-grid cczj-grid" :style="gridStyle">
        <VideoCard
          v-for="v in videoStore.videos"
          :key="`${v.vod_g_id ?? v.vod_id ?? v.id}`"
          :video="v"
          @click="goDetailVideo(v)"
        />
      </div>
    </div>

    <div
      v-if="hasSearched && videoStore.videos.length > 0 && searchTotalPages > 1"
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
        <span v-if="p === -1" class="page-ellipsis">…</span>
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
      <span class="page-info cczj-text-sm cczj-text-muted">{{ searchCurrentPage }} / {{ searchTotalPages }} {{ t('search.page') }}，{{ t('search.totalItems', { count: videoStore.total }) }}</span>
    </div>

    <!-- 源站搜索进度（独立展示，不受空状态组件约束） -->
    <div v-if="sourceSearching" class="source-search-progress-section cczj-mb-6">
      <div class="search-progress-card">
        <!-- 步骤指示器 -->
        <div class="sp-steps">
          <div class="sp-step" :class="{ active: searchProgress.stage === 'fetching_list' || !searchProgress.stage, done: searchProgress.stage === 'fetching_details' }">
            <div class="sp-step-dot">
              <svg v-if="searchProgress.stage === 'fetching_details'" class="sp-check" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3"><path d="M5 13l4 4L19 7"/></svg>
              <div v-else class="sp-pulse"></div>
            </div>
            <span class="sp-step-label">{{ t('search.fetchingList') || '获取列表' }}</span>
          </div>
          <div class="sp-step-line" :class="{ filled: searchProgress.stage === 'fetching_details' }"></div>
          <div class="sp-step" :class="{ active: searchProgress.stage === 'fetching_details' }">
            <div class="sp-step-dot">
              <div v-if="searchProgress.stage === 'fetching_details'" class="sp-pulse"></div>
              <svg v-else class="sp-check" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3" style="opacity:0.3"><path d="M5 13l4 4L19 7"/></svg>
            </div>
            <span class="sp-step-label">{{ t('search.fetchingDetails') || '获取详情' }}</span>
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

    <div
      v-if="
        hasSearched &&
        !videoStore.loading &&
        videoStore.videos.length === 0 &&
        sourceStore.currentSourceKey &&
        !sourceSearching
      "
    >
      <EmptyState
        icon="🔍"
        :title="t('search.noResultsTitle')"
        :description="t('search.noResultsHint')"
      >
        <div class="source-search-wrap cczj-flex cczj-flex-col cczj-gap-3 cczj-items-center cczj-mt-4">
          <Button
            v-if="!hasSourceSearched"
            variant="primary"
            @click="doSourceSearch"
            class="cczj-flex cczj-items-center cczj-gap-2"
          >
            <Icon name="search" :size="12" />
            <span>{{ t('search.searchFromSource', { keyword: keyword }) }}</span>
          </Button>
        </div>
      </EmptyState>
    </div>

    <!-- 源站搜索结果 -->
    <div
      v-if="hasSourceSearched && !sourceSearching && sourceSearchTotal > 0"
      class="source-search-results cczj-mb-4"
    >
      <div class="results-header source-search-header cczj-flex cczj-items-center cczj-justify-between cczj-gap-2 cczj-mb-3">
        <span class="results-label cczj-flex cczj-items-center cczj-gap-1">
          <Icon name="globe" :size="12" />
          {{ t('search.sourceSearchResults') }}
        </span>
        <span class="results-count cczj-text-sm cczj-text-muted">{{ t('search.totalItems', { count: sourceSearchTotal }) }}（{{ t('search.autoImported') }}）</span>
      </div>
      <div class="video-grid cczj-grid" :style="gridStyle">
        <VideoCard
          v-for="v in sourceSearchResults"
          :key="`src-${v.vod_g_id ?? v.vod_id ?? v.id}`"
          :video="v"
          @click="goDetailVideo(v)"
        />
      </div>
    </div>

    <div
      v-if="hasSourceSearched && !sourceSearching && sourceSearchTotal === 0"
      class="cczj-mb-4"
    >
      <EmptyState
        icon="📡"
        :title="t('search.noSourceResults')"
        :description="t('search.noSourceResultsHint')"
      />
    </div>

    <div v-if="!currentSearchSource && !videoStore.loading" class="cczj-mb-4">
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

<style scoped>
.search-page {
  max-width: 100%;
  color: var(--text-primary);
  animation: fadeInUp 0.4s ease;
}

@keyframes fadeInUp {
  from { opacity: 0; transform: translateY(10px); }
  to { opacity: 1; transform: translateY(0); }
}

/* 搜索栏 */
.search-bar {
  display: flex;
  gap: 10px;
  margin-bottom: 16px;
  padding: 10px;
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 14px;
  transition: all 0.2s ease;
}
.search-bar:focus-within {
  border-color: var(--accent);
  box-shadow: 0 0 0 3px var(--accent-alpha-10);
}

.source-picker {
  position: relative;
  display: flex;
  align-items: center;
}
.pick-icon {
  position: absolute;
  right: 10px;
  color: var(--text-muted);
  pointer-events: none;
}

.input-wrap {
  flex: 1;
  position: relative;
  display: flex;
  align-items: center;
}
.input-icon {
  position: absolute;
  left: 14px;
  color: var(--text-muted);
  pointer-events: none;
}
.search-input {
  flex: 1;
  padding: 10px 16px 10px 40px;
  border-radius: 10px;
  border: 1px solid var(--border);
  background: var(--bg-input);
  color: var(--text-primary);
  outline: none;
  font-size: 14px;
  font-family: inherit;
  transition: border-color 0.15s;
}
.search-input::placeholder { color: var(--text-muted); }
.search-input:focus { border-color: var(--accent); }

.clear-x {
  position: absolute;
  right: 8px;
  width: 24px !important;
  height: 24px !important;
  min-width: 24px !important;
  padding: 0 !important;
  background: transparent !important;
  color: var(--text-muted) !important;
  border-radius: 50% !important;
  border: none !important;
}
.clear-x:hover {
  background: var(--bg-hover) !important;
  color: var(--text-primary) !important;
}

/* 历史搜索标签 */
.history-tags {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 18px;
  padding: 10px 14px;
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 12px;
  flex-wrap: wrap;
}
.history-label {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  color: var(--text-muted);
  font-size: 12px;
  font-weight: 500;
}
.tag-wrap {
  display: inline-flex;
  align-items: center;
  border-radius: 16px;
  border: 1px solid var(--border);
  background: var(--bg-input);
  overflow: hidden;
  transition: all 0.15s ease;
}
.tag-wrap:hover {
  border-color: var(--accent);
  background: var(--accent-alpha-10);
}
.tag-btn {
  border: none !important;
  background: transparent !important;
  color: var(--text-secondary);
  cursor: pointer;
}
.tag-wrap:hover .tag-btn {
  color: var(--accent);
}
.tag-remove {
  width: 22px !important;
  height: 22px !important;
  min-width: 22px !important;
  padding: 0 !important;
  margin-right: 4px;
  background: transparent !important;
  color: var(--text-muted) !important;
  border-radius: 50% !important;
  border: none !important;
}
.tag-remove:hover {
  background: rgba(255, 90, 95, 0.15) !important;
  color: #ff5a5f !important;
}
/* ============ 推荐区域 ============ */
.recommend-section {
  margin-bottom: 20px;
  padding: 16px;
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 14px;
}
.recommend-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 14px;
}
.recommend-header h3 {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  margin: 0;
  font-size: 15px;
  font-weight: 600;
  color: var(--text-primary);
}
.recommend-loading {
  padding: 20px;
  text-align: center;
}
.recommend-groups {
  display: flex;
  flex-direction: column;
  gap: 18px;
}
.recommend-group {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.group-label-row {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  color: var(--text-muted);
  font-weight: 500;
}
.group-label-row .dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--accent);
}
.recommend-row {
  display: grid;
  grid-template-columns: repeat(6, minmax(0, 1fr));
  gap: 12px;
}
@media (max-width: 1200px) { .recommend-row { grid-template-columns: repeat(5, minmax(0, 1fr)); } }
@media (max-width: 900px) { .recommend-row { grid-template-columns: repeat(4, minmax(0, 1fr)); } }
@media (max-width: 640px) { .recommend-row { grid-template-columns: repeat(3, minmax(0, 1fr)); } }

.rec-card {
  cursor: pointer;
  transition: transform 0.2s ease;
}
.rec-card:hover { transform: translateY(-3px); }
.rec-card:hover .rec-poster { border-color: var(--accent); }
.rec-card:hover .rec-overlay { opacity: 1; }
.rec-card:hover .rec-title { color: var(--accent); }

.rec-poster {
  position: relative;
  aspect-ratio: 2/3;
  border-radius: 10px;
  overflow: hidden;
  background: var(--bg-secondary);
  border: 1px solid var(--border);
  transition: border-color 0.2s ease;
}
.rec-poster img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
}
.rec-poster-placeholder, .rec-poster-loading {
  width: 100%;
  height: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--text-muted);
  opacity: 0.55;
}
.rec-overlay {
  position: absolute;
  inset: 0;
  background: linear-gradient(to top, rgba(0,0,0,0.5) 0%, transparent 60%);
  display: flex;
  align-items: center;
  justify-content: center;
  color: #fff;
  opacity: 0;
  transition: opacity 0.2s ease;
}

.rec-title {
  margin-top: 6px;
  font-size: 12px;
  line-height: 1.4;
  color: var(--text-primary);
  text-align: center;
  overflow: hidden;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  transition: color 0.15s ease;
}

/* 搜索结果 */
.search-results { margin-top: 8px; }
.results-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 14px;
}
.results-label {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: 13px;
  font-weight: 500;
  color: var(--text-primary);
}
.results-count { font-size: 12px; color: var(--text-muted); }
.video-grid {
  display: grid;
  gap: 18px;
}

.load-more {
  text-align: center;
  padding: 28px 16px 20px;
}
.spinner-inline { display: inline-block; }

/* 分页控件 */
.search-pagination {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  padding: 20px 0 8px;
  flex-wrap: wrap;
}
.page-btn {
  min-width: 32px;
  height: 32px;
  padding: 0 8px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--bg-card);
  color: var(--text-secondary);
  font-size: 13px;
  cursor: pointer;
  transition: all 0.15s ease;
}
.page-btn:hover:not(:disabled):not(.active) {
  border-color: var(--accent);
  color: var(--accent);
  background: var(--accent-alpha-10);
}
.page-btn.active {
  background: var(--accent);
  border-color: var(--accent);
  color: #fff;
  font-weight: 600;
}
.page-btn:disabled {
  opacity: 0.35;
  cursor: not-allowed;
}
.page-ellipsis {
  padding: 0 4px;
  color: var(--text-muted);
  font-size: 14px;
  user-select: none;
}
.page-info {
  margin-left: 12px;
  font-size: 12px;
  color: var(--text-muted);
}

/* 源站搜索区域 */
.source-search-wrap {
  margin-top: 8px;
}

/* 源站搜索进度（独立全宽区域） */
.source-search-progress-section {
  max-width: 560px;
  margin: 0 auto;
}

/* ============ 搜索进度卡片 ============ */
.search-progress-card {
  background: var(--bg-card);
  border: 1px solid var(--accent-alpha-20);
  border-radius: 14px;
  padding: 24px 28px 20px;
  animation: sp-fadeIn 0.35s ease;
}
@keyframes sp-fadeIn {
  from { opacity: 0; transform: translateY(6px); }
  to { opacity: 1; transform: translateY(0); }
}

/* 步骤指示器 */
.sp-steps {
  display: flex;
  align-items: center;
  gap: 0;
  margin-bottom: 20px;
}
.sp-step {
  display: flex;
  align-items: center;
  gap: 10px;
}
.sp-step-dot {
  width: 30px;
  height: 30px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  background: var(--bg-secondary);
  border: 2px solid var(--border);
  transition: all 0.3s ease;
}
.sp-step.done .sp-step-dot {
  background: var(--success, #22c55e);
  border-color: var(--success, #22c55e);
}
.sp-step.active .sp-step-dot {
  background: var(--accent);
  border-color: var(--accent);
  box-shadow: 0 0 0 4px var(--accent-alpha-15, rgba(99,102,241,0.15));
}
.sp-check {
  width: 14px;
  height: 14px;
  color: #fff;
}
.sp-pulse {
  width: 10px;
  height: 10px;
  border-radius: 50%;
  background: #fff;
  animation: sp-pulse 1.2s ease-in-out infinite;
}
@keyframes sp-pulse {
  0%, 100% { opacity: 1; transform: scale(1); }
  50% { opacity: 0.5; transform: scale(0.7); }
}
.sp-step-label {
  font-size: 14px;
  font-weight: 500;
  color: var(--text-muted);
  white-space: nowrap;
  transition: color 0.3s;
}
.sp-step.active .sp-step-label {
  color: var(--text-primary);
  font-weight: 600;
}
.sp-step.done .sp-step-label {
  color: var(--text-secondary, var(--text-muted));
}
.sp-step-line {
  flex: 1;
  height: 2px;
  background: var(--border);
  margin: 0 14px;
  border-radius: 1px;
  transition: background 0.4s;
}
.sp-step-line.filled {
  background: var(--success, #22c55e);
}

/* 进度条 */
.sp-bar-wrap {
  display: flex;
  align-items: center;
  gap: 14px;
  margin-bottom: 12px;
}
.sp-bar {
  flex: 1;
  height: 10px;
  background: var(--bg-secondary);
  border-radius: 5px;
  overflow: hidden;
  position: relative;
}
.sp-bar-fill {
  height: 100%;
  background: linear-gradient(90deg, var(--accent), color-mix(in srgb, var(--accent) 70%, #a78bfa));
  border-radius: 5px;
  transition: width 0.4s cubic-bezier(0.4, 0, 0.2, 1);
}
.sp-bar-shimmer {
  position: absolute;
  top: 0; left: 0; right: 0; bottom: 0;
  background: linear-gradient(90deg, transparent 0%, rgba(255,255,255,0.15) 50%, transparent 100%);
  background-size: 200% 100%;
  animation: sp-shimmer 1.8s ease-in-out infinite;
}
@keyframes sp-shimmer {
  0% { background-position: 200% 0; }
  100% { background-position: -200% 0; }
}
.sp-bar-pct {
  font-size: 13px;
  font-weight: 600;
  color: var(--accent);
  font-family: 'SF Mono', Consolas, monospace;
  min-width: 52px;
  text-align: right;
  white-space: nowrap;
}

/* 状态消息 */
.sp-msg {
  margin: 0;
  font-size: 13px;
  color: var(--text-muted);
  line-height: 1.5;
}

.source-search-results {
  margin-top: 16px;
  padding: 16px;
  background: var(--bg-card);
  border: 1px solid var(--accent-alpha-20);
  border-radius: 14px;
}
.source-search-header {
  margin-bottom: 14px;
}
</style>
