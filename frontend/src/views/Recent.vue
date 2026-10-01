<script setup lang="ts">
defineOptions({ name: 'Recent' })
import { computed, onActivated, onDeactivated, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useSourceStore } from '../stores/source'
import { useVideoStore } from '../stores/video'
import { useVideoList } from '../composables/useVideoList'
import VideoCard from '../components/VideoCard.vue'
import Icon from '../components/Icon.vue'
import { Tag, Spinner as LoadingSpinner, Empty as EmptyState } from '../components/ui'
import { getDetailPath } from '../utils'

const { t } = useI18n()
const router = useRouter()
const sourceStore = useSourceStore()
const videoStore = useVideoStore()
const list = useVideoList()
const period = ref(7)
const typeId = ref('')
const periods = computed(() => [
  { value: 1, label: t('recent.today') },
  { value: 7, label: t('recent.week') },
  { value: 30, label: t('recent.month') },
])
const typeOptions = computed(() => [{ type_id: '', name: t('home.allTypes') }, ...videoStore.types])

async function load(): Promise<void> {
  if (!sourceStore.currentSourceKey) return
  await list.load(sourceStore.currentSourceKey, { type_id: typeId.value, year: '', area: '', keyword: '', sort: '', recent_days: period.value }, 1, 50)
}
function goDetail(video: any): void {
  if (sourceStore.currentSourceKey) router.push(getDetailPath(sourceStore.currentSourceKey, video))
}

// 这一页进了 KeepAlive 名单，onActivated 从此真的会触发。
// 首次挂载由 onMounted 负责，所以用 wasDeactivated 把"刚激活"和"回访激活"分开：
// 回访时重新拉一遍列表，否则用户离开再回来看到的还是旧数据。
// 重拉的成本已经很低——源列表和分类都做了会话缓存，只有 recent 列表本身走一次 IPC。
async function bootstrap(): Promise<void> {
  await sourceStore.loadSources()
  const key = sourceStore.currentSourceKey
  if (key) await videoStore.loadTypes(key)
  await load()
}
let wasDeactivated = false
onMounted(bootstrap)
onActivated(() => {
  if (!wasDeactivated) return
  wasDeactivated = false
  void bootstrap()
})
onDeactivated(() => { wasDeactivated = true })
</script>

<template>
  <div class="recent-page cczj-max-w-full cczj-text-primary">
    <div class="recent-head cczj-flex cczj-items-center cczj-justify-between cczj-gap-4 cczj-mb-6 cczj-flex-wrap">
      <div><h2 class="cczj-font-bold cczj-text-xl"><Icon name="clock" :size="18" /> {{ t('recent.title') }}</h2><p class="cczj-text-sm cczj-text-muted">{{ t('recent.description') }}</p></div>
    </div>
    <div class="recent-tabs cczj-flex cczj-gap-2 cczj-mb-6">
      <button v-for="item in periods" :key="item.value" class="period-tab" :class="{ active: period === item.value }" @click="period = item.value; load()">{{ item.label }}</button>
    </div>
    <div class="type-filter cczj-flex cczj-items-center cczj-flex-wrap cczj-gap-2 cczj-mb-6">
      <span class="cczj-text-sm cczj-text-muted">{{ t('home.type') }}</span>
      <Tag v-for="item in typeOptions" :key="item.type_id || 'all'" :active="typeId === item.type_id" @click="typeId = item.type_id; load()">{{ item.name }}</Tag>
    </div>
    <LoadingSpinner v-if="list.loading" size="sm" :label="t('common.loading')" />
    <EmptyState v-else-if="list.videos.length === 0" :title="t('recent.empty')" />
    <div v-else class="recent-grid"><VideoCard v-for="video in list.videos" :key="`${video.global_id}-${video.vod_id}`" :video="video" @click="goDetail(video)" /></div>
  </div>
</template>

<style scoped>
.recent-head h2 { display: flex; align-items: center; gap: 8px; }
.recent-tabs { border-bottom: 1px solid var(--accent-alpha-20); }
.period-tab { padding: 8px 16px; border: 0; border-bottom: 2px solid transparent; background: transparent; color: var(--text-muted); cursor: pointer; font: inherit; }
.period-tab:hover, .period-tab.active { color: var(--accent); border-bottom-color: var(--accent); }
.recent-grid { display: grid; grid-template-columns: repeat(5, minmax(140px, 1fr)); gap: 16px; }
@media (max-width: 900px) { .recent-grid { grid-template-columns: repeat(3, minmax(120px, 1fr)); } }
</style>
