<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, watch } from 'vue'
import { useRouter } from 'vue-router'
import { getDetailPath, getProxiedImageUrl } from '../utils'
import type { Video } from '../types'

interface Props {
  slides: Video[]
  sourceKey: string
}

const props = defineProps<Props>()
const router = useRouter()

const PAGE_HEIGHT = 360
const PAGE_PADDING = 20
const FLIP_DURATION = 700
const AUTO_INTERVAL = 6000

const currentIndex = ref(0)
const flipState = ref<'idle' | 'next'>('idle')
const pendingIndex = ref(0)
const animationKey = ref(0)
let timer: ReturnType<typeof setInterval> | null = null

const total = computed(() => props.slides.length)
const activeSlide = computed(() => props.slides[currentIndex.value])
const nextSlideIndex = computed(() => (currentIndex.value + 1) % total.value)

function goNext(): void {
  if (flipState.value !== 'idle' || total.value <= 1) return
  pendingIndex.value = nextSlideIndex.value
  flipState.value = 'next'
  animationKey.value++
}

function onFlipEnd(): void {
  if (flipState.value === 'idle') return
  currentIndex.value = pendingIndex.value
  flipState.value = 'idle'
}

function startAutoPlay(): void {
  stopAutoPlay()
  if (total.value <= 1) return
  timer = setInterval(goNext, AUTO_INTERVAL)
}

function stopAutoPlay(): void {
  if (timer) { clearInterval(timer); timer = null }
}

function goDetail(video: Video): void {
  const vodId = String((video as any).vod_id ?? '')
  const sk = String((video as any).source_key || props.sourceKey || '')
  if (sk && vodId) {
    router.push(getDetailPath(sk, { vod_id: vodId }))
  }
}

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
  flipState.value = 'idle'
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
})

function onMouseEnter(): void {
  stopAutoPlay()
}

function onMouseLeave(): void {
  startAutoPlay()
}
</script>

