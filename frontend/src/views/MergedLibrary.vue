<script setup lang="ts">
defineOptions({ name: 'MergedLibrary' })
import { computed, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { GetGlobalTypes, GetMergedLibraryList, GetMergedLibraryYearsAndAreas } from '../api/app'
import { useErrorStore } from '../stores/error'
import { useVideoStore } from '../stores/video'
import { useGeneration } from '../composables/useGeneration'
import VideoCard from '../components/VideoCard.vue'
import Icon from '../components/Icon.vue'
import { Button, Select, Spinner, Empty, Tag } from '../components/ui'
import { getDetailPath } from '../utils'

/**
 * 跨源合并曲库：一张卡片 = 一个 global_id。
 *
 * 首页/最近更新按当前源前缀查询，所以同一部片接了两个源就是两张互不可见的卡片。
 * 这里读的是 db.GetCatalogUnionPage：代表行取该身份里时间最新的那条目录行，
 * 卡片上标出这部片还在哪些源里有货，点进去仍是普通的详情页。
 */
const { t } = useI18n()
const router = useRouter()
const errorStore = useErrorStore()
const videoStore = useVideoStore()

interface UnionCard {
  id: number
  global_id: number
  source_key: string
  vod_id: string
  type_id: string
  type_name?: string
  vod_name?: string
  vod_pic?: string
  vod_remarks?: string
  vod_year?: string
  vod_area?: string
  source_count?: number
  sources?: string[]
}

const PAGE_SIZE = 24

const cards = ref<UnionCard[]>([])
const total = ref(0)
const loading = ref(false)
const loadingMore = ref(false)
const hasMore = ref(false)
const keyword = ref('')
const typeId = ref('')
const year = ref('')
const area = ref('')
const types = ref<{ id: string; name: string }[]>([])
const years = ref<string[]>([])
const areas = ref<string[]>([])

let nextCursor = ''
const gen = useGeneration()

const yearOptions = computed(() => [{ value: '', label: t('mergedLibrary.allYears') }, ...years.value.map(y => ({ value: y, label: y }))])
const areaOptions = computed(() => [{ value: '', label: t('mergedLibrary.allAreas') }, ...areas.value.map(a => ({ value: a, label: a }))])
const typeOptions = computed(() => [{ id: '', name: t('home.allTypes') }, ...types.value])

async function load(append = false): Promise<void> {
  const my = gen.begin()
  if (append) loadingMore.value = true
  else loading.value = true
  if (!append) nextCursor = ''
  try {
    const resp = (await GetMergedLibraryList({
      recent_days: 0,
      keyword: keyword.value.trim(),
      type_id: typeId.value,
      year: year.value,
      area: area.value,
      cursor: append ? nextCursor : '',
      page_size: PAGE_SIZE,
    })) as any
    if (!gen.isCurrent(my)) return
    const list: UnionCard[] = Array.isArray(resp?.videos) ? resp.videos : []
    nextCursor = typeof resp?.next_cursor === 'string' ? resp.next_cursor : ''
    cards.value = append ? [...cards.value, ...list] : list
    total.value = typeof resp?.total === 'number' ? resp.total : cards.value.length
    hasMore.value = nextCursor !== ''
  } catch (e: any) {
    if (!gen.isCurrent(my)) return
    errorStore.fromError(t('mergedLibrary.loadFailed'), e, 'MergedLibrary.load')
    if (!append) {
      cards.value = []
      total.value = 0
      hasMore.value = false
    }
  } finally {
    if (gen.isCurrent(my)) {
      loading.value = false
      loadingMore.value = false
    }
  }
}

async function loadFilterMeta(): Promise<void> {
  try {
    const rows = await GetGlobalTypes()
    // GlobalTypeRow 没有 json tag，绑定里保留的是 Go 字段名。
    types.value = (Array.isArray(rows) ? rows : [])
      .filter((row: any) => row?.CollectEnabled !== 0)
      .map((row: any) => ({ id: String(row?.Id ?? ''), name: String(row?.TypeName ?? '') }))
  } catch (e: any) {
    errorStore.fromError(t('mergedLibrary.loadFiltersFailed'), e, 'MergedLibrary.types')
  }
  try {
    const resp = (await GetMergedLibraryYearsAndAreas()) as any
    years.value = Array.isArray(resp?.years) ? resp.years : []
    areas.value = Array.isArray(resp?.areas) ? resp.areas : []
  } catch (e: any) {
    errorStore.fromError(t('mergedLibrary.loadFiltersFailed'), e, 'MergedLibrary.areas')
  }
}

function sourceLabel(card: UnionCard): string {
  return (card.source_count ?? 1) > 1 ? t('mergedLibrary.sourceCount', { count: card.source_count }) : ''
}

function openDetail(card: UnionCard): void {
  if (!card.source_key) return
  router.push(getDetailPath(card.source_key, card))
}

// 合并、采集或清库之后重取一次：身份集合变了，卡片数量也会变。
watch(() => videoStore.refreshTrigger, () => { void load() })

onMounted(async () => {
  await Promise.all([load(), loadFilterMeta()])
})
</script>

<template>
  <div class="merged-page cczj-max-w-full cczj-text-primary">
    <div class="merged-head cczj-flex cczj-items-center cczj-justify-between cczj-gap-4 cczj-mb-4 cczj-flex-wrap">
      <div>
        <h2 class="cczj-font-bold cczj-text-xl cczj-flex cczj-items-center cczj-gap-2"><Icon name="layers" :size="18" /> {{ t('mergedLibrary.title') }}</h2>
        <p class="cczj-text-sm cczj-text-muted">{{ t('mergedLibrary.description') }}</p>
      </div>
      <Button variant="secondary" size="sm" :loading="loading" @click="load()">
        <Icon name="refresh" :size="12" /> {{ t('common.refresh') }}
      </Button>
    </div>

    <div class="merged-filters cczj-flex cczj-items-center cczj-flex-wrap cczj-gap-3 cczj-mb-4">
      <label class="search-box cczj-flex cczj-items-center cczj-gap-2">
        <Icon name="search" :size="14" />
        <input v-model="keyword" type="text" class="search-input cczj-flex-1" :placeholder="t('mergedLibrary.searchPlaceholder')" @keyup.enter="load()" />
      </label>
      <Select v-model="year" :options="yearOptions" size="sm" @update:model-value="load()" />
      <Select v-model="area" :options="areaOptions" size="sm" @update:model-value="load()" />
    </div>

    <div class="merged-types cczj-flex cczj-items-center cczj-flex-wrap cczj-gap-2 cczj-mb-6">
      <span class="cczj-text-sm cczj-text-muted">{{ t('home.type') }}</span>
      <Tag v-for="item in typeOptions" :key="item.id || 'all'" :active="typeId === item.id" @click="typeId = item.id; load()">{{ item.name }}</Tag>
    </div>

    <Spinner v-if="loading" size="sm" :label="t('common.loading')" />
    <Empty v-else-if="cards.length === 0" :title="t('mergedLibrary.empty')" :description="t('mergedLibrary.emptyHint')" />
    <template v-else>
      <div class="merged-count cczj-text-sm cczj-text-muted cczj-mb-3">
        {{ t('mergedLibrary.count', { shown: cards.length, total }) }}
      </div>
      <div class="merged-grid">
        <VideoCard
          v-for="card in cards"
          :key="`${card.global_id}-${card.source_key}-${card.vod_id}`"
          :video="card"
          :source-label="sourceLabel(card)"
          @click="openDetail(card)"
        />
      </div>
      <div v-if="hasMore" class="merged-more cczj-flex cczj-justify-center cczj-mt-6">
        <Button variant="secondary" size="md" :loading="loadingMore" @click="load(true)">
          {{ t('mergedLibrary.loadMore') }}
        </Button>
      </div>
    </template>
  </div>
</template>

<style scoped>
.merged-head h2 { display: flex; align-items: center; gap: 8px; }
.search-box { padding: 6px 12px; border: 1px solid var(--border); border-radius: 8px; background: var(--bg-secondary); min-width: 240px; color: var(--text-muted); }
.search-input { border: none; outline: none; background: transparent; font-size: 13px; color: var(--text-primary); font-family: inherit; }
.search-input::placeholder { color: var(--text-muted); }
.merged-grid { display: grid; grid-template-columns: repeat(6, minmax(130px, 1fr)); gap: 16px; }
@media (max-width: 1200px) { .merged-grid { grid-template-columns: repeat(4, minmax(130px, 1fr)); } }
@media (max-width: 900px) { .merged-grid { grid-template-columns: repeat(3, minmax(120px, 1fr)); } }
</style>
