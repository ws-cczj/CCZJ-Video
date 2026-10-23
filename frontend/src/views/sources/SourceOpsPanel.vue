<script setup lang="ts">
import { ref } from 'vue'
import { tr } from '../../locales'
import { RunSourceAction } from '../../api/app'
import { useCollectStore } from '../../stores/collect'
import { useConfirmStore } from '../../stores/confirm'
import { useErrorStore } from '../../stores/error'
import { useSourceStore } from '../../stores/source'
import Icon from '../../components/Icon.vue'
import { useSourceDisplay } from './useSourceDisplay'

/**
 * 卡片展开区 3：后端状态明细 + 数据维护。
 * 状态读自 collect store 的权威快照；清空数据仍走页面同款确认框 + 同一条 loadSources 刷新。
 */
defineProps<{ sourceKey: string }>()

const collectStore = useCollectStore()
const confirmStore = useConfirmStore()
const errorStore = useErrorStore()
const sourceStore = useSourceStore()
const { lastRunText, lastRunClass, modeLabel, formatSyncTime, sourceName } = useSourceDisplay()

const statusSyncing = ref('')
const truncating = ref('')

async function refreshStatus(key: string): Promise<void> {
  statusSyncing.value = key
  try {
    await collectStore.syncFromBackend(key)
  } finally {
    statusSyncing.value = ''
  }
}

async function truncateSource(key: string): Promise<void> {
  const ok = await confirmStore.confirm({
    title: tr('sources.truncateTitle'),
    message: tr('sources.truncateConfirm', { name: sourceName(key) }),
    okText: tr('common.clearAll'),
    level: 'danger',
  })
  if (!ok) return
  truncating.value = key
  try {
    await RunSourceAction({ source_key: key, action: 'truncate', vod_id: '' })
    errorStore.info(tr('sources.truncateDone'), tr('sources.truncateDoneMsg', { name: sourceName(key) }), '', 'Sources')
    await sourceStore.loadSources(true)
    await collectStore.syncFromBackend(key)
  } catch (e: any) {
    errorStore.fromError(tr('sources.truncateFailed'), e, 'Sources.truncateSource')
  } finally {
    truncating.value = ''
  }
}
</script>

<template>
  <!-- 3. 后端状态明细 + 数据维护 -->
  <div class="source-ops-panel">
    <div class="sop-header">
      <span class="sop-title">
        <Icon name="database" :size="12" />
        {{ tr('sources.backendStatus') }}
      </span>
      <span v-if="collectStore.backendStatus[sourceKey]" class="sop-synced">
        {{ tr('sources.statusSyncedAt', { time: formatSyncTime(collectStore.backendStatus[sourceKey].syncedAt) }) }}
      </span>
      <button class="mini-btn" :disabled="statusSyncing === sourceKey" @click="refreshStatus(sourceKey)">
        <Icon name="refresh" :size="11" />
        <span>{{ statusSyncing === sourceKey ? tr('common.loading') : tr('sources.refreshStatus') }}</span>
      </button>
    </div>

    <div v-if="collectStore.backendStatus[sourceKey]" class="sop-grid">
      <div class="sop-cell">
        <span class="sop-label">{{ tr('common.status') }}</span>
        <span class="sop-value" :class="{ on: collectStore.backendStatus[sourceKey].running }">
          {{ collectStore.backendStatus[sourceKey].running
            ? (collectStore.backendStatus[sourceKey].paused ? tr('sources.statusPaused') : tr('sources.statusRunning'))
            : tr('sources.statusIdle') }}
        </span>
      </div>
      <div class="sop-cell">
        <span class="sop-label">{{ tr('sources.scheduleMode') }}</span>
        <span class="sop-value">{{ collectStore.backendStatus[sourceKey].mode ? modeLabel(collectStore.backendStatus[sourceKey].mode) : '--' }}</span>
      </div>
      <div class="sop-cell">
        <span class="sop-label">{{ tr('sources.statPage') }}</span>
        <span class="sop-value">{{ collectStore.backendStatus[sourceKey].page || '--' }}</span>
      </div>
      <div class="sop-cell">
        <span class="sop-label">{{ tr('sources.statProcessed') }}</span>
        <span class="sop-value">{{ collectStore.backendStatus[sourceKey].current }} / {{ collectStore.backendStatus[sourceKey].total || '?' }}</span>
      </div>
      <div class="sop-cell sop-cell-wide">
        <span class="sop-label">{{ tr('sources.lastRunTitle') }}</span>
        <span class="sop-value sop-mono" :class="lastRunClass(sourceKey)" :title="lastRunText(sourceKey)">{{ lastRunText(sourceKey) }}</span>
      </div>
      <div class="sop-cell sop-cell-wide">
        <span class="sop-label">{{ tr('sources.lastLogLine') }}</span>
        <span class="sop-value sop-mono" :title="collectStore.backendStatus[sourceKey].log">{{ collectStore.backendStatus[sourceKey].log || '--' }}</span>
      </div>
    </div>
    <div v-else class="sop-empty">{{ tr('sources.statusNotLoaded') }}</div>

    <div class="sop-danger">
      <span class="sop-danger-label">
        <Icon name="alert-triangle" :size="12" />
        {{ tr('sources.dangerZone') }}
      </span>
      <button class="mini-btn danger" :disabled="truncating === sourceKey" @click="truncateSource(sourceKey)">
        <Icon name="trash" :size="11" />
        <span>{{ truncating === sourceKey ? tr('sources.truncating') : tr('sources.truncateBtn') }}</span>
      </button>
    </div>
  </div>
</template>

<style scoped>

/* === 3. 后端状态与数据维护 === */
.source-ops-panel {
  background: rgba(255,255,255,0.02);
  @apply cczj-border;
  border-radius: 10px;
  padding: 10px 12px;
  @apply cczj-flex cczj-flex-col cczj-gap-4;
}
.sop-header {
  @apply cczj-flex cczj-items-center cczj-gap-4;
}
.sop-title {
  @apply cczj-inline-flex cczj-items-center;
  gap: 5px;
  @apply cczj-text-sm cczj-font-semibold cczj-text-secondary;
}
.sop-synced {
  @apply cczj-text-11 cczj-text-muted cczj-ml-auto;
}
.sop-grid {
  @apply cczj-grid;
  grid-template-columns: repeat(auto-fit, minmax(120px, 1fr));
  gap: 6px 12px;
}
.sop-cell { @apply cczj-flex cczj-flex-col cczj-gap-1; }
.sop-cell-wide { grid-column: 1 / -1; }
.sop-label { @apply cczj-text-11 cczj-text-muted; }
.sop-value { @apply cczj-text-sm cczj-text-primary; }
.sop-value.on { @apply cczj-text-accent; }
.sop-value.warn { @apply cczj-text-warning; }
.sop-value.bad { @apply cczj-text-danger; }
.sop-mono {
  font-family: 'SF Mono', Consolas, monospace;
  @apply cczj-whitespace-nowrap cczj-overflow-hidden;
  text-overflow: ellipsis;
}
.sop-empty { @apply cczj-text-sm cczj-text-muted; }
.sop-danger {
  @apply cczj-flex cczj-items-center cczj-justify-between cczj-gap-4 cczj-pt-4;
  border-top: 1px dashed var(--border);
}
.sop-danger-label {
  @apply cczj-inline-flex cczj-items-center;
  gap: 5px;
  @apply cczj-text-11 cczj-text-danger;
}

</style>
