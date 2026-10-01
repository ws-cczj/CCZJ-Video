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
  border: 1px solid var(--border);
  border-radius: 10px;
  padding: 10px 12px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.sop-header {
  display: flex;
  align-items: center;
  gap: 8px;
}
.sop-title {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  font-size: 12px;
  font-weight: 600;
  color: var(--text-secondary);
}
.sop-synced {
  font-size: 11px;
  color: var(--text-muted);
  margin-left: auto;
}
.sop-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(120px, 1fr));
  gap: 6px 12px;
}
.sop-cell { display: flex; flex-direction: column; gap: 2px; }
.sop-cell-wide { grid-column: 1 / -1; }
.sop-label { font-size: 11px; color: var(--text-muted); }
.sop-value { font-size: 12px; color: var(--text-primary); }
.sop-value.on { color: var(--accent); }
.sop-value.warn { color: var(--warning); }
.sop-value.bad { color: var(--danger); }
.sop-mono {
  font-family: 'SF Mono', Consolas, monospace;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.sop-empty { font-size: 12px; color: var(--text-muted); }
.sop-danger {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding-top: 8px;
  border-top: 1px dashed var(--border);
}
.sop-danger-label {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  font-size: 11px;
  color: var(--danger);
}

</style>
