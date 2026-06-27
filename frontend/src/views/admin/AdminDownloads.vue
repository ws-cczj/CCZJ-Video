<script setup lang="ts">
import { onMounted } from 'vue'
import { useDownloadStore, formatBytes, formatSpeed, formatEta, percent } from '../../stores/download'
import Icon from '../../components/Icon.vue'
import { Button, Badge, Empty } from '../../components/ui'

const dl = useDownloadStore()

function statusVariant(status: string): 'primary' | 'success' | 'warning' | 'danger' | 'default' {
  switch (status) {
    case 'downloading': return 'primary'
    case 'paused': return 'warning'
    case 'done': return 'success'
    case 'error': return 'danger'
    case 'cancelled': return 'default'
    case 'queued': return 'primary'
    default: return 'default'
  }
}
function statusLabel(status: string): string {
  switch (status) {
    case 'downloading': return '下载中'
    case 'paused': return '已暂停'
    case 'done': return '已完成'
    case 'error': return '错误'
    case 'cancelled': return '已取消'
    case 'queued': return '排队中'
    default: return status
  }
}

onMounted(async () => {
  await dl.init()
})
</script>

<template>
  <div>
    <div class="a-card">
      <div class="a-card-hd">
        <h3>下载任务 ({{ dl.tasks.length }})</h3>
        <div class="a-card-hd-acts">
          <span class="a-desc" v-if="dl.dir">目录: {{ dl.dir }}</span>
          <Button variant="secondary" size="sm" @click="dl.init()">
            <Icon name="refresh" :size="12" /> 刷新
          </Button>
        </div>
      </div>

      <Empty v-if="dl.tasks.length === 0" title="暂无下载任务" />
      <table v-else class="a-tb">
        <thead>
          <tr>
            <th>文件名</th>
            <th>状态</th>
            <th>进度</th>
            <th>大小</th>
            <th>速度</th>
            <th>ETA</th>
            <th>操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="t in dl.tasks" :key="t.task_id">
            <td class="a-tb-name cczj-truncate" :title="t.filename || t.vod_name">
              {{ t.vod_name ? `${t.vod_name} - ${t.ep_name || ''}` : t.filename }}
            </td>
            <td>
              <Badge :variant="statusVariant(t.status)">{{ statusLabel(t.status) }}</Badge>
              <span v-if="t.error" class="a-desc" style="margin-left:4px;color:var(--danger)" :title="t.error">!</span>
            </td>
            <td>
              <div class="dl-prog">
                <div class="dl-prog-bar" :style="{ width: percent(t) + '%' }"></div>
                <span class="dl-prog-txt">{{ percent(t) }}%</span>
              </div>
            </td>
            <td class="a-tb-mono cczj-truncate">{{ formatBytes(t.downloaded) }} / {{ formatBytes(t.total) }}</td>
            <td class="a-tb-mono cczj-truncate">{{ t.status === 'downloading' ? formatSpeed(t.speed_bps) : '--' }}</td>
            <td class="a-tb-mono cczj-truncate">{{ t.status === 'downloading' ? formatEta(t.eta_sec) : '--' }}</td>
            <td class="a-tb-acts">
              <Button v-if="t.status === 'downloading'" variant="secondary" size="sm" @click="dl.pause(t.task_id)">暂停</Button>
              <Button v-if="t.status === 'paused'" variant="primary" size="sm" @click="dl.resume(t.task_id)">恢复</Button>
              <Button v-if="t.status === 'downloading' || t.status === 'paused' || t.status === 'queued'" variant="danger" size="sm" @click="dl.cancel(t.task_id)">取消</Button>
              <Button v-if="t.status === 'done' && t.save_path" variant="secondary" size="sm" @click="dl.openFile(t.save_path)">打开</Button>
              <Button variant="secondary" size="sm" @click="dl.remove(t.task_id)">移除</Button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>

<style scoped>
.dl-prog {
  position: relative;
  width: 80px;
  height: 16px;
  background: var(--bg-hover);
  border-radius: 4px;
  overflow: hidden;
}
.dl-prog-bar {
  height: 100%;
  background: var(--accent);
  border-radius: 4px;
  transition: width 0.3s;
}
.dl-prog-txt {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 10px;
  font-weight: 600;
  color: var(--text-primary);
}
</style>
