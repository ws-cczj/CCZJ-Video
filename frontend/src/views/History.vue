<script setup lang="ts">
defineOptions({ name: 'History' })
import { ref, onMounted, onActivated, computed } from 'vue'
import { useRouter } from 'vue-router'
import { GetRecentHistory, DeleteHistoryByVideo, ClearAllHistory } from '../../bindings/cczjVideo/app'
import { usePosterCacheStore } from '../stores/posterCache'
import Icon from '../components/Icon.vue'
import { Button, Badge, Spinner as LoadingSpinner, Empty as EmptyState } from '../components/ui'
import { formatTime } from '../utils'
import { epProgressKey, getEpProgress, getEpProgressPct } from '../utils/episodeProgress'
import type { HistoryItem } from '../types'

const router = useRouter()
const posterCache = usePosterCacheStore()

const history = ref<HistoryItem[]>([])
const loading = ref(false)
const loadingPoster = ref<Set<string>>(new Set())
const manageMode = ref(false)
const selectedKeys = ref<Set<string>>(new Set<string>())
const searchKeyword = ref('')
const batchRemoving = ref(false)

async function reloadHistory(): Promise<void> {
  loading.value = true
  try {
    const result = await GetRecentHistory(200)
    history.value = Array.isArray(result) ? (result as HistoryItem[]) : []
  } catch (e) {
    console.error('加载历史失败:', e)
    history.value = []
  } finally {
    loading.value = false
  }
  await hydrateMissingPosters()
}

onMounted(() => { reloadHistory() })
onActivated(() => { if (!manageMode.value) reloadHistory() })

/**
 * 对没有名称或封面的历史项，异步从详情接口补齐信息。
 */
async function hydrateMissingPosters(): Promise<void> {
  const limit = 8
  const toLoad: HistoryItem[] = []
  for (const h of history.value) {
    const cached = posterCache.get(h.source_key, h.vod_id)
    const hasName = h.vod_name || cached?.vod_name
    const hasPic = cached?.vod_pic
    if (!hasName || !hasPic) toLoad.push(h)
    if (toLoad.length >= limit) break
  }
  for (const h of toLoad) {
    const key = `${h.source_key}:${h.vod_id}`
    if (loadingPoster.value.has(key)) continue
    loadingPoster.value.add(key)
    const entry = await posterCache.ensureLoaded(h.source_key, h.vod_id)
    if (entry?.vod_name) {
      const idx = history.value.findIndex(
        x => x.source_key === h.source_key && x.vod_id === h.vod_id
      )
      if (idx !== -1 && !history.value[idx].vod_name) {
        history.value[idx].vod_name = entry.vod_name
      }
    }
    loadingPoster.value.delete(key)
  }
}

// 按整部影视聚合：同来源+同视频 → 只保留最近观看的那一集
const dedupedHistory = computed(() => {
  const seen = new Map<string, HistoryItem>()
  const sorted = [...history.value].sort((a, b) => toTs(b.updated_at) - toTs(a.updated_at))
  for (const h of sorted) {
    const k = keyOf(h)
    if (!seen.has(k)) seen.set(k, h)
  }
  return Array.from(seen.values())
})

// 按关键字（名称/来源）过滤
const filteredHistory = computed(() => {
  const kw = searchKeyword.value.trim().toLowerCase()
  if (!kw) return dedupedHistory.value
  return dedupedHistory.value.filter((h) => {
    const name = (h.vod_name || posterCache.get(h.source_key, h.vod_id)?.vod_name || '').toLowerCase()
    const src = (h.source_key || '').toLowerCase()
    return name.includes(kw) || src.includes(kw)
  })
})

function toTs(v?: string | number): number {
  if (v == null) return 0
  if (typeof v === 'number') return v
  const n = new Date(v).getTime()
  return isNaN(n) ? 0 : n
}

const groupedByDate = computed(() => {
  const groups: Record<string, HistoryItem[]> = {}
  for (const h of filteredHistory.value) {
    const dateKey = formatDateKey(h.updated_at)
    if (!groups[dateKey]) groups[dateKey] = []
    groups[dateKey].push(h)
  }
  return Object.keys(groups)
    .sort((a, b) => (a < b ? 1 : -1))
    .map(key => ({
      label: formatGroupLabel(key),
      items: groups[key],
    }))
})

const allKeys = computed(() => filteredHistory.value.map(keyOf))
const allSelected = computed(() => allKeys.value.length > 0 && allKeys.value.every(k => selectedKeys.value.has(k)))

