<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ClearRecycleBin, GetRecycleBin, PurgeVideo, RestoreVideo } from '../api/app'
import { useConfirmStore } from '../stores/confirm'
import { useErrorStore } from '../stores/error'
import { useSourceStore } from '../stores/source'
import { useVideoStore } from '../stores/video'
import { formatTime } from '../utils'
import Icon from '../components/Icon.vue'
import RemoteImage from '../components/RemoteImage.vue'
import { Button, Empty, Select, Spinner } from '../components/ui'

const { t } = useI18n()
const confirmStore = useConfirmStore()
const errorStore = useErrorStore()
const sourceStore = useSourceStore()
const videoStore = useVideoStore()

// 回收站是跨源的：一行自带 source_key，恢复/彻底删除都要按这对坐标下命令。
const pageSize = 50
const items = ref<any[]>([])
const total = ref(0)
const page = ref(1)
const sourceKey = ref('')
const loading = ref(false)
const busy = ref('')

const sourceOptions = computed(() => [
  { value: '', label: t('recycle.allSources') },
  ...sourceStore.sources.map((s: any) => ({ value: s.source_key, label: s.name || s.source_key })),
])

const pageCount = computed(() => Math.max(1, Math.ceil(total.value / pageSize)))

function sourceName(key: string): string {
  return sourceStore.sources.find((s: any) => s.source_key === key)?.name || key
}

function rowKey(item: any): string {
  return `${item.source_key}:${item.vod_id}`
}

async function load(): Promise<void> {
  loading.value = true
  try {
    const resp = await GetRecycleBin({ source_key: sourceKey.value, page: page.value, page_size: pageSize })
    items.value = resp?.items || []
    total.value = Number(resp?.total || 0)
  } catch (e: any) {
    items.value = []
    total.value = 0
    errorStore.fromError(t('recycle.loadFailed'), e, 'RecycleBin')
  } finally {
    loading.value = false
  }
}

function onFilterChange(): void {
  page.value = 1
  void load()
}

function goPage(next: number): void {
  if (next < 1 || next > pageCount.value || next === page.value) return
  page.value = next
  void load()
}

async function restore(item: any): Promise<void> {
  busy.value = `restore:${rowKey(item)}`
  try {
    await RestoreVideo({ source_key: item.source_key, vod_id: item.vod_id })
    errorStore.info(t('recycle.restoredTitle'), t('recycle.restored', { name: item.vod_name }), '', 'RecycleBin')
    // 列表页要重新取：这条视频又回到视频库了，收藏和历史也一起回来。
    videoStore.notifyRefresh()
    await load()
  } catch (e: any) {
    errorStore.fromError(t('recycle.restoreFailed'), e, 'RecycleBin')
  } finally {
    busy.value = ''
  }
}

async function purge(item: any): Promise<void> {
  const ok = await confirmStore.confirm({
    title: t('recycle.purge'),
    message: t('recycle.purgeConfirm', { name: item.vod_name, source: sourceName(item.source_key) }),
    okText: t('recycle.purge'),
    cancelText: t('common.cancel'),
  })
  if (!ok) return
  busy.value = `purge:${rowKey(item)}`
  try {
    await PurgeVideo({ source_key: item.source_key, vod_id: item.vod_id })
    await load()
  } catch (e: any) {
    errorStore.fromError(t('recycle.purgeFailed'), e, 'RecycleBin')
  } finally {
    busy.value = ''
  }
}

async function clearAll(): Promise<void> {
  const scope = sourceKey.value ? sourceName(sourceKey.value) : t('recycle.allSources')
  const ok = await confirmStore.confirm({
    title: t('recycle.clearAll'),
    message: t('recycle.clearAllConfirm', { count: total.value, scope }),
    okText: t('recycle.clearAll'),
    cancelText: t('common.cancel'),
  })
  if (!ok) return
  busy.value = 'clear'
  try {
    const result = await ClearRecycleBin({ source_key: sourceKey.value, page: 1, page_size: pageSize })
    errorStore.info(t('recycle.clearedTitle'), t('recycle.cleared', { count: result?.affected ?? 0 }), '', 'RecycleBin')
    page.value = 1
    await load()
  } catch (e: any) {
    errorStore.fromError(t('recycle.clearAllFailed'), e, 'RecycleBin')
  } finally {
    busy.value = ''
  }
}

onMounted(() => {
  void load()
})
</script>

