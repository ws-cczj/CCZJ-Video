<script setup lang="ts">
defineOptions({ name: 'Player' })
import { ref, computed, onMounted, onBeforeUnmount, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { GetRecentHistory, SaveWatchHistory, AddFavorite, RemoveFavorite, IsFavorite } from '../../bindings/cczjVideo/app'
import { useSourceStore } from '../stores/source'
import { useVideoStore } from '../stores/video'
import VideoPlayer from '../components/VideoPlayer.vue'
import Icon from '../components/Icon.vue'
import { Button, Modal } from '../components/ui'
import { resolveEpisodeUrl, stripHtmlTags } from '../utils'
import { TsCache } from '../utils/tsCache'
import { epProgressKey, loadEpProgress, saveEpProgress, getEpProgressPct } from '../utils/episodeProgress'
import { bumpFavoritesRefresh } from '../stores/favoritesSync'
import type { HistoryItem } from '../types'

const route = useRoute()
const router = useRouter()
const sourceStore = useSourceStore()
const videoStore = useVideoStore()

// ==================== 路由参数解析 ====================
const vodId = computed(() => {
  const p = route.params.vodId
  if (Array.isArray(p)) return p[0] || ''
  return String(p || route.query.id || '')
})

const sourceKey = computed(() => {
  const p = route.params.sourceKey
  if (Array.isArray(p)) return p[0] || ''
  return String(p || route.query.source || sourceStore.currentSourceKey || '')
})

const epIndexParam = computed(() => {
  const p = route.params.epIndex
  if (Array.isArray(p)) return parseInt(p[0] || '0', 10)
  return parseInt(String(p || route.query.ep || '0'), 10)
})

// ==================== 页面状态 ====================
const loading = ref(false)
const video = computed(() => videoStore.currentVideo)
const episodes = computed(() => videoStore.episodes)

/** 当前正在播放的集数（0-based） */
const currentEpIndex = ref(0)

/** 从右侧选集面板触发的集数切换信号（VideoPlayer 监听用） */
const _playToken = ref(0)

/** 当前集的播放地址 */
const currentUrl = computed(() => {
  const ep = episodes.value[currentEpIndex.value]
  return ep ? resolveEpisodeUrl(ep) : ''
})

/** 给视频组件一个"播放历史 key"，让进度记忆按集分开 */
const currentVideoKey = computed(() => {
  return `player_${String(vodId.value)}_${String(currentEpIndex.value)}`
})

const currentEpName = computed(() => {
  const ep = episodes.value[currentEpIndex.value]
  if (!ep) return ''
  if (ep.ep_name) return ep.ep_name
  const num = ep.ep_num ?? (currentEpIndex.value + 1)
  return '第' + String(num) + '集'
})

const hasPrev = computed(() => currentEpIndex.value > 0)
const hasNext = computed(() => currentEpIndex.value < episodes.value.length - 1)

/* ==================== TsCache 响应式（顶部栏片段进度） ====================
 * 每 250ms TsCache 可能有新片段缓存 → 触发此函数更新页面的 cached/total 显示
 */
const cacheReadTick = ref(0)
let _unsubTsCache: (() => void) | null = null

function refreshCacheUI(): void { cacheReadTick.value++ }

/* ==================== "已观看" 集数进度（从独立 localStorage 存储读取，1 个月自动淘汰） ====================
 * 键：`${sourceKey}-${vodId}-${epNum}`；值：{ position, duration?, updatedAt }
 * 每集独立一条记录，播放时持续写入，读取时自动淘汰过期记录。
 */
// 缓存每一集的进度百分比（0-100）
const epProgressMap = ref<Record<number, number>>({})

function epKeyOf(idx: number): string {
  return epProgressKey(sourceKey.value, vodId.value, episodes.value[idx]?.ep_num ?? idx)
}

// 从独立存储刷新 UI 可见的进度百分比表
function refreshEpProgressUI(): void {
  try {
    const store = loadEpProgress()
    const out: Record<number, number> = {}
    for (let i = 0; i < episodes.value.length; i++) {
      const k = epKeyOf(i)
      if (store[k]) out[i] = getEpProgressPct(store[k])
    }
    epProgressMap.value = out
  } catch { /* ignore */ }
}

// 播放开始时先执行一次
refreshEpProgressUI()

function getEpWatchPct(idx: number): number {
  return epProgressMap.value[idx] ?? 0
}
function isWatchedEp(idx: number): boolean { return getEpWatchPct(idx) > 0 }

let lastHistorySyncAt = 0
const HISTORY_SYNC_INTERVAL_MS = 10000

function syncHistoryToDb(position: number): void {
  const now = Date.now()
  if (now - lastHistorySyncAt < HISTORY_SYNC_INTERVAL_MS) return
  lastHistorySyncAt = now
  if (!vodId.value || currentEpIndex.value < 0) return
  const ep = episodes.value[currentEpIndex.value]
  if (!ep) return
  SaveWatchHistory({
    source_key: sourceKey.value,
    vod_id: String(vodId.value),
    vod_name: video.value?.vod_name || '',
    ep_num: ep.ep_num ?? (currentEpIndex.value + 1),
    position,
  } as any).catch(() => {})
}

// 写入当前集播放时持续写入独立存储（position/duration 会在用户播放过程中不断被写入
function updateCurrentEpProgress(position: number, duration?: number): void {
  if (currentEpIndex.value < 0) return
  const k = epKeyOf(currentEpIndex.value)
  saveEpProgress(k, position, duration)
  syncHistoryToDb(position)
  // 同时同步 UI
  const store = loadEpProgress()
  const out: Record<number, number> = { ...epProgressMap.value }
  out[currentEpIndex.value] = getEpProgressPct(store[k])
  epProgressMap.value = out
}

/* ==================== 鼠标位置跟踪（用于键盘快捷键作用域） ==================== */
const mouseInside = ref(false)
function onPageKeyDown(e: KeyboardEvent): void {
  // 键盘焦点在输入框时不响应
  const activeTag = (document.activeElement?.tagName || '').toLowerCase()
  if (activeTag === 'input' || activeTag === 'textarea') return

  // ESC 总是允许退出全屏（由 VideoPlayer 组件内部处理）
  // 其他键（空格、上下左右、f、m）需要鼠标位于播放器区域内才响应
  const v = document.querySelector('.native-video') as HTMLVideoElement | null
  if (!v) return

  switch (e.key) {
    case 'ArrowUp':
      if (!mouseInside.value) return
      e.preventDefault()
      v.volume = Math.min(1, v.volume + 0.05)
      break
    case 'ArrowDown':
      if (!mouseInside.value) return
      e.preventDefault()
      v.volume = Math.max(0, v.volume - 0.05)
      break
    case 'ArrowLeft':
      if (!mouseInside.value) return
      e.preventDefault()
      v.currentTime = Math.max(0, v.currentTime - 5)
      break
    case 'ArrowRight':
      if (!mouseInside.value) return
      e.preventDefault()
      v.currentTime = Math.min(v.duration || 0, v.currentTime + 5)
      break
    case ' ':
    case 'k':
    case 'K':
      if (!mouseInside.value) return
      e.preventDefault()
      if (v.paused) v.play()
      else v.pause()
      break
    case 'm':
    case 'M':
      if (!mouseInside.value) return
      e.preventDefault()
      v.muted = !v.muted
      break
    case 'f':
    case 'F':
      if (!mouseInside.value) return
      e.preventDefault()
      if (!document.fullscreenElement) v.requestFullscreen?.()
      else document.exitFullscreen?.()
      break
    case 'Escape':
      // 保留给系统和 VideoPlayer 内部使用（退出全屏）
      break
  }
}

// 监听视频元素的 timeupdate 和 durationchange（用于进度条）
function bindVideoTimeTracking(): void {
  const v = document.querySelector('.native-video') as HTMLVideoElement | null
  if (!v) return
  v.addEventListener('timeupdate', () => {
    updateCurrentEpProgress(v.currentTime, v.duration)
  })
  v.addEventListener('loadedmetadata', () => {
    updateCurrentEpProgress(v.currentTime, v.duration)
  })
}

/* ==================== 收藏状态 ==================== */
const isFav = ref(false)
const favBusy = ref(false)
const showFavFolderModal = ref(false)

interface FavFolder { id: string; name: string; default: boolean }
const favFolders = ref<FavFolder[]>([
  { id: 'default', name: '默认收藏夹', default: true },
])
const favTargetFolderId = ref<string>('default')

function loadFavFolders(): void {
  try {
    const raw = localStorage.getItem('cczj_fav_folders')
    if (raw) {
      const parsed = JSON.parse(raw) as FavFolder[]
      if (Array.isArray(parsed) && parsed.length > 0) {
        favFolders.value = parsed
        return
      }
    }
  } catch { /* ignore */ }
  favFolders.value = [{ id: 'default', name: '默认收藏夹', default: true }]
}

async function refreshFav(): Promise<void> {
  if (!vodId.value) return
  try {
    const val = await IsFavorite({ source_key: sourceKey.value, vod_id: String(vodId.value) }) as boolean
    isFav.value = !!val
  } catch { /* ignore */ }
}
async function toggleFavorite(): Promise<void> {
  if (!vodId.value || favBusy.value) return
  if (isFav.value) {
    favBusy.value = true
    try {
      await RemoveFavorite({ source_key: sourceKey.value, vod_id: String(vodId.value) })
      const key = `${sourceKey.value}-${vodId.value}`
      try {
        const raw = localStorage.getItem('cczj_fav_mapping')
        if (raw) {
          const obj = JSON.parse(raw) as Record<string, string>
          delete obj[key]
          localStorage.setItem('cczj_fav_mapping', JSON.stringify(obj))
        }
      } catch { /* ignore */ }
      isFav.value = false
      bumpFavoritesRefresh()
    } catch { /* ignore */ }
    finally { favBusy.value = false }
    return
  }
  loadFavFolders()
  favTargetFolderId.value = 'default'
  showFavFolderModal.value = true
}

async function confirmAddToFolder(): Promise<void> {
  if (!vodId.value) return
  favBusy.value = true
  showFavFolderModal.value = false
  try {
    await AddFavorite({
      source_key: sourceKey.value,
      vod_id: String(vodId.value),
      vod_name: video.value?.vod_name || '',
    } as any)
    try {
      const key = `${sourceKey.value}-${vodId.value}`
      const raw = localStorage.getItem('cczj_fav_mapping')
      const obj: Record<string, string> = raw ? JSON.parse(raw) : {}
      obj[key] = favTargetFolderId.value
      localStorage.setItem('cczj_fav_mapping', JSON.stringify(obj))
    } catch { /* ignore */ }
    isFav.value = true
    bumpFavoritesRefresh()
  } catch { /* ignore */ }
  finally { favBusy.value = false }
}

/* ==================== 记录观看历史（点击就记录，无需等 30 秒 ==================== */
function recordHistory(idx: number): void {
  if (!vodId.value) return
  const ep = episodes.value[idx]
  if (!ep) return
  const epNum = ep.ep_num ?? (idx + 1)
  const progKey = epProgressKey(sourceKey.value, vodId.value, epNum)
  const entry = loadEpProgress()[progKey]
  const position = entry?.position ?? 0
  lastHistorySyncAt = Date.now()
  try {
    SaveWatchHistory({
      source_key: sourceKey.value,
      vod_id: String(vodId.value),
      vod_name: video.value?.vod_name || '',
      ep_num: epNum,
      position,
    } as any).catch(() => {})
  } catch { /* ignore */ }
}

// ==================== 简介文本（去除 HTML 标签） ====================
const overviewText = computed(() => stripHtmlTags(video.value?.vod_content || ''))

// ==================== 集数切换 & 路由更新 ====================
function goToEpisode(idx: number): void {
  if (idx < 0 || idx >= episodes.value.length) return
  if (idx === currentEpIndex.value) return
  recordHistory(idx)
  currentEpIndex.value = idx
  _playToken.value++
  try { TsCache.setCurrentEpisode(idx) } catch {}
  router.replace(`/player/${sourceKey.value}/${vodId.value}/${idx}`).catch(() => {})
}
function prevEpisode(): void { if (hasPrev.value) goToEpisode(currentEpIndex.value - 1) }
function nextEpisode(): void { if (hasNext.value) goToEpisode(currentEpIndex.value + 1) }

function goBack(): void {
  try { TsCache.clear() } catch {}
  router.back()
}

// ==================== 顶部栏读取当前集预取进度 ====================
function getCurrentEpCached(): { cached: number; total: number } {
  cacheReadTick.value // 订阅：触发响应式重算
  try { return TsCache.episodeProgress(currentEpIndex.value) } catch { return { cached: 0, total: 0 } }
}
function getNextEpCached(): { cached: number; total: number } {
  cacheReadTick.value
  try { return TsCache.episodeProgress(currentEpIndex.value + 1) } catch { return { cached: 0, total: 0 } }
}
function getHitRate(): number {
  cacheReadTick.value
  try { return TsCache.stats().hitRate } catch { return 0 }
}

// ==================== 加载流程 ====================
async function loadData(): Promise<void> {
  if (!sourceKey.value || !vodId.value) return
  loading.value = true
  try {
    const currentVodId = video.value?.vod_id
    if (String(currentVodId || '') !== String(vodId.value)) {
      await videoStore.loadDetail(sourceKey.value, vodId.value)
    }
    if (!video.value) { loading.value = false; return }

    let targetIdx = 0
    if (!isNaN(epIndexParam.value) && epIndexParam.value >= 0 && epIndexParam.value < episodes.value.length) {
      targetIdx = epIndexParam.value
    } else {
      try {
        const history = (await GetRecentHistory(1)) as HistoryItem[] | null | undefined
        if (Array.isArray(history) && history.length > 0) {
          const last = history[0]
          if (String(last.vod_id) === String(vodId.value)) {
            const idx = episodes.value.findIndex((e) => Number(e.ep_num) === Number(last.ep_num))
            if (idx >= 0) targetIdx = idx
          }
        }
      } catch {}
    }
    currentEpIndex.value = targetIdx
    _playToken.value++

    try {
      TsCache.setEpisodes(
        episodes.value.map((ep) => ({
          source_key: sourceKey.value,
          vod_id: String(vodId.value),
          ep_url: resolveEpisodeUrl(ep),
          ep_name: ep.ep_name || '',
          ep_num: ep.ep_num ?? 0,
        })),
      )
      TsCache.setCurrentEpisode(targetIdx)
    } catch {}
  } catch {}
  finally { loading.value = false }
}

watch(currentEpIndex, (idx) => {
  // 播放中 → 把该集加入进度（即使没有具体位置，也标记为"开始看过"）
  if (idx >= 0 && !isWatchedEp(idx)) {
    const k = epKeyOf(idx)
    saveEpProgress(k, 0, undefined)
    refreshEpProgressUI()
  }
  try { TsCache.setCurrentEpisode(idx) } catch {}
})

// 页面可见性变化时刷新进度（从其他页面返回时同步）
function onVisibilityChange(): void {
  if (document.visibilityState === 'visible') {
    refreshEpProgressUI()
  }
}

onMounted(async () => {
  await sourceStore.loadSources()
  await loadData()
  // 首集（或恢复的上一集）也记录历史
  if (currentEpIndex.value >= 0 && episodes.value.length > 0) {
    recordHistory(currentEpIndex.value)
  }
  // 从后端加载已观看集数（含 position）作为补充
  try {
    const history = await GetRecentHistory(50) as any[]
    if (Array.isArray(history) && history.length > 0) {
      const epNumToIdx = new Map<number, number>()
      for (let i = 0; i < episodes.value.length; i++) {
        const num = Number(episodes.value[i].ep_num)
        if (!isNaN(num)) epNumToIdx.set(num, i)
      }
      for (const h of history) {
        if (String(h.vod_id) === String(vodId.value) && h.ep_num != null) {
          const idx = epNumToIdx.get(Number(h.ep_num))
          if (idx !== undefined) {
            const k = epKeyOf(idx)
            // 仅在本地无数据时用后端历史作为占位（避免覆盖用户实际播放进度）
            const local = loadEpProgress()
            if (!local[k]) saveEpProgress(k, Number(h.position) || 0, undefined)
          }
        }
      }
      refreshEpProgressUI()
    }
    // 当前集至少也要"已观看过"
    if (currentEpIndex.value >= 0 && !isWatchedEp(currentEpIndex.value)) {
      saveEpProgress(epKeyOf(currentEpIndex.value), 0, undefined)
      refreshEpProgressUI()
    }
  } catch { /* ignore */ }
  // 刷新收藏状态
  refreshFav().catch(() => {})
  try {
    _unsubTsCache = TsCache.onStateChange(refreshCacheUI)
  } catch {}

  // 页面级键盘监听
  document.addEventListener('keydown', onPageKeyDown)

  // 视频时间跟踪（延迟到 VideoPlayer 挂载后）
  setTimeout(() => { bindVideoTimeTracking() }, 200)

  // 页面可见性变化时刷新进度（从其他页面返回时同步）
  document.addEventListener('visibilitychange', onVisibilityChange)
})

onBeforeUnmount(() => {
  document.removeEventListener('keydown', onPageKeyDown)
  document.removeEventListener('visibilitychange', onVisibilityChange)
  if (_unsubTsCache) {
    try { _unsubTsCache() } catch {}
    _unsubTsCache = null
  }
})

/* ==================== 小工具：生成集数显示 ==================== */
function epLabel(i: number, ep: { ep_num?: number; ep_name?: string }): string {
  if (ep.ep_name) return ep.ep_name
  const num = ep.ep_num ?? (i + 1)
  return '第' + String(num) + '集'
}
</script>

<template>
  <div class="player-page" @mouseenter="mouseInside = true" @mouseleave="mouseInside = false">
    <div v-if="loading" class="player-loading">
      <div class="spinner"></div>
      <span>加载中...</span>
    </div>

    <template v-else-if="currentUrl">
      <div class="player-layout">
        <!-- ============= 左侧视频区 ============= -->
        <div
          class="player-col-main"
          @mouseenter="mouseInside = true"
          @dblclick.stop
        >
          <VideoPlayer
            :url="currentUrl"
            :autoplay="true"
            :has-prev="hasPrev"
            :has-next="hasNext"
            :video-key="currentVideoKey"
            :show-title-bar="true"
            :title="currentEpName"
            :is-fav="isFav"
            :fav-busy="favBusy"
            @toggle-favorite="toggleFavorite"
            @back="goBack"
            @prev="prevEpisode"
            @next="nextEpisode"
            :force-play-token="_playToken"
          />

          <!-- 底部浮层：切集提示（显示"即将播放：下一集"） -->
          <transition name="fade">
            <div v-if="hasNext && getNextEpCached().cached > 0" class="prefetch-hint">
              <span class="prefetch-icon">⬇</span>
              <span>下一集已预取 {{ getNextEpCached().cached }} 片，切换即可秒开</span>
            </div>
          </transition>
        </div>

        <!-- ============= 右侧选集面板 ============= -->
        <aside class="player-col-side">
          <!-- 顶部卡：标题 + 年份/地区/分类 + 简介 + 收藏按钮 -->
          <div class="side-header">
            <div class="side-top-bar">
              <h1 class="side-title">{{ video?.vod_name || '视频播放' }}</h1>
              <Button variant="text" size="sm" class="close-btn-panel" @click="router.back()" title="关闭">
                <Icon name="x" :size="18" />
              </Button>
            </div>
            <div class="side-meta">
              <span v-if="video?.vod_year" class="side-meta-chip">{{ video.vod_year }}</span>
              <span v-if="video?.vod_area" class="side-meta-chip">{{ video.vod_area }}</span>
              <span v-if="video?.type_name" class="side-meta-chip">{{ video.type_name }}</span>
            </div>
            <p v-if="overviewText" class="side-blurb">{{ overviewText }}</p>
          </div>

          <!-- 选集区 -->
          <section class="side-section">
            <div class="side-section-title">
              <div class="side-section-title-left">
                <span class="bullet"></span>
                <span>选集</span>
              </div>
              <div class="side-section-right">
                <span class="side-count">共 {{ episodes.length }} 集</span>
                <span
                  v-if="getCurrentEpCached().total > 0"
                  class="side-cache-chip"
                  :title="'当前集已预取 ' + getCurrentEpCached().cached + ' / ' + getCurrentEpCached().total + ' 片段；命中率 ' + Math.round(getHitRate() * 100) + '%'"
                >
                  <span class="side-cache-dot"></span>
                  预取 {{ getCurrentEpCached().cached }}/{{ getCurrentEpCached().total }} · {{ Math.round(getHitRate() * 100) }}%
                </span>
              </div>
            </div>

            <div v-if="episodes.length === 0" class="side-empty">暂无可播放剧集</div>

            <div v-else class="ep-grid">
              <button
                v-for="(ep, i) in episodes"
                :key="String(i)"
                class="ep-item"
                :class="{
                  active: i === currentEpIndex,
                  watched: isWatchedEp(i),
                  future: i > currentEpIndex && !isWatchedEp(i),
                }"
                @click="goToEpisode(i)"
                :title="epLabel(i, ep) + (getEpWatchPct(i) > 0 ? ' · 已观看 ' + Math.round(getEpWatchPct(i)) + '%' : '')"
              >
                <span class="ep-item-num">{{ epLabel(i, ep) }}</span>

                <!-- ⭐ 播放中动画徽章 -->
                <span v-if="i === currentEpIndex" class="ep-playing-badge">
                  <span class="bar b1"></span>
                  <span class="bar b2"></span>
                  <span class="bar b3"></span>
                </span>

                <!-- ⭐ 已观看：底部进度条填充 + 右上角百分比 -->
                <span
                  v-if="isWatchedEp(i) && getEpWatchPct(i) > 0"
                  class="ep-watched-progress"
                  :style="{ width: getEpWatchPct(i) + '%' }"
                ></span>
                <span
                  v-if="isWatchedEp(i) && getEpWatchPct(i) > 0"
                  class="ep-watched-pct"
                >{{ Math.round(getEpWatchPct(i)) }}%</span>
              </button>
            </div>
          </section>
        </aside>
      </div>
    </template>

    <div v-else class="player-error-page">
      <div class="error-msg">暂无播放资源</div>
      <Button variant="text" size="md" @click="goBack"><span>返回</span></Button>
    </div>

    <!-- 收藏夹选择弹窗 -->
    <Modal
      :model-value="showFavFolderModal"
      title="收藏到文件夹"
      width="420px"
      :show-footer="true"
      @update:model-value="(v: boolean) => !v && (showFavFolderModal = false)"
    >
      <div class="folder-select-list">
        <label
          v-for="folder in favFolders"
          :key="folder.id"
          class="folder-select-item"
          :class="{ active: favTargetFolderId === folder.id }"
        >
          <input type="radio" v-model="favTargetFolderId" :value="folder.id" />
          <span class="folder-radio" />
          <Icon :name="folder.default ? 'star' : 'list'" :size="14" />
          <span class="folder-name">{{ folder.name }}</span>
        </label>
      </div>
      <template #footer>
        <Button variant="secondary" size="md" @click="showFavFolderModal = false">取消</Button>
        <Button variant="primary" size="md" @click="confirmAddToFolder">确认收藏</Button>
      </template>
    </Modal>
  </div>
