<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { ImportSource, ExportSource } from '../../../bindings/cczjVideo/app'
import { useErrorStore } from '../../stores/error'
import { useConfirmStore } from '../../stores/confirm'
import Icon from '../../components/Icon.vue'
import { Button } from '../../components/ui'
import { useAdminData } from './composables/useAdminData'

const errorStore = useErrorStore()
const confirmStore = useConfirmStore()
const { sourceStats, loadSourcesAndStats } = useAdminData()

const importFilePath = ref('')
const importLoading = ref(false)

async function doImport(): Promise<void> {
  if (!importFilePath.value.trim()) {
    errorStore.info('提示', '请输入文件路径', '', 'AdminDataOps')
    return
  }
  const ok = await confirmStore.confirm({
    title: '导入数据', message: '确认导入？同名源数据将合并。', okText: '导入', level: 'warn',
  })
  if (!ok) return
  importLoading.value = true
  try {
    const r = await ImportSource(importFilePath.value.trim())
    errorStore.info('导入成功', r, '', 'AdminDataOps')
    importFilePath.value = ''
    await loadSourcesAndStats()
  } catch (e: any) {
    errorStore.fromError('导入失败', e, 'AdminDataOps')
  } finally {
    importLoading.value = false
  }
}

async function doExport(sourceKey: string): Promise<void> {
  try {
    const p = await ExportSource(sourceKey)
    errorStore.info('导出成功', `文件: ${p}`, '', 'AdminDataOps')
  } catch (e: any) {
    errorStore.fromError('导出失败', e, 'AdminDataOps')
  }
}

onMounted(async () => {
  await loadSourcesAndStats()
})
</script>

<template>
  <div>
    <!-- 导入 -->
    <div class="a-card">
      <div class="a-card-hd">
        <h3>导入数据</h3>
      </div>
      <p class="a-desc" style="margin-bottom:10px">支持 .json.br / .json.gz / .json 格式。同名源数据将合并。</p>
      <div class="a-row">
        <input v-model="importFilePath" type="text" placeholder="输入文件路径..." class="a-inp" style="flex:1" />
        <Button variant="primary" @click="doImport" :disabled="importLoading">
          {{ importLoading ? '导入中...' : '导入' }}
        </Button>
      </div>
    </div>

    <!-- 导出 -->
    <div class="a-card" v-if="sourceStats.length">
      <div class="a-card-hd">
        <h3>导出数据</h3>
      </div>
      <table class="a-tb">
        <thead>
          <tr>
            <th>源名称</th>
            <th>Key</th>
            <th>视频数</th>
            <th>剧集数</th>
            <th>操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="st in sourceStats" :key="st.source_key">
            <td class="a-tb-name cczj-truncate">{{ st.name }}</td>
            <td class="a-tb-mono cczj-truncate">{{ st.source_key }}</td>
            <td class="a-tb-num">{{ st.video_count }}</td>
            <td class="a-tb-num">{{ st.episode_count }}</td>
            <td class="a-tb-acts">
              <Button variant="secondary" size="sm" @click="doExport(st.source_key)">
                <Icon name="download" :size="12" /> 导出
              </Button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>