function keyOf(h: HistoryItem): string {
  if (h.global_id != null) return `g-${h.global_id}`
  return `${h.source_key}-${h.vod_id}`
}

function getWatchPct(h: HistoryItem): number {
  const entry = getEpProgress(epProgressKey(h.global_id, h.vod_name, h.ep_num))
  if (entry) return getEpProgressPct(entry)
  if (h.position && h.position > 0 && h.position <= 100) return Math.round(h.position)
  return 0
}

function enterManageMode(): void {
  selectedKeys.value = new Set()
  manageMode.value = true
}

function toggleSelect(h: HistoryItem): void {
  const k = keyOf(h)
  if (selectedKeys.value.has(k)) {
    selectedKeys.value.delete(k)
  } else {
    selectedKeys.value.add(k)
  }
  selectedKeys.value = new Set(selectedKeys.value)
}

function isSelected(h: HistoryItem): boolean {
  return selectedKeys.value.has(keyOf(h))
}

function toggleSelectAll(): void {
  if (allSelected.value) {
    selectedKeys.value = new Set()
  } else {
    selectedKeys.value = new Set(allKeys.value)
  }
}

async function deleteSelected(): Promise<void> {
  if (selectedKeys.value.size === 0 || batchRemoving.value) return
  batchRemoving.value = true
  try {
    const toDelete = filteredHistory.value.filter(h => selectedKeys.value.has(keyOf(h)))
    for (const h of toDelete) {
      try {
        await DeleteHistoryByVideo({ source_key: h.source_key, vod_id: String(h.vod_id) })
      } catch (e) {
        console.error('删除失败:', e)
      }
    }
    selectedKeys.value = new Set()
    manageMode.value = false
    await reloadHistory()
  } finally {
    batchRemoving.value = false
  }
}

async function clearAll(): Promise<void> {
  if (!window.confirm('确定要清空所有观看历史吗？此操作不可撤销。')) return
  batchRemoving.value = true
  try {
    await ClearAllHistory()
    history.value = []
    selectedKeys.value = new Set()
    manageMode.value = false
  } catch (e) {
    console.error('清空失败:', e)
  } finally {
    batchRemoving.value = false
  }
}

function exitManage(): void {
  manageMode.value = false
  selectedKeys.value = new Set()
}

function formatDateKey(ts?: string | number): string {
  if (!ts) return 'unknown'
  const d = new Date(typeof ts === 'number' ? ts * 1000 : ts)
  if (isNaN(d.getTime())) return 'unknown'
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}

function formatGroupLabel(key: string): string {
  if (key === 'unknown') return '更早'
  const today = new Date()
  const todayKey = formatDateKey(today.getTime() / 1000)
  if (key === todayKey) return '今天'
  const yesterday = new Date(today.getTime() - 86400000)
  const yesterdayKey = formatDateKey(yesterday.getTime() / 1000)
  if (key === yesterdayKey) return '昨天'
  const parts = key.split('-')
  if (parts.length === 3) {
    return `${parseInt(parts[1])}月${parseInt(parts[2])}日`
  }
  return key
}

function formatRelativeTime(ts?: string | number): string {
  if (!ts) return ''
  let timestamp: number
  if (typeof ts === 'number') {
    timestamp = ts * 1000
  } else {
    timestamp = new Date(ts).getTime()
  }
  if (isNaN(timestamp)) return ''
  const diff = Date.now() - timestamp
  const minutes = Math.floor(diff / 60000)
  if (minutes < 1) return '刚刚'
  if (minutes < 60) return `${minutes} 分钟前`
  const hours = Math.floor(minutes / 60)
  if (hours < 24) return `${hours} 小时前`
  const days = Math.floor(hours / 24)
  if (days < 7) return `${days} 天前`
  return formatTime(String(ts))
}

function resolveName(h: HistoryItem): string {
  if (h.vod_name) return h.vod_name
  const cached = posterCache.get(h.source_key, h.vod_id)
  return cached?.vod_name || '未命名视频'
}

function resolvePic(h: HistoryItem): string {
  const cached = posterCache.get(h.source_key, h.vod_id)
  return cached?.vod_pic || ''
}

function isPicLoading(h: HistoryItem): boolean {
  return loadingPoster.value.has(`${h.source_key}:${h.vod_id}`)
}

function goDetail(h: HistoryItem): void {
  if (manageMode.value) {
    toggleSelect(h)
    return
  }
  posterCache.recordClick(h.source_key, h.vod_id)
  router.push(`/detail/${h.source_key}/${h.vod_id}`)
}
</script>

