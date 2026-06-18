<script setup lang="ts">
import { ref, onMounted } from 'vue'
import {
  GetVideoList, GetVideoDetail, DeleteVideo as DeleteVideoApi, GetAllSources,
} from '../../../bindings/cczjVideo/app'
import { useErrorStore } from '../../stores/error'
import { useConfirmStore } from '../../stores/confirm'
import { useVideoStore } from '../../stores/video'
import Icon from '../../components/Icon.vue'
import { Button, Modal, Empty, Spinner } from '../../components/ui'
import { useAdminData } from './composables/useAdminData'

const errorStore = useErrorStore()
const confirmStore = useConfirmStore()
const videoStore = useVideoStore()
const { loadSourcesAndStats } = useAdminData()

// ========== 数据 ==========
const videoList = ref<any[]>([])
const videoPage = ref(1)
const videoTotal = ref(0)
const videoPageSize = 20
const videoSourceKey = ref('')
const videoSearch = ref('')
const videoLoading = ref(false)
const sourceKeys = ref<string[]>([])
const selectedVideos = ref<Set<string>>(new Set())
const selectAll = ref(false)

const detailOpen = ref(false)
const detailItem = ref<any>(null)
const detailRaw = ref<any>(null)

async function loadSourceKeys(): Promise<void> {
  try {
    const s = await GetAllSources()
    sourceKeys.value = (s as any[]).map((x: any) => x.source_key || '')
  } catch { /* */ }
}

async function loadVideoList(): Promise<void> {
  videoLoading.value = true
  selectedVideos.value.clear()
  selectAll.value = false
  try {
    const req: any = {
      source_key: videoSourceKey.value || '',
      page: videoPage.value,
      page_size: videoPageSize,
      keyword: videoSearch.value || '',
      type_id: '', year: '', area: '', sort: '',
    }
    const resp = await GetVideoList(req)
    videoList.value = ((resp as any)?.videos || []).map((v: any) => ({
      source_key: v.source_key || v.vod_source || '',
      vod_id: v.vod_id || '',
      vod_name: v.vod_name || '',
      type_name: v.type_name || '',
      vod_remarks: v.vod_remarks || '',
      vod_year: v.vod_year || '',
      vod_area: v.vod_area || '',
      vod_score: v.vod_score || '',
      vod_pic: v.vod_pic || '',
      vod_time: v.vod_time || '',
      vod_actor: v.vod_actor || '',
      vod_director: v.vod_director || '',
      vod_hits: v.vod_hits || '',
    }))
    videoTotal.value = (resp as any)?.total || 0
  } catch {
    videoList.value = []
  } finally {
    videoLoading.value = false
  }
}

function toggleSelect(v: any): void {
  const key = `${v.source_key}:${v.vod_id}`
  if (selectedVideos.value.has(key)) selectedVideos.value.delete(key)
  else selectedVideos.value.add(key)
  // 同步全选状态
  selectAll.value = selectedVideos.value.size === videoList.value.length && videoList.value.length > 0
}
function toggleSelectAll(): void {
  if (selectAll.value) videoList.value.forEach(v => selectedVideos.value.add(`${v.source_key}:${v.vod_id}`))
  else selectedVideos.value.clear()
}

async function batchDelete(): Promise<void> {
  if (selectedVideos.value.size === 0) return
  const ok = await confirmStore.confirm({
    title: '批量删除', message: `确认删除选中的 ${selectedVideos.value.size} 个视频？不可恢复。`,
    okText: '删除', level: 'warn',
  })
  if (!ok) return
  let failed = 0
  for (const key of selectedVideos.value) {
    const idx = key.indexOf(':')
    const sk = key.substring(0, idx)
    const vid = key.substring(idx + 1)
    try {
      await DeleteVideoApi({ source_key: sk, vod_id: vid })
      videoStore.notifyDeletion(sk, vid)
    } catch { failed++ }
  }
  errorStore.info('批量删除', `成功 ${selectedVideos.value.size - failed}，失败 ${failed}`, '', 'AdminVideos')
  await loadVideoList()
  await loadSourcesAndStats()
}

async function deleteVideo(v: any): Promise<void> {
  const ok = await confirmStore.confirm({
    title: '删除视频', message: `确认删除「${v.vod_name}」？`, okText: '删除', level: 'warn',
  })
  if (!ok) return
  try {
    await DeleteVideoApi({ source_key: v.source_key, vod_id: v.vod_id })
    videoStore.notifyDeletion(v.source_key, String(v.vod_id))
    errorStore.info('删除成功', `已删除: ${v.vod_name}`, '', 'AdminVideos')
    await loadVideoList()
    await loadSourcesAndStats()
  } catch (e: any) {
    errorStore.fromError('删除失败', e, 'AdminVideos')
  }
}

async function openDetail(v: any): Promise<void> {
  try {
    detailRaw.value = await GetVideoDetail({ source_key: v.source_key, vod_id: v.vod_id })
  } catch { detailRaw.value = null }
  detailItem.value = v
  detailOpen.value = true
}

