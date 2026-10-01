<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, onActivated, onDeactivated, watch, nextTick } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { getDetailPath } from '../utils'
import type { Video } from '../types'
import RemoteImage from './RemoteImage.vue'

interface Props {
  slides: Video[]
  sourceKey: string
  onSlideClick?: (video: Video) => void | Promise<void>
}

const props = defineProps<Props>()
const router = useRouter()
const { t } = useI18n()

const PAGE_HEIGHT = 360
const PAGE_PADDING = 20
const SLIDE_DURATION = 420
const AUTO_INTERVAL = 6000

const currentIndex = ref(0)
const isTransitioning = ref(false)
const slideDirection = ref<'left' | 'right'>('left')
const pendingIndex = ref(-1)

let timer: ReturnType<typeof setInterval> | null = null
let transitionTimer: ReturnType<typeof setTimeout> | null = null

const total = computed(() => props.slides.length)
// Source refreshes can mutate the slide array in place. Never let a stale
// index turn the whole viewport into an empty carousel.
const activeSlide = computed(() => {
  const count = total.value
  if (count === 0) return undefined
  const index = ((currentIndex.value % count) + count) % count
  return props.slides[index]
})

// Slide positions: current at 0, next at 100%, prev at -100%
const currentOffset = ref(0) // percentage offset for current slide
const incomingOffset = ref(0) // percentage offset for incoming slide
/** 来片节点本身：过渡开始前要对它强制一次样式计算，收尾要按它的实际时长等。 */
const incomingEl = ref<HTMLElement | null>(null)

function goTo(idx: number): void {
  if (isTransitioning.value || total.value <= 1 || idx === currentIndex.value) return

  const direction: 'left' | 'right' = idx > currentIndex.value ? 'left' : 'right'

  // Handle wrap-around
  if (currentIndex.value === 0 && idx === total.value - 1) {
    slideDirection.value = 'right'
  } else if (currentIndex.value === total.value - 1 && idx === 0) {
    slideDirection.value = 'left'
  } else {
    slideDirection.value = direction
  }

  pendingIndex.value = idx
  startTransition()
}

function goNext(): void {
  if (isTransitioning.value) return
  const nextIdx = (currentIndex.value + 1) % total.value
  slideDirection.value = 'left'
  pendingIndex.value = nextIdx
  startTransition()
}

function goPrev(): void {
  if (isTransitioning.value) return
  const prevIdx = (currentIndex.value - 1 + total.value) % total.value
  slideDirection.value = 'right'
  pendingIndex.value = prevIdx
  startTransition()
}

function startTransition(): void {
  isTransitioning.value = true
  const direction = slideDirection.value

  // Incoming slide starts off-screen
  if (direction === 'left') {
    // Going forward: current slides left, incoming comes from right
    currentOffset.value = 0
    incomingOffset.value = 100
  } else {
    // Going backward: current slides right, incoming comes from left
    currentOffset.value = 0
    incomingOffset.value = -100
  }

  nextTick(() => {
    const el = incomingEl.value
    if (!el) { finish(); return }
    // 来片是 v-if 刚建出来的节点，起始偏移必须先真的被算一次样式，浏览器才有「从哪儿动」可比。
    // 原来这里只有 nextTick + 单层 rAF（注释一直写着 force reflow，代码却没有）：那一帧新节点
    // 还没算过样式，来片第一帧就落在 0% 上盖住旧片——用户看到的「图片瞬间变换」就是这个。
    el.getBoundingClientRect()
    requestAnimationFrame(() => {
      if (direction === 'left') {
        currentOffset.value = -100
        incomingOffset.value = 0
      } else {
        currentOffset.value = 100
        incomingOffset.value = 0
      }
      if (transitionTimer) clearTimeout(transitionTimer)
      transitionTimer = setTimeout(finish, slideMs(el))
    })
  })
}