</template>

<style scoped>
/* ============ 根层：铺满整屏 ============== */
.player-page {
  position: fixed;
  inset: 0;
  background: #0b0d10;
  color: #fff;
  overflow: hidden;
  z-index: 9999;
}

/* ============ 布局：左侧视频，右侧选集 ============== */
.player-layout {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: stretch;
  background: radial-gradient(ellipse at 30% 30%, #12161b 0%, #0b0d10 60%, #060709 100%);
}

.player-col-main {
  flex: 1 1 auto;
  min-width: 0;
  min-height: 0;
  position: relative;
  background: #000;
  display: flex;
  align-items: stretch;
  justify-content: stretch;
  padding: 0;
  margin: 0;
}
.player-col-main > * {
  width: 100%;
  height: 100%;
  flex: 1 1 auto;
}

.player-col-side {
  flex: 0 0 360px;
  max-width: 380px;
  background: linear-gradient(180deg, #111419 0%, #0b0d10 100%);
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

/* 响应式：窄屏收起右侧 */
@media (max-width: 960px) {
  .player-col-side { display: none; }
}

/* ============ 右侧：标题/简介区 ============== */
.side-header {
  padding: 24px 22px 18px;
  border-bottom: 1px solid rgba(255, 255, 255, 0.05);
  background: linear-gradient(180deg, rgba(24, 144, 255, 0.04) 0%, transparent 100%);
}

.side-top-bar {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 12px;
  margin-bottom: 10px;
}

.side-title {
  margin: 0;
  flex: 1;
  font-size: 20px;
  font-weight: 700;
  letter-spacing: 0.3px;
  line-height: 1.35;
  color: #fff;
  text-shadow: 0 1px 8px rgba(0, 0, 0, 0.35);
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
}

.close-btn-panel {
  flex-shrink: 0;
  width: 30px;
  height: 30px;
  border-radius: 8px;
  border: 1px solid rgba(255, 255, 255, 0.12);
  background: rgba(255, 255, 255, 0.04);
  color: rgba(255, 255, 255, 0.55);
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: all 0.2s ease;
}
.close-btn-panel:hover {
  color: #ff4d4f;
  border-color: rgba(255, 77, 79, 0.5);
  background: rgba(255, 77, 79, 0.12);
}

.fav-btn-text {
  width: 100%;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  padding: 9px 14px;
  margin-bottom: 12px;
  border-radius: 10px;
  border: 1px solid rgba(255, 255, 255, 0.1);
  background: rgba(255, 255, 255, 0.05);
  color: rgba(255, 255, 255, 0.75);
  cursor: pointer;
  font-size: 13px;
  font-weight: 500;
  font-family: inherit;
  transition: all 0.2s ease;
}
.fav-btn-text .fav-icon {
  font-size: 14px;
}
.fav-btn-text:hover {
  background: rgba(255, 255, 255, 0.09);
  color: #fff;
}
.fav-btn-text.active {
  color: #ffc107;
  border-color: rgba(255, 193, 7, 0.55);
  background: rgba(255, 193, 7, 0.12);
}
.fav-btn-text.busy {
  opacity: 0.6;
  pointer-events: none;
}

.side-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-bottom: 12px;
}
.side-meta-chip {
  display: inline-block;
  padding: 3px 10px;
  font-size: 11px;
  color: #9aa4b2;
  background: rgba(255, 255, 255, 0.04);
  border: 1px solid rgba(255, 255, 255, 0.06);
  border-radius: 12px;
}

.side-blurb {
  margin: 0;
  font-size: 12.5px;
  line-height: 1.7;
  color: rgba(255, 255, 255, 0.6);
  max-height: 88px;
  overflow: hidden;
  display: -webkit-box;
  -webkit-line-clamp: 4;
  -webkit-box-orient: vertical;
}

/* ============ 右侧：选集区 ============== */
.side-section {
  flex: 1 1 auto;
  min-height: 0;
  display: flex;
  flex-direction: column;
  padding: 14px 22px 22px;
}

.side-section-title {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
  margin-bottom: 12px;
}
.side-section-title-left {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
  font-weight: 600;
  color: #fff;
  letter-spacing: 0.3px;
}
.side-section-title-left .bullet {
  width: 3px;
  height: 14px;
  background: linear-gradient(180deg, #40a9ff, #1890ff);
  border-radius: 2px;
}
.side-section-right {
  display: flex;
  align-items: center;
  gap: 8px;
}
.side-count {
  font-size: 11.5px;
  color: rgba(255, 255, 255, 0.45);
}
.side-cache-chip {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 3px 8px;
  font-size: 10.5px;
  color: #7ee3b6;
  background: rgba(126, 227, 182, 0.1);
  border: 1px solid rgba(126, 227, 182, 0.25);
  border-radius: 10px;
  white-space: nowrap;
}
.side-cache-chip .side-cache-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: #7ee3b6;
  box-shadow: 0 0 6px #7ee3b6;
  animation: glow-pulse 1.4s ease-in-out infinite;
}
@keyframes glow-pulse {
  0%, 100% { opacity: 0.6; }
  50% { opacity: 1; }
}

.side-empty {
  color: rgba(255, 255, 255, 0.45);
  font-size: 13px;
  padding: 20px 0;
}

/* ============ 集数网格：美化核心 ============ */
.ep-grid {
  flex: 1 1 auto;
  overflow-y: auto;
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 10px;
  padding-right: 4px;
  align-content: start;
  scrollbar-width: thin;
  scrollbar-color: rgba(255, 255, 255, 0.18) transparent;
}
.ep-grid::-webkit-scrollbar { width: 6px; }
.ep-grid::-webkit-scrollbar-thumb {
  background: rgba(255, 255, 255, 0.14);
  border-radius: 3px;
}
.ep-grid::-webkit-scrollbar-thumb:hover { background: rgba(255, 255, 255, 0.28); }
.ep-grid::-webkit-scrollbar-track { background: transparent; }

/* 单个集数卡片：默认态（未观看 & 非当前集）——默认显示明显 */
.ep-item {
  position: relative;
  font-family: inherit;
  min-height: 48px;
  height: 48px;
  padding: 0 8px;
  border-radius: 10px;
  border: 1px solid rgba(64, 169, 255, 0.55);
  background: linear-gradient(180deg, rgba(24, 144, 255, 0.16) 0%, rgba(64, 169, 255, 0.08) 100%);
  color: #fff;
  font-size: 12.5px;
  font-weight: 500;
  cursor: pointer;
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 2px;
  transition: transform 0.15s ease,
              background 0.15s ease,
              border-color 0.15s ease,
              box-shadow 0.15s ease,
              color 0.15s ease,
              opacity 0.15s ease;
}
.ep-item-num {
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
/* hover：收住（次要态） */
.ep-item:hover {
  background: linear-gradient(180deg, rgba(255, 255, 255, 0.035) 0%, rgba(255, 255, 255, 0.015) 100%);
  border-color: rgba(255, 255, 255, 0.12);
  color: #d9dde3;
  transform: none;
  box-shadow: none;
}
.ep-item:active { transform: translateY(0); }

/* 未来集（在当前集之后，且未观看）：正常中性，不灰 */
.ep-item.future { opacity: 1; }

/* 已观看：绿色高亮（明显态） */
.ep-item.watched {
  background: linear-gradient(180deg, rgba(76, 175, 80, 0.18) 0%, rgba(139, 195, 74, 0.10) 100%);
  border-color: rgba(76, 175, 80, 0.55);
  color: #e8f5e9;
  cursor: pointer;
  font-weight: 500;
}
.ep-item.watched:hover {
  background: linear-gradient(180deg, rgba(76, 175, 80, 0.08) 0%, rgba(76, 175, 80, 0.04) 100%);
  border-color: rgba(76, 175, 80, 0.25);
  color: rgba(255, 255, 255, 0.75);
  transform: none;
  box-shadow: none;
}

/* 已观看：底部进度条 + 右上角百分比 */
.ep-watched-progress {
  position: absolute;
  left: 0;
  bottom: 0;
  height: 4px;
  background: linear-gradient(90deg, #4caf50 0%, #8bc34a 100%);
  z-index: 1;
  pointer-events: none;
}
.ep-watched-pct {
  position: absolute;
  top: 3px;
  right: 4px;
  font-size: 9px;
  font-weight: 600;
  color: #4caf50;
  background: rgba(0, 0, 0, 0.45);
  padding: 1px 4px;
  border-radius: 3px;
  line-height: 1.3;
  z-index: 2;
  pointer-events: none;
}
/* 当前播放集：百分比移到左上角，避免与播放中徽章重叠 */
.ep-item.active .ep-watched-pct {
  top: 3px;
  right: auto;
  left: 4px;
  color: #69c0ff;
  background: rgba(0, 0, 0, 0.55);
}
/* 当前播放集：进度条颜色调整为适配蓝色主题 */
.ep-item.active .ep-watched-progress {
  background: linear-gradient(90deg, #40a9ff 0%, #69c0ff 100%);
}

/* 当前播放的集：发光边框 + 内部动画 + 彩色背景 */
.ep-item.active {
  background: linear-gradient(135deg, rgba(24, 144, 255, 0.22) 0%, rgba(64, 169, 255, 0.14) 60%, rgba(100, 181, 246, 0.1) 100%);
  border-color: rgba(64, 169, 255, 0.8);
  color: #fff;
  font-weight: 700;
  box-shadow:
    0 0 0 1px rgba(64, 169, 255, 0.4),
    0 0 14px rgba(24, 144, 255, 0.55),
    0 4px 14px rgba(24, 144, 255, 0.35);
  transform: translateY(-1px);
  animation: active-ep-pulse 1.6s ease-in-out infinite;
}
@keyframes active-ep-pulse {
  0%, 100% {
    box-shadow:
      0 0 0 1px rgba(64, 169, 255, 0.35),
      0 0 10px rgba(24, 144, 255, 0.45),
      0 4px 12px rgba(24, 144, 255, 0.25);
  }
  50% {
    box-shadow:
      0 0 0 1px rgba(100, 181, 246, 0.6),
      0 0 18px rgba(24, 144, 255, 0.7),
      0 4px 18px rgba(24, 144, 255, 0.5);
  }
}

/* 当前集卡片右上角："播放中"三条竖线律动 */
.ep-playing-badge {
  position: absolute;
  top: 6px;
  right: 6px;
  display: inline-flex;
  align-items: flex-end;
  gap: 2px;
  height: 14px;
}
.ep-playing-badge .bar {
  display: inline-block;
  width: 2px;
  height: 6px;
  border-radius: 2px;
  background: #40a9ff;
  box-shadow: 0 0 4px #40a9ff;
  animation: bar-bounce 0.9s ease-in-out infinite;
}
.ep-playing-badge .b1 { animation-delay: -0.2s; }
.ep-playing-badge .b2 { animation-delay: -0.5s; }
.ep-playing-badge .b3 { animation-delay: -0.8s; }
@keyframes bar-bounce {
  0%, 100% { height: 4px; }
  50% { height: 12px; background: #69c0ff; box-shadow: 0 0 6px #69c0ff; }
}

/* 左下角：下一集预取提示 */
.prefetch-hint {
  position: absolute;
  left: 20px;
  bottom: 20px;
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 8px 14px;
  background: rgba(20, 22, 26, 0.75);
  backdrop-filter: blur(8px);
  border: 1px solid rgba(126, 227, 182, 0.35);
  border-radius: 10px;
  color: #c9f0dc;
  font-size: 12px;
  font-weight: 500;
  pointer-events: none;
  z-index: 10;
  box-shadow: 0 4px 16px rgba(0, 0, 0, 0.35);
}
.prefetch-icon {
  color: #7ee3b6;
  font-size: 14px;
  text-shadow: 0 0 6px #7ee3b6;
  animation: float-down 1.4s ease-in-out infinite;
}
@keyframes float-down {
  0%, 100% { transform: translateY(0); opacity: 0.7; }
  50% { transform: translateY(2px); opacity: 1; }
}

/* loading / error */
.player-loading {
  position: absolute;
  inset: 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 16px;
  color: #8892a0;
  font-size: 14px;
}
.player-loading .spinner {
  width: 44px;
  height: 44px;
  border: 3px solid rgba(255, 255, 255, 0.08);
  border-top-color: #1890ff;
  border-radius: 50%;
  animation: spin 0.8s linear infinite;
}
@keyframes spin {
  to { transform: rotate(360deg); }
}

.player-error-page {
  position: absolute;
  inset: 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 20px;
  color: #aaa;
}
.player-error-page .error-msg {
  font-size: 15px;
  color: #e8e8e8;
}
.player-error-page .back-btn {
  padding: 8px 20px;
  border-radius: 20px;
  background: rgba(24, 144, 255, 0.15);
  color: #40a9ff;
  border: 1px solid rgba(24, 144, 255, 0.35);
  font-size: 13px;
  cursor: pointer;
  font-family: inherit;
}
.player-error-page .back-btn:hover { background: rgba(24, 144, 255, 0.25); }

/* ============ fade transition（vue built-in） ============ */
.fade-enter-active, .fade-leave-active {
  transition: opacity 0.35s ease, transform 0.35s ease;
}
.fade-enter-from, .fade-leave-to {
  opacity: 0;
  transform: translateY(4px);
}

/* ============ 收藏夹弹窗 ============ */
.modal-backdrop {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.55);
  backdrop-filter: blur(6px);
  z-index: 100;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
  animation: fadeIn 0.18s ease;
}
@keyframes fadeIn { from { opacity: 0 } to { opacity: 1 } }
@keyframes scaleIn { from { opacity: 0; transform: scale(0.96) } to { opacity: 1; transform: scale(1) } }

.modal-box {
  width: 100%;
  max-width: 480px;
  background: #14181f;
  border: 1px solid rgba(255, 255, 255, 0.08);
  border-radius: 14px;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  box-shadow: 0 10px 40px rgba(0, 0, 0, 0.3);
  animation: scaleIn 0.2s cubic-bezier(0.4, 0, 0.2, 1);
}
.modal-box.small { max-width: 420px; }

.modal-head {
  padding: 14px 18px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  border-bottom: 1px solid rgba(255, 255, 255, 0.06);
}
.modal-title {
  margin: 0;
  font-size: 15px;
  font-weight: 600;
  color: #fff;
  display: inline-flex;
  align-items: center;
  gap: 8px;
}
.modal-close {
  background: transparent;
  border: none;
  color: rgba(255, 255, 255, 0.5);
  cursor: pointer;
  padding: 4px;
  border-radius: 6px;
  line-height: 0;
  transition: all 0.15s ease;
}
.modal-close:hover { background: rgba(255, 255, 255, 0.06); color: #fff; }

.modal-body { padding: 18px; }
.modal-foot {
  padding: 12px 18px;
  border-top: 1px solid rgba(255, 255, 255, 0.06);
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}

.btn-ghost {
  padding: 8px 20px;
  border-radius: 8px;
  border: 1px solid rgba(255, 255, 255, 0.12);
  background: transparent;
  color: rgba(255, 255, 255, 0.65);
  cursor: pointer;
  font-size: 13px;
  font-family: inherit;
  transition: all 0.15s ease;
}
.btn-ghost:hover {
  background: rgba(255, 255, 255, 0.06); color: #fff; border-color: rgba(64, 169, 255, 0.45);
}

.btn-primary {
  padding: 8px 20px;
  border-radius: 8px;
  border: none;
  background: #1890ff;
  color: #fff;
  cursor: pointer;
  font-size: 13px;
  font-weight: 600;
  font-family: inherit;
  transition: all 0.15s ease;
}
.btn-primary:hover {
  background: #40a9ff;
  transform: translateY(-1px);
  box-shadow: 0 4px 12px rgba(24, 144, 255, 0.35);
}

/* 文件夹选择列表 */
.folder-select-list { display: flex; flex-direction: column; gap: 8px; }
.folder-select-item {
  display: flex; align-items: center; gap: 10px;
  padding: 10px 14px; border-radius: 8px;
  border: 1px solid var(--border);
  background: var(--bg-secondary);
  cursor: pointer;
  font-size: 13px; color: var(--text-primary);
  transition: all 0.15s ease;
  position: relative;
}
.folder-select-item:hover { border-color: var(--accent); background: var(--accent-alpha-10); }
.folder-select-item.active {
  border-color: var(--accent);
  background: var(--accent);
  color: var(--accent-contrast);
  font-weight: 600;
}
.folder-select-item input {
  position: absolute;
  opacity: 0;
  width: 0;
  height: 0;
  pointer-events: none;
}
.folder-radio {
  flex-shrink: 0;
  width: 16px;
  height: 16px;
  border-radius: 50%;
  border: 2px solid var(--border-strong);
  background: var(--bg-card);
  transition: all 0.15s ease;
  position: relative;
}
.folder-select-item.active .folder-radio {
  border-color: var(--accent-contrast);
  background: var(--accent-contrast);
}
.folder-select-item.active .folder-radio::after {
  content: '';
  position: absolute;
  inset: 3px;
  border-radius: 50%;
  background: var(--accent);
}
.folder-name { flex: 1; min-width: 0; }
</style>
