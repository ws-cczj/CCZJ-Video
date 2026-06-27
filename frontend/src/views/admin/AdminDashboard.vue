<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { useDownloadStore } from '../../stores/download'
import { useCollectStore } from '../../stores/collect'
import { useErrorStore } from '../../stores/error'
import { TriggerCollectNow } from '../../../bindings/cczjVideo/app'
import Icon from '../../components/Icon.vue'
import { Button, Badge } from '../../components/ui'
import { useAdminData } from './composables/useAdminData'

const dl = useDownloadStore()
const collect = useCollectStore()
const errorStore = useErrorStore()
const { sourceStats, collectStatusMap, loadSourcesAndStats } = useAdminData()

const totalVideos = computed(() =>
  sourceStats.value.reduce((s: number, st: any) => s + (st.video_count || 0), 0),
)
const totalEpisodes = computed(() =>
  sourceStats.value.reduce((s: number, st: any) => s + (st.episode_count || 0), 0),
)

async function triggerAllCollect(mode: string): Promise<void> {
  try {
    await TriggerCollectNow({ source_key: '', mode, hours: 0 })
    errorStore.info('已触发', `全量${mode === 'full' ? '' : '增量'}采集已启动`, '', 'AdminDashboard')
  } catch (e: any) {
    errorStore.fromError('触发失败', e, 'AdminDashboard')
  }
}

onMounted(async () => {
  await loadSourcesAndStats()
  await collect.loadSchedule()
})
</script>

<template>
  <div>
    <!-- 统计卡片 -->
    <div class="a-stat-grid">
      <div class="a-stat">
        <div class="a-stat-n">{{ sourceStats.length }}</div>
        <div class="a-stat-l">采集源</div>
      </div>
      <div class="a-stat">
        <div class="a-stat-n">{{ totalVideos }}</div>
        <div class="a-stat-l">视频总数</div>
      </div>
      <div class="a-stat">
        <div class="a-stat-n">{{ totalEpisodes }}</div>
        <div class="a-stat-l">剧集总数</div>
      </div>
      <div class="a-stat">
        <div class="a-stat-n">{{ dl.activeCount }}</div>
        <div class="a-stat-l">活动下载</div>
      </div>
    </div>

    <!-- 调度器状态 -->
    <div class="a-card">
      <div class="a-card-hd">
        <h3>采集调度器</h3>
        <div class="a-card-hd-acts">
          <Button variant="primary" size="sm" @click="triggerAllCollect('full')">
            <Icon name="play" :size="12" /> 全量采集
          </Button>
          <Button variant="secondary" size="sm" @click="triggerAllCollect('incremental')">
            <Icon name="refresh" :size="12" /> 增量采集
          </Button>
        </div>
      </div>
      <div class="scheduler-info">
        <div class="sched-item">
          <span class="sched-label">运行状态</span>
          <Badge :variant="collect.schedulerStatus?.running ? 'primary' : 'default'">
            {{ collect.schedulerStatus?.running ? '运行中' : '已停止' }}
          </Badge>
        </div>
        <div class="sched-item">
          <span class="sched-label">后台采集</span>
          <Badge :variant="collect.schedulerStatus?.background ? 'success' : 'default'">
            {{ collect.schedulerStatus?.background ? '已启用' : '未启用' }}
          </Badge>
        </div>
        <div class="sched-item" v-if="collect.schedulerStatus?.background_every_seconds">
          <span class="sched-label">循环间隔</span>
          <span class="sched-val">{{ collect.schedulerStatus.background_every_seconds }}秒</span>
        </div>
        <div class="sched-item" v-if="collect.schedulerStatus?.note">
          <span class="sched-label">备注</span>
          <span class="sched-val">{{ collect.schedulerStatus.note }}</span>
        </div>
      </div>
    </div>

    <!-- 各源数据统计 -->
    <div class="a-card" v-if="sourceStats.length">
      <div class="a-card-hd">
        <h3>各源数据统计</h3>
        <Button variant="secondary" size="sm" @click="loadSourcesAndStats">
          <Icon name="refresh" :size="12" /> 刷新
        </Button>
      </div>
      <table class="a-tb">
        <thead>
          <tr>
            <th>源名称</th>
            <th>Key</th>
            <th>视频数</th>
            <th>剧集数</th>
            <th>采集状态</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="st in sourceStats" :key="st.source_key">
            <td class="a-tb-name cczj-truncate">{{ st.name }}</td>
            <td class="a-tb-mono cczj-truncate">{{ st.source_key }}</td>
            <td class="a-tb-num">{{ st.video_count }}</td>
            <td class="a-tb-num">{{ st.episode_count }}</td>
            <td>
              <Badge v-if="collectStatusMap[st.source_key]?.running" variant="primary">
                采集中 ({{ collectStatusMap[st.source_key]?.page || 0 }}页)
              </Badge>
              <Badge v-else variant="default">空闲</Badge>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>

<style scoped>
.scheduler-info {
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
</style>
