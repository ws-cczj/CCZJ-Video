<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, watch, nextTick } from 'vue'
import { useRouter } from 'vue-router'
import { getDetailPath, getProxiedImageUrl } from '../utils'
import type { Video } from '../types'
import imageNotFound from '../assets/images/image_notfound.png'

interface Props {
  slides: Video[]
  sourceKey: string
  onSlideClick?: (video: Video) => void | Promise<void>
}

const props = defineProps<Props>()
const router = useRouter()

const PAGE_HEIGHT = 360
const PAGE_PADDING = 20
const SLIDE_DURATION = 500
const AUTO_INTERVAL = 6000

const currentIndex = ref(0)
const isTransitioning = ref(false)
const slideDirection = ref<'left' | 'right'>('left')
const pendingIndex = ref(-1)

let timer: ReturnType<typeof setInterval> | null = null
let transitionTimer: ReturnType<typeof setTimeout> | null = null

const total = computed(() => props.slides.length)
const activeSlide = computed(() => props.slides[currentIndex.value])

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
    currentIndex.value = pendingIndex.value
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
const proxiedUrls = ref<Map<string, string>>(new Map())
const slideImgUrls = ref<Map<number, string>>(new Map())

async function getImgUrl(rawUrl: string): Promise<string> {
  if (!rawUrl) return ''
  if (proxiedUrls.value.has(rawUrl)) return proxiedUrls.value.get(rawUrl)!
  try {
    const url = await getProxiedImageUrl(rawUrl)
    proxiedUrls.value.set(rawUrl, url)
    return url
  } catch {
    return rawUrl
  }
}

watch(() => props.slides, async (slides) => {
  slideImgUrls.value.clear()
  proxiedUrls.value.clear()
  currentIndex.value = 0
  isTransitioning.value = false
  currentOffset.value = 0
  pendingIndex.value = -1
  for (let i = 0; i < slides.length; i++) {
    const url = await getImgUrl((slides[i] as any)?.vod_pic || '')
    slideImgUrls.value.set(i, url)
  }
}, { immediate: true })

onMounted(() => {
  startAutoPlay()
})

onUnmounted(() => {
  stopAutoPlay()
  if (transitionTimer) clearTimeout(transitionTimer)
})