/** 过渡跑完的收场：把来片转成正式的当前页，叠加的临时节点随之消失。 */
function finish(): void {
  currentIndex.value = pendingIndex.value >= 0 && pendingIndex.value < total.value ? pendingIndex.value : 0
  isTransitioning.value = false
  currentOffset.value = 0
  pendingIndex.value = -1
}

/**
 * 等多久由 CSS 那条 transition 自己说了算：动效时长现在归 motion 扩展包管，JS 里再抄一份
 * 420 就会把调慢了的过渡从中间截断。关掉动画时算出 0ms，正好立刻收场，不留悬挂状态。
 */
function slideMs(el: HTMLElement): number {
  const parsed = Number.parseFloat(getComputedStyle(el).transitionDuration)
  if (!Number.isFinite(parsed)) return SLIDE_DURATION
  return Math.round(parsed * 1000) + 16
}

function startAutoPlay(): void {
  stopAutoPlay()
  if (total.value <= 1) return
  timer = setInterval(goNext, AUTO_INTERVAL)
}

// Home is kept alive while the player is open. Pause the carousel while it is
// hidden so a failed Douban poster is not mounted again every six seconds.
onActivated(() => startAutoPlay())
onDeactivated(() => {
  stopAutoPlay()
  if (transitionTimer) {
    clearTimeout(transitionTimer)
    transitionTimer = null
  }
})