<template>
  <div v-if="total > 0" class="book-carousel"
    :style="{ height: PAGE_HEIGHT + PAGE_PADDING * 2 + 'px', padding: PAGE_PADDING + 'px' }" @mouseenter="onMouseEnter"
    @mouseleave="onMouseLeave">
    <div class="bc-page-wrapper" :style="{ height: PAGE_HEIGHT + 'px' }">
      <div class="bc-page bc-page-next">
        <div class="bc-page-inner">
          <div class="bc-image-wrap">
            <img :src="slideImgUrls.get(nextSlideIndex) || ''" :alt="slides[nextSlideIndex]?.vod_name || ''"
              class="bc-image" draggable="false" />
            <div class="bc-image-gradient" />
          </div>
          <div class="bc-detail-wrap">
            <div class="bc-detail-bg" />
            <div class="bc-detail-content">
              <span class="bc-badge">热播推荐</span>
              <h2 class="bc-title">{{ slides[nextSlideIndex]?.vod_name || '' }}</h2>
              <p class="bc-desc">{{ slides[nextSlideIndex]?.vod_content?.replace(/<[^>]+>/g, '') ||
                slides[nextSlideIndex]?.vod_remarks || '' }}</p>
              <div class="bc-tags">
                <span v-if="slides[nextSlideIndex]?.type_name" class="bc-tag">{{ slides[nextSlideIndex].type_name
                }}</span>
                <span v-if="slides[nextSlideIndex]?.vod_year" class="bc-tag">{{ slides[nextSlideIndex].vod_year
                }}</span>
                <span v-if="slides[nextSlideIndex]?.vod_area" class="bc-tag">{{ slides[nextSlideIndex].vod_area
                }}</span>
                <span v-if="slides[nextSlideIndex]?.vod_score && Number(slides[nextSlideIndex].vod_score) > 0"
                  class="bc-tag bc-tag-score">{{ slides[nextSlideIndex].vod_score }}分</span>
              </div>
            </div>
            <div class="bc-actions">
              <button class="bc-btn bc-btn-primary" @click.stop="goDetail(slides[nextSlideIndex])">立即播放</button>
              <button class="bc-btn bc-btn-secondary" @click.stop="goDetail(slides[nextSlideIndex])">详情</button>
            </div>
          </div>
        </div>
      </div>

      <div v-if="flipState === 'next'" :key="animationKey" class="bc-page bc-page-flipping"
        :style="{ animationDuration: FLIP_DURATION + 'ms' }" @animationend="onFlipEnd">
        <div class="bc-page-inner">
          <div class="bc-image-wrap">
            <img :src="slideImgUrls.get(currentIndex) || ''" :alt="activeSlide?.vod_name || ''" class="bc-image"
              draggable="false" />
            <div class="bc-image-gradient" />
          </div>
          <div class="bc-detail-wrap">
            <div class="bc-detail-bg" />
            <div class="bc-detail-content">
              <span class="bc-badge">热播推荐</span>
              <h2 class="bc-title">{{ activeSlide?.vod_name || '' }}</h2>
              <p class="bc-desc">{{ activeSlide?.vod_content?.replace(/<[^>]+>/g, '') || activeSlide?.vod_remarks || ''
              }}</p>
              <div class="bc-tags">
                <span v-if="activeSlide?.type_name" class="bc-tag">{{ activeSlide.type_name }}</span>
                <span v-if="activeSlide?.vod_year" class="bc-tag">{{ activeSlide.vod_year }}</span>
                <span v-if="activeSlide?.vod_area" class="bc-tag">{{ activeSlide.vod_area }}</span>
                <span v-if="activeSlide?.vod_score && Number(activeSlide.vod_score) > 0" class="bc-tag bc-tag-score">{{
                  activeSlide.vod_score }}分</span>
              </div>
            </div>
            <div class="bc-actions">
              <button class="bc-btn bc-btn-primary" @click.stop="goDetail(activeSlide)">立即播放</button>
              <button class="bc-btn bc-btn-secondary" @click.stop="goDetail(activeSlide)">详情</button>
            </div>
          </div>
        </div>
        <div class="bc-flip-shadow" />
      </div>

      <div class="bc-page bc-page-current" v-if="flipState === 'idle'">
        <div class="bc-page-inner">
          <div class="bc-image-wrap">
            <img :src="slideImgUrls.get(currentIndex) || ''" :alt="activeSlide?.vod_name || ''" class="bc-image"
              draggable="false" />
            <div class="bc-image-gradient" />
          </div>
          <div class="bc-detail-wrap">
            <div class="bc-detail-bg" />
            <div class="bc-detail-content">
              <span class="bc-badge">热播推荐</span>
              <h2 class="bc-title">{{ activeSlide?.vod_name || '' }}</h2>
              <p class="bc-desc">{{ activeSlide?.vod_content?.replace(/<[^>]+>/g, '') || activeSlide?.vod_remarks || ''
              }}</p>
              <div class="bc-tags">
                <span v-if="activeSlide?.type_name" class="bc-tag">{{ activeSlide.type_name }}</span>
                <span v-if="activeSlide?.vod_year" class="bc-tag">{{ activeSlide.vod_year }}</span>
                <span v-if="activeSlide?.vod_area" class="bc-tag">{{ activeSlide.vod_area }}</span>
                <span v-if="activeSlide?.vod_score && Number(activeSlide.vod_score) > 0" class="bc-tag bc-tag-score">{{
                  activeSlide.vod_score }}分</span>
              </div>
            </div>
            <div class="bc-actions">
              <button class="bc-btn bc-btn-primary" @click.stop="goDetail(activeSlide)">立即播放</button>
              <button class="bc-btn bc-btn-secondary" @click.stop="goDetail(activeSlide)">详情</button>
            </div>
          </div>
        </div>

        <div class="bc-corner" @click.stop="goNext">
          <div class="bc-corner-fold" />
          <div class="bc-corner-shine" />
          <div class="bc-corner-shadow" />
        </div>
      </div>
    </div>

    <div class="bc-indicators">
      <button v-for="(_, idx) in slides.slice(0, Math.min(total, 10))" :key="'ind-' + idx" class="bc-indicator"
        :class="{ active: idx === currentIndex }" :disabled="flipState !== 'idle'" @click.stop="currentIndex = idx" />
    </div>
  </div>