onMounted(async () => {
  await loadSourceKeys()
  await loadVideoList()
})
</script>

<template>
  <div>
    <div class="a-card">
      <div class="a-card-hd">
        <h3>视频列表</h3>
        <div class="a-card-hd-acts">
          <select v-model="videoSourceKey" class="a-sel" @change="videoPage = 1; loadVideoList()">
            <option value="">全部源</option>
            <option v-for="sk in sourceKeys" :key="sk" :value="sk">{{ sk }}</option>
          </select>
          <input v-model="videoSearch" type="text" placeholder="搜索关键词..." class="a-inp" style="max-width:200px" @keyup.enter="videoPage = 1; loadVideoList()" />
          <Button variant="primary" size="sm" @click="videoPage = 1; loadVideoList()">
            <Icon name="search" :size="12" /> 搜索
          </Button>
          <Button variant="danger" size="sm" :disabled="selectedVideos.size === 0" @click="batchDelete">
            <Icon name="trash" :size="12" /> 批量删除 ({{ selectedVideos.size }})
          </Button>
        </div>
      </div>

      <Spinner v-if="videoLoading" size="sm" label="加载中..." />
      <Empty v-else-if="videoList.length === 0" title="暂无视频数据" />
      <template v-else>
        <table class="a-tb">
          <thead>
            <tr>
              <th style="width:36px"><input type="checkbox" v-model="selectAll" @change="toggleSelectAll" /></th>
              <th>源</th><th>名称</th><th>类型</th><th>年份</th><th>地区</th><th>评分</th><th>操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="v in videoList" :key="`${v.source_key}-${v.vod_id}`">
              <td><input type="checkbox" :checked="selectedVideos.has(`${v.source_key}:${v.vod_id}`)" @change="toggleSelect(v)" /></td>
              <td class="a-tb-mono">{{ v.source_key }}</td>
              <td class="a-tb-name" :title="v.vod_name">{{ v.vod_name }}</td>
              <td>{{ v.type_name }}</td>
              <td>{{ v.vod_year }}</td>
              <td>{{ v.vod_area }}</td>
              <td>{{ v.vod_score || '-' }}</td>
              <td class="a-tb-acts">
                <Button variant="secondary" size="sm" @click="openDetail(v)">详情</Button>
                <Button variant="danger" size="sm" @click="deleteVideo(v)">删除</Button>
              </td>
            </tr>
          </tbody>
        </table>
        <div class="a-pgn">
          <span>共 {{ videoTotal }} 条</span>
          <div class="a-pgn-btns">
            <Button variant="secondary" size="sm" :disabled="videoPage <= 1" @click="videoPage--; loadVideoList()">上一页</Button>
            <span class="a-pgn-num">{{ videoPage }}</span>
            <Button variant="secondary" size="sm" :disabled="videoPage * videoPageSize >= videoTotal" @click="videoPage++; loadVideoList()">下一页</Button>
          </div>
        </div>
      </template>
    </div>

    <Modal :model-value="detailOpen" title="视频详情" width="min(800px, 94vw)" :show-footer="true"
      @update:model-value="(v: boolean) => { if (!v) detailOpen = false }">
      <div v-if="detailItem" style="display:flex;flex-direction:column;gap:6px;max-height:60vh;overflow-y:auto">
        <div class="a-detail-row"><strong>名称：</strong>{{ detailItem.vod_name }}</div>
        <div class="a-detail-row"><strong>源Key：</strong>{{ detailItem.source_key }}</div>
        <div class="a-detail-row"><strong>VodId：</strong>{{ detailItem.vod_id }}</div>
        <div class="a-detail-row"><strong>类型：</strong>{{ detailItem.type_name }}</div>
        <div class="a-detail-row"><strong>备注：</strong>{{ detailItem.vod_remarks }}</div>
        <div class="a-detail-row"><strong>年份：</strong>{{ detailItem.vod_year }}</div>
        <div class="a-detail-row"><strong>地区：</strong>{{ detailItem.vod_area }}</div>
        <div class="a-detail-row"><strong>导演：</strong>{{ detailItem.vod_director }}</div>
        <div class="a-detail-row"><strong>演员：</strong>{{ detailItem.vod_actor }}</div>
        <div class="a-detail-row"><strong>评分：</strong>{{ detailItem.vod_score }}</div>
        <div class="a-detail-row"><strong>点击：</strong>{{ detailItem.vod_hits }}</div>
        <div class="a-detail-row"><strong>更新：</strong>{{ detailItem.vod_time }}</div>
        <div v-if="detailItem.vod_pic" class="a-detail-row"><strong>封面：</strong><img :src="detailItem.vod_pic" class="a-detail-pic" /></div>
        <div v-if="detailRaw" class="a-detail-row"><strong>完整数据：</strong><pre class="a-detail-json">{{ JSON.stringify(detailRaw, null, 2) }}</pre></div>
      </div>
      <template #footer><Button variant="secondary" @click="detailOpen = false">关闭</Button></template>
    </Modal>
  </div>
</template>

