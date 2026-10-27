<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { tr } from '../locales'
import { GetGlobalTypes, SetGlobalTypeCollectEnabled, SyncGlobalTypes } from '../api/app'
import { useErrorStore } from '../stores/error'
import { useVideoStore } from '../stores/video'
import Icon from '../components/Icon.vue'
import { Button, Badge, Spinner, Empty } from '../components/ui'
import { presentationProfile } from '../platform/presentation'

const errorStore = useErrorStore()
const videoStore = useVideoStore()
const mobile = presentationProfile().ui === 'mobile'

const types = ref<any[]>([])
const loading = ref(false)
const searchKey = ref('')

const filteredTypes = computed(() => {
  if (!searchKey.value) return types.value
  const q = searchKey.value.toLowerCase()
  return types.value.filter((t: any) =>
    (t.TypeName || '').toLowerCase().includes(q)
  )
})

const collectEnabledCount = computed(() => types.value.filter((t: any) => t.CollectEnabled === 1).length)

async function loadTypes() {
  loading.value = true
  try {
    types.value = (await GetGlobalTypes()) || []
  } catch (e: any) {
    errorStore.fromError(tr('videoTypes.loadFailed'), e, 'VideoTypes')
  } finally {
    loading.value = false
  }
}

const batchBusy = ref(false)

async function toggleCollect(typeRow: any) {
  const newEnabled = typeRow.CollectEnabled === 1 ? false : true
  try {
    await SetGlobalTypeCollectEnabled({ type_name: typeRow.TypeName, enabled: newEnabled })
    await loadTypes()
    videoStore.notifyRefresh()
  } catch (e: any) {
    errorStore.fromError(tr('videoTypes.setTypeStatusFailed', { name: typeRow.TypeName }), e, 'VideoTypes')
  }
}

async function syncTypes() {
  try {
    const count = await SyncGlobalTypes()
    errorStore.info(tr('videoTypes.syncDone'), tr('videoTypes.syncedCount', { count }), '', 'VideoTypes')
    await loadTypes()
  } catch (e: any) {
    errorStore.fromError(tr('videoTypes.syncFailed'), e, 'VideoTypes')
  }
}

async function toggleAllCollect(enable: boolean) {
  if (batchBusy.value) return
  batchBusy.value = true
  const targets = types.value.filter(t => t.CollectEnabled !== (enable ? 1 : 0))
  let ok = 0
  let fail = 0
  for (const t of targets) {
    try {
      await SetGlobalTypeCollectEnabled({ type_name: t.TypeName, enabled: enable })
      t.CollectEnabled = enable ? 1 : 0
      ok++
    } catch (e: any) {
      fail++
      errorStore.fromError(tr('videoTypes.setTypeFailed', { name: t.TypeName }), e, 'VideoTypes')
    }
  }
  if (targets.length > 0) {
    videoStore.notifyRefresh()
    errorStore.info(tr('videoTypes.batchDone'), tr('videoTypes.batchOk', { ok }) + (fail > 0 ? tr('videoTypes.batchFail', { fail }) : ''), '', 'VideoTypes')
  }
  batchBusy.value = false
}

onMounted(async () => {
  await loadTypes()
})
</script>

<template>
  <div class="video-types-page">
    <header v-if="!mobile"><h1 class="page-title cczj-text-2xl cczj-font-bold">{{ tr('videoTypes.title') }}</h1></header>
    <!-- 状态栏 -->
    <div class="status-card">
      <div class="status-header">
        <h3 class="status-title">{{ tr('videoTypes.collectionStatus') }}</h3>
        <div class="status-actions">
          <Button variant="secondary" size="sm" @click="loadTypes">
            <Icon name="refresh" :size="12" /> {{ tr('common.refresh') }}
          </Button>
        </div>
      </div>
      <div class="status-grid">
        <div class="status-item">
          <span class="status-label">{{ tr('videoTypes.collectEnabled') }}</span>
          <span class="status-value" :class="{ active: collectEnabledCount > 0 }">
            {{ collectEnabledCount }} / {{ types.length }}
          </span>
        </div>
      </div>
    </div>

    <!-- 类型列表 -->
    <div class="types-card">
      <div class="types-header">
        <h3 class="types-title">{{ tr('videoTypes.typesTitle') }}</h3>
        <div class="types-actions">
          <div class="search-box">
            <Icon name="search" :size="14" />
            <input v-model="searchKey" type="text" :placeholder="tr('videoTypes.searchPlaceholder')" class="search-input" />
          </div>
          <Button variant="secondary" size="sm" @click="syncTypes" class="sync-btn">
            <Icon name="refresh" :size="12" /> {{ tr('videoTypes.sync') }}
          </Button>
          <div class="batch-actions">
            <Button variant="success" size="sm" @click="toggleAllCollect(true)" :disabled="batchBusy" class="batch-btn">
              <Icon name="check" :size="12" /> {{ tr('videoTypes.enableAll') }}
            </Button>
            <Button variant="warning" size="sm" @click="toggleAllCollect(false)" :disabled="batchBusy" class="batch-btn">
              <Icon name="x" :size="12" /> {{ tr('videoTypes.disableAll') }}
            </Button>
          </div>
        </div>
      </div>

      <Spinner v-if="loading" size="sm" :label="tr('common.loading')" />
      <Empty v-else-if="types.length === 0" :title="tr('videoTypes.emptyTitle')">
        <p class="empty-hint">{{ tr('videoTypes.emptyHint') }}</p>
      </Empty>
      <div v-else class="types-grid">
        <div
          v-for="row in filteredTypes"
          :key="row.Id"
          class="type-card"
        >
          <div class="type-card-left">
            <div class="type-dot" :class="{ collect: row.CollectEnabled === 1 }"></div>
            <span class="type-name">{{ row.TypeName }}</span>
          </div>
          <div class="type-card-right">
            <button
              :class="['type-switch', 'collect-switch', { active: row.CollectEnabled === 1 }]"
              @click.stop="toggleCollect(row)"
              :title="tr('videoTypes.toggleHint')"
            >
              <span class="switch-icon">{{ row.CollectEnabled === 1 ? '✓' : '' }}</span>
              <span class="switch-label">{{ tr('videoTypes.collect') }}</span>
            </button>
          </div>
        </div>
        <div v-if="filteredTypes.length === 0" class="types-empty">
          {{ tr('videoTypes.noMatch') }}
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.page-title { margin: 0; }
.video-types-page {
  @apply cczj-flex cczj-flex-col cczj-gap-8;
}

