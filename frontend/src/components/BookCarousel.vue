<script setup lang="ts">
import { computed, nextTick, onActivated, onDeactivated, onMounted, onUnmounted, reactive, ref, watch } from 'vue'
import type { ComponentPublicInstance } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { getDetailPath } from '../utils'
import type { Video } from '../types'
import { useMotionStore } from '../stores/motion'
import RemoteImage from './RemoteImage.vue'

const PAGE_HEIGHT = 360
const PAGE_PADDING = 20
const SLIDE_DURATION = 420
const AUTO_INTERVAL = 6000

interface Props {
  slides: Video[]
  sourceKey: string
  onSlideClick?: (video: Video) => void | Promise<void>
  /** 自动翻页开关与停留时长由「设置 → 基本 → 首页」给，组件自己不读设置，改动才即时。 */
  autoPlay?: boolean
  intervalMs?: number
}

const props = withDefaults(defineProps<Props>(), {
  autoPlay: true,
  intervalMs: AUTO_INTERVAL,
})
const router = useRouter()
const { t } = useI18n()
const motion = useMotionStore()

/**
 * 两张常驻图层轮流上台：一张在台前，另一张在侧幕里把下一张先画好。
 *
 * 这里原来是「当前页 + 一个 v-if 临时叠层」，同一张海报由两个 RemoteImage 各渲染一遍，
 * 两头都在闪：叠层是刚创建的实例，phase 从 'hidden' 起步即 opacity:0，滑入那 420ms 它
 * 自己在淡进来；动画结束交棒时叠层被销毁、当前页的 src 这才变成这张海报，于是 RemoteImage
 * 先按 --cczj-motion-fast 淡出（140ms）、换源、等 @load、再按 normal 淡入（220ms）。用户
 * 说的「图片瞬间替换」就是交棒这 360ms——两张明明同 URL 的图之间白闪一次，纯亏。
 *
 * 图层常驻后交棒只是换个身份：src 永远不在台前的节点上改动，切换全程只剩 transform。
 */
interface Layer {
  /** props.slides 下标；越界一律当没这张，别让坏索引把轮播清空。 */
  slide: number
  /** translateX 百分比，0 是台前。 */
  offset: number
  /** 是否挂上 CSS 过渡。停位、跳转都要先摘掉它，否则位移本身会滑一段。 */
  animate: boolean
}

const layers = reactive<Layer[]>([
  { slide: 0, offset: 0, animate: false },
  { slide: 1, offset: 100, animate: false },
])
const front = ref(0)
const animating = ref(false)

let timer: ReturnType<typeof setInterval> | null = null
let transitionTimer: ReturnType<typeof setTimeout> | null = null
const layerEls: Array<HTMLElement | null> = [null, null]
/** 两格里 RemoteImage 交出来的公开实例，闸门问的就是它的 ready。 */
type LayerImage = InstanceType<typeof RemoteImage>
const layerImgs: Array<LayerImage | null> = [null, null]

/**
 * 交棒之前先确认来片真的落好了。
 *
 * 海报的 src 要过一趟异步代理（getProxiedImageUrl，慢的时候还带镜像回退），settle 里
 * 「提前把下一张装进侧幕」只是给了它一个停留时长，并不保证画完。没画完就交棒，滑进来的是一张
 * 空底板，海报在半路或落位后突然亮起来——这就是「图片瞬间替换」剩下的那半条。
 * 等不到也照样滑：轮播不能因为一张图卡死，超时是有界的。
 *
 * 问的是 RemoteImage 自己，不是 DOM。屏幕在这里帮不上忙：src 换了而代理还没回话的那一段，
 * 节点上挂着的仍是**上一张**海报，complete、有宽度、opacity 1——按 DOM 读就是"已经好了"，
 * 于是闸门恰好在最需要它的那一跳直接放行。settling 只有组件自己知道。
 */
const READY_WAIT_MS = 1500
const READY_POLL_MS = 100

