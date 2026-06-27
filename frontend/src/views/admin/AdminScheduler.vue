<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useCollectStore, type CollectScheduleConfig } from '../../stores/collect'
import { useErrorStore } from '../../stores/error'
import Icon from '../../components/Icon.vue'
import { Button, Badge, Empty } from '../../components/ui'
import { useAdminData } from './composables/useAdminData'

const collect = useCollectStore()
const errorStore = useErrorStore()
const { loadSourcesAndStats } = useAdminData()

// 本地编辑表单
const editing = ref(false)
const form = ref<CollectScheduleConfig>({
  enable_background: true,
  background_interval_seconds: 60,
  background_interval_minutes: 1,
  enable_startup_catchup: true,
  enable_initial_full_collect: false,
  source_gap_seconds: 10,
  page_gap_seconds: 30,
})

// 单源调度编辑
const sourceEditKey = ref('')
const sourceEditEnabled = ref(true)
const sourceEditMode = ref('incremental')
const sourceEditInterval = ref(30)

function startEdit(): void {
  if (collect.scheduleConfig) {
    form.value = { ...collect.scheduleConfig }
  }
  editing.value = true
}

async function saveConfig(): Promise<void> {
  try {
    await collect.saveSchedule(form.value)
    errorStore.info('已保存', '调度配置已更新', '', 'AdminScheduler')
    editing.value = false
  } catch (e: any) {
    errorStore.fromError('保存失败', e, 'AdminScheduler')
  }
}

async function triggerCollect(mode: string): Promise<void> {
  try {
    await collect.triggerNow('', mode as any)
    errorStore.info('已触发', `${mode === 'full' ? '全量' : '增量'}采集已触发`, '', 'AdminScheduler')
  } catch (e: any) {
    errorStore.fromError('触发失败', e, 'AdminScheduler')
  }
}

async function stopBg(): Promise<void> {
  try {
    await collect.stopBackground()
    errorStore.info('已停止', '后台采集已停止', '', 'AdminScheduler')
  } catch (e: any) {
    errorStore.fromError('停止失败', e, 'AdminScheduler')
  }
}

function openSourceEdit(item: any): void {
  sourceEditKey.value = item.source_key
  sourceEditEnabled.value = item.enabled
  sourceEditMode.value = item.mode || 'incremental'
  sourceEditInterval.value = item.interval_min || 30
}

async function saveSourceSchedule(): Promise<void> {
  try {
    await collect.saveSourceSchedule(sourceEditKey.value, sourceEditEnabled.value, sourceEditMode.value, sourceEditInterval.value)
    errorStore.info('已保存', `${sourceEditKey.value} 调度配置已更新`, '', 'AdminScheduler')
    sourceEditKey.value = ''
  } catch (e: any) {
    errorStore.fromError('保存失败', e, 'AdminScheduler')
  }
}

onMounted(async () => {
  await loadSourcesAndStats()
  await collect.loadSchedule()
})
</script>

