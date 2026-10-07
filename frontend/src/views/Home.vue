<script setup lang="ts">
defineOptions({ name: 'Home' })
import { ref, computed, onMounted, onActivated, onDeactivated, onBeforeUnmount, watch, nextTick } from 'vue'
import { storeToRefs } from 'pinia'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { GetRecentHistory, DeleteHistoryByVideo, DoubanChart, DoubanChartResolve, GetRecommend } from '../api/app'
import { useSourceStore } from '../stores/source'
import { useVideoStore } from '../stores/video'
import { useLayoutStore } from '../stores/layout'
import { useVideoList, type VideoFilter } from '../composables/useVideoList'
import { useErrorStore } from '../stores/error'
import VideoCard from '../components/VideoCard.vue'
import { Button, Tag, Spinner as LoadingSpinner, Empty as EmptyState, Select as SelectDropdown } from '../components/ui'
import BookCarousel from '../components/BookCarousel.vue'
import Icon from '../components/Icon.vue'
import { getDetailPath, getSearchPath } from '../utils'
import { readStorage, writeStorage } from '../platform/storage'
import type { Video } from '../types'

const { t } = useI18n()

const router = useRouter()
const sourceStore = useSourceStore()
const videoStore = useVideoStore()
const errorStore = useErrorStore()

// 首页自己的列表状态：与搜索页各持一份，互不覆盖数组、互不串游标。
// list 是 reactive 对象，必须整体持有（解构会丢解包），模板里也写 list.videos。
const list = useVideoList({
  // 列表本身由 composable 处理，这里只清理派生出来的轮播图与推荐分组。
  // 通知跨源也会到：vod_id 只在源内唯一，不按源过滤会误删同号的另一部片。
  onDeleted(vodId: string, sourceKey: string) {
    if (sourceKey !== sourceStore.currentSourceKey) return
    const carIdx = carouselSlides.value.findIndex(s => String(s.vod_id) === vodId)
    if (carIdx >= 0) {
      carouselSlides.value.splice(carIdx, 1)
    }
    for (const g of recommendGroups.value) {
      const gi = g.items.findIndex(i => String(i.vod_id) === vodId)
      if (gi >= 0) g.items.splice(gi, 1)
    }
  },
})

// ==================== 网格布局设置 ====================
// 列数与密度都在 layout store 里：这一页挂在 KeepAlive 下，设置页改完不会重挂，
// 只有跟着同一份状态走才能立刻看见。storeToRefs 保住的是那份 computed，直接取
// layoutStore.gridStyle 拿到的是当次的普通对象，之后就再也不跟着变了。
const layoutStore = useLayoutStore()
const { gridStyle } = storeToRefs(layoutStore)

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

// debounce 防止快速连续触发（如点击chip同时watcher触发）
let applyTimer: ReturnType<typeof setTimeout> | null = null

function applyFilters(): void {
  if (!sourceStore.currentSourceKey) return
  if (applyTimer) clearTimeout(applyTimer)
  applyTimer = setTimeout(() => {
    applyTimer = null
    doApplyFilters()
  }, 80)
}

function doApplyFilters(): void {
  if (!sourceStore.currentSourceKey) return
  const f: VideoFilter = {
    type_id: activeFilters.value.typeId === 'all' ? '' : activeFilters.value.typeId,
    year: activeFilters.value.year === 'all' ? '' : activeFilters.value.year,
    area: activeFilters.value.area === 'all' ? '' : activeFilters.value.area,
    keyword: '',
    sort: activeFilters.value.sort === 'default' ? '' : activeFilters.value.sort,
  }
  list.load(sourceStore.currentSourceKey, f, 1, 50)
}

function resetFilters(): void {
  activeFilters.value = { typeId: 'all', year: 'all', area: 'all', sort: 'default' }
  expanded.value = false
  if (applyTimer) { clearTimeout(applyTimer); applyTimer = null }
  if (sourceStore.currentSourceKey) {
    list.load(sourceStore.currentSourceKey, {
      type_id: '', year: '', area: '', keyword: '', sort: '',
    }, 1, 50)
  }
}

function setSort(sort: string): void {
  if (activeFilters.value.sort === sort) return
  activeFilters.value.sort = sort
  // watcher 会自动触发 applyFilters
}