function onImageError(evt: Event): void {
  const img = evt.target as HTMLImageElement
  if (img && img.src !== imageNotFound) {
    img.src = imageNotFound
  }
}

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
        :style="{ transform: `translateX(${currentOffset}%)`, transition: isTransitioning ? `transform ${SLIDE_DURATION}ms ease-in-out` : 'none' }">
        <div class="slide-inner" v-if="activeSlide">
          <div class="slide-image-wrap">
            <img :src="slideImgUrls.get(currentIndex) || ''" :alt="activeSlide?.vod_name || ''"
              class="slide-image" draggable="false" referrerpolicy="no-referrer" @error="onImageError" />
            <div class="slide-image-gradient" />
          </div>
          <div class="slide-detail-wrap">
            <div class="slide-detail-bg" />
            <div class="slide-detail-content">
              <span class="slide-badge">豆瓣热榜</span>
              <h2 class="slide-title">{{ activeSlide?.vod_name || '' }}</h2>
              <div class="slide-meta">
                <div v-if="(activeSlide as any)?.release_date" class="slide-meta-row">
                  <span class="slide-meta-label">上映</span>
                  <span class="slide-meta-value">{{ (activeSlide as any).release_date }}</span>
                </div>
                <div v-if="(activeSlide as any)?.director" class="slide-meta-row">
                  <span class="slide-meta-label">导演</span>
                  <span class="slide-meta-value">{{ (activeSlide as any).director }}</span>
                </div>
                <div v-if="(activeSlide as any)?.actors" class="slide-meta-row">
                  <span class="slide-meta-label">主演</span>
                  <span class="slide-meta-value slide-meta-actors">{{ (activeSlide as any).actors }}</span>
                </div>
              </div>
              <div class="slide-tags">
                <span v-if="(activeSlide as any)?.year" class="slide-tag">{{ (activeSlide as any).year }}</span>
                <span v-if="(activeSlide as any)?.area" class="slide-tag">{{ (activeSlide as any).area }}</span>
                <span v-if="activeSlide?.vod_score && Number(activeSlide.vod_score) > 0"
                  class="slide-tag slide-tag-score">{{ activeSlide.vod_score }}分</span>
                <span v-if="activeSlide?.vod_remarks" class="slide-tag slide-tag-votes">{{ activeSlide.vod_remarks }}</span>
              </div>
            </div>
            <div class="slide-actions">
              <button class="slide-btn slide-btn-primary" @click.stop="goDetail(activeSlide)">查看详情</button>
            </div>
          </div>
        </div>
      </div>

      <!-- Incoming slide (during transition) -->
      <div v-if="isTransitioning && pendingIndex >= 0 && getSlideData(pendingIndex)"
        class="carousel-slide carousel-slide-incoming"
        :style="{ transform: `translateX(${incomingOffset}%)`, transition: isTransitioning ? `transform ${SLIDE_DURATION}ms ease-in-out` : 'none' }">
        <div class="slide-inner">
          <div class="slide-image-wrap">
            <img :src="slideImgUrls.get(pendingIndex) || ''" :alt="getSlideData(pendingIndex)?.vod_name || ''"
              class="slide-image" draggable="false" referrerpolicy="no-referrer" @error="onImageError" />
            <div class="slide-image-gradient" />
          </div>
          <div class="slide-detail-wrap">
            <div class="slide-detail-bg" />
            <div class="slide-detail-content">
              <span class="slide-badge">豆瓣热榜</span>
              <h2 class="slide-title">{{ getSlideData(pendingIndex)?.vod_name || '' }}</h2>
              <div class="slide-meta">
                <div v-if="(getSlideData(pendingIndex) as any)?.release_date" class="slide-meta-row">
                  <span class="slide-meta-label">上映</span>
                  <span class="slide-meta-value">{{ (getSlideData(pendingIndex) as any).release_date }}</span>
                </div>
                <div v-if="(getSlideData(pendingIndex) as any)?.director" class="slide-meta-row">
                  <span class="slide-meta-label">导演</span>
                  <span class="slide-meta-value">{{ (getSlideData(pendingIndex) as any).director }}</span>
                </div>
                <div v-if="(getSlideData(pendingIndex) as any)?.actors" class="slide-meta-row">
                  <span class="slide-meta-label">主演</span>
                  <span class="slide-meta-value slide-meta-actors">{{ (getSlideData(pendingIndex) as any).actors }}</span>
                </div>
              </div>
              <div class="slide-tags">
                <span v-if="(getSlideData(pendingIndex) as any)?.year" class="slide-tag">{{ (getSlideData(pendingIndex) as any).year }}</span>
                <span v-if="(getSlideData(pendingIndex) as any)?.area" class="slide-tag">{{ (getSlideData(pendingIndex) as any).area }}</span>
                <span v-if="getSlideData(pendingIndex)?.vod_score && Number(getSlideData(pendingIndex)!.vod_score) > 0"
                  class="slide-tag slide-tag-score">{{ getSlideData(pendingIndex)!.vod_score }}分</span>
                <span v-if="getSlideData(pendingIndex)?.vod_remarks" class="slide-tag slide-tag-votes">{{ getSlideData(pendingIndex)!.vod_remarks }}</span>
              </div>
            </div>
            <div class="slide-actions">
              <button class="slide-btn slide-btn-primary" @click.stop="goDetail(activeSlide)">查看详情</button>
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- Loading overlay when clicking -->
    <div v-if="clickingLoading" class="carousel-loading-overlay">
      <div class="carousel-loading-spinner" />
      <span class="carousel-loading-text">加载中...</span>
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
  background: rgba(239, 68, 68, 0.9);
  color: #fff;
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
  background: rgba(255, 255, 255, 0.1);
  color: rgba(255, 255, 255, 0.65);
  font-size: 11px;
}

.slide-tag-score {
  background: rgba(245, 158, 11, 0.2);
  color: #fcd34d;
  font-weight: 600;
}

.slide-tag-votes {
  background: rgba(99, 102, 241, 0.15);
  color: rgba(165, 180, 252, 0.85);
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
  transition: all 0.25s ease;
}

.slide-btn-primary {
  background: linear-gradient(135deg, #10b981 0%, #059669 100%);
  color: #fff;
}

.slide-btn-primary:hover {
  transform: translateY(-1px);
  box-shadow: 0 4px 12px rgba(16, 185, 129, 0.35);
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
  background: rgba(0, 0, 0, 0.5);
  color: #fff;
  font-size: 24px;
  line-height: 1;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: all 0.2s ease;
  padding: 0;
}

.carousel-arrow:hover {
  background: rgba(0, 0, 0, 0.7);
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
  transition: all 0.3s ease;
  background: rgba(255, 255, 255, 0.2);
  width: 6px;
  padding: 0;
}

.carousel-indicator:hover {
  background: rgba(255, 255, 255, 0.4);
}

.carousel-indicator.active {
  width: 28px;
  background: #10b981;
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
  border-top-color: #10b981;
  border-radius: 50%;
  animation: carousel-spin 0.8s linear infinite;
}

@keyframes carousel-spin {
  to { transform: rotate(360deg); }
}

.carousel-loading-text {
  margin-top: 12px;
  color: rgba(255, 255, 255, 0.9);
  font-size: 14px;
}
</style>
