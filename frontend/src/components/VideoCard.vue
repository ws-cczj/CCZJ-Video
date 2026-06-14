<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount } from 'vue'
import { Tag } from './ui'

defineProps<{ video: { vod_pic?: string; vod_name?: string; vod_remarks?: string; type_name?: string } }>()

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
  <div ref="cardEl" class="video-card" :class="{ visible }">
    <div class="poster-wrap">
      <img v-if="video.vod_pic" :src="video.vod_pic" :alt="video.vod_name" loading="lazy" referrerpolicy="no-referrer" />
      <div v-else class="poster-placeholder">
        <svg width="48" height="48" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
          <rect x="3" y="4" width="18" height="16" rx="2" />
          <path d="M10 9l5 3-5 3V9z" fill="currentColor" />
        </svg>
      </div>
      <Tag v-if="video.vod_remarks" size="sm" class="poster-badge">{{ video.vod_remarks }}</Tag>
      <div class="overlay">
        <div class="play-btn">
          <svg width="18" height="18" viewBox="0 0 24 24" fill="currentColor">
            <path d="M5 3l14 9-14 9V3z" />
          </svg>
        </div>
      </div>
    </div>
    <div class="info">
      <h4 class="title" :title="video.vod_name">{{ video.vod_name }}</h4>
      <div v-if="video.type_name" class="sub">{{ video.type_name }}</div>
    </div>
  </div>
</template>

<style scoped>
.video-card {
  background: var(--bg-card);
  border-radius: 12px;
  overflow: hidden;
  cursor: pointer;
  transition: transform 0.25s cubic-bezier(0.4, 0, 0.2, 1),
              box-shadow 0.25s cubic-bezier(0.4, 0, 0.2, 1),
              border-color 0.25s ease,
              opacity 0.4s ease;
  border: 1px solid var(--border);
  display: flex;
  flex-direction: column;
  opacity: 0;
  transform: translateY(8px);
}

.video-card.visible {
  opacity: 1;
  transform: translateY(0);
}

.video-card:hover {
  transform: translateY(-6px);
  box-shadow: var(--shadow);
  border-color: var(--accent);
}

.poster-wrap {
  position: relative;
  aspect-ratio: 2/3;
  background: var(--bg-secondary);
  overflow: hidden;
}

.poster-wrap > img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  transition: transform 0.4s cubic-bezier(0.4, 0, 0.2, 1);
  display: block;
}

.video-card:hover .poster-wrap > img {
  transform: scale(1.08);
}

.poster-placeholder {
  width: 100%;
  height: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--text-muted);
  opacity: 0.4;
}

.poster-badge {
  position: absolute;
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
  position: absolute;
  inset: 0;
  background: linear-gradient(to top, rgba(0, 0, 0, 0.7) 0%, transparent 50%);
  display: flex;
  align-items: center;
  justify-content: center;
  opacity: 0;
  transition: opacity 0.25s ease;
  z-index: 1;
}

.video-card:hover .overlay {
  opacity: 1;
}

.play-btn {
  width: 52px;
  height: 52px;
  border-radius: 50%;
  background: var(--accent);
  color: var(--accent-contrast);
  display: flex;
  align-items: center;
  justify-content: center;
  box-shadow: 0 6px 20px var(--accent-alpha-35);
  transform: scale(0.9);
  transition: transform 0.25s cubic-bezier(0.4, 0, 0.2, 1);
}

.video-card:hover .play-btn {
  transform: scale(1);
}

.info {
  padding: 12px 14px 14px;
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.title {
  font-size: 15px;
  font-weight: 600;
  line-height: 1.4;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--text-primary);
  margin: 0;
  transition: color 0.15s ease;
}

.video-card:hover .title {
  color: var(--accent);
}

.sub {
  font-size: 13px;
  color: var(--text-muted);
  margin: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>
