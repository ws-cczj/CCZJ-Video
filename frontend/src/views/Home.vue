<script setup lang="ts">
defineOptions({ name: 'Home' })
import { ref, computed, onMounted, onActivated, onBeforeUnmount, watch, watchEffect, defineComponent, h } from 'vue'
import { useRouter } from 'vue-router'
import { GetRecentHistory, DeleteHistoryByVideo } from '../../bindings/cczjVideo/app'
import { useSourceStore } from '../stores/source'
import { useVideoStore, type VideoFilter } from '../stores/video'
import VideoCard from '../components/VideoCard.vue'
import { Button, Tag, Spinner as LoadingSpinner, Empty as EmptyState, Select as SelectDropdown } from '../components/ui'
import BookCarousel from '../components/BookCarousel.vue'
import Icon from '../components/Icon.vue'
import { getDetailPath } from '../utils'
import type { Video } from '../types'

const router = useRouter()
const sourceStore = useSourceStore()
const videoStore = useVideoStore()

// ==================== 筛选状态 ====================
interface ActiveFilters {
  typeId: string // 'all' 或具体 type_id
  year: string   // 'all' 或具体年份
  area: string   // 'all' 或具体地区
  sort: string   // 'default' | 'rating' | 'hot'
}

const activeFilters = ref<ActiveFilters>({
  typeId: 'all',
  year: 'all',
  area: 'all',
  sort: 'default',
})

// 单个展开/收起按钮：未展开时只显示类型，展开后同时显示年份与地区
const expanded = ref(false)

function applyFilters(): void {
  if (!sourceStore.currentSourceKey) return
  const f: VideoFilter = {
    type_id: activeFilters.value.typeId === 'all' ? '' : activeFilters.value.typeId,
    year: activeFilters.value.year === 'all' ? '' : activeFilters.value.year,
    area: activeFilters.value.area === 'all' ? '' : activeFilters.value.area,
    keyword: '',
    sort: activeFilters.value.sort === 'default' ? '' : activeFilters.value.sort,
  }
  videoStore.loadVideos(sourceStore.currentSourceKey, f, 1, 50)
}

function resetFilters(): void {
  activeFilters.value = { typeId: 'all', year: 'all', area: 'all', sort: 'default' }
  expanded.value = false
  if (sourceStore.currentSourceKey) {
    videoStore.loadVideos(sourceStore.currentSourceKey, {
      type_id: '', year: '', area: '', keyword: '', sort: '',
    }, 1, 50)
  }
}

function setSort(sort: string): void {
  if (activeFilters.value.sort === sort) return
  activeFilters.value.sort = sort
  applyFilters()
}

function selectChip(category: 'type' | 'year' | 'area', value: string): void {
  if (category === 'type') activeFilters.value.typeId = value
  if (category === 'year') activeFilters.value.year = value
  if (category === 'area') activeFilters.value.area = value
  applyFilters()
}

const hasActiveFilter = computed<boolean>(() =>
  activeFilters.value.typeId !== 'all' ||
  activeFilters.value.year !== 'all' ||
  activeFilters.value.area !== 'all' ||
  activeFilters.value.sort !== 'default'
)

// 类型 chip 列表
const typeChipList = computed(() => {
  const base: Array<{ value: string; label: string }> = [{ value: 'all', label: '全部' }]
  if (videoStore.types && videoStore.types.length > 0) {
    for (const t of videoStore.types) {
      base.push({ value: String(t.type_id ?? ''), label: t.name || String(t.type_id ?? '') })
    }
  }
  return base
})

// 年份 chip 列表（倒序：新的在前）
const yearChipList = computed(() => {
  const base: Array<{ value: string; label: string }> = [{ value: 'all', label: '全部年份' }]
  const years = [...(videoStore.years || [])].sort((a, b) => {
    const na = Number(a)
    const nb = Number(b)
    if (!Number.isNaN(na) && !Number.isNaN(nb)) return nb - na
    return String(b).localeCompare(String(a))
  })
  for (const y of years) base.push({ value: y, label: y })
  return base
})

// 地区 chip 列表
const areaChipList = computed(() => {
  const base: Array<{ value: string; label: string }> = [{ value: 'all', label: '全部地区' }]
  for (const a of (videoStore.areas || [])) base.push({ value: a, label: a })
  return base
})