</template>

<style scoped>
@keyframes bookFlipLeft {
  0% {
    transform-origin: left top;
    transform: rotateX(0deg) rotateY(0deg) rotateZ(0deg) scale(1);
    opacity: 1;
    box-shadow: 0 10px 40px rgba(0, 0, 0, 0.4);
  }

  20% {
    transform-origin: left top;
    transform: rotateX(-15deg) rotateY(10deg) rotateZ(-2deg) scale(1.01);
    opacity: 1;
    box-shadow: -5px 5px 30px rgba(0, 0, 0, 0.45);
  }

  45% {
    transform-origin: left top;
    transform: rotateX(-45deg) rotateY(35deg) rotateZ(-8deg) scale(0.97);
    opacity: 1;
    box-shadow: -20px 10px 50px rgba(0, 0, 0, 0.5);
  }

  70% {
    transform-origin: left top;
    transform: rotateX(-70deg) rotateY(60deg) rotateZ(-15deg) scale(0.88);
    opacity: 0.7;
    box-shadow: -35px 5px 40px rgba(0, 0, 0, 0.35);
  }

  100% {
    transform-origin: left top;
    transform: rotateX(-90deg) rotateY(90deg) rotateZ(-20deg) scale(0.6);
    opacity: 0;
    box-shadow: none;
  }
}

.book-carousel {
  position: relative;
  width: 100%;
  user-select: none;
  box-sizing: border-box;
}

.bc-page-wrapper {
  position: relative;
  width: 100%;
  perspective: 2500px;
}

.bc-page {
  position: absolute;
  inset: 0;
  border-radius: 12px;
  overflow: hidden;
  box-shadow: 0 10px 40px rgba(0, 0, 0, 0.4);
  cursor: default;
}

.bc-page-inner {
  position: relative;
  height: 100%;
  display: flex;
}

.bc-page-flipping {
  z-index: 10;
  transform-style: preserve-3d;
  animation: bookFlipLeft cubic-bezier(0.45, 0, 0.25, 1) forwards;
}

.bc-page-next {
  z-index: 1;
}

.bc-page-current {
  z-index: 3;
}

.bc-flip-shadow {
  position: absolute;
  inset: 0;
  background: linear-gradient(to left, transparent 0%, rgba(0, 0, 0, 0.5) 100%);
  pointer-events: none;
  z-index: 5;
}

.bc-image-wrap {
  position: relative;
  overflow: hidden;
  background: #0a0a0f;
  border-radius: 12px 0 0 12px;
}

.bc-image {
  height: 100%;
  object-fit: contain;
  display: block;
}

.bc-image-gradient {
  position: absolute;
  inset: 0;
  background: linear-gradient(to right,
      transparent 0%,
      rgba(10, 10, 15, 0.05) 30%,
      rgba(10, 10, 15, 0.15) 60%,
      rgba(10, 10, 15, 0.25) 100%);
  pointer-events: none;
}

.bc-detail-wrap {
  flex: 1;
  position: relative;
  display: flex;
  flex-direction: column;
  justify-content: space-between;
  padding: 24px;
  padding-top: 28px;
  overflow: hidden;
}

.bc-detail-bg {
  position: absolute;
  inset: 0;
  background: linear-gradient(to right,
      rgba(10, 10, 15, 0.9) 0%,
      rgba(10, 10, 15, 0.95) 35%,
      rgba(8, 8, 12, 0.98) 50%);
  z-index: -1;
}

.bc-detail-content {
  position: relative;
  z-index: 2;
}