function layerReady(slot: number): boolean {
  const img = layerImgs[slot]
  // 拿不到实例就是这一格没有图要等（slide 为空时 RemoteImage 根本不渲染），别白等满超时。
  return img ? img.ready : true
}

function waitLayerReady(slot: number): Promise<void> {
  return new Promise((resolve) => {
    const deadline = Date.now() + READY_WAIT_MS
    const check = (): void => {
      if (layerReady(slot) || Date.now() >= deadline) {
        resolve()
        return
      }
      window.setTimeout(check, READY_POLL_MS)
    }
    check()
  })
}

const total = computed(() => props.slides.length)

function slideAt(idx: number): Video | undefined {
  if (idx < 0 || idx >= total.value) return undefined
  return props.slides[idx]
}

// 源刷新可能原地改数组，台前那张每次现取，别存一份过期引用。
const views = computed(() => layers.map((layer, i) => ({ layer, slide: slideAt(layer.slide), slot: i })))
const currentSlideIndex = computed(() => layers[front.value].slide)

function setLayerEl(slot: number, el: Element | ComponentPublicInstance | null): void {
  layerEls[slot] = el instanceof HTMLElement ? el : null
}

function setLayerImg(slot: number, el: Element | ComponentPublicInstance | null): void {
  layerImgs[slot] = el instanceof Element ? null : (el as LayerImage | null)
}

function wrap(idx: number): number {
  if (total.value === 0) return 0
  return ((idx % total.value) + total.value) % total.value
}

/** 往前进时旧片向左退、新片从右来；往后退正好相反。首尾互跳按最短直觉给方向。 */
function directionTo(target: number): 'left' | 'right' {
  const from = layers[front.value].slide
  if (from === 0 && target === total.value - 1) return 'right'
  if (from === total.value - 1 && target === 0) return 'left'
  return target > from ? 'left' : 'right'
}

function moveTo(target: number): void {
  if (animating.value || total.value <= 1 || target === layers[front.value].slide) return

  const dir = directionTo(target)
  const spareSlot = 1 - front.value
  const spare = layers[spareSlot]

  // 闸门在入口处就落下，不等 rAF：否则连点两下会各自改一遍侧幕、各排一次收场。
  animating.value = true

  // 侧幕先就位：内容不对就当场换（屏幕外，那一下淡出淡入没人看见），再摘掉过渡跳到位。
  spare.animate = false
  spare.slide = target
  spare.offset = dir === 'left' ? 100 : -100

  nextTick(async () => {
    const el = layerEls[spareSlot]
    if (!el) {
      settle()
      return
    }
    await waitLayerReady(spareSlot)
    // 闸门可能被外部放下：watch 在等的时候换过 slides 就会把几何与 animating 一并复位，
    // 这时再补滑动会把已经落位的那张从中间推走。
    if (!animating.value) return
    if (layers[spareSlot].slide !== target) {
      // 目标在这段等待里变了（点箭头点快了）：松开闸门就够，几何本来就停在侧幕。
      animating.value = false
      return
    }
    // 缺这一行就没有动画：停位与过渡开关会被并进同一次样式计算，浏览器无从比较起止值。
    el.getBoundingClientRect()
    requestAnimationFrame(() => {
      const leaving = layers[front.value]
      leaving.animate = true
      spare.animate = true
      leaving.offset = dir === 'left' ? -100 : 100
      spare.offset = 0
      if (transitionTimer) clearTimeout(transitionTimer)
      transitionTimer = setTimeout(settle, slideMs())
    })
  })
}

/**
 * 动画跑完只是视觉上到位，账还得记平：侧幕转正并钉回台前，退场那张摘掉过渡跳回侧幕，
 * 顺手把「再下一张」装进去——它有整个停留时长在台后把海报画完，下一次滑动就只剩
 * 位移，不会再带出一次换图。几何一并写死，节点没赶上滑动时也能落回正确的一帧。
 */
