<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from './Icon.vue'
import { Button } from './ui'
import {
  DoubanGetAll,
  DoubanStatus,
  DoubanTriggerNow,
  DoubanUpdateVideo,
} from '../api/app'
import { useErrorStore } from '../stores/error'

const { t } = useI18n()
const errorStore = useErrorStore()

const status = ref<any>(null)
const rows = ref<any[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = 20
const loading = ref(false)
const busy = ref('')
const keyword = ref('')

const totalPages = computed(() => Math.max(1, Math.ceil(total.value / pageSize)))

const visibleRows = computed(() => {
  if (!keyword.value.trim()) return rows.value
  const q = keyword.value.trim().toLowerCase()
  return rows.value.filter((r: any) =>
    String(r.VodName || '').toLowerCase().includes(q) ||
    String(r.SubjectID || '').toLowerCase().includes(q),
  )
})

async function loadStatus(): Promise<void> {
  try { status.value = await DoubanStatus() } catch { status.value = null }
}

async function loadPage(p = page.value): Promise<void> {
  loading.value = true
  try {
    const resp = await DoubanGetAll({ page: p, page_size: pageSize })
    rows.value = resp?.rows || []
    total.value = Number(resp?.total) || 0
    page.value = Number(resp?.page) || p
  } catch (e: any) {
    rows.value = []
    total.value = 0
    errorStore.fromError(t('advanced.loadFailed'), e, 'DoubanQueuePanel.loadPage')
  } finally {
    loading.value = false
  }
}

function goPage(p: number): void {
  if (p < 1 || p > totalPages.value || p === page.value) return
  void loadPage(p)
}

let statusTimer: ReturnType<typeof setTimeout> | undefined

async function triggerNow(): Promise<void> {
  busy.value = 'trigger'
  try {
    const count = await DoubanTriggerNow()
    errorStore.info(t('advanced.triggered'), t('advanced.triggeredMsg', { count }), '', 'DoubanQueuePanel')
    if (statusTimer !== undefined) clearTimeout(statusTimer)
    statusTimer = setTimeout(() => { void loadStatus(); void loadPage() }, 2000)
  } catch (e: any) {
    errorStore.fromError(t('advanced.triggerFailed'), e, 'DoubanQueuePanel.triggerNow')
  } finally {
    busy.value = ''
  }
}

async function completeOne(row: any): Promise<void> {
  const name = String(row?.VodName || '')
  if (!name) return
  busy.value = name
  try {
    await DoubanUpdateVideo({ keyword: name })
    errorStore.info(t('advanced.completedTitle'), t('advanced.completedMsg', { name }), '', 'DoubanQueuePanel')
    await loadPage()
  } catch (e: any) {
    errorStore.fromError(t('advanced.completeFailed'), e, 'DoubanQueuePanel.completeOne')
  } finally {
    busy.value = ''
  }
}

onMounted(async () => {
  await loadStatus()
  await loadPage(1)
})

onBeforeUnmount(() => {
  if (statusTimer !== undefined) clearTimeout(statusTimer)
})
</script>

<template>
  <div class="adv-douban">
    <div class="adv-card">
      <div class="adv-card-hd">
        <span class="adv-card-title">
          <Icon name="flame" :size="13" />
          {{ t('advanced.doubanScheduler') }}
        </span>
        <div class="adv-card-acts">
          <Button variant="primary" size="sm" :disabled="busy === 'trigger'" @click="triggerNow">
            <Icon name="play" :size="12" />
            <span>{{ busy === 'trigger' ? t('advanced.working') : t('advanced.triggerNow') }}</span>
          </Button>
          <Button variant="secondary" size="sm" @click="loadStatus">
            <Icon name="refresh" :size="12" />
            <span>{{ t('common.refresh') }}</span>
          </Button>
        </div>
      </div>
      <div v-if="status" class="adv-stats">
        <div class="adv-stat">
          <span class="adv-stat-label">{{ t('advanced.state') }}</span>
          <span class="adv-stat-value" :class="{ on: status.running }">
            {{ status.running ? t('advanced.stateRunning') : t('advanced.stateIdle') }}
          </span>
        </div>
        <div v-if="status.total !== undefined" class="adv-stat">
          <span class="adv-stat-label">{{ t('advanced.totalRecords') }}</span>
          <span class="adv-stat-value">{{ status.total }}</span>
        </div>
        <div v-if="status.completed !== undefined" class="adv-stat">
          <span class="adv-stat-label">{{ t('advanced.completed') }}</span>
          <span class="adv-stat-value ok">{{ status.completed }}</span>
        </div>
        <div v-if="status.pending !== undefined" class="adv-stat">
          <span class="adv-stat-label">{{ t('advanced.pending') }}</span>
          <span class="adv-stat-value warn">{{ status.pending }}</span>
        </div>
      </div>
      <div v-else class="adv-note">{{ t('advanced.noStatus') }}</div>
    </div>

    <div class="adv-card">
      <div class="adv-card-hd">
        <span class="adv-card-title">
          <Icon name="list" :size="13" />
          {{ t('advanced.doubanQueue') }}
          <span class="adv-count">{{ total }}</span>
        </span>
        <div class="adv-card-acts">
          <input v-model="keyword" class="adv-search" type="text" :placeholder="t('advanced.filterPlaceholder')" />
          <Button variant="secondary" size="sm" @click="loadPage()">
            <Icon name="refresh" :size="12" />
            <span>{{ t('common.refresh') }}</span>
          </Button>
        </div>
      </div>

      <div v-if="loading" class="adv-note">{{ t('common.loading') }}</div>
      <div v-else-if="visibleRows.length === 0" class="adv-note">{{ t('advanced.noRows') }}</div>
      <table v-else class="adv-tb">
        <thead>
          <tr>
            <th>{{ t('advanced.colName') }}</th>
            <th>SubjectID</th>
            <th class="adv-th-num">{{ t('advanced.colRating') }}</th>
            <th class="adv-th-num">{{ t('advanced.colYear') }}</th>
            <th class="adv-th-act">{{ t('common.actions') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="(row, idx) in visibleRows" :key="row.GlobalID || row.VodName || idx">
            <td class="adv-td-name" :title="row.VodName">{{ row.VodName || '--' }}</td>
            <td class="adv-td-mono" :title="row.SubjectID">{{ row.SubjectID || '--' }}</td>
            <td class="adv-td-num">{{ row.Rating || '--' }}</td>
            <td class="adv-td-num">{{ row.ReleaseDate ? String(row.ReleaseDate).slice(0, 4) : '--' }}</td>
            <td class="adv-td-act">
              <Button variant="secondary" size="sm"
                :disabled="busy === String(row.VodName || '')"
                @click="completeOne(row)">
                {{ busy === String(row.VodName || '') ? t('advanced.working') : t('advanced.complete') }}
              </Button>
            </td>
          </tr>
        </tbody>
      </table>

      <div v-if="totalPages > 1" class="adv-pager">
        <button class="adv-page-btn" :disabled="page <= 1" @click="goPage(page - 1)">
          <Icon name="chevron-left" :size="12" />
        </button>
        <span class="adv-page-info">{{ page }} / {{ totalPages }}</span>
        <button class="adv-page-btn" :disabled="page >= totalPages" @click="goPage(page + 1)">
          <Icon name="chevron-right" :size="12" />
        </button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.adv-douban {
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.adv-card {
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 12px;
  padding: 12px 14px;
}
.adv-card-hd {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
  margin-bottom: 10px;
}
.adv-card-title {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: 13px;
  font-weight: 600;
  color: var(--text-primary);
}
.adv-card-acts {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  margin-left: auto;
}
.adv-count {
  font-size: 11px;
  font-weight: 500;
  color: var(--text-muted);
  background: var(--bg-hover);
  border-radius: 980px;
  padding: 1px 7px;
}
.adv-stats {
  display: flex;
  flex-wrap: wrap;
  gap: 8px 22px;
}
.adv-stat { display: flex; flex-direction: column; gap: 2px; }
.adv-stat-label { font-size: 11px; color: var(--text-muted); }
.adv-stat-value { font-size: 14px; font-weight: 600; color: var(--text-primary); }
.adv-stat-value.on { color: var(--accent); }
.adv-stat-value.ok { color: var(--success, #4caf50); }
.adv-stat-value.warn { color: var(--warning, #ff9800); }
.adv-note { font-size: 12px; color: var(--text-muted); padding: 6px 0; }
.adv-search {
  height: 26px;
  width: 150px;
  padding: 0 9px;
  font-size: 12px;
  color: var(--text-primary);
  background: var(--bg-input);
  border: 1px solid var(--border);
  border-radius: 8px;
  outline: none;
}
.adv-search:focus { border-color: var(--accent); }
.adv-tb {
  width: 100%;
  border-collapse: collapse;
  font-size: 12px;
}
.adv-tb th {
  text-align: left;
  font-weight: 600;
  color: var(--text-muted);
  padding: 6px 8px;
  border-bottom: 1px solid var(--border);
  white-space: nowrap;
}
.adv-tb td {
  padding: 6px 8px;
  border-bottom: 1px solid var(--border);
  color: var(--text-secondary);
  vertical-align: middle;
}
.adv-tb tbody tr:hover td { background: var(--bg-hover); }
.adv-th-num, .adv-td-num { text-align: right; width: 70px; }
.adv-th-act { text-align: right; width: 96px; }
.adv-td-act { text-align: right; }
.adv-td-name {
  max-width: 260px;
  color: var(--text-primary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.adv-td-mono {
  max-width: 140px;
  font-family: 'SF Mono', Consolas, monospace;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.adv-pager {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 10px;
  margin-top: 10px;
}
.adv-page-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 26px;
  height: 26px;
  border-radius: 8px;
  border: 1px solid var(--border);
  background: var(--bg-card);
  color: var(--text-secondary);
  cursor: pointer;
  transition: all .15s;
}
.adv-page-btn:hover:not(:disabled) { border-color: var(--accent); color: var(--accent); }
.adv-page-btn:disabled { opacity: .4; cursor: not-allowed; }
.adv-page-info { font-size: 12px; color: var(--text-muted); min-width: 54px; text-align: center; }
</style>
