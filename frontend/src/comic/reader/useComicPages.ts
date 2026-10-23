import { computed, ref, type ComputedRef, type Ref } from 'vue'
import type { ComicPageState } from '../domain'

/** 当前页前后各预取这么多页。再大就是拿几十张位图换一点流畅度，不值。 */
export const PREFETCH_WINDOW = 2

export interface ComicPagesController {
  pages: Ref<ComicPageState[]>
  current: Ref<number>
  /** 代号。切话时加一：上一话迟到的 load/error 回调会因为代号不匹配被丢掉。 */
  generation: Ref<number>
  count: ComputedRef<number>
  reset: (count: number, at?: number,ratios?:readonly number[]) => void
  seek: (index: number) => void
  /** 这两个都带代号：来自上一话的回调直接忽略。 */
  succeed: (index: number, generation: number, ratio: number) => void
  fail: (index: number, generation: number) => void
  retry: (index: number) => void
  inWindow: (index: number) => boolean
  clamp: (index: number) => number
}

/**
 * 一话的页级加载状态机。
 *
 * 后端只给"一话的图片地址列表"，每张图的实际下载是前端 <img> 自己做的，所以
 * 状态机在这里。三件事是它存在的理由：
 *
 * - **窗口**：只有当前页前后 PREFETCH_WINDOW 页挂着 <img>。一话几十页全部发起请求
 *   会把代理与源站同时打满，而用户九成时间只看当前这一页。
 * - **取消**：页滚出窗口时它的 <img> 被卸载（在途请求随之放弃），状态收回 queued
 *   而不是 error——取消是可重试的（ADR 0016 引 Mihon Page.kt:51-57）。
 * - **代号**：切话时整表换掉、代号加一。少了这一层，上一话的慢响应会把新话的页
 *   状态覆盖掉，而那种故障类型检查、构建与单元测试全都查不出来。
 */
export function useComicPages(windowSize = PREFETCH_WINDOW): ComicPagesController {
  const pages = ref<ComicPageState[]>([]) as Ref<ComicPageState[]>
  const current = ref(0)
  const generation = ref(0)
  const count = computed(() => pages.value.length)

  function clamp(index: number): number {
    const last = pages.value.length - 1
    if (!Number.isFinite(index) || last < 0) return 0
    return Math.max(0, Math.min(last, Math.trunc(index)))
  }

  function inWindow(index: number): boolean {
    return Math.abs(index - current.value) <= windowSize
  }

  /** 窗口内的 queued 开始加载，窗口外的 loading 收回 queued。 */
  function sync(): void {
    for (const [index, page] of pages.value.entries()) {
      const wanted = inWindow(index)
      if (wanted && page.status === 'queued') page.status = 'loading'
      else if (!wanted && page.status === 'loading') page.status = 'queued'
    }
  }

  function reset(nextCount: number, at = 0, ratios:readonly number[]=[]): void {
    generation.value += 1
    const total = Math.max(0, Math.trunc(Number.isFinite(nextCount) ? nextCount : 0))
    pages.value = Array.from({ length: total }, (_,index) => ({ status: 'queued', ratio: Number.isFinite(ratios[index])&&ratios[index]!>0?ratios[index]!:0, nonce: 0 }))
    current.value = clamp(at)
    sync()
  }

  function seek(index: number): void {
    current.value = clamp(index)
    sync()
  }

  function at(index: number, generationToken: number): ComicPageState | null {
    if (generationToken !== generation.value) return null
    return pages.value[index] ?? null
  }

  function succeed(index: number, generationToken: number, ratio: number): void {
    const page = at(index, generationToken)
    if (!page) return
    page.status = 'ready'
    // 记住真实宽高比：位图释放之后格子还得占同样的高度，否则条漫会跳。
    if (Number.isFinite(ratio) && ratio > 0) page.ratio = ratio
  }

  function fail(index: number, generationToken: number): void {
    const page = at(index, generationToken)
    if (page) page.status = 'error'
  }

  /** 单页重试：换 nonce 逼出一个新的 <img>，整话不判死。 */
  function retry(index: number): void {
    const page = pages.value[index]
    if (!page) return
    page.nonce += 1
    page.status = inWindow(index) ? 'loading' : 'queued'
  }

  return { pages, current, generation, count, reset, seek, succeed, fail, retry, inWindow, clamp }
}