<template>
  <div class="history-page">
    <div class="page-header">
      <div class="page-header-left">
        <h2><Icon name="clock" :size="20" /> 观看历史</h2>
        <p class="desc" v-if="dedupedHistory.length > 0 && !manageMode">
          最近观看了 {{ dedupedHistory.length }} 个视频{{ searchKeyword ? `（搜索结果 ${filteredHistory.length} 个）` : '' }}
        </p>
        <p class="desc" v-else-if="manageMode">已选 {{ selectedKeys.size }} 条</p>
        <p class="desc" v-else>还没有观看记录，去首页看看有什么精彩内容</p>
      </div>
      <div class="page-header-actions">
        <template v-if="!manageMode">
          <div class="search-box">
            <Icon name="search" :size="14" />
            <input v-model="searchKeyword" type="text" placeholder="搜索视频名称或来源..." />
          </div>
          <Button v-if="dedupedHistory.length > 0" variant="ghost" size="sm" @click="enterManageMode">
            <Icon name="settings" :size="14" />
            <span>管理</span>
          </Button>
        </template>
        <template v-else>
          <Button variant="secondary" size="sm" @click="toggleSelectAll">
            <Icon name="check" :size="14" />
            <span>{{ allSelected ? '取消全选' : '全选' }}</span>
          </Button>
          <Button variant="danger" size="sm" @click="deleteSelected" :disabled="selectedKeys.size === 0 || batchRemoving">
            <Icon name="trash" :size="14" />
            <span>删除所选</span>
          </Button>
          <Button variant="ghost" size="sm" @click="clearAll" :disabled="batchRemoving">
            <Icon name="x" :size="14" />
            <span>清空全部</span>
          </Button>
          <Button variant="primary" size="sm" @click="exitManage">
            <Icon name="check-circle" :size="14" />
            <span>完成</span>
          </Button>
        </template>
      </div>
    </div>

    <!-- 加载状态 -->
    <div v-if="loading" class="loading-wrap">
      <LoadingSpinner label="加载历史记录中..." />
    </div>

    <!-- 空状态 -->
    <div v-else-if="dedupedHistory.length === 0">
      <EmptyState
        icon="📺"
        title="暂无观看记录"
        description="在视频详情页开始观看就会出现在这里"
      >
        <Button variant="primary" @click="router.push('/')">去首页看看</Button>
      </EmptyState>
    </div>

    <!-- 历史列表（按日期分组） -->
    <div v-else class="history-list">
      <div v-for="group in groupedByDate" :key="group.label" class="history-group">
        <div class="group-header">
          <span class="group-label">{{ group.label }}</span>
          <span class="group-count">{{ group.items.length }} 条记录</span>
        </div>

        <div class="history-cards">
          <div
            v-for="h in group.items"
            :key="keyOf(h)"
            class="history-card"
            :class="{ 'is-selected': manageMode && isSelected(h) }"
            @click="goDetail(h)"
          >
            <label v-if="manageMode" class="card-checkbox" @click.stop>
              <input
                type="checkbox"
                :checked="isSelected(h)"
                :disabled="batchRemoving"
                @change="toggleSelect(h)"
              />
              <span class="check-mark" />
            </label>
            <div class="poster">
              <img
                v-if="resolvePic(h)"
                :src="resolvePic(h)"
                :alt="resolveName(h)"
                loading="lazy"
              />
              <div v-else-if="isPicLoading(h)" class="poster-loading">
                <LoadingSpinner size="sm" />
              </div>
              <div v-else class="poster-placeholder">
                <Icon name="film" :size="24" />
              </div>
              <div class="play-overlay">
                <Icon name="play" :size="16" />
              </div>
            </div>

            <div class="info">
              <span class="title">{{ resolveName(h) }}</span>
              <div class="meta-row">
                <Badge variant="primary">看到第 {{ h.ep_num }} 集</Badge>
                <Badge v-if="getWatchPct(h) > 0" variant="success">{{ getWatchPct(h) }}%</Badge>
                <Badge>{{ h.source_key }}</Badge>
              </div>
              <div class="time-row">
                <Icon name="clock" :size="11" />
                <span>{{ formatRelativeTime(h.updated_at) }}</span>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.history-page {
  max-width: 100%;
  color: var(--text-primary);
  animation: fadeInUp 0.4s ease;
}

@keyframes fadeInUp {
  from { opacity: 0; transform: translateY(10px); }
  to { opacity: 1; transform: translateY(0); }
}