function selectChip(category: 'type' | 'year' | 'area', value: string): void {
  if (category === 'type') activeFilters.value.typeId = value
  if (category === 'year') activeFilters.value.year = value
  if (category === 'area') activeFilters.value.area = value
  // watcher 会自动触发 applyFilters
}

// 是否有内容筛选（不含排序），用于控制推荐区显隐
const hasContentFilter = computed<boolean>(() =>
  activeFilters.value.typeId !== 'all' ||
  activeFilters.value.year !== 'all' ||
  activeFilters.value.area !== 'all'
)

// 是否有任何筛选（含排序），用于显示筛选状态栏
const hasActiveFilter = computed<boolean>(() =>
  hasContentFilter.value || activeFilters.value.sort !== 'default'
)

// 类型 chip 列表
const typeChipList = computed(() => {
  const base: Array<{ value: string; label: string }> = [{ value: 'all', label: t('home.allTypes') }]
  if (videoStore.types && videoStore.types.length > 0) {
    for (const t of videoStore.types) {
      base.push({ value: String(t.type_id ?? ''), label: t.name || String(t.type_id ?? '') })
    }
  }
  return base
})

// 年份 chip 列表（倒序：新的在前）
const yearChipList = computed(() => {
  const base: Array<{ value: string; label: string }> = [{ value: 'all', label: t('home.allYears') }]
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
  const base: Array<{ value: string; label: string }> = [{ value: 'all', label: t('home.allRegions') }]
  for (const a of (videoStore.areas || [])) base.push({ value: a, label: a })
  return base
})