.status-card {
  background: var(--bg-card);
  @apply cczj-rounded-lg cczj-border;
  padding: 20px;
}

.status-header {
  @apply cczj-flex cczj-justify-between cczj-items-center cczj-mb-8;
}

.status-title {
  @apply cczj-text-lg cczj-font-semibold cczj-text-primary;
  margin: 0;
}

.status-actions {
  @apply cczj-flex cczj-gap-4;
}

.status-grid {
  @apply cczj-flex;
  gap: 32px;
  @apply cczj-flex-wrap;
}

.status-item {
  @apply cczj-flex cczj-items-center cczj-gap-4;
}

.status-label {
  @apply cczj-text-13 cczj-text-muted;
}

.status-value {
  @apply cczj-text-base cczj-font-semibold cczj-text-muted;
}

.status-value.active {
  @apply cczj-text-success;
}

.types-card {
  background: var(--bg-card);
  @apply cczj-rounded-lg cczj-border;
  padding: 20px;
}

.types-header {
  @apply cczj-flex cczj-justify-between cczj-items-center cczj-mb-10 cczj-flex-wrap cczj-gap-6;
}

.types-title {
  @apply cczj-text-lg cczj-font-semibold cczj-text-primary;
  margin: 0;
}

.types-actions {
  @apply cczj-flex cczj-items-center cczj-gap-6 cczj-flex-wrap;
}

.search-box {
  @apply cczj-flex cczj-items-center cczj-gap-4 cczj-px-6-py-3 cczj-border cczj-rounded-md;
  background: var(--bg-secondary);
  min-width: 200px;
}

.search-box svg {
  @apply cczj-text-muted;
}

.search-input {
  border: none;
  @apply cczj-outline-none;
  background: transparent;
  @apply cczj-text-13 cczj-text-primary cczj-flex-1;
}

.search-input::placeholder {
  @apply cczj-text-muted;
}

.batch-actions {
  @apply cczj-flex cczj-items-center cczj-gap-3 cczj-p-2;
  background: var(--bg-secondary);
  @apply cczj-rounded-md;
}

.batch-btn {
  padding: 4px 10px;
}

.types-grid {
  @apply cczj-grid;
  grid-template-columns: repeat(auto-fill, minmax(260px, 1fr));
  @apply cczj-gap-5;
}

.type-card {
  @apply cczj-flex cczj-justify-between cczj-items-center cczj-px-8-py-6;
  background: var(--bg-secondary);
  border-radius: 10px;
  border: 1px solid transparent;
  @apply cczj-transition;
  user-select: none;
}

.type-card:hover {
  border-color: var(--accent-alpha-30);
  background: var(--accent-alpha-5);
}

.type-card-left {
  @apply cczj-flex cczj-items-center cczj-gap-5;
}

.type-dot {
  @apply cczj-w-2 cczj-h-2 cczj-rounded-50;
  background: var(--text-muted);
  @apply cczj-transition;
}

.type-dot.collect {
  background: var(--success);
  box-shadow: 0 0 8px rgba(34, 197, 94, 0.4);
}

.type-name {
  @apply cczj-text-base cczj-font-medium cczj-text-primary;
}

.type-card-right {
  @apply cczj-flex cczj-gap-3;
}

.type-switch {
  @apply cczj-flex cczj-items-center cczj-gap-2;
  padding: 5px 10px;
  @apply cczj-rounded;
  border: 1.5px solid var(--border);
  background: transparent;
  @apply cczj-text-sm cczj-cursor-pointer cczj-transition;
}

.type-switch:hover {
  border-color: var(--accent-alpha-40);
}

.switch-icon {
  @apply cczj-text-11;
  font-weight: bold;
}

.switch-label {
  @apply cczj-font-medium;
}

.collect-switch {
  @apply cczj-text-muted;
}

.collect-switch.active {
  border-color: var(--success);
  background: rgba(34, 197, 94, 0.12);
  @apply cczj-text-success;
}

.types-empty {
  grid-column: 1 / -1;
  @apply cczj-text-center;
  padding: 32px;
  @apply cczj-text-muted cczj-text-base;
}

.empty-hint {
  @apply cczj-text-13 cczj-text-muted cczj-mt-6;
}
</style>