// ==================== 推荐区数据 ====================
interface ContinueItem extends Video {
  source_key: string
  ep_num?: number
}

interface RecommendGroup {
  key: string
  title: string
  description: string
  items: Video[]
  continueItems?: ContinueItem[]
}

const recommendLoading = ref(false)
const recommendGroups = ref<RecommendGroup[]>([])

const carouselSlides = ref<Video[]>([])

async function loadRecommendations(): Promise<void> {
  if (!sourceStore.currentSourceKey) return
  recommendLoading.value = true
  recommendGroups.value = []

  try {
    const usedIds = new Set<string>()
    const groups: RecommendGroup[] = []

    // 1) 继续观看
    let historyItems: any[] = []
    try {
      const hist = await GetRecentHistory(30)
      if (Array.isArray(hist)) historyItems = hist
    } catch { /* ignore */ }

    if (historyItems.length > 0) {
      const continueItems: ContinueItem[] = []
      for (const h of historyItems) {
        const id = String(h.vod_id ?? '')
        if (!id || usedIds.has(id)) continue
        usedIds.add(id)
        continueItems.push({
          vod_id: h.vod_id,
          vod_name: h.vod_name || '',
          vod_pic: h.vod_pic || '',
          vod_remarks: h.vod_remarks || '',
          type_name: '',
          source_key: h.source_key || sourceStore.currentSourceKey || '',
          ep_num: h.ep_num,
        } as ContinueItem)
        if (continueItems.length >= 8) break
      }
      if (continueItems.length > 0) groups.push({
        key: 'continue',
        title: '继续观看',
        description: '你看过的剧集，继续从上次的位置开始',
        items: continueItems,
        continueItems,
      })
    }

    // 2) 猜你喜欢（后端 GetRecommend 排除已出现的 id）
    try {
      const exclude = Array.from(usedIds)
      const liked = await videoStore.loadRecommend(sourceStore.currentSourceKey, exclude, 24)
      if (liked.length > 0) {
        const items: Video[] = []
        const seen = new Set<string>()
        for (const v of liked) {
          const id = String((v as any).vod_id ?? '')
          if (!id || usedIds.has(id) || seen.has(id)) continue
          seen.add(id)
          items.push(v)
          if (items.length >= 8) break
        }
        if (items.length > 0) {
          groups.push({
            key: 'liked',
            title: '猜你喜欢',
            description: '基于你最近观看的内容为你推荐',
            items,
          })
          for (const id of seen) usedIds.add(id)
        }
      }
    } catch { /* ignore */ }

    // 3) 最新上线：从当前 videos 取前若干条，跳过已出现
    const allVideos: Video[] = Array.isArray(videoStore.videos) ? videoStore.videos : []
    if (allVideos.length > 0) {
      const newest: Video[] = []
      const seenNew = new Set<string>()
      for (const v of allVideos) {
        const id = String((v as any).vod_id ?? '')
        if (!id || usedIds.has(id) || seenNew.has(id)) continue
        seenNew.add(id)
        newest.push(v)
        if (newest.length >= 10) break
      }
      if (newest.length > 0) groups.push({
        key: 'newest',
        title: '最新上线',
        description: '近期新入库的视频资源',
        items: newest,
      })
    }

    // 0) 热榜：从当前 videos 取热度最高的若干条 → 同时用于轮播图
    if (allVideos.length > 0) {
      const hotList: Video[] = [...allVideos]
        .sort((a, b) => {
          const sa = Number((a as any).vod_score ?? 0) || 0
          const sb = Number((b as any).vod_score ?? 0) || 0
          return sb - sa
        })
      const hotItems: Video[] = []
      const seenHot = new Set<string>()
      for (const v of hotList) {
        const id = String((v as any).vod_id ?? '')
        if (!id || seenHot.has(id)) continue
        seenHot.add(id)
        hotItems.push(v)
        if (hotItems.length >= 10) break
      }
      if (hotItems.length > 0) {
        carouselSlides.value = hotItems
      }
    }

    recommendGroups.value = groups
  } finally {
    recommendLoading.value = false
  }
}

function goDetailFromRecommend(item: Video): void {
  const vodId = String((item as any).vod_id ?? '')
  const sk = String((item as any).source_key || sourceStore.currentSourceKey || '')
  if (sk && vodId) {
    router.push(getDetailPath(sk, { vod_id: vodId }))
  }
}