// ==================== 推荐区数据 ====================
interface ContinueItem extends Video {
  source_key: string
  ep_num?: number
  global_id?: number
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
const DOUBAN_CHART_CACHE_KEY = 'cczj_douban_chart_cache_v1'
// 与 Go 侧 chartCacheTTL 同值：这一小时内连绑定都不用发。
const DOUBAN_CHART_TTL = 60 * 60 * 1000
// 过了 TTL 不等于不能看，只代表该悄悄换新的了。上限之内一律先上屏，
// 只有首次（或榜单已经老到没有参考价值）才真的让首页等一次网络。
const DOUBAN_CHART_STALE_CEILING = 7 * 24 * 60 * 60 * 1000
let chartLoadInFlight: Promise<any[]> | null = null
let chartSignatureValue = ''

function readChartCache(): { items: any[]; age: number } | null {
  const cached = readStorage<{ cachedAt: number; items: any[] } | null>(DOUBAN_CHART_CACHE_KEY, null)
  if (!cached || !Array.isArray(cached.items) || cached.items.length === 0) return null
  return { items: cached.items, age: Date.now() - (cached.cachedAt || 0) }
}

// 榜单条目 + 后端异步补上的匹配状态。签名不变就别重新赋值轮播：
// BookCarousel 换 slides 会重置翻页动画和当前页，静默刷新不该打断正在看的那一张。
function chartSignature(chart: any[]): string {
  return chart
    .map((i: any) => `${i.subject_id}|${i.rating}|${i.votes}|${i.status}|${i.source_key}|${i.vod_id}`)
    .join(';')
}

function applyChart(chart: any[]): void {
  if (!Array.isArray(chart) || chart.length === 0) return
  const sig = chartSignature(chart)
  if (sig === chartSignatureValue) return
  chartSignatureValue = sig
  carouselSlides.value = chart.map((item: any) => ({
    global_id: item.global_id || 0,
    vod_id: item.subject_id || '',
    vod_name: item.title || '',
    vod_pic: item.poster_url || '',
    vod_score: item.rating || '',
    vod_remarks: item.votes ? t('home.votesCount', { count: item.votes }) : '',
    vod_content: item.info || '',
    year: item.year || '',
    area: item.area || '',
    director: item.director || '',
    actors: item.actors || '',
    release_date: item.release_date || '',
    chart_status: item.status || 'searching',
    chart_source_key: item.source_key || '',
    chart_vod_id: item.vod_id || '',
  })) as Video[]
}

// 请求一趟热榜并写缓存。同一时刻只跑一趟；后台那一趟回来时也会自己 applyChart，
// 所以调用方不必等它。
function refreshChartCache(): Promise<any[]> {
  if (chartLoadInFlight) return chartLoadInFlight
  chartLoadInFlight = (async () => {
    try {
      const chart = (await DoubanChart()) as any[]
      if (Array.isArray(chart) && chart.length > 0) {
        writeStorage(DOUBAN_CHART_CACHE_KEY, { cachedAt: Date.now(), items: chart })
        applyChart(chart)
        return chart
      }
      return []
    } catch (error) {
      // 抓不到就继续用旧榜单，别把整个轮播藏起来。
      console.warn('加载豆瓣热榜失败，使用旧缓存:', error)
      return readChartCache()?.items || []
    } finally {
      chartLoadInFlight = null
    }
  })()
  return chartLoadInFlight
}

async function loadDoubanChartData(): Promise<any[]> {
  const cached = readChartCache()
  if (cached && cached.age < DOUBAN_CHART_TTL) return cached.items
  if (cached) {
    // 旧的先摆上，新的悄悄换。这里不等，等就等于把「缓存过期」又变回一次转圈。
    void refreshChartCache()
    return cached.items
  }
  return refreshChartCache()
}

async function loadRecommendations(): Promise<void> {
  if (!sourceStore.currentSourceKey) return
  recommendLoading.value = true
  // 不清空 recommendGroups：这些是本地库查询，重跑一次通常几十毫秒。先把上一页留着，
  // 结尾整体替换，首页就不会每次刷新都白一下。
  // 冷启动时这一趟和下面的历史/推荐并行发，谁先回来谁先上屏。
  const chartPromise = loadDoubanChartData()

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
        const gid = h.global_id != null ? String(h.global_id) : `${h.source_key || ''}-${h.vod_id || ''}`
        if (!gid || usedIds.has(gid)) continue
        usedIds.add(gid)
        continueItems.push({
          vod_id: h.vod_id,
          vod_name: h.vod_name || '',
          vod_pic: h.vod_pic || '',
          vod_remarks: h.vod_remarks || '',
          type_name: '',
          source_key: h.source_key || sourceStore.currentSourceKey || '',
          ep_num: h.ep_num,
          global_id: h.global_id,
        } as ContinueItem)
        if (continueItems.length >= 8) break
      }
      if (continueItems.length > 0) groups.push({
        key: 'continue',
        title: t('home.continueWatching'),
        description: t('home.continueWatchingDesc'),
        items: continueItems,
        continueItems,
      })
    }

    // 2) 猜你喜欢（后端 GetRecommend 排除已出现的 id）
    try {
      const exclude = Array.from(usedIds)
      const raw = (await GetRecommend({
        source_key: sourceStore.currentSourceKey,
        limit: 24,
        exclude_ids: exclude,
      })) as unknown as Video[]
      const liked = Array.isArray(raw) ? raw : []
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
            title: t('home.recommended'),
            description: t('home.recommendedDesc'),
            items,
          })
          for (const id of seen) usedIds.add(id)
        }
      }
    } catch (e) {
      errorStore.fromError(t('errors.loadRecommendFailed'), e, 'home.recommendations')
    }

    // 3) 最新上线：从当前 videos 取前若干条，跳过已出现
    const allVideos: Video[] = Array.isArray(list.videos) ? list.videos : []
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
        title: t('home.latest'),
        description: t('home.latestDesc'),
        items: newest,
      })
    }

    // 0) 热榜：从豆瓣热榜获取数据用于轮播图（立即展示，带匹配状态）
    try {
      applyChart(await chartPromise)
    } catch (e) {
      console.warn('加载豆瓣热榜失败:', e)
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

function onChartSlideClick(video: Video): void {
  const slide = video as any
  const status = slide.chart_status as string
  const subjectID = String(video.vod_id || '')

  // 已匹配到资源，直接跳转详情页
  if (status === 'matched' && slide.chart_source_key && slide.chart_vod_id) {
    router.push(getDetailPath(slide.chart_source_key, { vod_id: slide.chart_vod_id }))
    return
  }

  if (status === 'not_found') {
    errorStore.warn(t('home.noPlayableTitle'), t('home.noPlayableMsg'))
    return
  }

  // searching 状态，尝试实时解析
  if (subjectID) {
    DoubanChartResolve(subjectID).then((result: any) => {
      if (result?.status === 'matched' && result.source_key && result.vod_id) {
        router.push(getDetailPath(result.source_key, { vod_id: result.vod_id }))
      } else if (result?.status === 'searching') {
        errorStore.info(t('home.searchingTitle'), t('home.searchingMsg'))
      } else {
        errorStore.warn(t('home.noPlayableTitle'), t('home.noPlayableMsg'))
      }
    }).catch(() => {
      errorStore.warn(t('home.noPlayableTitle'), t('home.noPlayableMsg'))
    })
  }
}

const removingContinueId = ref('')

async function removeContinueItem(item: ContinueItem): Promise<void> {
  const key = item.global_id != null ? `g-${item.global_id}` : `${item.source_key}-${item.vod_id}`
  if (removingContinueId.value === key) return
  removingContinueId.value = key
  try {
    await DeleteHistoryByVideo({ source_key: item.source_key, vod_id: String(item.vod_id), global_id: item.global_id || 0 })
    const group = recommendGroups.value.find(g => g.key === 'continue')
    if (group?.continueItems) {
      group.continueItems = group.continueItems.filter(
        x => (x.global_id != null && item.global_id != null) ? x.global_id !== item.global_id : !(x.source_key === item.source_key && String(x.vod_id) === String(item.vod_id))
      )
      group.items = group.continueItems
      if (group.continueItems.length === 0) {
        recommendGroups.value = recommendGroups.value.filter(g => g.key !== 'continue')
      }
    }
  } catch (e) {
    console.error(t('home.removeContinueFailed'), e)
  } finally {
    removingContinueId.value = ''
  }
}

function goDetail(video: Video): void {
  router.push(getDetailPath(sourceStore.currentSourceKey, video))
}

// ==================== 生命周期 ====================
let homePageWasDeactivated = false
let suppressSourceWatch = false

async function refreshHomeFeed(): Promise<void> {
  const key = sourceStore.currentSourceKey
  if (!key) return
  await Promise.all([
    videoStore.loadTypes(key),
    videoStore.loadYearsAndAreas(key),
    list.load(key, { type_id: '', year: '', area: '', keyword: '' }, 1, 50),
  ])
  await loadRecommendations()
}

onMounted(async () => {
  suppressSourceWatch = true
  await layoutStore.load()
  await sourceStore.loadSources()
  await refreshHomeFeed()
  // The currentSourceKey watcher below is for user-driven switches; loadSources()
  // above sets that key, so hold the watcher off until those jobs have flushed.
  await nextTick()
  suppressSourceWatch = false
})

onBeforeUnmount(() => {
})

// KeepAlive activates right after mounting as well; gating on a real
// deactivation keeps the first visit to a single round of requests.
onActivated(async () => {
  if (!homePageWasDeactivated) return
  homePageWasDeactivated = false
  await refreshHomeFeed()
})

onDeactivated(() => { homePageWasDeactivated = true })

watch(() => sourceStore.currentSourceKey, async (key: string) => {
  if (!key || suppressSourceWatch) return
  resetFilters()
  await videoStore.loadTypes(key)
  await videoStore.loadYearsAndAreas(key)
  await loadRecommendations()
})

// ⭐ 筛选 watch：监听所有筛选条件变化，自动触发（带 debounce）
watch(
  () => [activeFilters.value.typeId, activeFilters.value.year, activeFilters.value.area, activeFilters.value.sort] as const,
  () => { applyFilters() },
)
</script>

<template>
  <div class="home cczj-max-w-full cczj-text-primary">
    <BookCarousel v-if="layoutStore.showCarousel && carouselSlides.length > 0" :slides="carouselSlides"
      :source-key="sourceStore.currentSourceKey" :on-slide-click="onChartSlideClick"
      :auto-play="layoutStore.carouselAutoPlay" :interval-ms="layoutStore.carouselIntervalMs" />
    <!-- ============ 推荐区域（仅在无内容筛选时展示，排序不影响） ============ -->
    <section v-if="layoutStore.showRecommend && !hasContentFilter && (carouselSlides.length > 0 || recommendGroups.length > 0 || recommendLoading)"
      class="recommend-section cczj-mb-10 cczj-rounded-md">
      <!-- 有内容可看时不再叠一层转圈：转圈一消失整块内容就要跳一次位。 -->
      <div v-if="recommendLoading && recommendGroups.length === 0" class="recommend-loading cczj-text-center">
        <LoadingSpinner size="sm" :label="t('home.loadingRecommendations')" />
      </div>

      <!-- 推荐分组（紧凑行） -->
      <div v-if="recommendGroups.length > 0" class="recommend-groups cczj-flex cczj-flex-col">
        <div v-for="(group, groupIdx) in recommendGroups" :key="group.key" class="recommend-group cczj-flex cczj-flex-col cczj-gap-5">
          <div class="group-head cczj-flex cczj-items-baseline cczj-gap-5 cczj-flex-wrap">
            <h4 class="group-title cczj-font-bold cczj-text-primary">{{ group.title }}</h4>
            <span class="group-desc cczj-text-base cczj-text-muted">{{ group.description }}</span>
          </div>
          <div class="recommend-row cczj-grid cczj-gap-6">
            <template v-if="group.key === 'continue' && group.continueItems">
              <div v-for="(item, idx) in group.continueItems" :key="`rec-continue-${item.source_key}-${item.vod_id}`"
                class="rec-card-wrap cczj-relative">
                <VideoCard :video="item" @click="goDetailFromRecommend(item)" />
                <Button variant="overlay" size="sm" icon class="rec-remove-btn cczj-absolute cczj-rounded-50 cczj-opacity-0 cczj-transition-fast" :title="t('home.removeFromContinue')"
                  :disabled="removingContinueId === `${item.source_key}-${item.vod_id}`"
                  @click.stop="removeContinueItem(item)">
                  <Icon name="x" :size="12" />
                </Button>
              </div>
            </template>
            <template v-else-if="group.items && group.items.length > 0">
              <VideoCard v-for="(item, idx) in group.items"
                :key="`rec-${group.key}-${String((item as any).vod_id ?? '')}-${idx}`" :video="item"
                @click="goDetailFromRecommend(item)" />
            </template>
          </div>
        </div>
      </div>
    </section>

    <!-- ============ 筛选区域 ============ -->
    <section class="filter-section cczj-bg-card cczj-border cczj-mb-10">
      <div class="filter-head cczj-flex cczj-items-center cczj-justify-between cczj-gap-6 cczj-mb-6">
        <h3 class="cczj-inline-flex cczj-items-center cczj-gap-4 cczj-font-bold cczj-text-primary">
          <Icon name="sliders" :size="14" />
          <span>{{ t('home.filter') }}</span>
        </h3>
        <Button variant="secondary" size="sm" @click="expanded = !expanded" class="cczj-flex cczj-items-center cczj-gap-1">
          <span>{{ expanded ? t('home.collapse') : t('home.expand') }}</span>
          <Icon :name="expanded ? 'chevron-up' : 'chevron-down'" :size="14" />
        </Button>
      </div>

      <!-- 类型：收起时显示六个，展开后显示全部 -->
      <div class="filter-row cczj-flex cczj-items-start cczj-gap-7">
        <label class="filter-row-label cczj-flex-shrink-0 cczj-text-13 cczj-font-medium cczj-text-muted">{{ t('home.type') }}</label>
        <div class="filter-row-chips cczj-flex cczj-flex-1 cczj-flex-wrap cczj-items-center cczj-gap-4 cczj-min-w-0">
          <Tag v-show="!expanded" v-for="opt in typeChipList.slice(0, 6)" :key="'type-' + opt.value"
            :active="activeFilters.typeId === opt.value" @click="selectChip('type', opt.value)">
            {{ opt.label }}
          </Tag>
          <Tag v-show="expanded" v-for="opt in typeChipList" :key="'type-' + opt.value"
            :active="activeFilters.typeId === opt.value" @click="selectChip('type', opt.value)">
            {{ opt.label }}
          </Tag>
        </div>
      </div>

      <!-- 仅展开时显示：年份 + 地区 -->
      <template v-if="expanded">
        <div class="filter-row cczj-flex cczj-items-start cczj-gap-7">
          <label class="filter-row-label cczj-flex-shrink-0 cczj-text-13 cczj-font-medium cczj-text-muted">{{ t('home.year') }}</label>
          <div class="filter-row-chips cczj-flex cczj-flex-1 cczj-flex-wrap cczj-items-center cczj-gap-4 cczj-min-w-0">
            <Tag v-for="opt in yearChipList" :key="'year-' + opt.value" :active="activeFilters.year === opt.value"
              @click="selectChip('year', opt.value)">
              {{ opt.label }}
            </Tag>
          </div>
        </div>

        <div class="filter-row cczj-flex cczj-items-start cczj-gap-7">
          <label class="filter-row-label cczj-flex-shrink-0 cczj-text-13 cczj-font-medium cczj-text-muted">{{ t('home.region') }}</label>
          <div class="filter-row-chips cczj-flex cczj-flex-1 cczj-flex-wrap cczj-items-center cczj-gap-4 cczj-min-w-0">
            <Tag v-for="opt in areaChipList" :key="'area-' + opt.value" :active="activeFilters.area === opt.value"
              @click="selectChip('area', opt.value)">
              {{ opt.label }}
            </Tag>
          </div>
        </div>
      </template>

      <!-- 排序 -->
      <div class="filter-row filter-row-flex cczj-flex cczj-gap-7 cczj-items-center cczj-flex-wrap cczj-border-top cczj-mt-2">
        <div class="sort-row cczj-flex cczj-items-center cczj-gap-4 cczj-flex-wrap">
          <span class="sort-label cczj-text-13 cczj-text-muted cczj-font-medium">{{ t('home.sort') }}</span>
          <Tag :active="activeFilters.sort === 'default'" @click="setSort('default')">{{ t('home.sortDefault') }}</Tag>
          <Tag :active="activeFilters.sort === 'rating'" @click="setSort('rating')">{{ t('home.byRating') }}</Tag>
          <Tag :active="activeFilters.sort === 'hot'" @click="setSort('hot')">{{ t('home.byPopularity') }}</Tag>
          <Button variant="secondary" size="sm" @click="resetFilters">{{ t('home.reset') }}</Button>
        </div>
      </div>

      <div v-if="hasActiveFilter" class="filter-status cczj-mt-6 cczj-pt-6 cczj-text-13 cczj-text-muted">
        {{ t('home.filteredResults', { count: list.total }) }}
      </div>
    </section>

    <!-- ============ 视频网格 / 空状态 ============ -->
    <div v-if="list.loading && list.videos.length === 0" class="center-pad">
      <LoadingSpinner :label="t('home.loadingVideos')" />
    </div>

    <div v-else-if="list.videos.length === 0" class="center-pad">
      <EmptyState icon="📺" :title="t('home.noData')" :description="t('home.noDataDesc')">
        <Button variant="primary" @click="router.push('/sources')">{{ t('home.goToSources') }}</Button>
      </EmptyState>
    </div>

    <div v-else class="video-grid" :style="gridStyle">
      <VideoCard v-for="(v, idx) in list.videos"
        :key="`${sourceStore.currentSourceKey}-${String((v as any).vod_id ?? '')}-${idx}`" :video="v"
        @click="goDetail(v)" />
    </div>
  </div>
</template>

<style scoped>
.home {
  animation: cczj-fade-in-up 0.4s ease;
}

/* ============ 推荐区域 ============ */
.recommend-loading {
  padding: 20px;
}

/* ========== 推荐分组 ========== */
.recommend-groups {
  gap: 18px;
}

.recommend-row {
  grid-template-columns: repeat(6, minmax(0, 1fr));
}

.rec-remove-btn {
  top: 6px;
  right: 6px;
  z-index: 3;
}

.rec-card-wrap:hover .rec-remove-btn {
  opacity: 1;
}

.rec-remove-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

@media (max-width: 1400px) {
  .recommend-row {
    grid-template-columns: repeat(5, minmax(0, 1fr));
  }
}

@media (max-width: 1100px) {
  .recommend-row {
    grid-template-columns: repeat(4, minmax(0, 1fr));
  }
}

@media (max-width: 780px) {
  .recommend-row {
    grid-template-columns: repeat(3, minmax(0, 1fr));
  }
}

@media (max-width: 480px) {
  .recommend-row {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

/* ============ 筛选区域 ============ */
.filter-section {
  border-radius: 14px;
  padding: 16px 20px 18px;
}

.filter-head h3 {
  margin: 0;
  font-size: 17px;
}

/* chip 行 */
.filter-row {
  padding: 8px 0;
  border-bottom: 1px dashed transparent;
}

.filter-row+.filter-row {
  border-top: 1px dashed var(--border-light, rgba(255, 255, 255, 0.08));
  padding-top: 10px;
}

.filter-row-flex {
  padding-top: 14px;
}

.filter-row-label {
  width: 48px;
  padding-top: 6px;
}

.filter-status {
  border-top: 1px dashed var(--border);
}

.filter-status .highlight {
  font-size: 15px;
}

@media (max-width: 720px) {
  .filter-row-label {
    width: 40px;
  }
}

/* ============ 视频网格 ============ */
.center-pad {
  padding: 40px 20px;
}
</style>
