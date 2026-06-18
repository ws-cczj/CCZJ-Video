<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, watch } from 'vue'
import { useRouter } from 'vue-router'
import Icon from './Icon.vue'
import { Button } from './ui'
import { getDetailPath, getProxiedImageUrl } from '../utils'
import type { Video } from '../types'

interface Props {
  slides: Video[]
  sourceKey: string
}

const props = defineProps<Props>()
const router = useRouter()

const activeIndex = ref(0)
const isFlipping = ref(false)
const flipDirection = ref<'left' | 'right'>('right')
const imageColors = ref<Map<number, string>>(new Map())
const loadedImages = ref<Set<number>>(new Set())
let timer: ReturnType<typeof setInterval> | null = null
const INTERVAL = 5000

const activeSlide = computed(() => props.slides[activeIndex.value])

function next(): void {
  const N = props.slides.length
  if (N <= 1 || isFlipping.value) return
  flipDirection.value = 'right'
  isFlipping.value = true
  activeIndex.value = (activeIndex.value + 1) % N
  setTimeout(() => { isFlipping.value = false }, 600)
}

function prev(): void {
  const N = props.slides.length
  if (N <= 1 || isFlipping.value) return
  flipDirection.value = 'left'
  isFlipping.value = true
  activeIndex.value = (activeIndex.value - 1 + N) % N
  setTimeout(() => { isFlipping.value = false }, 600)
}

function goTo(index: number): void {
  const N = props.slides.length
  if (index === activeIndex.value || isFlipping.value) return
  flipDirection.value = index > activeIndex.value ? 'right' : 'left'
  isFlipping.value = true
  activeIndex.value = index
  setTimeout(() => { isFlipping.value = false }, 600)
}

function start(): void {
  stop()
  if (props.slides.length <= 1) return
  timer = setInterval(next, INTERVAL)
}

function stop(): void {
  if (timer) { clearInterval(timer); timer = null }
}

function goDetail(video: Video): void {
  const vodId = String((video as any).vod_id ?? '')
  const sk = String((video as any).source_key || props.sourceKey || '')
  if (sk && vodId) {
    router.push(getDetailPath(sk, { vod_id: vodId }))
  }
}

async function extractImageColor(index: number, src: string): Promise<void> {
  if (loadedImages.value.has(index)) return
  loadedImages.value.add(index)
  if (!src) {
    imageColors.value.set(index, '30,30,40')
    return
  }

  async function tryExtract(url: string): Promise<boolean> {
    return new Promise((resolve) => {
      const img = new Image()
      img.crossOrigin = 'anonymous'
      img.onload = () => {
        try {
          const canvas = document.createElement('canvas')
          const ctx = canvas.getContext('2d')
          if (!ctx) {
            resolve(false)
            return
          }

          canvas.width = 100
          canvas.height = 100
          ctx.drawImage(img, 0, 0, 100, 100)

          const imageData = ctx.getImageData(0, 0, 100, 100)
          const data = imageData.data

          let r = 0, g = 0, b = 0, count = 0
          for (let i = 0; i < data.length; i += 4) {
            const alpha = data[i + 3]
            if (alpha > 128) {
              r += data[i]
              g += data[i + 1]
              b += data[i + 2]
              count++
            }
          }

          if (count > 0) {
            r = Math.round(r / count)
            g = Math.round(g / count)
            b = Math.round(b / count)

            const luminance = (0.299 * r + 0.587 * g + 0.114 * b) / 255
            let darkenFactor = 0.4
            if (luminance > 0.7) {
              darkenFactor = 0.6
            } else if (luminance < 0.3) {
              darkenFactor = 0.2
            }

            r = Math.round(r * darkenFactor)
            g = Math.round(g * darkenFactor)
            b = Math.round(b * darkenFactor)

            imageColors.value.set(index, `${r},${g},${b}`)
            resolve(true)
          } else {
            resolve(false)
          }
        } catch {
          resolve(false)
        }
      }
      img.onerror = () => {
        resolve(false)
      }
      img.src = url
    })
  }

  const success = await tryExtract(src)
  if (!success) {
    try {
      const proxiedUrl = await getProxiedImageUrl(src)
      if (proxiedUrl && proxiedUrl !== src) {
        await tryExtract(proxiedUrl)
      }
    } catch {
      imageColors.value.set(index, '30,30,40')
    }
  }
}

