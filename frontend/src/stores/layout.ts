/**
 * 首页的显示偏好：网格列数、卡片密度，以及轮播/推荐两块内容的显隐与翻页节奏。
 *
 * 首页、搜索、收藏三个列表页各自算过同一套 grid-template-columns，也各自在挂载时
 * GetSetting 一遍。这三个页面都挂在 KeepAlive 下，切走再切回来不会重挂，所以在设置页
 * 拖动列数滑块要等到下次启动才看得见。状态收到这里：读一次、写一次，三个页面跟着同一份
 * 状态立刻变。
 *
 * 轮播那三项放这里也是同一个理由：Home 不会因为设置页改了值而重挂，只有跟着这份状态走
 * 才能立刻看见。布尔项沿用 ui_motion 的写法——库里存 '0' 才是关，缺省和别的值都算开，
 * 这样从没配过的用户拿到的就是「全部显示」。
 */
import { computed, ref } from 'vue'
import { defineStore } from 'pinia'
import { GetSetting, SetSetting } from '../api/app'

export type LayoutDensity = 'comfortable' | 'compact' | 'spacious'

const COLUMNS_KEY = 'grid_columns'
const DENSITY_KEY = 'layout_density'
const SHOW_CAROUSEL_KEY = 'home_show_carousel'
const SHOW_RECOMMEND_KEY = 'home_show_recommend'
const CAROUSEL_AUTOPLAY_KEY = 'home_carousel_autoplay'
const CAROUSEL_SECONDS_KEY = 'home_carousel_seconds'
const DEFAULT_COLUMNS = 5
const DEFAULT_DENSITY: LayoutDensity = 'comfortable'
const MIN_COLUMNS = 2
const MAX_COLUMNS = 10
const DEFAULT_CAROUSEL_SECONDS = 6
const MIN_CAROUSEL_SECONDS = 3
const MAX_CAROUSEL_SECONDS = 60

// 密度只改间距和卡片宽度下限；列数仍是硬约束，窗口窄到装不下 minmax 下限时才换行。
const DENSITY_STYLE: Record<LayoutDensity, { gap: string; minWidth: string }> = {
  compact: { gap: '10px', minWidth: '120px' },
  comfortable: { gap: '16px', minWidth: '150px' },
  spacious: { gap: '20px', minWidth: '180px' },
}

/** 滑块本来就限在 2..10，这里防的是设置被别处写坏：0 列会让整个网格塌掉。 */
function clampColumns(value: number): number {
  if (!Number.isFinite(value)) return DEFAULT_COLUMNS
  return Math.min(MAX_COLUMNS, Math.max(MIN_COLUMNS, Math.round(value)))
}

export const useLayoutStore = defineStore('layout', () => {
  const columns = ref(DEFAULT_COLUMNS)
  const density = ref<LayoutDensity>(DEFAULT_DENSITY)
  const showCarousel = ref(true)
  const showRecommend = ref(true)
  const carouselAutoPlay = ref(true)
  const carouselSeconds = ref(DEFAULT_CAROUSEL_SECONDS)
  const loaded = ref(false)

  const carouselIntervalMs = computed(() => carouselSeconds.value * 1000)

  const gridStyle = computed(() => {
    const style = DENSITY_STYLE[density.value]
    return {
      display: 'grid',
      gridTemplateColumns: `repeat(${columns.value}, minmax(${style.minWidth}, 1fr))`,
      gap: style.gap,
    }
  })

  /** 三个页面都调它，只有第一次真的出 IPC；先置 loaded 是为了让并发调用合并成一次。 */
  async function load(): Promise<void> {
    if (loaded.value) return
    loaded.value = true
    try {
      const [col, den, carousel, recommend, autoPlay, seconds] = await Promise.all([
        GetSetting(COLUMNS_KEY), GetSetting(DENSITY_KEY),
        GetSetting(SHOW_CAROUSEL_KEY), GetSetting(SHOW_RECOMMEND_KEY),
        GetSetting(CAROUSEL_AUTOPLAY_KEY), GetSetting(CAROUSEL_SECONDS_KEY),
      ])
      columns.value = clampColumns(Number.parseInt(String(col ?? ''), 10))
      density.value = den === 'compact' || den === 'spacious' ? den : DEFAULT_DENSITY
      showCarousel.value = notOff(carousel)
      showRecommend.value = notOff(recommend)
      carouselAutoPlay.value = notOff(autoPlay)
      carouselSeconds.value = clampSeconds(Number.parseInt(String(seconds ?? ''), 10))
    } catch {
      // 读不到就用缺省值，和这些项从没配过的表现一致。
    }
  }

  /** 拖动滑块过程中的即时预览：只改内存里的列数，不落库。 */
  function previewColumns(next: number): void {
    columns.value = clampColumns(next)
  }

  async function setColumns(next: number): Promise<void> {
    columns.value = clampColumns(next)
    await write(COLUMNS_KEY, String(columns.value))
  }

  async function setDensity(next: LayoutDensity): Promise<void> {
    density.value = next
    await write(DENSITY_KEY, next)
  }

  async function setShowCarousel(next: boolean): Promise<void> {
    showCarousel.value = next
    await write(SHOW_CAROUSEL_KEY, next ? '1' : '0')
  }

  async function setShowRecommend(next: boolean): Promise<void> {
    showRecommend.value = next
    await write(SHOW_RECOMMEND_KEY, next ? '1' : '0')
  }

  async function setCarouselAutoPlay(next: boolean): Promise<void> {
    carouselAutoPlay.value = next
    await write(CAROUSEL_AUTOPLAY_KEY, next ? '1' : '0')
  }

  async function setCarouselSeconds(next: number): Promise<void> {
    carouselSeconds.value = clampSeconds(next)
    await write(CAROUSEL_SECONDS_KEY, String(carouselSeconds.value))
  }

  return {
    columns, density, showCarousel, showRecommend, carouselAutoPlay, carouselSeconds,
    loaded, gridStyle, carouselIntervalMs,
    load, previewColumns, setColumns, setDensity,
    setShowCarousel, setShowRecommend, setCarouselAutoPlay, setCarouselSeconds,
  }
})

/** 库里存 '0' 才是关；缺省和别的值都算开，从没配过的用户不用先做一次「打开开关」。 */
function notOff(raw: string | undefined | null): boolean {
  return String(raw ?? '').trim() !== '0'
}

/** 间隔留一点下限：1 秒的轮播会把交棒动画截在中间，也没人看得清。 */
function clampSeconds(value: number): number {
  if (!Number.isFinite(value)) return DEFAULT_CAROUSEL_SECONDS
  return Math.min(MAX_CAROUSEL_SECONDS, Math.max(MIN_CAROUSEL_SECONDS, Math.round(value)))
}

// 与其余设置项一致：写库失败不打断界面，下次启动退回上次落库的值。
async function write(key: string, value: string): Promise<void> {
  try {
    await SetSetting(key, value)
  } catch {
    /* 忽略 */
  }
}
