<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from 'vue'
import {
  DoubanStatus, DoubanGetAll, DoubanTriggerNow, DoubanUpdateVideo,
} from '../../../bindings/cczjVideo/app'
import { useErrorStore } from '../../stores/error'
import Icon from '../../components/Icon.vue'
import { Button, Badge, Empty, Spinner } from '../../components/ui'

const errorStore = useErrorStore()

const status = ref<any>(null)
const allRows = ref<any[]>([])
const loading = ref(false)
const searchKey = ref('')
const updating = ref('')

// 分页
const PAGE_SIZE = 20
const currentPage = ref(1)
const totalCount = ref(0)
const totalPages = computed(() => Math.max(1, Math.ceil(totalCount.value / PAGE_SIZE)))

const filteredRows = computed(() => {
  if (!searchKey.value) return allRows.value
  const q = searchKey.value.toLowerCase()
  return allRows.value.filter((r: any) =>
    (r.VodName || '').toLowerCase().includes(q) ||
    (r.SubjectID || '').toString().includes(q),
  )
})

async function loadStatus(): Promise<void> {
  try { status.value = await DoubanStatus() } catch { /* */ }
}

async function loadAll(page?: number): Promise<void> {
  loading.value = true
  const p = page || currentPage.value
  try {
    const resp = await DoubanGetAll({ page: p, page_size: PAGE_SIZE })
    if (resp) {
      allRows.value = resp.rows || []
      totalCount.value = resp.total || 0
      currentPage.value = resp.page || 1
    } else {
      allRows.value = []
      totalCount.value = 0
    }
  } catch {
    allRows.value = []
    totalCount.value = 0
  } finally {
    loading.value = false
  }
}

function goPage(p: number): void {
  if (p < 1 || p > totalPages.value || p === currentPage.value) return
  loadAll(p)
}

let _statusTimer: ReturnType<typeof setTimeout> | undefined

async function triggerNow(): Promise<void> {
  try {
    const count = await DoubanTriggerNow()
    errorStore.info('已触发', `豆瓣补全已触发，处理 ${count} 条`, '', 'AdminDouban')
    _statusTimer = setTimeout(loadStatus, 2000)
  } catch (e: any) {
    errorStore.fromError('触发失败', e, 'AdminDouban')
  }
}

onUnmounted(() => {
  if (_statusTimer !== undefined) clearTimeout(_statusTimer)
})

async function updateOne(row: any): Promise<void> {
  const key = row.VodName || row.GlobalID
  updating.value = key
  try {
    await DoubanUpdateVideo({ keyword: row.VodName || '' })
    errorStore.info('已更新', `${row.VodName} 豆瓣信息已补全`, '', 'AdminDouban')
    await loadAll()
  } catch (e: any) {
    errorStore.fromError('更新失败', e, 'AdminDouban')
  } finally {
    updating.value = ''
  }
}

onMounted(async () => {
  await loadStatus()
  await loadAll()
})
</script>

<template>
  <div>
    <!-- 调度器状态 -->
    <div class="a-card">
      <div class="a-card-hd">
        <h3>豆瓣调度器</h3>
        <div class="a-card-hd-acts">
          <Button variant="primary" size="sm" @click="triggerNow">
            <Icon name="play" :size="12" /> 触发补全
          </Button>
          <Button variant="secondary" size="sm" @click="loadStatus">
            <Icon name="refresh" :size="12" /> 刷新
          </Button>
        </div>
      </div>
      <div v-if="status" class="sched-grid">
        <div class="sched-item">
          <span class="sched-label">运行中</span>
          <Badge :variant="status.running ? 'primary' : 'default'">{{ status.running ? '是' : '否' }}</Badge>
        </div>
        <div class="sched-item" v-if="status.total !== undefined">
          <span class="sched-label">总记录</span>
          <span class="sched-val">{{ status.total }}</span>
        </div>
        <div class="sched-item" v-if="status.completed !== undefined">
          <span class="sched-label">已补全</span>
          <span class="sched-val" style="color:var(--success)">{{ status.completed }}</span>
        </div>
        <div class="sched-item" v-if="status.pending !== undefined">
          <span class="sched-label">待补全</span>
          <span class="sched-val" style="color:var(--warning)">{{ status.pending }}</span>
        </div>
      </div>
    </div>

    <!-- 数据列表 -->
    <div class="a-card">
      <div class="a-card-hd">
        <h3>豆瓣数据 ({{ totalCount }})</h3>
        <div class="a-card-hd-acts">
          <input v-model="searchKey" type="text" placeholder="搜索..." class="a-inp" style="max-width:180px" />
          <Button variant="secondary" size="sm" @click="loadAll()">
            <Icon name="refresh" :size="12" /> 刷新
          </Button>
        </div>
      </div>

      <Spinner v-if="loading" size="sm" label="加载中..." />
      <Empty v-else-if="allRows.length === 0" title="暂无豆瓣数据" />
      <div v-else class="douban-list">
        <table class="a-tb">
          <thead>
            <tr>
              <th>视频名</th>
              <th>SubjectID</th>
              <th>评分</th>
              <th>年份</th>
              <th>状态</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="row in filteredRows" :key="row.GlobalID || row.VodName || row.Id">
              <td class="a-tb-name cczj-truncate">{{ row.VodName || `(记录 #${row.GlobalID})` }}</td>
              <td class="a-tb-mono cczj-truncate">{{ row.SubjectID || '--' }}</td>
              <td class="a-tb-num">{{ row.Rating || '--' }}</td>
              <td>{{ row.ReleaseDate ? row.ReleaseDate.substring(0, 4) : '--' }}</td>
              <td>
                <Badge :variant="row.SubjectID ? 'success' : 'warning'">
                  {{ row.SubjectID ? '已补全' : '待补全' }}
                </Badge>
              </td>
              <td class="a-tb-acts">
                <Button variant="secondary" size="sm"
                  :disabled="updating === (row.VodName || row.GlobalID)"
                  @click="updateOne(row)">
                  {{ updating === (row.VodName || row.GlobalID) ? '更新中...' : '补全' }}
                </Button>
              </td>
            </tr>
            <tr v-if="filteredRows.length === 0">
              <td colspan="6" class="a-tb-empty">无匹配数据</td>
            </tr>
          </tbody>
        </table>

        <!-- 分页 -->
        <div v-if="totalPages > 1" class="pagination">
          <button class="page-btn" :disabled="currentPage <= 1" @click="goPage(currentPage - 1)">上一页</button>
          <span class="page-info">{{ currentPage }} / {{ totalPages }}</span>
          <button class="page-btn" :disabled="currentPage >= totalPages" @click="goPage(currentPage + 1)">下一页</button>
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

/* 分页 */
.pagination {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 12px;
  margin-top: 16px;
  padding-top: 12px;
  border-top: 1px solid var(--border);
}
.page-btn {
  padding: 4px 12px;
  border-radius: 6px;
  border: 1px solid var(--border);
  background: var(--bg-card);
  color: var(--text-secondary);
  font-size: 12px;
  cursor: pointer;
  transition: all 0.15s;
  font-family: inherit;
}
.page-btn:hover:not(:disabled) {
  border-color: var(--accent);
  color: var(--accent);
}
.page-btn:disabled {
  opacity: 0.4;
  cursor: not-allowed;
}
.page-info {
  font-size: 12px;
  color: var(--text-muted);
  min-width: 60px;
  text-align: center;
}
</style>
