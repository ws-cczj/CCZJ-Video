/**
 * 网格列数与卡片密度。
 *
 * 首页、搜索、收藏三个列表页各自算过同一套 grid-template-columns，也各自在挂载时
 * GetSetting 一遍。这三个页面都挂在 KeepAlive 下，切走再切回来不会重挂，所以在设置页
 * 拖动列数滑块要等到下次启动才看得见。状态收到这里：读一次、写一次，三个页面跟着同一份
 * 状态立刻变。
 */
import { computed, ref } from 'vue'
import { defineStore } from 'pinia'
import { GetSetting, SetSetting } from '../api/app'

export type LayoutDensity = 'comfortable' | 'compact' | 'spacious'

const COLUMNS_KEY = 'grid_columns'
const DENSITY_KEY = 'layout_density'
const DEFAULT_COLUMNS = 5
const DEFAULT_DENSITY: LayoutDensity = 'comfortable'
const MIN_COLUMNS = 2
const MAX_COLUMNS = 10

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
  const loaded = ref(false)

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
      const [col, den] = await Promise.all([GetSetting(COLUMNS_KEY), GetSetting(DENSITY_KEY)])
      columns.value = clampColumns(Number.parseInt(String(col ?? ''), 10))
      density.value = den === 'compact' || den === 'spacious' ? den : DEFAULT_DENSITY
    } catch {
      // 读不到就用缺省值，和这两项从没配过的表现一致。
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

  return { columns, density, loaded, gridStyle, load, previewColumns, setColumns, setDensity }
})

// 与其余设置项一致：写库失败不打断界面，下次启动退回上次落库的值。
async function write(key: string, value: string): Promise<void> {
  try {
    await SetSetting(key, value)
  } catch {
    /* 忽略 */
  }
}
