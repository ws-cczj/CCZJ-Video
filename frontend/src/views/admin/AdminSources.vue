<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import {
  AddSource as AddSourceApi, UpdateSource as UpdateSourceApi, DeleteSource as DeleteSourceApi,
  RunSourceAction, ExportSource, StartCollect, StopCollect,
} from '../../../bindings/cczjVideo/app'
import { useErrorStore } from '../../stores/error'
import { useConfirmStore } from '../../stores/confirm'
import Icon from '../../components/Icon.vue'
import { Button, Modal, Badge } from '../../components/ui'
import { useAdminData } from './composables/useAdminData'

const errorStore = useErrorStore()
const confirmStore = useConfirmStore()
const { sources, collectStatusMap, loadSourcesAndStats, refreshCollectStatus } = useAdminData()

// 搜索
const search = ref('')
const filteredSources = computed(() => {
  if (!search.value) return sources.value
  const q = search.value.toLowerCase()
  return sources.value.filter((s: any) =>
    (s.name || '').toLowerCase().includes(q) || (s.source_key || '').toLowerCase().includes(q),
  )
})

// 添加/编辑弹窗
const modalOpen = ref(false)
const modalMode = ref<'add' | 'edit'>('add')
const form = ref({
  source_key: '', name: '', api_url: '', enabled: true,
  adv_config: '', collect_limit: 0, collect_hours: 0,
})

function openAdd(): void {
  modalMode.value = 'add'
  form.value = { source_key: '', name: '', api_url: '', enabled: true, adv_config: '', collect_limit: 0, collect_hours: 0 }
  modalOpen.value = true
}
function openEdit(s: any): void {
  modalMode.value = 'edit'
  form.value = {
    source_key: s.source_key || '', name: s.name || '', api_url: s.api_url || '',
    enabled: (s.enabled ?? 1) === 1,
    adv_config: s.adv_config ? (typeof s.adv_config === 'string' ? s.adv_config : JSON.stringify(s.adv_config, null, 2)) : '',
    collect_limit: s.collect_limit || 0,
    collect_hours: s.collect_hours || 0,
  }
  modalOpen.value = true
}

async function saveSource(): Promise<void> {
  try {
    const payload: any = {
      source_key: form.value.source_key.trim(),
      name: form.value.name.trim(),
      api_url: form.value.api_url.trim(),
      enabled: form.value.enabled ? 1 : 0,
      collect_limit: form.value.collect_limit,
      collect_hours: form.value.collect_hours,
    }
    if (form.value.adv_config.trim()) {
      try { payload.adv_config = JSON.parse(form.value.adv_config.trim()) } catch { payload.adv_config = form.value.adv_config.trim() }
    }
    if (modalMode.value === 'add') {
      await AddSourceApi(payload)
      errorStore.info('添加成功', `源 "${payload.name}" 已添加`, '', 'AdminSources')
    } else {
      await UpdateSourceApi(payload)
      errorStore.info('修改成功', `源 "${payload.name}" 已更新`, '', 'AdminSources')
    }
    modalOpen.value = false
    await loadSourcesAndStats()
  } catch (e: any) { errorStore.fromError('保存失败', e, 'AdminSources.saveSource') }
}

async function handleDelete(sk: string): Promise<void> {
  const ok = await confirmStore.confirm({ title: '删除源', message: `确认删除源「${sk}」及所有视频数据？不可恢复。`, okText: '删除', level: 'danger' })
  if (!ok) return
  try { await DeleteSourceApi(sk); errorStore.info('删除成功', `源 ${sk} 已删除`, '', 'AdminSources'); await loadSourcesAndStats() }
  catch (e: any) { errorStore.fromError('删除失败', e, 'AdminSources') }
}

async function handleTruncate(sk: string): Promise<void> {
  const ok = await confirmStore.confirm({ title: '清空数据', message: `确认清空源「${sk}」的视频数据？源配置保留。`, okText: '清空', level: 'warn' })
  if (!ok) return
  try { await RunSourceAction({ source_key: sk, action: 'truncate', vod_id: '' }); errorStore.info('已清空', `源 ${sk} 数据已清空`, '', 'AdminSources'); await loadSourcesAndStats() }
  catch (e: any) { errorStore.fromError('清空失败', e, 'AdminSources') }
}

async function exportSource(sk: string): Promise<void> {
  try { const p = await ExportSource(sk); errorStore.info('导出成功', `文件: ${p}`, '', 'AdminSources') }
  catch (e: any) { errorStore.fromError('导出失败', e, 'AdminSources') }
}

