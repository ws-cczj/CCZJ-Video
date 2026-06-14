<script setup lang="ts">
defineOptions({ name: 'Search' })
import { ref, onMounted, computed, watch, nextTick } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { GetSetting, GetRecentHistory, GetVideoList, SearchSource } from '../../bindings/cczjVideo/app'
import { useSourceStore } from '../stores/source'
import { useVideoStore } from '../stores/video'
import { usePosterCacheStore } from '../stores/posterCache'
import VideoCard from '../components/VideoCard.vue'
import Icon from '../components/Icon.vue'
import SelectDropdown from '../components/SelectDropdown.vue'
import LoadingSpinner from '../components/LoadingSpinner.vue'
import EmptyState from '../components/EmptyState.vue'
import { Button, Tag } from '../components/ui'
import { getDetailPath } from '../utils'
import type { Video, HistoryItem } from '../types'

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
const gridStyle = computed(() => ({
  gridTemplateColumns: `repeat(${gridColumns.value}, minmax(0, 1fr))`,
}))

onMounted(async () => {
  try {
    const col = await GetSetting('grid_columns')
    if (col) gridColumns.value = parseInt(col as string, 10) || 5
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
      groups.push({ typeName: '继续观看', items: historyItems })

      // 过滤掉已看过的视频作为"猜你喜欢"
      const seenIds = new Set(sameSourceHistory.map(h => h.vod_id))
      const unknownVideos = freshVideos.filter(v => !seenIds.has(String(v.vod_id)))
      if (unknownVideos.length > 0) {
        groups.push({
          typeName: '猜你喜欢',
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
        typeName: '最新上线',
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
  return cached?.vod_name || '视频'
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

async function doSourceSearch(): Promise<void> {
  const kw = keyword.value.trim()
  if (!kw || !currentSearchSource.value) return
  sourceSearching.value = true
  hasSourceSearched.value = true
  sourceSearchResults.value = []
  try {
    const resp = (await SearchSource(currentSearchSource.value, kw, 50)) as any
    sourceSearchTotal.value = resp?.total || 0
    sourceSearchResults.value = (resp?.videos as Video[]) || []
  } catch (e) {
    console.warn('源站搜索失败', e)
  } finally {
    sourceSearching.value = false
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
    <div class="search-bar">
      <div class="source-picker">
        <SelectDropdown
          v-model="currentSearchSource"
          :options="sourceOptions"
          :disabled="sourceStore.sources.length === 0"
          placeholder="选择数据源"
        />
        <Icon name="source" :size="14" class="pick-icon" />
      </div>

      <div class="input-wrap">
        <Icon name="search" :size="16" class="input-icon" />
        <input
          v-model="keyword"
          @keyup.enter="doSearch"
          placeholder="输入视频名称搜索..."
          class="search-input"
        />
        <Button
          v-if="keyword"
          variant="text"
          size="sm"
          icon
          class="clear-x"
          @click="keyword = ''"
          aria-label="清除"
        >
          <Icon name="close" :size="12" />
        </Button>
      </div>

      <Button variant="primary" @click="doSearch">
        <Icon name="search" :size="14" />
        <span>搜索</span>
      </Button>
    </div>

    <!-- 历史搜索 -->
    <div v-if="searchHistory.length > 0 && !hasSearched" class="history-tags">
      <span class="history-label">
        <Icon name="clock" :size="12" />
        历史搜索
      </span>
      <div
        v-for="h in searchHistory"
        :key="h"
        class="tag-wrap"
      >
        <Tag class="tag-btn" @click="keyword = h; doSearch()">{{ h }}</Tag>
        <Button variant="text" size="sm" icon class="tag-remove" title="删除此记录" @click.stop="removeHistoryItem(h)">
          <Icon name="close" :size="10" />
        </Button>
      </div>
      <Button variant="text" size="sm" @click="clearHistory">清空</Button>
    </div>

    <!-- ============ 推荐区域（未搜索时显示） ============ -->
    <section v-if="!hasSearched && (recommendGroups.length > 0 || recommendLoading)" class="recommend-section">
      <div class="recommend-header">
        <h3>
          <Icon name="play" :size="14" />
          <span>为你推荐</span>
        </h3>
      </div>

      <div v-if="recommendLoading" class="recommend-loading">
        <LoadingSpinner size="sm" label="加载推荐中..." />
      </div>

      <div v-else class="recommend-groups">
        <div v-for="group in recommendGroups" :key="group.typeName" class="recommend-group">
          <div class="group-label-row">
            <span class="dot"></span>
            <span>{{ group.typeName }}</span>
          </div>
          <div class="recommend-row">
            <div
              v-for="(item, idx) in group.items"
              :key="`rec-${group.typeName}-${item.vod_id}-${idx}`"
              class="rec-card"
              @click="goDetail(item)"
            >
              <div class="rec-poster">
                <img
                  v-if="resolvePic(item)"
                  :src="resolvePic(item)"
                  :alt="resolveName(item)"
                  loading="lazy"
                />
                <div v-else-if="isPicLoading(item)" class="rec-poster-loading">
                  <LoadingSpinner size="sm" />
                </div>
                <div v-else class="rec-poster-placeholder">
                  <Icon name="film" :size="22" />
                </div>
                <div class="rec-overlay">
                  <Icon name="play" :size="16" />
                </div>
              </div>
              <div class="rec-title">{{ resolveName(item) }}</div>
            </div>
          </div>
        </div>
      </div>
    </section>

    <!-- 搜索结果 -->
    <div v-if="hasSearched && videoStore.loading && videoStore.videos.length === 0">
      <LoadingSpinner label="搜索中..." />
    </div>

    <div
      v-else-if="hasSearched && videoStore.videos.length > 0"
      class="search-results"
    >
      <div class="results-header">
        <span class="results-label">
          <Icon name="search" :size="12" />
          搜索结果
        </span>
        <span class="results-count">共 {{ videoStore.total }} 条</span>
      </div>
      <div class="video-grid" :style="gridStyle">
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
      class="search-pagination"
    >
      <button
        class="page-btn"
        :disabled="searchCurrentPage <= 1"
        @click="goSearchPage(searchCurrentPage - 1)"
      >
        <Icon name="back" :size="12" />
      </button>
      <template v-for="p in searchPageRange" :key="p">
        <span v-if="p === -1" class="page-ellipsis">…</span>
        <button
          v-else
          class="page-btn"
          :class="{ active: p === searchCurrentPage }"
          @click="goSearchPage(p)"
        >{{ p }}</button>
      </template>
      <button
        class="page-btn"
        :disabled="searchCurrentPage >= searchTotalPages"
        @click="goSearchPage(searchCurrentPage + 1)"
      >
        <Icon name="chevron-right" :size="12" />
      </button>
      <span class="page-info">{{ searchCurrentPage }} / {{ searchTotalPages }}页，共 {{ videoStore.total }} 条</span>
    </div>

    <div
      v-if="
        hasSearched &&
        !videoStore.loading &&
        videoStore.videos.length === 0 &&
        sourceStore.currentSourceKey
      "
    >
      <EmptyState
        icon="🔍"
        title="未找到相关视频"
        description="试试换个关键词，或直接到源站搜索最新结果"
      >
        <div class="source-search-wrap">
          <Button
            v-if="!sourceSearching && !hasSourceSearched"
            variant="primary"
            @click="doSourceSearch"
          >
            <Icon name="search" :size="12" />
            <span>到源站搜索 "{{ keyword }}"</span>
          </Button>
          <div v-else-if="sourceSearching" class="source-search-loading">
            <LoadingSpinner size="sm" label="正在从源站搜索..." />
          </div>
        </div>
      </EmptyState>
    </div>

    <!-- 源站搜索结果 -->
    <div
      v-if="hasSourceSearched && !sourceSearching && sourceSearchTotal > 0"
      class="source-search-results"
    >
      <div class="results-header source-search-header">
        <span class="results-label">
          <Icon name="globe" :size="12" />
          源站搜索结果
        </span>
        <span class="results-count">共 {{ sourceSearchTotal }} 条（已自动入库）</span>
      </div>
      <div class="video-grid" :style="gridStyle">
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
    >
      <EmptyState
        icon="📡"
        title="源站也无结果"
        description="该关键词在源站也找不到相关视频，试试换个关键词"
      />
    </div>

    <div v-if="!currentSearchSource && !videoStore.loading">
      <EmptyState
        icon="📡"
        title="请先选择采集源"
        description="在上方下拉框中选择一个来源，然后输入关键词搜索"
      >
        <Button variant="primary" @click="router.push('/sources')">
          管理采集源
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
.source-search-loading {
  margin-top: 16px;
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