const removingContinueId = ref('')

async function removeContinueItem(item: ContinueItem): Promise<void> {
  const key = `${item.source_key}-${item.vod_id}`
  if (removingContinueId.value === key) return
  removingContinueId.value = key
  try {
    await DeleteHistoryByVideo({ source_key: item.source_key, vod_id: String(item.vod_id) })
    const group = recommendGroups.value.find(g => g.key === 'continue')
    if (group?.continueItems) {
      group.continueItems = group.continueItems.filter(
        x => !(x.source_key === item.source_key && String(x.vod_id) === String(item.vod_id))
      )
      group.items = group.continueItems
      if (group.continueItems.length === 0) {
        recommendGroups.value = recommendGroups.value.filter(g => g.key !== 'continue')
      }
    }
  } catch (e) {
    console.error('删除继续观看失败:', e)
  } finally {
    removingContinueId.value = ''
  }
}

function goDetail(video: Video): void {
  router.push(getDetailPath(sourceStore.currentSourceKey, video))
}

// ==================== 生命周期 ====================
onMounted(async () => {
  await sourceStore.loadSources()
  if (sourceStore.currentSourceKey) {
    await Promise.all([
      videoStore.loadTypes(sourceStore.currentSourceKey),
      videoStore.loadYearsAndAreas(sourceStore.currentSourceKey),
      videoStore.loadVideos(sourceStore.currentSourceKey, { type_id: '', year: '', area: '', keyword: '' }, 1, 50),
    ])
    await loadRecommendations()
  }
})

onBeforeUnmount(() => {
})

onActivated(async () => {
  if (sourceStore.currentSourceKey) {
    await Promise.all([
      videoStore.loadTypes(sourceStore.currentSourceKey),
      videoStore.loadYearsAndAreas(sourceStore.currentSourceKey),
      videoStore.loadVideos(sourceStore.currentSourceKey, { type_id: '', year: '', area: '', keyword: '' }, 1, 50),
    ])
    await loadRecommendations()
  }
})

watch(() => sourceStore.currentSourceKey, async (key: string) => {
  if (!key) return
  resetFilters()
  await videoStore.loadTypes(key)
  await videoStore.loadYearsAndAreas(key)
  await loadRecommendations()
})

// 删除通知：从详情页删除视频后，自动移除本地列表中的对应项
watch(() => videoStore.deletedVodId, (vodId) => {
  if (!vodId || videoStore.deletedSourceKey !== sourceStore.currentSourceKey) return
  // 从视频列表中移除
  const idx = videoStore.videos.findIndex(v => String(v.vod_id) === vodId)
  if (idx >= 0) {
    videoStore.videos.splice(idx, 1)
    videoStore.total = Math.max(0, videoStore.total - 1)
  }
  // 从轮播图中移除
  const carIdx = carouselSlides.value.findIndex(s => String(s.vod_id) === vodId)
  if (carIdx >= 0) {
    carouselSlides.value.splice(carIdx, 1)
  }
  // 从推荐分组中移除
  for (const g of recommendGroups.value) {
    const gi = g.items.findIndex(i => String(i.vod_id) === vodId)
    if (gi >= 0) g.items.splice(gi, 1)
  }
  videoStore.clearDeletionNotify()
})

// ⭐ 合并三个筛选 watch 为一个，减少冗余监听器
watch(
  () => [activeFilters.value.typeId, activeFilters.value.year, activeFilters.value.area] as const,
  () => { applyFilters() },
)
</script>