async function startCollect(sk: string, mode: string): Promise<void> {
  try { await StartCollect({ source_key: sk, mode, hours: 24 }); errorStore.info('开始采集', `${sk} ${mode}采集已启动`, '', 'AdminSources'); await refreshCollectStatus(sk) }
  catch (e: any) { errorStore.fromError('采集失败', e, 'AdminSources') }
}
async function stopCollect(sk: string): Promise<void> {
  try { await StopCollect({ source_key: sk, mode: '', hours: 0 }); errorStore.info('已停止', `${sk} 采集已停止`, '', 'AdminSources'); await refreshCollectStatus(sk) }
  catch (e: any) { errorStore.fromError('停止失败', e, 'AdminSources') }
}

onMounted(() => loadSourcesAndStats())
</script>

<template>
  <div>
    <div class="a-card">
      <div class="a-card-hd">
        <h3>采集源列表 ({{ sources.length }})</h3>
        <div class="a-card-hd-acts">
          <input v-model="search" type="text" placeholder="搜索源..." class="a-inp" style="max-width:200px" />
          <Button variant="primary" size="sm" @click="openAdd">
            <Icon name="plus" :size="12" /> 添加源
          </Button>
        </div>
      </div>
      <table class="a-tb">
        <thead>
          <tr>
            <th>ID</th>
            <th>名称</th>
            <th>Key</th>
            <th>API</th>
            <th>状态</th>
            <th>采集控制</th>
            <th style="width:180px">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="s in filteredSources" :key="s.source_key">
            <td class="a-tb-num">{{ s.id }}</td>
            <td class="a-tb-name">{{ s.name }}</td>
            <td class="a-tb-mono">{{ s.source_key }}</td>
            <td class="a-tb-mono" :title="s.api_url">{{ s.api_url?.substring(0, 45) }}{{ s.api_url?.length > 45 ? '...' : '' }}</td>
            <td><Badge :variant="s.enabled ? 'success' : 'default'">{{ s.enabled ? '启用' : '禁用' }}</Badge></td>
            <td class="a-tb-acts">
              <Button v-if="collectStatusMap[s.source_key]?.running" variant="danger" size="sm" @click="stopCollect(s.source_key)">停止</Button>
              <template v-else>
                <Button variant="secondary" size="sm" @click="startCollect(s.source_key, 'full')">全量</Button>
                <Button variant="secondary" size="sm" @click="startCollect(s.source_key, 'incremental')">增量</Button>
              </template>
            </td>
            <td class="a-tb-acts">
              <Button variant="secondary" size="sm" @click="openEdit(s)">编辑</Button>
              <Button variant="secondary" size="sm" @click="exportSource(s.source_key)">导出</Button>
              <Button variant="secondary" size="sm" style="background:#f59e0b;color:#fff;border-color:#f59e0b" @click="handleTruncate(s.source_key)">清空</Button>
              <Button variant="danger" size="sm" @click="handleDelete(s.source_key)">删除</Button>
            </td>
          </tr>
          <tr v-if="filteredSources.length === 0"><td colspan="7" class="a-tb-empty">暂无数据</td></tr>
        </tbody>
      </table>
    </div>

    <!-- 添加/编辑弹窗 -->
    <Modal :model-value="modalOpen" :title="modalMode === 'add' ? '添加采集源' : '编辑采集源'" width="600px"
      :show-footer="true" ok-text="保存" cancel-text="取消"
      @update:model-value="(v: boolean) => { if (!v) modalOpen = false }" @ok="saveSource"
    >
      <div class="a-form">
        <label class="a-form-label">名称</label>
        <input v-model="form.name" type="text" class="a-inp" placeholder="采集源显示名称" />
        <label class="a-form-label">Key</label>
        <input v-model="form.source_key" type="text" class="a-inp" placeholder="唯一标识符" :disabled="modalMode === 'edit'" />
        <label class="a-form-label">API 地址</label>
        <input v-model="form.api_url" type="text" class="a-inp" placeholder="http://..." />
        <label class="a-form-label">
          <input type="checkbox" v-model="form.enabled" style="width:auto;margin-right:6px" />
          <span>启用</span>
        </label>
        <label class="a-form-label">采集限制（0=不限）</label>
        <input v-model.number="form.collect_limit" type="number" class="a-inp" style="width:100px" min="0" />
        <label class="a-form-label">增量回溯小时（0=使用源配置）</label>
        <input v-model.number="form.collect_hours" type="number" class="a-inp" style="width:100px" min="0" />
        <label class="a-form-label">高级配置 (JSON)</label>
        <textarea v-model="form.adv_config" class="a-inp a-form-ta" rows="5" placeholder='{"field_mapping":{}, "collect_limit": 50}'></textarea>
      </div>
    </Modal>
  </div>
</template>