watch(() => props.slides, (newSlides) => {
  imageColors.value.clear()
  loadedImages.value.clear()
  activeIndex.value = 0
  start()
}, { immediate: true })

onMounted(() => {
  start()
})

onUnmounted(() => {
  stop()
})
</script>

<template>
  <div
    class="b-carousel"
    @mouseenter="stop"
    @mouseleave="start"
  >
    <div class="b-carousel-left">
      <div class="b-carousel-image-wrapper">
        <div
          class="b-carousel-flip-container"
          :class="{ 'is-flipping': isFlipping, 'flip-left': flipDirection === 'left', 'flip-right': flipDirection === 'right' }"
        >
          <div
            v-if="activeSlide"
            :key="String((activeSlide as any).vod_id)"
            class="b-carousel-image-inner"
          >
            <img
              :src="(activeSlide as any)?.vod_pic || ''"
              :alt="(activeSlide as any)?.vod_name || ''"
              class="b-carousel-image"
              @load="extractImageColor(activeIndex, (activeSlide as any)?.vod_pic || '')"
              referrerpolicy="no-referrer"
              draggable="false"
              @click="goDetail(activeSlide)"
            />
            <div
              class="b-carousel-gradient-bar"
              :style="{
                background: imageColors.has(activeIndex)
                  ? `linear-gradient(to right, rgba(${imageColors.get(activeIndex)}, 0.9) 0%, rgba(${imageColors.get(activeIndex)}, 0.5) 60%, transparent 100%)`
                  : 'linear-gradient(to right, rgba(30,30,40,0.9) 0%, rgba(30,30,40,0.5) 60%, transparent 100%)'
              }"
            >
              <div class="b-carousel-gradient-title">{{ (activeSlide as any)?.vod_name || '' }}</div>
              <div class="b-carousel-gradient-desc">{{ (activeSlide as any)?.vod_remarks || '' }}</div>
            </div>
          </div>
        </div>
      </div>

      <button class="b-carousel-prev" @click="prev">
        <Icon name="chevron-left" :size="28" />
      </button>
      <button class="b-carousel-next" @click="next">
        <Icon name="chevron-right" :size="28" />
      </button>

      <div class="b-carousel-indicators">
        <button
          v-for="(_, idx) in slides"
          :key="`b-cs-ind-${idx}`"
          class="b-carousel-indicator"
          :class="{ active: idx === activeIndex }"
          @click="goTo(idx)"
        ></button>
      </div>
    </div>

    <div class="b-carousel-right">
      <div class="b-carousel-info">
        <div class="b-carousel-badge">热播</div>
        <h2 class="b-carousel-title">{{ (activeSlide as any)?.vod_name || '' }}</h2>
        <p class="b-carousel-desc">{{ (activeSlide as any)?.vod_blurb || (activeSlide as any)?.vod_content || (activeSlide as any)?.vod_remarks || '' }}</p>
        <div class="b-carousel-tags">
          <span class="b-carousel-tag">{{ (activeSlide as any)?.type_name || '' }}</span>
          <span class="b-carousel-tag">{{ (activeSlide as any)?.vod_year || '' }}</span>
          <span class="b-carousel-tag">{{ (activeSlide as any)?.vod_score || '0.0' }}分</span>
        </div>
      </div>

      <div class="b-carousel-actions">
        <Button variant="primary" size="lg" @click="goDetail(activeSlide!)">
          <Icon name="play" :size="18" />
          <span>立即播放</span>
        </Button>
        <Button variant="secondary" size="lg" @click="goDetail(activeSlide!)">
          <Icon name="info" :size="16" />
          <span>详情</span>
        </Button>
        <Button variant="ghost" size="lg" icon>
          <Icon name="star" :size="18" />
        </Button>
      </div>

      <div class="b-carousel-pages">
        <div
          v-for="(item, idx) in slides.slice(0, 6)"
          :key="`b-cs-page-${idx}`"
          class="b-carousel-page"
          :class="{ 
            active: idx === activeIndex,
            'page-0': idx === 0,
            'page-1': idx === 1,
            'page-2': idx === 2,
            'page-3': idx === 3,
            'page-4': idx === 4,
            'page-5': idx === 5
          }"
          @click="goTo(idx)"
        >
          <div class="b-carousel-page-inner">
            <img
              :src="(item as any)?.vod_pic || ''"
              :alt="(item as any)?.vod_name || ''"
              class="b-carousel-page-img"
              referrerpolicy="no-referrer"
              draggable="false"
            />
            <div class="b-carousel-page-shadow"></div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.b-carousel {
  display: flex;
  width: 100%;
  height: 380px;
  border-radius: 12px;
  overflow: hidden;
  background: rgba(20, 20, 30, 0.95);
  box-shadow: 0 8px 32px rgba(0, 0, 0, 0.4);
}

