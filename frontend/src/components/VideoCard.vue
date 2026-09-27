<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount } from 'vue'
import { Tag } from './ui'
import RemoteImage from './RemoteImage.vue'

defineProps<{
  video: { vod_pic?: string; vod_name?: string; vod_remarks?: string; type_name?: string }
  inCatalog?: boolean
  catalogLabel?: string
}>()

const cardEl = ref<HTMLDivElement>()
const visible = ref(false)
let observer: IntersectionObserver | undefined

onMounted(() => {
  if (!('IntersectionObserver' in window)) {
    visible.value = true
    return
  }
  observer = new IntersectionObserver(
    (entries) => {
      entries.forEach((entry) => {
        if (entry.isIntersecting) {
          visible.value = true
          if (observer && entry.target) observer.unobserve(entry.target)
        }
      })
    },
    { threshold: 0.08 }
  )
  if (cardEl.value) observer.observe(cardEl.value)
})

onBeforeUnmount(() => {
  if (observer) {
    observer.disconnect()
    observer = undefined
  }
})
</script>

<template>
  <div ref="cardEl" class="video-card cczj-bg-card cczj-rounded-lg cczj-overflow-hidden cczj-cursor-pointer cczj-border cczj-flex cczj-flex-col cczj-opacity-0" :class="{ visible }">
    <div class="poster-wrap cczj-relative cczj-bg-secondary cczj-overflow-hidden">
      <RemoteImage v-if="video.vod_pic" :src="video.vod_pic" :alt="video.vod_name" loading="lazy" class="cczj-w-full cczj-h-full cczj-block" />
      <div v-else class="poster-placeholder cczj-w-full cczj-h-full cczj-flex cczj-items-center cczj-justify-center cczj-text-muted cczj-opacity-40">
        <svg width="48" height="48" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
          <rect x="3" y="4" width="18" height="16" rx="2" />
          <path d="M10 9l5 3-5 3V9z" fill="currentColor" />
        </svg>
      </div>
      <Tag v-if="video.vod_remarks" size="sm" class="poster-badge cczj-absolute">{{ video.vod_remarks }}</Tag>
      <div v-if="inCatalog" class="catalog-badge cczj-absolute">
        <svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3">
          <path d="M5 13l4 4L19 7" />
        </svg>
        <span>{{ catalogLabel }}</span>
      </div>
      <div class="overlay cczj-absolute cczj-inset-0 cczj-flex cczj-items-center cczj-justify-center cczj-opacity-0 cczj-transition-fast">
        <div class="play-btn cczj-rounded-full cczj-bg-accent cczj-flex cczj-items-center cczj-justify-center">
          <svg width="18" height="18" viewBox="0 0 24 24" fill="currentColor">
            <path d="M5 3l14 9-14 9V3z" />
          </svg>
        </div>
      </div>
    </div>
    <div class="info cczj-flex-1 cczj-flex cczj-flex-col cczj-gap-2">
      <h4 class="title cczj-truncate cczj-font-semibold cczj-text-primary" :title="video.vod_name">{{ video.vod_name }}</h4>
      <div v-if="video.type_name" class="sub cczj-truncate cczj-text-13 cczj-text-muted">{{ video.type_name }}</div>
    </div>
  </div>
</template>

<style scoped>
.video-card {
  transition: transform 0.25s cubic-bezier(0.4, 0, 0.2, 1),
              box-shadow 0.25s cubic-bezier(0.4, 0, 0.2, 1),
              border-color 0.25s ease,
              opacity 0.4s ease;
  transform: translateY(8px);
  contain: layout style;
}

.video-card.visible {
  opacity: 1;
  transform: translateY(0);
}

.video-card:hover {
  transform: translateY(-6px) scale(1.02);
  box-shadow: var(--shadow);
  border-color: var(--accent);
}

.poster-wrap {
  aspect-ratio: 2/3;
}

.poster-wrap > img {
  object-fit: cover;
  transition: transform 0.4s cubic-bezier(0.4, 0, 0.2, 1);
}

.video-card:hover .poster-wrap > img {
  transform: scale(1.08);
}

.poster-badge {
  top: 10px;
  right: 10px;
  z-index: 2;
  background: var(--accent) !important;
  border-color: var(--accent) !important;
  color: var(--accent-contrast) !important;
  box-shadow: 0 2px 10px var(--accent-alpha-35);
  backdrop-filter: blur(4px);
}

.overlay {
  background: linear-gradient(to top, rgba(0, 0, 0, 0.7) 0%, transparent 50%);
  z-index: 1;
}

/* 目录命中态：放在海报左下角，避开右上角的更新状态角标 */
.catalog-badge {
  left: 8px;
  bottom: 8px;
  z-index: 3;
  padding: 2px 7px;
  border-radius: var(--radius-sm);
  background: var(--success);
  color: var(--success-contrast);
  font-size: 11px;
  font-weight: 600;
  display: flex;
  align-items: center;
  gap: 3px;
  box-shadow: 0 2px 8px var(--success-alpha-10);
}

.video-card:hover .overlay {
  opacity: 1;
}

.play-btn {
  width: 52px;
  height: 52px;
  color: var(--accent-contrast);
  box-shadow: 0 6px 20px var(--accent-alpha-35);
  transform: scale(0.9);
  transition: transform 0.25s cubic-bezier(0.4, 0, 0.2, 1);
}

.video-card:hover .play-btn {
  transform: scale(1);
}

.info {
  padding: 12px 14px 14px;
}

.title {
  font-size: 15px;
  line-height: 1.4;
  margin: 0;
  transition: color 0.15s ease;
}

.video-card:hover .title {
  color: var(--accent);
}

.sub {
  margin: 0;
}
</style>