<template>
  <div class="recycle-page">
    <section class="block">
      <div class="block-hd cczj-flex cczj-items-center cczj-justify-between">
        <div>
          <h3>{{ t('recycle.title') }}</h3>
          <p class="desc">{{ t('recycle.intro') }}</p>
        </div>
        <div class="cczj-flex cczj-items-center cczj-gap-4">
          <Button variant="secondary" size="sm" :loading="loading" @click="load">
            <Icon name="refresh" :size="12" /> {{ t('common.refresh') }}
          </Button>
          <Button
            variant="danger"
            size="sm"
            :disabled="!items.length"
            :loading="busy === 'clear'"
            @click="clearAll"
          >
            <Icon name="trash" :size="12" /> {{ t('recycle.clearAll') }}
          </Button>
        </div>
      </div>

      <div class="toolbar cczj-flex cczj-items-center cczj-gap-4 cczj-flex-wrap">
        <Select v-model="sourceKey" size="sm" :options="sourceOptions" @change="onFilterChange" />
        <span class="count">{{ t('recycle.count', { count: total }) }}</span>
      </div>

      <Spinner v-if="loading" size="sm" :label="t('common.loading')" />
      <Empty v-else-if="!items.length" :title="t('recycle.emptyTitle')" :description="t('recycle.emptyHint')" />
      <ul v-else class="row-list cczj-flex cczj-flex-col cczj-gap-3">
        <li v-for="item in items" :key="rowKey(item)" class="row cczj-flex cczj-items-center cczj-gap-7">
          <RemoteImage :src="item.vod_pic" :alt="item.vod_name" class="poster" />
          <div class="info cczj-flex-1 cczj-min-w-0">
            <div class="name">{{ item.vod_name || t('common.unnamedVideo') }}</div>
            <div class="meta cczj-flex cczj-items-center cczj-gap-4 cczj-flex-wrap">
              <span>{{ sourceName(item.source_key) }}</span>
              <span v-if="item.type_name">{{ item.type_name }}</span>
              <span v-if="item.vod_year">{{ item.vod_year }}</span>
              <span>{{ t('recycle.deletedAt', { time: formatTime(item.deleted_at) }) }}</span>
              <span class="vid">{{ item.vod_id }}</span>
            </div>
          </div>
          <div class="cczj-flex cczj-items-center cczj-gap-4">
            <Button
              variant="secondary"
              size="sm"
              :loading="busy === `restore:${rowKey(item)}`"
              @click="restore(item)"
            >
              {{ t('recycle.restore') }}
            </Button>
            <Button
              variant="danger"
              size="sm"
              :loading="busy === `purge:${rowKey(item)}`"
              @click="purge(item)"
            >
              {{ t('recycle.purge') }}
            </Button>
          </div>
        </li>
      </ul>

      <div v-if="!loading && pageCount > 1" class="pager cczj-flex cczj-items-center cczj-gap-4">
        <Button variant="ghost" size="sm" :disabled="page <= 1" @click="goPage(page - 1)">
          {{ t('recycle.prev') }}
        </Button>
        <span class="count">{{ t('common.pageOf', { current: page, total: pageCount }) }}</span>
        <Button variant="ghost" size="sm" :disabled="page >= pageCount" @click="goPage(page + 1)">
          {{ t('recycle.next') }}
        </Button>
      </div>
    </section>
  </div>
</template>

<style scoped>
.recycle-page {
  display: flex;
  flex-direction: column;
  gap: 16px;
}
.block {
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 12px;
  padding: 20px;
}
.block-hd h3 {
  margin: 0;
  font-size: 16px;
  font-weight: 600;
  color: var(--text-primary);
}
.desc {
  margin: 6px 0 0;
  font-size: 12px;
  line-height: 1.6;
  color: var(--text-muted);
}
.toolbar {
  margin-top: 14px;
}
.count {
  font-size: 12px;
  color: var(--text-muted);
}
.row-list {
  margin: 14px 0 0;
  padding: 0;
  list-style: none;
}
.row {
  padding: 10px 12px;
  border: 1px solid var(--border);
  border-radius: 10px;
  background: var(--bg-secondary);
}
.poster {
  width: 44px;
  height: 62px;
  border-radius: 6px;
  object-fit: cover;
  flex: none;
  background: var(--bg-card);
}
.info {
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.name {
  font-size: 13px;
  color: var(--text-primary);
  word-break: break-all;
}
.meta {
  font-size: 11px;
  color: var(--text-muted);
}
.vid {
  padding: 1px 6px;
  border-radius: 6px;
  background: var(--bg-card);
  font-size: 11px;
}
.pager {
  margin-top: 14px;
  justify-content: center;
}
</style>