.b-carousel-left {
  flex: 1;
  position: relative;
  overflow: hidden;
  background: linear-gradient(135deg, #1a1a2e 0%, #16213e 50%, #0f3460 100%);
}

.b-carousel-image-wrapper {
  position: absolute;
  inset: 0;
  perspective: 1500px;
}

.b-carousel-flip-container {
  position: absolute;
  inset: 0;
  transform-style: preserve-3d;
}

.b-carousel-flip-container.is-flipping.flip-right {
  animation: flipPageRight 0.6s ease-in-out;
}

.b-carousel-flip-container.is-flipping.flip-left {
  animation: flipPageLeft 0.6s ease-in-out;
}

@keyframes flipPageRight {
  0% {
    transform: rotateY(0deg);
  }
  50% {
    transform: rotateY(-20deg);
  }
  100% {
    transform: rotateY(0deg);
  }
}

@keyframes flipPageLeft {
  0% {
    transform: rotateY(0deg);
  }
  50% {
    transform: rotateY(20deg);
  }
  100% {
    transform: rotateY(0deg);
  }
}

.b-carousel-image-inner {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
}

.b-carousel-image {
  width: 100%;
  height: 100%;
  object-fit: contain;
  cursor: pointer;
  transition: transform 0.5s ease;
}

.b-carousel-image:hover {
  transform: scale(1.03);
}

.b-carousel-gradient-bar {
  position: absolute;
  left: 0;
  bottom: 0;
  width: 55%;
  height: 70px;
  padding: 12px 18px;
  display: flex;
  flex-direction: column;
  justify-content: flex-end;
  pointer-events: none;
}

.b-carousel-gradient-title {
  font-size: 15px;
  font-weight: 700;
  color: #fff;
  margin: 0 0 4px 0;
  line-height: 1.3;
  text-shadow: 0 2px 8px rgba(0, 0, 0, 0.6);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.b-carousel-gradient-desc {
  font-size: 11px;
  color: rgba(255, 255, 255, 0.85);
  margin: 0;
  line-height: 1.4;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  text-shadow: 0 1px 4px rgba(0, 0, 0, 0.5);
}

.b-carousel-prev,
.b-carousel-next {
  position: absolute;
  top: 50%;
  transform: translateY(-50%);
  width: 44px;
  height: 44px;
  border-radius: 50%;
  background: rgba(0, 0, 0, 0.5);
  border: none;
  color: #fff;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: all 0.3s ease;
  z-index: 10;
}

.b-carousel-prev {
  left: 12px;
}

.b-carousel-next {
  right: 12px;
}

.b-carousel-prev:hover,
.b-carousel-next:hover {
  background: rgba(0, 0, 0, 0.8);
  transform: translateY(-50%) scale(1.1);
}

.b-carousel-indicators {
  position: absolute;
  bottom: 16px;
  right: 16px;
  display: flex;
  gap: 6px;
}

.b-carousel-indicator {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: rgba(255, 255, 255, 0.4);
  border: none;
  cursor: pointer;
  transition: all 0.3s ease;
}

.b-carousel-indicator:hover {
  background: rgba(255, 255, 255, 0.7);
}

.b-carousel-indicator.active {
  width: 16px;
  border-radius: 3px;
  background: #fff;
}

.b-carousel-right {
  width: 320px;
  display: flex;
  flex-direction: column;
  padding: 18px;
  background: linear-gradient(to right, rgba(25, 25, 35, 0.98), rgba(35, 35, 50, 0.95));
}

.b-carousel-info {
  flex: 1;
}

.b-carousel-badge {
  display: inline-block;
  padding: 3px 12px;
  background: linear-gradient(135deg, #ff6b6b, #ee5a5a);
  color: #fff;
  border-radius: 4px;
  font-size: 12px;
  font-weight: 600;
  margin-bottom: 8px;
}

.b-carousel-title {
  font-size: 18px;
  font-weight: 800;
  color: #fff;
  margin: 0 0 8px 0;
  line-height: 1.3;
  overflow: hidden;
  text-overflow: ellipsis;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
}

.b-carousel-desc {
  font-size: 12px;
  color: rgba(255, 255, 255, 0.6);
  margin: 0 0 12px 0;
  line-height: 1.5;
  overflow: hidden;
  text-overflow: ellipsis;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
}

.b-carousel-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.b-carousel-tag {
  padding: 2px 8px;
  background: rgba(255, 255, 255, 0.1);
  border-radius: 4px;
  font-size: 11px;
  color: rgba(255, 255, 255, 0.75);
}

.b-carousel-actions {
  display: flex;
  gap: 8px;
  margin: 16px 0;
}

.b-carousel-pages {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 6px;
  perspective: 800px;
}

.b-carousel-page {
  position: relative;
  height: 56px;
  border-radius: 6px;
  overflow: hidden;
  cursor: pointer;
  transform-style: preserve-3d;
  transition: all 0.4s cubic-bezier(0.25, 0.46, 0.45, 0.94);
}

.b-carousel-page:hover {
  transform: translateY(-4px) rotateX(5deg);
  z-index: 10;
}

.b-carousel-page.active {
  transform: translateY(-6px) rotateX(8deg) scale(1.05);
  z-index: 20;
}

.b-carousel-page.active .b-carousel-page-inner {
  border-color: var(--accent);
  box-shadow: 0 8px 24px rgba(var(--accent-rgb), 0.4);
}

.b-carousel-page-inner {
  position: absolute;
  inset: 0;
  border-radius: 6px;
  border: 2px solid transparent;
  overflow: hidden;
  transition: all 0.3s ease;
}

.b-carousel-page-img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}

.b-carousel-page-shadow {
  position: absolute;
  inset: 0;
  background: linear-gradient(180deg, transparent 0%, rgba(0, 0, 0, 0.3) 100%);
  pointer-events: none;
}

.b-carousel-page:not(.active) .b-carousel-page-shadow {
  background: linear-gradient(180deg, rgba(0, 0, 0, 0.2) 0%, rgba(0, 0, 0, 0.5) 100%);
}

.b-carousel-page.page-1 {
  transform: translateY(2px);
}

.b-carousel-page.page-2 {
  transform: translateY(4px);
}

.b-carousel-page.page-3 {
  transform: translateY(6px);
}

.b-carousel-page.page-4 {
  transform: translateY(8px);
}

.b-carousel-page.page-5 {
  transform: translateY(10px);
}

.b-carousel-page.page-1:hover,
.b-carousel-page.page-2:hover,
.b-carousel-page.page-3:hover,
.b-carousel-page.page-4:hover,
.b-carousel-page.page-5:hover {
  transform: translateY(-4px) rotateX(5deg);
}

.b-carousel-page.page-1.active,
.b-carousel-page.page-2.active,
.b-carousel-page.page-3.active,
.b-carousel-page.page-4.active,
.b-carousel-page.page-5.active {
  transform: translateY(-6px) rotateX(8deg) scale(1.05);
}
</style>