<template>
  <div>
    <!-- 全局配置 -->
    <div class="a-card">
      <div class="a-card-hd">
        <h3>全局调度配置</h3>
        <div class="a-card-hd-acts">
          <Button v-if="!editing" variant="secondary" size="sm" @click="startEdit">
            <Icon name="edit" :size="12" /> 编辑
          </Button>
          <template v-else>
            <Button variant="primary" size="sm" :disabled="collect.scheduleSaving" @click="saveConfig">
              <Icon name="save" :size="12" /> 保存
            </Button>
            <Button variant="secondary" size="sm" @click="editing = false">取消</Button>
          </template>
        </div>
      </div>

      <template v-if="!editing">
        <div class="sched-grid">
          <div class="sched-item">
            <span class="sched-label">后台采集</span>
            <Badge :variant="collect.scheduleConfig?.enable_background ? 'success' : 'default'">
              {{ collect.scheduleConfig?.enable_background ? '已启用' : '未启用' }}
            </Badge>
          </div>
          <div class="sched-item">
            <span class="sched-label">循环间隔</span>
            <span class="sched-val">{{ collect.scheduleConfig?.background_interval_seconds || '--' }}秒</span>
          </div>
          <div class="sched-item">
            <span class="sched-label">启动补采</span>
            <Badge :variant="collect.scheduleConfig?.enable_startup_catchup ? 'success' : 'default'">
              {{ collect.scheduleConfig?.enable_startup_catchup ? '是' : '否' }}
            </Badge>
          </div>
          <div class="sched-item">
            <span class="sched-label">首次全量</span>
            <Badge :variant="collect.scheduleConfig?.enable_initial_full_collect ? 'success' : 'default'">
              {{ collect.scheduleConfig?.enable_initial_full_collect ? '是' : '否' }}
            </Badge>
          </div>
          <div class="sched-item">
            <span class="sched-label">源间隔</span>
            <span class="sched-val">{{ collect.scheduleConfig?.source_gap_seconds || '--' }}秒</span>
          </div>
          <div class="sched-item">
            <span class="sched-label">页间隔</span>
            <span class="sched-val">{{ collect.scheduleConfig?.page_gap_seconds || '--' }}秒</span>
          </div>
        </div>
      </template>
      <template v-else>
        <div class="a-form">
          <label class="a-form-label">
            <input type="checkbox" v-model="form.enable_background" style="width:auto;margin-right:6px" /> 启用后台采集
          </label>
          <label class="a-form-label">
            循环间隔（秒，最小30）
            <input v-model.number="form.background_interval_seconds" type="number" class="a-inp" style="width:100px" min="30" />
          </label>
          <label class="a-form-label">
            <input type="checkbox" v-model="form.enable_startup_catchup" style="width:auto;margin-right:6px" /> 启动时补采
          </label>
          <label class="a-form-label">
            <input type="checkbox" v-model="form.enable_initial_full_collect" style="width:auto;margin-right:6px" /> 首次启动全量采集
          </label>
          <label class="a-form-label">
            源间隔（秒）
            <input v-model.number="form.source_gap_seconds" type="number" class="a-inp" style="width:100px" min="1" />
          </label>
          <label class="a-form-label">
            页间隔（秒）
            <input v-model.number="form.page_gap_seconds" type="number" class="a-inp" style="width:100px" min="1" />
          </label>
        </div>
      </template>
    </div>

    <!-- 操作区 -->
    <div class="a-card">
      <div class="a-card-hd">
        <h3>快捷操作</h3>
      </div>
      <div class="a-row">
        <Button variant="primary" size="sm" @click="triggerCollect('full')">
          <Icon name="play" :size="12" /> 触发全量采集
        </Button>
        <Button variant="secondary" size="sm" @click="triggerCollect('incremental')">
          <Icon name="refresh" :size="12" /> 触发增量采集
        </Button>
        <Button variant="danger" size="sm" @click="stopBg">
          <Icon name="stop" :size="12" /> 停止后台采集
        </Button>
        <Button variant="secondary" size="sm" @click="collect.loadSchedule()">
          <Icon name="refresh" :size="12" /> 刷新状态
        </Button>
      </div>
    </div>

    <!-- 各源调度状态 -->
    <div class="a-card">
      <div class="a-card-hd">
        <h3>各源调度状态</h3>
      </div>
      <Empty v-if="!collect.schedulerStatus?.source_schedules?.length" title="暂无源调度配置" />
      <table v-else class="a-tb">
        <thead>
          <tr>
            <th>源名称</th>
            <th>Key</th>
            <th>启用</th>
            <th>模式</th>
            <th>间隔(分)</th>
            <th>状态</th>
            <th>操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="item in collect.schedulerStatus.source_schedules" :key="item.source_key">
            <td class="a-tb-name cczj-truncate">{{ item.name || item.source_key }}</td>
            <td class="a-tb-mono cczj-truncate">{{ item.source_key }}</td>
            <td><Badge :variant="item.enabled ? 'success' : 'default'">{{ item.enabled ? '是' : '否' }}</Badge></td>
            <td>{{ item.mode || '-' }}</td>
            <td class="a-tb-num">{{ item.interval_min || '-' }}</td>
            <td><Badge :variant="item.running ? 'primary' : 'default'">{{ item.running ? '运行中' : '空闲' }}</Badge></td>
            <td class="a-tb-acts">
              <Button variant="secondary" size="sm" @click="openSourceEdit(item)">配置</Button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <!-- 单源编辑弹窗 -->
    <div v-if="sourceEditKey" class="source-edit-overlay" @click.self="sourceEditKey = ''">
      <div class="source-edit-modal">
        <h4 style="margin:0 0 14px">{{ sourceEditKey }} — 调度配置</h4>
        <div class="a-form">
          <label class="a-form-label">
            <input type="checkbox" v-model="sourceEditEnabled" style="width:auto;margin-right:6px" /> 启用后台采集
          </label>
          <label class="a-form-label">
            采集模式
            <select v-model="sourceEditMode" class="a-sel">
              <option value="incremental">增量</option>
              <option value="full">全量</option>
            </select>
          </label>
          <label class="a-form-label">
            间隔（分钟）
            <input v-model.number="sourceEditInterval" type="number" class="a-inp" style="width:100px" min="1" />
          </label>
        </div>
        <div class="a-row" style="margin-top:14px;justify-content:flex-end">
          <Button variant="secondary" size="sm" @click="sourceEditKey = ''">取消</Button>
          <Button variant="primary" size="sm" @click="saveSourceSchedule">保存</Button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.sched-grid {
  display: flex;
  flex-wrap: wrap;
  gap: 12px 24px;
}
.sched-item {
  display: flex;
  align-items: center;
  gap: 8px;
}
.sched-label {
  font-size: 12px;
  color: var(--text-muted);
}
.sched-val {
  font-size: 13px;
  font-weight: 500;
  color: var(--text-primary);
}
.source-edit-overlay {
  position: fixed;
  inset: 0;
  background: rgba(0,0,0,0.4);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 1000;
}
.source-edit-modal {
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 10px;
  padding: 24px;
  min-width: 360px;
  max-width: 90vw;
}
</style>