<template>
  <div class="home">
    <!-- ============ 推荐区域（仅在无筛选时展示） ============ -->
    <section v-if="!hasActiveFilter && (carouselSlides.length > 0 || recommendGroups.length > 0 || recommendLoading)" class="recommend-section">
      <div v-if="recommendLoading" class="recommend-loading">
        <LoadingSpinner size="sm" label="加载推荐中..." />
      </div>

      <!-- 推荐分组（紧凑行） -->
      <div v-if="recommendGroups.length > 0" class="recommend-groups">
        <div v-for="group in recommendGroups" :key="group.key" class="recommend-group">
          <div class="group-head">
            <h4 class="group-title">{{ group.title }}</h4>
            <span class="group-desc">{{ group.description }}</span>
          </div>
          <div class="recommend-row">
            <template v-if="group.key === 'continue' && group.continueItems">
              <div
                v-for="item in group.continueItems"
                :key="`rec-continue-${item.source_key}-${item.vod_id}`"
                class="rec-card-wrap"
              >
                <VideoCard :video="item" @click="goDetailFromRecommend(item)" />
                <Button
                  variant="text"
                  size="sm"
                  icon
                  class="rec-remove-btn"
                  title="从继续观看中移除"
                  :disabled="removingContinueId === `${item.source_key}-${item.vod_id}`"
                  @click.stop="removeContinueItem(item)"
                >
                  <Icon name="x" :size="12" />
                </Button>
              </div>
            </template>
            <template v-else-if="group.items && group.items.length > 0">
              <VideoCard
                v-for="(item, idx) in group.items"
                :key="`rec-${group.key}-${String((item as any).vod_id ?? '')}-${idx}`"
                :video="item"
                @click="goDetailFromRecommend(item)"
              />
            </template>
          </div>
        </div>
      </div>
    </section>

    <!-- ============ 筛选区域 ============ -->
    <section class="filter-section">
      <div class="filter-head">
        <h3>
          <Icon name="sliders" :size="14" />
          <span>筛选</span>
        </h3>
        <Button variant="secondary" size="sm" @click="expanded = !expanded">
          <span>{{ expanded ? '收起' : '展开' }}</span>
          <Icon :name="expanded ? 'chevron-up' : 'chevron-down'" :size="14" />
        </Button>
      </div>
      
      <!-- 类型：始终显示 -->
      <div class="filter-row">
        <label class="filter-row-label">类型</label>
        <div class="filter-row-chips">
          <Tag
            v-for="opt in typeChipList"
            :key="'type-' + opt.value"
            :active="activeFilters.typeId === opt.value"
            @click="selectChip('type', opt.value)"
          >
            {{ opt.label }}
          </Tag>
        </div>
      </div>

      <!-- 仅展开时显示：年份 + 地区 -->
      <template v-if="expanded">
        <div class="filter-row">
          <label class="filter-row-label">年份</label>
          <div class="filter-row-chips">
            <Tag
              v-for="opt in yearChipList"
              :key="'year-' + opt.value"
              :active="activeFilters.year === opt.value"
              @click="selectChip('year', opt.value)"
            >
              {{ opt.label }}
            </Tag>
          </div>
        </div>

        <div class="filter-row">
          <label class="filter-row-label">地区</label>
          <div class="filter-row-chips">
            <Tag
              v-for="opt in areaChipList"
              :key="'area-' + opt.value"
              :active="activeFilters.area === opt.value"
              @click="selectChip('area', opt.value)"
            >
              {{ opt.label }}
            </Tag>
          </div>
        </div>
      </template>

      <!-- 排序 -->
      <div class="filter-row filter-row-flex">
        <div class="sort-row">
          <span class="sort-label">排序</span>
          <Tag
            :active="activeFilters.sort === 'default'"
            @click="setSort('default')"
          >默认</Tag>
          <Tag
            :active="activeFilters.sort === 'rating'"
            @click="setSort('rating')"
          >按评分</Tag>
          <Tag
            :active="activeFilters.sort === 'hot'"
            @click="setSort('hot')"
          >按热度</Tag>
          <Button variant="secondary" size="sm" @click="resetFilters">重置</Button>
        </div>
      </div>

      <div v-if="hasActiveFilter" class="filter-status">
        已筛选出 <span class="highlight">{{ videoStore.total }}</span> 条结果
      </div>
    </section>

    <BookCarousel v-if="carouselSlides.length > 0" :slides="carouselSlides" :source-key="sourceStore.currentSourceKey" />

    <!-- ============ 视频网格 / 空状态 ============ -->
    <div v-if="videoStore.loading && videoStore.videos.length === 0" class="center-pad">
      <LoadingSpinner label="加载视频中..." />
    </div>

    <div v-else-if="videoStore.videos.length === 0" class="center-pad">
      <EmptyState icon="📺" title="暂无数据" description="请先在「采集源」中添加来源并采集数据">
        <Button variant="primary" @click="router.push('/sources')">前往采集源</Button>
      </EmptyState>
    </div>

    <div v-else class="video-grid">
      <VideoCard
        v-for="(v, idx) in videoStore.videos"
        :key="`${sourceStore.currentSourceKey}-${String((v as any).vod_id ?? '')}-${idx}`"
        :video="v"
        @click="goDetail(v)"
      />
    </div>
  </div>