function settle(): void {
  if (transitionTimer) {
    clearTimeout(transitionTimer)
    transitionTimer = null
  }
  const retiredSlot = front.value
  const arrivedSlot = 1 - retiredSlot
  front.value = arrivedSlot
  animating.value = false

  const arrived = layers[arrivedSlot]
  arrived.animate = false
  arrived.offset = 0

  const retired = layers[retiredSlot]
  retired.animate = false
  retired.offset = 100
  retired.slide = wrap(arrived.slide + 1)
}

/**
 * 要等多久由 motion 扩展包的时长令牌说了算：JS 里再抄一份 420 会把调慢了的过渡从中间截断，
 * 而读元素自身的 transitionDuration 拿到的是「改 animate 之前」的样式（Vue 的 DOM 更新还在
 * 微任务里排队），只会得到 0 —— 于是 16ms 就收场，滑动被拦腰掐断。
 * 关掉动画时令牌是 1ms，正好立刻收场，不留悬挂状态；令牌读不出来才退回内置的 420。
 */
function slideMs(): number {
  const ms = motion.tokenMs('--cczj-motion-carousel')
  return (ms > 0 ? ms : SLIDE_DURATION) + 16
}

function goNext(): void {
  moveTo(wrap(layers[front.value].slide + 1))
}

function goPrev(): void {
  moveTo(wrap(layers[front.value].slide - 1))
}

function goTo(idx: number): void {
  moveTo(idx)
}

function startAutoPlay(): void {
  stopAutoPlay()
  if (!props.autoPlay || total.value <= 1) return
  timer = setInterval(goNext, props.intervalMs)
}

// 设置页改了开关或间隔要立刻重排：Home 挂在 KeepAlive 下，组件不会因为设置变化而重挂。
watch(() => [props.autoPlay, props.intervalMs] as const, () => startAutoPlay())

// Home is kept alive while the player is open. Pause the carousel while it is
// hidden so a failed Douban poster is not mounted again every six seconds.
onActivated(() => startAutoPlay())
onDeactivated(() => {
  stopAutoPlay()
  // 中途切走只把闸门松开，不替这一页收场。原先在这儿调 settle()：闸门在等图时 animating
  // 已经落下，settle 会把还没画完的来片直接钉到台前——等于把「图片瞬间替换」搬到从播放页
  // 返回首页那一下。松闸就够：待命的续篇看到 animating 已清会自己放弃（不会永远挂着把
  // 后续翻页全挡掉），几何仍停在侧幕，回来照常从下一次翻页滑进来。
  animating.value = false
})

function stopAutoPlay(): void {
  if (timer) {
    clearInterval(timer)
    timer = null
  }
}

const clickingLoading = ref(false)

async function goDetail(video: Video): Promise<void> {
  if (props.onSlideClick) {
    clickingLoading.value = true
    try {
      await props.onSlideClick(video)
    } finally {
      clickingLoading.value = false
    }
    return
  }
  const vodId = String((video as any).vod_id ?? '')
  const sk = String((video as any).source_key || props.sourceKey || '')
  if (sk && vodId) {
    router.push(getDetailPath(sk, { vod_id: vodId }))
  }
}

// Image proxying
watch(
  () => props.slides,
  () => {
    if (transitionTimer) {
      clearTimeout(transitionTimer)
      transitionTimer = null
    }
    animating.value = false
    front.value = 0
    layers[0].slide = 0
    layers[0].offset = 0
    layers[0].animate = false
    layers[1].slide = 1
    layers[1].offset = 100
    layers[1].animate = false
  },
  { immediate: true },
)

watch(
  () => props.slides.length,
  (count) => {
    if (count === 0) return
    for (const layer of layers) {
      if (layer.slide >= count || layer.slide < 0) layer.slide = 0
    }
    startAutoPlay()
  },
)

onMounted(() => {
  startAutoPlay()
})

onUnmounted(() => {
  stopAutoPlay()
  if (transitionTimer) clearTimeout(transitionTimer)
})

function onMouseEnter(): void {
  stopAutoPlay()
}

function onMouseLeave(): void {
  startAutoPlay()
}
</script>

