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

  // Force reflow then animate
  nextTick(() => {
    requestAnimationFrame(() => {
      if (direction === 'left') {
        currentOffset.value = -100
        incomingOffset.value = 0
      } else {
        currentOffset.value = 100
        incomingOffset.value = 0
      }
    })
  })

  // After transition completes
  if (transitionTimer) clearTimeout(transitionTimer)
  transitionTimer = setTimeout(() => {
    currentIndex.value = pendingIndex.value >= 0 && pendingIndex.value < total.value ? pendingIndex.value : 0
    isTransitioning.value = false
    currentOffset.value = 0
    pendingIndex.value = -1
  }, SLIDE_DURATION)
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

<style scoped>
.carousel {
  position: relative;
  width: 100%;
  user-select: none;
  box-sizing: border-box;
}

.carousel-viewport {
  position: relative;
  width: 100%;
  border-radius: 12px;
  overflow: hidden;
}

.carousel-slide {
  position: absolute;
  inset: 0;
  border-radius: 12px;
  overflow: hidden;
  box-shadow: 0 10px 40px rgba(0, 0, 0, 0.4);
  will-change: transform;
}

.carousel-slide.is-transitioning {
  transition: transform var(--cczj-motion-carousel) var(--cczj-motion-ease-emphasis);
}

.carousel-slide-incoming {
  z-index: 2;
}

.slide-inner {
  position: relative;
  height: 100%;
  display: flex;
}

.slide-image-wrap {
  position: relative;
  overflow: hidden;
  background: #0a0a0f;
  border-radius: 12px 0 0 12px;
  flex-shrink: 0;
  width: 220px;
  min-width: 160px;
  max-width: 30%;
}

.slide-image {
  height: 100%;
  object-fit: contain;
  display: block;
}

.slide-image-gradient {
  position: absolute;
  inset: 0;
  background: linear-gradient(to right,
      transparent 0%,
      rgba(10, 10, 15, 0.05) 30%,
      rgba(10, 10, 15, 0.15) 60%,
      rgba(10, 10, 15, 0.25) 100%);
  pointer-events: none;
}

.slide-detail-wrap {
  flex: 1;
  position: relative;
  display: flex;
  flex-direction: column;
  justify-content: space-between;
  padding: 24px;
  padding-top: 28px;
  overflow: hidden;
}

.slide-detail-bg {
  position: absolute;
  inset: 0;
  background: linear-gradient(to right,
      rgba(10, 10, 15, 0.9) 0%,
      rgba(10, 10, 15, 0.95) 35%,
      rgba(8, 8, 12, 0.98) 50%);
  z-index: -1;
}

.slide-detail-content {
  position: relative;
  z-index: 2;
}

.slide-badge {
  display: inline-block;
  padding: 3px 10px;
  background: var(--danger);
  color: var(--danger-contrast);
  border-radius: 4px;
  font-size: 11px;
  font-weight: 700;
  margin-bottom: 12px;
  letter-spacing: 0.5px;
}

.slide-title {
  font-size: 20px;
  font-weight: 700;
  color: #fff;
  line-height: 1.3;
  margin: 0 0 12px 0;
  overflow: hidden;
  text-overflow: ellipsis;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
}

.slide-desc {
  font-size: 13px;
  color: rgba(255, 255, 255, 0.55);
  line-height: 1.6;
  margin: 0 0 14px 0;
  overflow: hidden;
  text-overflow: ellipsis;
  display: -webkit-box;
  -webkit-line-clamp: 3;
  -webkit-box-orient: vertical;
}

.slide-meta {
  display: flex;
  flex-direction: column;
  gap: 6px;
  margin-bottom: 14px;
}

.slide-meta-row {
  display: flex;
  align-items: baseline;
  gap: 8px;
  font-size: 13px;
  line-height: 1.5;
}

.slide-meta-label {
  color: rgba(255, 255, 255, 0.4);
  flex-shrink: 0;
  min-width: 32px;
}

.slide-meta-value {
  color: rgba(255, 255, 255, 0.75);
}

.slide-meta-actors {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.slide-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.slide-tag {
  padding: 3px 10px;
  border-radius: 4px;
  background: var(--bg-tag);
  color: var(--text-secondary);
  font-size: 11px;
}

.slide-tag-score {
  background: var(--warning-alpha-10);
  color: #fff;
  border: 1px solid var(--warning);
  font-weight: 600;
}

.slide-tag-votes {
  background: var(--accent-alpha-20);
  color: #fff;
  border: 1px solid var(--accent);
}

.slide-actions {
  display: flex;
  gap: 12px;
  position: relative;
  z-index: 2;
}

.slide-btn {
  padding: 8px 20px;
  border-radius: 6px;
  font-size: 13px;
  font-weight: 600;
  border: none;
  cursor: pointer;
  transition: background-color var(--cczj-motion-fast) var(--cczj-motion-ease-standard),
              box-shadow var(--cczj-motion-fast) var(--cczj-motion-ease-standard),
              transform var(--cczj-motion-fast) var(--cczj-motion-ease-standard);
}

.slide-btn-primary {
  background: var(--carousel-control);
  color: var(--carousel-control-text);
}

.slide-btn-primary:hover {
  transform: translateY(-1px);
  background: var(--accent-dim);
  box-shadow: 0 4px 12px var(--accent-alpha-35);
}

/* Navigation arrows */
.carousel-arrow {
  position: absolute;
  top: 50%;
  transform: translateY(-50%);
  z-index: 10;
  width: 40px;
  height: 40px;
  border-radius: 50%;
  border: none;
  background: var(--btn-solid);
  color: var(--btn-solid-text);
  font-size: 24px;
  line-height: 1;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: background-color var(--cczj-motion-fast) var(--cczj-motion-ease-standard),
              transform var(--cczj-motion-fast) var(--cczj-motion-ease-standard);
  padding: 0;
}

.carousel-arrow:hover {
  background: var(--accent-dim);
}

.carousel-arrow-left {
  left: 28px;
}

.carousel-arrow-right {
  right: 28px;
}

/* Indicators */
.carousel-indicators {
  position: absolute;
  bottom: 28px;
  left: 50%;
  transform: translateX(-50%);
  z-index: 30;
  display: flex;
  gap: 8px;
}

.carousel-indicator {
  height: 6px;
  border-radius: 9999px;
  border: none;
  cursor: pointer;
  transition: width var(--cczj-motion-normal) var(--cczj-motion-ease-standard),
              background-color var(--cczj-motion-fast) var(--cczj-motion-ease-standard);
  background: rgba(255, 255, 255, 0.2);
  width: 6px;
  padding: 0;
}

.carousel-indicator:hover {
  background: rgba(255, 255, 255, 0.4);
}

.carousel-indicator.active {
  width: 28px;
  background: var(--btn-solid);
}

/* Loading overlay */
.carousel-loading-overlay {
  position: absolute;
  inset: 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  background: rgba(0, 0, 0, 0.6);
  z-index: 20;
  border-radius: 12px;
}

.carousel-loading-spinner {
  width: 40px;
  height: 40px;
  border: 3px solid rgba(255, 255, 255, 0.2);
  border-top-color: var(--btn-solid);
  border-radius: 50%;
  animation: cczj-spin 800ms linear infinite;
}

.carousel-loading-text {
  margin-top: 12px;
  color: rgba(255, 255, 255, 0.9);
  font-size: 14px;
}
</style>
