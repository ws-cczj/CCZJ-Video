<script setup lang="ts">
defineOptions({ name: 'Recommendations' })
import { ref, computed, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useSourceStore } from '../stores/source'
import { useVideoStore } from '../stores/video'
import Icon from '../components/Icon.vue'
import { Button, Spinner as LoadingSpinner } from '../components/ui'
import { getDetailPath } from '../utils'
import { computeRecommendations, type RecommendItem, extractYear } from '../utils/recommend'
import type { Video } from '../types'

const route = useRoute()
const router = useRouter()
const sourceStore = useSourceStore()
const videoStore = useVideoStore()

const sourceKey = computed(() => String(route.query.sourceKey || ''))
const vodId = computed(() => String(route.query.vodId || ''))
const vodName = computed(() => String(route.query.vodName || ''))

const recommendations = ref<RecommendItem[]>([])
const loading = ref(true)

function goBack(): void {
  router.back()
}

function openVideo(item: RecommendItem): void {
  router.push(getDetailPath(sourceKey.value, { vod_id: item.vod_id }))
}

async function loadRecommendations(): Promise<void> {
  loading.value = true
  try {
    const list: Video[] = Array.isArray(videoStore.videos) ? videoStore.videos : []
    const currentId = vodId.value
    const currentVideo = list.find((v) => String(v.vod_id || '') === currentId)
    const currentName = currentVideo?.vod_name || vodName.value || ''
    const year = extractYear(currentVideo?.vod_year)

    recommendations.value = computeRecommendations(list, currentId, currentName, year)
  } catch {
    recommendations.value = []
  } finally {
    loading.value = false
  }
}

onMounted(async () => {
  await sourceStore.loadSources()
  await loadRecommendations()
})
</script>

<template>
  <div class="recommendations-page">
    <div class="rec-nav">
      <Button variant="text" size="md" @click="goBack">
        <Icon name="back" :size="14" />
        <span>返回</span>
      </Button>
      <div class="rec-title">
        <h2>「{{ vodName }}」的推荐视频</h2>
        <span v-if="!loading" class="rec-count">共 {{ recommendations.length }} 个推荐</span>
      </div>
    </div>

    <div v-if="loading" class="rec-loading">
      <LoadingSpinner label="正在分析推荐..." />
    </div>

    <div v-else-if="recommendations.length === 0" class="rec-empty">
      <Icon name="film" :size="48" />
      <p>暂无推荐视频</p>
    </div>

    <div v-else class="rec-grid">
      <div
        v-for="(item, i) in recommendations"
        :key="'rec-' + item.vod_id + '-' + i"
        class="rec-card"
        @click="openVideo(item)"
      >
        <div class="rec-cover">
          <img v-if="item.vod_pic" :src="item.vod_pic" :alt="item.vod_name" loading="lazy" referrerpolicy="no-referrer" />
          <div v-else class="rec-cover-empty">
            <Icon name="film" :size="28" />
          </div>
          <span v-if="item.vod_remarks" class="rec-remarks">{{ item.vod_remarks }}</span>
          <div class="rec-score-badge" :title="'匹配度: ' + item.score">
            {{ item.score }}
          </div>
          <div class="rec-overlay">
            <Icon name="play" :size="20" />
          </div>
        </div>
        <div class="rec-info">
          <div class="rec-name" :title="item.vod_name">{{ item.vod_name }}</div>
          <div class="rec-match">{{ item.matchKey }}</div>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.recommendations-page {
  max-width: 100%;
  color: var(--text-primary);
  padding: 0;
  animation: fadeInUp 0.3s ease;
}

@keyframes fadeInUp {
  from { opacity: 0; transform: translateY(8px); }
  to { opacity: 1; transform: translateY(0); }
}

.rec-nav {
  display: flex;
  align-items: center;
  gap: 16px;
  margin-bottom: 20px;
}

.rec-title {
  display: flex;
  align-items: baseline;
  gap: 12px;
}

.rec-title h2 {
  margin: 0;
  font-size: 20px;
  font-weight: 700;
}

.rec-count {
  font-size: 13px;
  color: var(--text-muted);
}

.rec-loading {
  padding: 60px 20px;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
}

.rec-empty {
  padding: 80px 20px;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 16px;
  color: var(--text-muted);
}

.rec-empty p {
  margin: 0;
  font-size: 14px;
}

.rec-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(150px, 1fr));
  gap: 16px;
}

.rec-card {
  cursor: pointer;
  transition: transform 0.2s ease;
}

.rec-card:hover {
  transform: translateY(-4px);
}

.rec-card:hover .rec-cover {
  border-color: var(--accent);
}

.rec-card:hover .rec-overlay {
  opacity: 1;
}

.rec-card:hover .rec-name {
  color: var(--accent);
}

.rec-cover {
  position: relative;
  aspect-ratio: 2/3;
  border-radius: 12px;
  overflow: hidden;
  background: var(--bg-secondary);
  border: 1px solid var(--border);
  transition: border-color 0.15s ease;
}

.rec-cover img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
}

.rec-cover-empty {
  width: 100%;
  height: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--text-muted);
  opacity: 0.45;
}

.rec-remarks {
  position: absolute;
  top: 6px;
  right: 6px;
  padding: 3px 8px;
  border-radius: 10px;
  background: var(--accent);
  color: var(--accent-contrast);
  font-size: 10px;
  font-weight: 600;
}

.rec-score-badge {
  position: absolute;
  top: 6px;
  left: 6px;
  padding: 2px 8px;
  border-radius: 8px;
  background: rgba(0, 0, 0, 0.6);
  color: #ffc107;
  font-size: 11px;
  font-weight: 700;
  backdrop-filter: blur(4px);
}

.rec-overlay {
  position: absolute;
  inset: 0;
  background: linear-gradient(to top, rgba(0, 0, 0, 0.55), transparent 60%);
  display: flex;
  align-items: center;
  justify-content: center;
  color: #fff;
  opacity: 0;
  transition: opacity 0.15s ease;
}

.rec-info {
  margin-top: 10px;
  text-align: center;
}

.rec-name {
  font-size: 13px;
  color: var(--text-primary);
  line-height: 1.35;
  overflow: hidden;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  transition: color 0.15s ease;
  font-weight: 500;
}

.rec-match {
  margin-top: 4px;
  font-size: 11px;
  color: var(--text-muted);
}
</style>