<template>
  <div v-if="total > 0" class="carousel"
    :style="{ height: PAGE_HEIGHT + PAGE_PADDING * 2 + 'px', padding: PAGE_PADDING + 'px' }"
    @mouseenter="onMouseEnter" @mouseleave="onMouseLeave">
    <div class="carousel-viewport" :style="{ height: PAGE_HEIGHT + 'px' }">
      <!-- 两张图层常驻：谁在台前只由 offset 决定，内容切换一律发生在台后 -->
      <div v-for="view in views" :key="view.slot"
        :ref="(el) => setLayerEl(view.slot, el)"
        class="carousel-slide"
        :class="{ 'is-transitioning': view.layer.animate }"
        :style="{ transform: `translateX(${view.layer.offset}%)` }">
        <div class="slide-inner" v-if="view.slide">
          <div class="slide-image-wrap">
            <RemoteImage :ref="(el) => setLayerImg(view.slot, el)"
              :src="(view.slide as any)?.vod_pic || ''" :alt="view.slide?.vod_name || ''"
              class="slide-image" draggable="false" loading="eager" />
            <div class="slide-image-gradient" />
          </div>
          <div class="slide-detail-wrap">
            <div class="slide-detail-bg" />
            <div class="slide-detail-content">
              <span class="slide-badge">{{ t('home.doubanHot') }}</span>
              <h2 class="slide-title">{{ view.slide?.vod_name || '' }}</h2>
              <div class="slide-meta">
                <div v-if="(view.slide as any)?.release_date" class="slide-meta-row">
                  <span class="slide-meta-label">{{ t('home.released') }}</span>
                  <span class="slide-meta-value">{{ (view.slide as any).release_date }}</span>
                </div>
                <div v-if="(view.slide as any)?.director" class="slide-meta-row">
                  <span class="slide-meta-label">{{ t('home.director') }}</span>
                  <span class="slide-meta-value">{{ (view.slide as any).director }}</span>
                </div>
                <div v-if="(view.slide as any)?.actors" class="slide-meta-row">
                  <span class="slide-meta-label">{{ t('home.actors') }}</span>
                  <span class="slide-meta-value slide-meta-actors">{{ (view.slide as any).actors }}</span>
                </div>
              </div>
              <div class="slide-tags">
                <span v-if="(view.slide as any)?.year" class="slide-tag">{{ (view.slide as any).year }}</span>
                <span v-if="(view.slide as any)?.area" class="slide-tag">{{ (view.slide as any).area }}</span>
                <span v-if="view.slide?.vod_score && Number(view.slide.vod_score) > 0"
                  class="slide-tag slide-tag-score">{{ view.slide.vod_score }}{{ t('common.scoreUnit') }}</span>
                <span v-if="view.slide?.vod_remarks" class="slide-tag slide-tag-votes">{{ view.slide.vod_remarks }}</span>
              </div>
            </div>
            <div class="slide-actions">
              <button class="slide-btn slide-btn-primary" @click.stop="goDetail(view.slide!)">{{ t('home.viewDetail') }}</button>
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- Loading overlay when clicking -->
    <div v-if="clickingLoading" class="carousel-loading-overlay">
      <div class="carousel-loading-spinner" />
      <span class="carousel-loading-text">{{ t('common.loading') }}</span>
    </div>

    <!-- Navigation arrows -->
    <button v-if="total > 1" class="carousel-arrow carousel-arrow-left" @click.stop="goPrev">‹</button>
    <button v-if="total > 1" class="carousel-arrow carousel-arrow-right" @click.stop="goNext">›</button>

    <!-- Indicators -->
    <div class="carousel-indicators">
      <button v-for="(_, idx) in slides.slice(0, Math.min(total, 10))" :key="'ind-' + idx" class="carousel-indicator"
        :class="{ active: idx === currentSlideIndex }" :disabled="animating" @click.stop="goTo(idx)" />
    </div>
  </div>
</template>

<style scoped src="../styles/components/book-carousel.css"></style>