function stopAutoPlay(): void {
  if (timer) { clearInterval(timer); timer = null }
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
watch(() => props.slides, () => {
  currentIndex.value = 0
  isTransitioning.value = false
  currentOffset.value = 0
  pendingIndex.value = -1
}, { immediate: true })

watch(() => props.slides.length, (count) => {
  if (count === 0) return
  if (currentIndex.value >= count || currentIndex.value < 0) currentIndex.value = 0
  if (pendingIndex.value >= count) pendingIndex.value = -1
  startAutoPlay()
})

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

// Helper to get slide data by index (with wrap-around for pending)
function getSlideData(idx: number): Video | undefined {
  if (idx < 0 || idx >= total.value) return undefined
  return props.slides[idx]
}
</script>

<template>
  <div v-if="total > 0" class="carousel"
    :style="{ height: PAGE_HEIGHT + PAGE_PADDING * 2 + 'px', padding: PAGE_PADDING + 'px' }"
    @mouseenter="onMouseEnter" @mouseleave="onMouseLeave">
    <div class="carousel-viewport" :style="{ height: PAGE_HEIGHT + 'px' }">
      <!-- Current slide -->
      <div class="carousel-slide"
        :class="{ 'is-transitioning': isTransitioning }"
        :style="{ transform: `translateX(${currentOffset}%)` }">
        <div class="slide-inner" v-if="activeSlide">
          <div class="slide-image-wrap">
            <RemoteImage :src="(activeSlide as any)?.vod_pic || ''" :alt="activeSlide?.vod_name || ''"
              class="slide-image" draggable="false" loading="eager" />
            <div class="slide-image-gradient" />
          </div>
          <div class="slide-detail-wrap">
            <div class="slide-detail-bg" />
            <div class="slide-detail-content">
              <span class="slide-badge">{{ t('home.doubanHot') }}</span>
              <h2 class="slide-title">{{ activeSlide?.vod_name || '' }}</h2>
              <div class="slide-meta">
                <div v-if="(activeSlide as any)?.release_date" class="slide-meta-row">
                  <span class="slide-meta-label">{{ t('home.released') }}</span>
                  <span class="slide-meta-value">{{ (activeSlide as any).release_date }}</span>
                </div>
                <div v-if="(activeSlide as any)?.director" class="slide-meta-row">
                  <span class="slide-meta-label">{{ t('home.director') }}</span>
                  <span class="slide-meta-value">{{ (activeSlide as any).director }}</span>
                </div>
                <div v-if="(activeSlide as any)?.actors" class="slide-meta-row">
                  <span class="slide-meta-label">{{ t('home.actors') }}</span>
                  <span class="slide-meta-value slide-meta-actors">{{ (activeSlide as any).actors }}</span>
                </div>
              </div>
              <div class="slide-tags">
                <span v-if="(activeSlide as any)?.year" class="slide-tag">{{ (activeSlide as any).year }}</span>
                <span v-if="(activeSlide as any)?.area" class="slide-tag">{{ (activeSlide as any).area }}</span>
                <span v-if="activeSlide?.vod_score && Number(activeSlide.vod_score) > 0"
                  class="slide-tag slide-tag-score">{{ activeSlide.vod_score }}{{ t('common.scoreUnit') }}</span>
                <span v-if="activeSlide?.vod_remarks" class="slide-tag slide-tag-votes">{{ activeSlide.vod_remarks }}</span>
              </div>
            </div>
          <div class="slide-actions">
              <button class="slide-btn slide-btn-primary" @click.stop="goDetail(activeSlide!)">{{ t('home.viewDetail') }}</button>
            </div>
          </div>
        </div>
      </div>

      <!-- Incoming slide (during transition) -->
      <div v-if="isTransitioning && pendingIndex >= 0 && getSlideData(pendingIndex)"
        ref="incomingEl"
        class="carousel-slide carousel-slide-incoming is-transitioning"
        :style="{ transform: `translateX(${incomingOffset}%)` }">
        <div class="slide-inner">
          <div class="slide-image-wrap">
            <RemoteImage :src="(getSlideData(pendingIndex) as any)?.vod_pic || ''" :alt="getSlideData(pendingIndex)?.vod_name || ''"
              class="slide-image" draggable="false" loading="eager" />
            <div class="slide-image-gradient" />
          </div>
          <div class="slide-detail-wrap">
            <div class="slide-detail-bg" />
            <div class="slide-detail-content">
              <span class="slide-badge">{{ t('home.doubanHot') }}</span>
              <h2 class="slide-title">{{ getSlideData(pendingIndex)?.vod_name || '' }}</h2>
              <div class="slide-meta">
                <div v-if="(getSlideData(pendingIndex) as any)?.release_date" class="slide-meta-row">
                  <span class="slide-meta-label">{{ t('home.released') }}</span>
                  <span class="slide-meta-value">{{ (getSlideData(pendingIndex) as any).release_date }}</span>
                </div>
                <div v-if="(getSlideData(pendingIndex) as any)?.director" class="slide-meta-row">
                  <span class="slide-meta-label">{{ t('home.director') }}</span>
                  <span class="slide-meta-value">{{ (getSlideData(pendingIndex) as any).director }}</span>
                </div>
                <div v-if="(getSlideData(pendingIndex) as any)?.actors" class="slide-meta-row">
                  <span class="slide-meta-label">{{ t('home.actors') }}</span>
                  <span class="slide-meta-value slide-meta-actors">{{ (getSlideData(pendingIndex) as any).actors }}</span>
                </div>
              </div>
              <div class="slide-tags">
                <span v-if="(getSlideData(pendingIndex) as any)?.year" class="slide-tag">{{ (getSlideData(pendingIndex) as any).year }}</span>
                <span v-if="(getSlideData(pendingIndex) as any)?.area" class="slide-tag">{{ (getSlideData(pendingIndex) as any).area }}</span>
                <span v-if="getSlideData(pendingIndex)?.vod_score && Number(getSlideData(pendingIndex)!.vod_score) > 0"
                  class="slide-tag slide-tag-score">{{ getSlideData(pendingIndex)!.vod_score }}{{ t('common.scoreUnit') }}</span>
                <span v-if="getSlideData(pendingIndex)?.vod_remarks" class="slide-tag slide-tag-votes">{{ getSlideData(pendingIndex)!.vod_remarks }}</span>
              </div>
            </div>
            <div class="slide-actions">
              <button class="slide-btn slide-btn-primary" @click.stop="goDetail(getSlideData(pendingIndex)!)">{{ t('home.viewDetail') }}</button>
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
        :class="{ active: idx === currentIndex }" :disabled="isTransitioning" @click.stop="goTo(idx)" />
    </div>
  </div>
</template>

<style scoped src="../styles/components/book-carousel.css"></style>