</template>

<style scoped>
.home {
  max-width: 100%;
  color: var(--text-primary);
  animation: fadeInUp 0.4s ease;
}

@keyframes fadeInUp {
  from { opacity: 0; transform: translateY(10px); }
  to { opacity: 1; transform: translateY(0); }
}

/* ============ 推荐区域 ============ */
.recommend-section {
  margin-bottom: 20px;
  border-radius: 8px;
}

.recommend-loading {
  padding: 20px;
  text-align: center;
}

/* ========== 推荐分组 ========== */
.recommend-groups {
  display: flex;
  flex-direction: column;
  gap: 18px;
}

.recommend-group {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.group-head {
  display: flex;
  align-items: baseline;
  gap: 10px;
  flex-wrap: wrap;
}

.group-title {
  margin: 0;
  font-size: 17px;
  font-weight: 700;
  color: var(--text-primary);
}

.group-desc {
  font-size: 14px;
  color: var(--text-muted);
}

.recommend-row {
  display: grid;
  grid-template-columns: repeat(6, minmax(0, 1fr));
  gap: 12px;
}

.rec-card-wrap {
  position: relative;
}

.rec-remove-btn {
  position: absolute;
  top: 6px;
  right: 6px;
  z-index: 3;
  width: 24px !important;
  height: 24px !important;
  min-width: 24px !important;
  padding: 0 !important;
  border-radius: 50% !important;
  border: none !important;
  background: rgba(0, 0, 0, 0.65) !important;
  color: #fff !important;
  display: flex;
  align-items: center;
  justify-content: center;
  opacity: 0;
  transition: opacity 0.15s ease, background 0.15s ease;
}

.rec-card-wrap:hover .rec-remove-btn {
  opacity: 1;
}

.rec-remove-btn:hover {
  background: rgba(255, 90, 95, 0.9);
}

.rec-remove-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

@media (max-width: 1400px) { .recommend-row { grid-template-columns: repeat(5, minmax(0, 1fr)); } }
@media (max-width: 1100px) { .recommend-row { grid-template-columns: repeat(4, minmax(0, 1fr)); } }
@media (max-width: 780px)  { .recommend-row { grid-template-columns: repeat(3, minmax(0, 1fr)); } }
@media (max-width: 480px)  { .recommend-row { grid-template-columns: repeat(2, minmax(0, 1fr)); } }

/* ============ 筛选区域 ============ */
.filter-section {
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 14px;
  padding: 16px 20px 18px;
  margin-bottom: 20px;
}

.filter-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 12px;
}

.filter-head h3 {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  margin: 0;
  font-size: 17px;
  font-weight: 700;
  color: var(--text-primary);
}

/* chip 行 */
.filter-row {
  display: flex;
  align-items: flex-start;
  gap: 14px;
  padding: 8px 0;
  border-bottom: 1px dashed transparent;
}
.filter-row + .filter-row {
  border-top: 1px dashed var(--border-light, rgba(255,255,255,0.08));
  padding-top: 10px;
}

.filter-row-flex {
  align-items: center;
  flex-wrap: wrap;
  border-top: 1px solid var(--border);
  padding-top: 14px;
  margin-top: 4px;
}

.filter-row-label {
  flex-shrink: 0;
  width: 48px;
  padding-top: 6px;
  font-size: 13px;
  font-weight: 500;
  color: var(--text-muted);
}

.filter-row-chips {
  flex: 1;
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
  min-width: 0;
}

/* 排序行 */
.sort-row {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.sort-label {
  font-size: 13px;
  color: var(--text-muted);
  font-weight: 500;
}

.filter-status {
  margin-top: 12px;
  padding-top: 12px;
  border-top: 1px dashed var(--border);
  font-size: 13px;
  color: var(--text-muted);
}

.filter-status .highlight {
  color: var(--accent);
  font-weight: 600;
  font-size: 15px;
}

@media (max-width: 720px) {
  .filter-row-label { width: 40px; }
}

/* ============ 视频网格 ============ */
.video-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(150px, 1fr));
  gap: 16px;
}

.center-pad {
  padding: 40px 20px;
}
</style>
