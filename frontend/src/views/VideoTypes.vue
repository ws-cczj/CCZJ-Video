<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { GetGlobalTypes, SetGlobalTypeCollectEnabled, SetGlobalTypeMagnetEnabled, SyncGlobalTypes, CiligouStatus, CiligouTriggerNow } from '../../bindings/cczjVideo/app'
import { useErrorStore } from '../stores/error'
import Icon from '../components/Icon.vue'
import { Button, Badge, Spinner, Empty } from '../components/ui'

const errorStore = useErrorStore()

const types = ref<any[]>([])
const loading = ref(false)
const searchKey = ref('')
const status = ref<any>({ running: false })
const triggering = ref(false)

const filteredTypes = computed(() => {
  if (!searchKey.value) return types.value
  const q = searchKey.value.toLowerCase()
  return types.value.filter((t: any) =>
    (t.TypeName || '').toLowerCase().includes(q)
  )
})

const collectEnabledCount = computed(() => types.value.filter((t: any) => t.CollectEnabled === 1).length)
const magnetEnabledCount = computed(() => types.value.filter((t: any) => t.MagnetEnabled === 1).length)

async function loadTypes() {
  loading.value = true
  try {
    types.value = (await GetGlobalTypes()) || []
  } catch (e: any) {
    errorStore.fromError('加载类型失败', e, 'VideoTypes')
  } finally {
    loading.value = false
  }
}

async function loadStatus() {
  try { status.value = await CiligouStatus() } catch { /* */ }
}

async function toggleCollect(typeRow: any) {
  const newEnabled = typeRow.CollectEnabled === 1 ? false : true
  try {
    await SetGlobalTypeCollectEnabled({ type_name: typeRow.TypeName, enabled: newEnabled })
    typeRow.CollectEnabled = newEnabled ? 1 : 0
  } catch (e: any) {
    errorStore.fromError('设置采集状态失败', e, 'VideoTypes')
  }
}

async function toggleMagnet(typeRow: any) {
  const newEnabled = typeRow.MagnetEnabled === 1 ? false : true
  try {
    await SetGlobalTypeMagnetEnabled({ type_name: typeRow.TypeName, enabled: newEnabled })
    typeRow.MagnetEnabled = newEnabled ? 1 : 0
  } catch (e: any) {
    errorStore.fromError('设置磁力状态失败', e, 'VideoTypes')
  }
}

async function syncTypes() {
  try {
    const count = await SyncGlobalTypes()
    errorStore.info('同步完成', `已同步 ${count} 个类型`, '', 'VideoTypes')
    await loadTypes()
  } catch (e: any) {
    errorStore.fromError('同步失败', e, 'VideoTypes')
  }
}

async function triggerNow() {
  triggering.value = true
  try {
    const count = await CiligouTriggerNow()
    if (count > 0) {
      errorStore.info('触发成功', `已更新 ${count} 条磁力链接`, '', 'VideoTypes')
    } else {
      errorStore.info('提示', '没有需要更新的磁力链接（可能无启用类型或所有记录已处理）', '', 'VideoTypes')
    }
    await loadStatus()
  } catch (e: any) {
    errorStore.fromError('触发失败', e, 'VideoTypes')
  } finally {
    triggering.value = false
  }
}

function toggleAllCollect(enable: boolean) {
  types.value.forEach(t => {
    if (t.CollectEnabled !== (enable ? 1 : 0)) {
      toggleCollect(t)
    }
  })
}

function toggleAllMagnet(enable: boolean) {
  types.value.forEach(t => {
    if (t.MagnetEnabled !== (enable ? 1 : 0)) {
      toggleMagnet(t)
    }
  })
}

onMounted(async () => {
  await loadTypes()
  await loadStatus()
})
</script>

