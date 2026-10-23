<script setup lang="ts">
defineOptions({ name: 'Recommendations' })
import { ref, computed, onMounted } from 'vue'
import { tr } from '../locales'
import { useRoute, useRouter } from 'vue-router'
import { useSourceStore } from '../stores/source'
import { GetVideoList } from '../api/app'
import Icon from '../components/Icon.vue'
import RemoteImage from '../components/RemoteImage.vue'
import { Button, Spinner as LoadingSpinner } from '../components/ui'
import { getDetailPath } from '../utils'
import { computeRecommendations, type RecommendItem, extractYear } from '../utils/recommend'
import type { Video } from '../types'

const route = useRoute()
const router = useRouter()
const sourceStore = useSourceStore()

const sourceKey = computed(() => String(route.query.sourceKey || '') || sourceStore.currentSourceKey)
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
    // 候选池自己取：以前读的是别的页面留下的列表，直接进这一页（或刚重启应用）
    // 时池子是空的，推荐结果取决于上一次恰好看过哪个列表页。
    const resp = (await (GetVideoList as any)({
      source_key: sourceKey.value,
      type_id: '',
      year: '',
      area: '',
      keyword: '',
      sort: '',
      recent_days: 0,
      cursor: '',
      page: 1,
      page_size: 200,
    })) as any
    const list: Video[] = Array.isArray(resp?.videos) ? resp.videos : []
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
        <span>{{ tr('common.back') }}</span>
      </Button>
      <div class="rec-title">
        <h2>{{ tr('recommendations.titleFor', { name: vodName }) }}</h2>
        <span v-if="!loading" class="rec-count">{{ tr('recommendations.totalCount', { count: recommendations.length }) }}</span>
      </div>
    </div>

    <div v-if="loading" class="rec-loading">
      <LoadingSpinner :label="tr('recommendations.analyzing')" />
    </div>

    <div v-else-if="recommendations.length === 0" class="rec-empty">
      <Icon name="film" :size="48" />
      <p>{{ tr('recommendations.empty') }}</p>
    </div>

    <div v-else class="rec-grid">
      <div
        v-for="(item, i) in recommendations"
        :key="'rec-' + item.vod_id + '-' + i"
        class="rec-card"
        @click="openVideo(item)"
      >
        <div class="rec-cover">
          <RemoteImage v-if="item.vod_pic" :src="item.vod_pic" :alt="item.vod_name" loading="lazy" />
          <div v-else class="rec-cover-empty">
            <Icon name="film" :size="28" />
          </div>
          <span v-if="item.vod_remarks" class="rec-remarks">{{ item.vod_remarks }}</span>
          <div class="rec-score-badge" :title="tr('recommendations.matchScore', { score: item.score })">
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
  @apply cczj-max-w-full cczj-text-primary;
  padding: 0;
  animation: cczj-fade-in-up 0.3s ease;
}

.rec-nav {
  @apply cczj-flex cczj-items-center cczj-gap-8 cczj-mb-10;
}

.rec-title {
  @apply cczj-flex cczj-items-baseline cczj-gap-6;
}

.rec-title h2 {
  margin: 0;
  @apply cczj-text-2xl cczj-font-bold;
}

.rec-count {
  @apply cczj-text-13 cczj-text-muted;
}

.rec-loading {
  padding: 60px 20px;
  @apply cczj-flex cczj-flex-col cczj-items-center cczj-justify-center;
}

.rec-empty {
  padding: 80px 20px;
  @apply cczj-flex cczj-flex-col cczj-items-center cczj-justify-center cczj-gap-8 cczj-text-muted;
}

.rec-empty p {
  margin: 0;
  @apply cczj-text-base;
}

.rec-grid {
  @apply cczj-grid;
  grid-template-columns: repeat(auto-fill, minmax(150px, 1fr));
  @apply cczj-gap-8;
}

.rec-card {
  @apply cczj-cursor-pointer;
  transition: transform 0.2s ease;
}

.rec-card:hover {
  transform: translateY(-4px);
}

.rec-card:hover .rec-cover {
  @apply cczj-border-primary;
}

.rec-card:hover .rec-overlay {
  @apply cczj-opacity-100;
}

.rec-card:hover .rec-name {
  @apply cczj-text-accent;
}

.rec-cover {
  @apply cczj-relative;
  aspect-ratio: 2/3;
  @apply cczj-rounded-lg cczj-overflow-hidden;
  background: var(--bg-secondary);
  @apply cczj-border;
  transition: border-color 0.15s ease;
}

.rec-cover img {
  @apply cczj-w-full cczj-h-full cczj-object-cover cczj-block;
}

.rec-cover-empty {
  @apply cczj-w-full cczj-h-full cczj-flex cczj-items-center cczj-justify-center cczj-text-muted;
  opacity: 0.45;
}

.rec-remarks {
  @apply cczj-absolute;
  top: 6px;
  right: 6px;
  padding: 3px 8px;
  border-radius: 10px;
  background: var(--accent);
  color: var(--accent-contrast);
  @apply cczj-text-xs cczj-font-semibold;
}

.rec-score-badge {
  @apply cczj-absolute;
  top: 6px;
  left: 6px;
  padding: 2px 8px;
  @apply cczj-rounded-md;
  background: rgba(0, 0, 0, 0.6);
  color: #ffc107;
  @apply cczj-text-11 cczj-font-bold;
  backdrop-filter: blur(4px);
}

.rec-overlay {
  @apply cczj-absolute;
  inset: 0;
  background: linear-gradient(to top, rgba(0, 0, 0, 0.55), transparent 60%);
  @apply cczj-flex cczj-items-center cczj-justify-center cczj-text-white cczj-opacity-0;
  transition: opacity 0.15s ease;
}

.rec-info {
  @apply cczj-mt-5 cczj-text-center;
}

.rec-name {
  @apply cczj-text-13 cczj-text-primary;
  line-height: 1.35;
  @apply cczj-overflow-hidden;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  transition: color 0.15s ease;
  @apply cczj-font-medium;
}

.rec-match {
  @apply cczj-mt-2 cczj-text-11 cczj-text-muted;
}
</style>