/* 页面头部 */
.page-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 24px;
  padding-bottom: 16px;
  border-bottom: 1px solid var(--border);
}
.page-header-left {
  min-width: 0;
  flex: 1;
}
.page-header h2 {
  display: inline-flex;
  align-items: center;
  gap: 10px;
  font-size: 22px;
  font-weight: 700;
  margin: 0 0 4px;
  color: var(--text-primary);
}
.page-header .desc {
  font-size: 13px;
  color: var(--text-muted);
  margin: 0;
}
.page-header-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
}

/* 搜索框 */
.search-box {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 8px 14px;
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 20px;
  color: var(--text-muted);
  transition: border-color 0.15s ease, background 0.15s ease;
}
.search-box:focus-within {
  border-color: var(--accent);
  background: var(--bg-hover);
}
.search-box input {
  border: none;
  outline: none;
  background: transparent;
  color: var(--text-primary);
  font-size: 13px;
  font-family: inherit;
  width: 180px;
}
.search-box input::placeholder {
  color: var(--text-muted);
  opacity: 0.8;
}

.loading-wrap { padding: 40px 0; }

/* 历史分组 */
.history-list {
  display: flex;
  flex-direction: column;
  gap: 24px;
}
.history-group {
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.group-header {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 0 4px;
}
.group-header .group-label {
  font-size: 14px;
  font-weight: 600;
  color: var(--text-primary);
}
.group-header .group-count {
  font-size: 12px;
  color: var(--text-muted);
}

/* 卡片网格 */
.history-cards {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
  gap: 14px;
}

/* 单张卡片 */
.history-card {
  display: flex;
  gap: 14px;
  padding: 14px;
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 12px;
  cursor: pointer;
  transition: all 0.2s ease;
  position: relative;
}
.history-card:hover {
  border-color: var(--accent);
  transform: translateY(-2px);
  box-shadow: 0 4px 16px var(--accent-alpha-10);
}
.history-card.is-selected {
  border-color: var(--accent);
  background: var(--accent-alpha-5);
  box-shadow: 0 2px 12px var(--accent-alpha-10);
}
.history-card:hover .play-overlay { opacity: 1; }

.card-checkbox {
  position: absolute;
  top: 10px;
  right: 10px;
  z-index: 2;
  width: 24px;
  height: 24px;
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
}
.card-checkbox input {
  position: absolute;
  opacity: 0;
  width: 0;
  height: 0;
  pointer-events: none;
}
.check-mark {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 22px;
  height: 22px;
  border-radius: 6px;
  background: rgba(255, 255, 255, 0.9);
  border: 2px solid var(--accent);
  transition: all 0.15s ease;
}
.check-mark::after {
  content: '';
  display: none;
  width: 6px;
  height: 11px;
  border: solid var(--accent);
  border-width: 0 2.5px 2.5px 0;
  transform: rotate(45deg) translate(-1px, -1px);
}
.card-checkbox input:checked + .check-mark {
  background: var(--accent);
}
.card-checkbox input:checked + .check-mark::after {
  display: block;
  border-color: #ffffff;
}
.card-checkbox input:disabled + .check-mark {
  opacity: 0.5;
  cursor: not-allowed;
}

/* 封面 */
.poster {
  position: relative;
  width: 90px;
  height: 130px;
  flex-shrink: 0;
  border-radius: 8px;
  overflow: hidden;
  background: var(--bg-secondary);
  border: 1px solid var(--border);
  transition: border-color 0.2s ease;
}
.history-card:hover .poster { border-color: var(--accent); }
.poster img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
}
.poster-placeholder, .poster-loading {
  width: 100%;
  height: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--text-muted);
  opacity: 0.5;
}
.play-overlay {
  position: absolute;
  inset: 0;
  background: linear-gradient(to top, rgba(0,0,0,0.55) 0%, transparent 55%);
  display: flex;
  align-items: center;
  justify-content: center;
  color: #fff;
  opacity: 0;
  transition: opacity 0.2s ease;
}

/* 信息区 */
.info {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.info .title {
  font-size: 15px;
  font-weight: 600;
  color: var(--text-primary);
  line-height: 1.4;
  overflow: hidden;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  transition: color 0.15s ease;
}
.history-card:hover .info .title { color: var(--accent); }

.meta-row {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.time-row {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  font-size: 12px;
  color: var(--text-muted);
  margin-top: auto;
  padding-top: 4px;
}
</style>