<template>
  <div class="video-types-page">
    <!-- 状态栏 -->
    <div class="status-card">
      <div class="status-header">
        <h3 class="status-title">视频类型管理</h3>
        <div class="status-actions">
          <Button variant="primary" size="sm" @click="triggerNow" :disabled="triggering">
            <Icon name="play" :size="12" /> {{ triggering ? '触发中...' : '启动磁力获取' }}
          </Button>
          <Button variant="secondary" size="sm" @click="loadStatus">
            <Icon name="refresh" :size="12" /> 刷新状态
          </Button>
        </div>
      </div>
      <div class="status-grid">
        <div class="status-item">
          <span class="status-label">调度器状态</span>
          <Badge :variant="status.running ? 'primary' : 'default'" class="status-badge">
            {{ status.running ? '运行中' : '待机' }}
          </Badge>
        </div>
        <div class="status-item">
          <span class="status-label">采集启用</span>
          <span class="status-value" :class="{ active: collectEnabledCount > 0 }">
            {{ collectEnabledCount }} / {{ types.length }}
          </span>
        </div>
        <div class="status-item">
          <span class="status-label">磁力启用</span>
          <span class="status-value" :class="{ active: magnetEnabledCount > 0 }">
            {{ magnetEnabledCount }} / {{ types.length }}
          </span>
        </div>
      </div>
    </div>

    <!-- 类型列表 -->
    <div class="types-card">
      <div class="types-header">
        <h3 class="types-title">视频类型</h3>
        <div class="types-actions">
          <div class="search-box">
            <Icon name="search" :size="14" />
            <input v-model="searchKey" type="text" placeholder="搜索类型..." class="search-input" />
          </div>
          <Button variant="secondary" size="sm" @click="syncTypes" class="sync-btn">
            <Icon name="refresh" :size="12" /> 同步
          </Button>
          <div class="batch-actions">
            <Button variant="secondary" size="sm" @click="toggleAllCollect(true)" class="batch-btn">
              <Icon name="check" :size="12" /> 采集全启
            </Button>
            <Button variant="secondary" size="sm" @click="toggleAllCollect(false)" class="batch-btn">
              <Icon name="x" :size="12" /> 采集全禁
            </Button>
            <div class="divider"></div>
            <Button variant="secondary" size="sm" @click="toggleAllMagnet(true)" class="batch-btn">
              <Icon name="check" :size="12" /> 磁力全启
            </Button>
            <Button variant="secondary" size="sm" @click="toggleAllMagnet(false)" class="batch-btn">
              <Icon name="x" :size="12" /> 磁力全禁
            </Button>
          </div>
        </div>
      </div>

      <Spinner v-if="loading" size="sm" label="加载中..." />
      <Empty v-else-if="types.length === 0" title="暂无类型数据">
        <template #extra>
          <p class="empty-hint">请先采集视频数据，然后点击「同步」按钮同步类型</p>
        </template>
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
              title="点击切换采集状态"
            >
              <span class="switch-icon">{{ row.CollectEnabled === 1 ? '✓' : '' }}</span>
              <span class="switch-label">采集</span>
            </button>
            <button
              :class="['type-switch', 'magnet-switch', { active: row.MagnetEnabled === 1 }]"
              @click.stop="toggleMagnet(row)"
              title="点击切换磁力状态"
            >
              <span class="switch-icon">{{ row.MagnetEnabled === 1 ? '✓' : '' }}</span>
              <span class="switch-label">磁力</span>
            </button>
          </div>
        </div>
        <div v-if="filteredTypes.length === 0" class="types-empty">
          无匹配类型
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.video-types-page {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.status-card {
  background: var(--bg-card);
  border-radius: 12px;
  border: 1px solid var(--border);
  padding: 20px;
}

.status-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 16px;
}

.status-title {
  font-size: 16px;
  font-weight: 600;
  color: var(--text-primary);
  margin: 0;
}

.status-actions {
  display: flex;
  gap: 8px;
}

.status-grid {
  display: flex;
  gap: 32px;
  flex-wrap: wrap;
}

.status-item {
  display: flex;
  align-items: center;
  gap: 8px;
}

.status-label {
  font-size: 13px;
  color: var(--text-muted);
}

.status-value {
  font-size: 14px;
  font-weight: 600;
  color: var(--text-muted);
}

.status-value.active {
  color: var(--success);
}

.status-badge {
  font-size: 12px;
  padding: 2px 8px;
}

.types-card {
  background: var(--bg-card);
  border-radius: 12px;
  border: 1px solid var(--border);
  padding: 20px;
}

.types-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 20px;
  flex-wrap: wrap;
  gap: 12px;
}

.types-title {
  font-size: 16px;
  font-weight: 600;
  color: var(--text-primary);
  margin: 0;
}

.types-actions {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
}

.search-box {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 12px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--bg-secondary);
  min-width: 200px;
}

.search-box svg {
  color: var(--text-muted);
}

.search-input {
  border: none;
  outline: none;
  background: transparent;
  font-size: 13px;
  color: var(--text-primary);
  flex: 1;
}

.search-input::placeholder {
  color: var(--text-muted);
}

.batch-actions {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 4px;
  background: var(--bg-secondary);
  border-radius: 8px;
}

.divider {
  width: 1px;
  height: 20px;
  background: var(--border);
  margin: 0 4px;
}

.batch-btn {
  padding: 4px 10px;
}

.types-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(260px, 1fr));
  gap: 10px;
}

.type-card {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 12px 16px;
  background: var(--bg-secondary);
  border-radius: 10px;
  border: 1px solid transparent;
  transition: all 0.2s ease;
  user-select: none;
}

.type-card:hover {
  border-color: var(--accent-alpha-30);
  background: var(--accent-alpha-5);
}

.type-card-left {
  display: flex;
  align-items: center;
  gap: 10px;
}

.type-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--text-muted);
  transition: all 0.2s ease;
}

.type-dot.collect {
  background: var(--success);
  box-shadow: 0 0 8px rgba(34, 197, 94, 0.4);
}

.type-name {
  font-size: 14px;
  font-weight: 500;
  color: var(--text-primary);
}

.type-card-right {
  display: flex;
  gap: 6px;
}

.type-switch {
  display: flex;
  align-items: center;
  gap: 4px;
  padding: 5px 10px;
  border-radius: 6px;
  border: 1.5px solid var(--border);
  background: transparent;
  font-size: 12px;
  cursor: pointer;
  transition: all 0.2s ease;
}

.type-switch:hover {
  border-color: var(--accent-alpha-40);
}

.switch-icon {
  font-size: 11px;
  font-weight: bold;
}

.switch-label {
  font-weight: 500;
}

.collect-switch {
  color: var(--text-muted);
}

.collect-switch.active {
  border-color: var(--success);
  background: rgba(34, 197, 94, 0.12);
  color: var(--success);
}

.magnet-switch {
  color: var(--text-muted);
}

.magnet-switch.active {
  border-color: var(--primary);
  background: rgba(59, 130, 246, 0.12);
  color: var(--primary);
}

.types-empty {
  grid-column: 1 / -1;
  text-align: center;
  padding: 32px;
  color: var(--text-muted);
  font-size: 14px;
}

.empty-hint {
  font-size: 13px;
  color: var(--text-muted);
  margin-top: 12px;
}
</style>