.bc-badge {
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

.bc-title {
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

.bc-desc {
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

.bc-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.bc-tag {
  padding: 3px 10px;
  border-radius: 4px;
  background: rgba(255, 255, 255, 0.1);
  color: rgba(255, 255, 255, 0.65);
  font-size: 11px;
}

.bc-tag-score {
  background: rgba(245, 158, 11, 0.2);
  color: #fcd34d;
  font-weight: 600;
}

.bc-actions {
  display: flex;
  gap: 12px;
  position: relative;
  z-index: 2;
}

.bc-btn {
  padding: 8px 20px;
  border-radius: 6px;
  font-size: 13px;
  font-weight: 600;
  border: none;
  cursor: pointer;
  transition: all 0.25s ease;
}

.bc-btn-primary {
  background: linear-gradient(135deg, #10b981 0%, #059669 100%);
  color: #fff;
}

.bc-btn-primary:hover {
  transform: translateY(-1px);
  box-shadow: 0 4px 12px rgba(16, 185, 129, 0.35);
}

.bc-btn-secondary {
  background: rgba(255, 255, 255, 0.1);
  color: rgba(255, 255, 255, 0.8);
  border: 1px solid rgba(255, 255, 255, 0.2);
}

.bc-btn-secondary:hover {
  background: rgba(255, 255, 255, 0.15);
  border-color: rgba(255, 255, 255, 0.3);
}

.bc-corner {
  position: absolute;
  bottom: 0;
  right: 0;
  width: 64px;
  height: 64px;
  z-index: 20;
  cursor: pointer;
  transform-style: preserve-3d;
  perspective: 300px;
}

.bc-corner-fold {
  position: absolute;
  inset: 0;
  clip-path: polygon(100% 0%, 100% 100%, 0% 100%);
  background: linear-gradient(225deg,
      rgba(255, 255, 255, 0.95) 0%,
      rgba(230, 230, 245, 0.95) 30%,
      rgba(200, 200, 220, 0.9) 60%,
      rgba(160, 160, 185, 0.85) 100%);
  transform-origin: right bottom;
  transition: transform 0.5s cubic-bezier(0.23, 1, 0.32, 1),
    box-shadow 0.5s ease;
  transform: rotateX(0deg) rotateY(0deg);
  box-shadow: -2px -2px 6px rgba(0, 0, 0, 0.2);
  backface-visibility: visible;
}

.bc-corner:hover .bc-corner-fold {
  transform: rotateX(-35deg) rotateY(25deg) rotateZ(-5deg) translateZ(15px);
  box-shadow:
    -8px -8px 20px rgba(0, 0, 0, 0.35),
    -3px -3px 8px rgba(255, 255, 255, 0.3);
}

.bc-corner-shine {
  position: absolute;
  inset: 0;
  clip-path: polygon(100% 0%, 100% 100%, 0% 100%);
  background: linear-gradient(to left top,
      rgba(255, 255, 255, 0.7) 0%,
      rgba(255, 255, 255, 0.2) 40%,
      transparent 70%);
  transform-origin: right bottom;
  transition: transform 0.5s cubic-bezier(0.23, 1, 0.32, 1), opacity 0.5s ease;
  transform: rotateX(0deg) rotateY(0deg);
  pointer-events: none;
}

.bc-corner:hover .bc-corner-shine {
  transform: rotateX(-35deg) rotateY(25deg) rotateZ(-5deg) translateZ(16px);
  opacity: 0.8;
}

.bc-corner-shadow {
  position: absolute;
  inset: 0;
  clip-path: polygon(100% 0%, 100% 100%, 0% 100%);
  background: radial-gradient(ellipse at 100% 100%,
      rgba(0, 0, 0, 0.3) 0%,
      rgba(0, 0, 0, 0.1) 50%,
      transparent 80%);
  transition: opacity 0.5s ease;
  pointer-events: none;
}

.bc-corner:hover .bc-corner-shadow {
  opacity: 0.3;
}

.bc-indicators {
  position: absolute;
  bottom: 28px;
  left: 50%;
  transform: translateX(-50%);
  z-index: 30;
  display: flex;
  gap: 8px;
}

.bc-indicator {
  height: 6px;
  border-radius: 9999px;
  border: none;
  cursor: pointer;
  transition: all 0.3s ease;
  background: rgba(255, 255, 255, 0.2);
  width: 6px;
  padding: 0;
}

.bc-indicator:hover {
  background: rgba(255, 255, 255, 0.4);
}

.bc-indicator.active {
  width: 28px;
  background: #10b981;
}
</style>
