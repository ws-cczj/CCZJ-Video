<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { GetLogList, GetLogContent, GetLogDir, ClearLogs } from '../../../bindings/cczjVideo/app'
import { useErrorStore } from '../../stores/error'
import { useConfirmStore } from '../../stores/confirm'
import Icon from '../../components/Icon.vue'
import { Button, Empty, Spinner } from '../../components/ui'

const errorStore = useErrorStore()
const confirmStore = useConfirmStore()

const logFiles = ref<string[]>([])
const selectedLogFile = ref('')
const logContent = ref('')
const logDir = ref('')
const logLoading = ref(false)
const logSearch = ref('')
const logLevelFilter = ref('')

const logLineCount = computed(() => logContent.value ? logContent.value.split('\n').filter(Boolean).length : 0)

const filteredLogLines = computed(() => {
  if (!logContent.value) return []
  let lines = logContent.value.split('\n').filter(Boolean)
  if (logLevelFilter.value) lines = lines.filter(l => l.includes(`[${logLevelFilter.value}]`))
  if (logSearch.value) {
    const lower = logSearch.value.toLowerCase()
    lines = lines.filter(l => l.toLowerCase().includes(lower))
  }
  return lines
})

function logLineClass(line: string): string {
  if (line.includes('[ERROR]')) return 'a-log-err'
  if (line.includes('[WARN]')) return 'a-log-warn'
  if (line.includes('[DEBUG]')) return 'a-log-dbg'
  if (line.includes('[INFO]')) return 'a-log-info'
  return ''
}

function escapeHtml(s: string): string {
  return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;')
}

function highlightSearch(line: string): string {
  const safe = escapeHtml(line)
  if (!logSearch.value) return safe
  const esc = logSearch.value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  return safe.replace(new RegExp(`(${esc})`, 'gi'), '<mark>$1</mark>')
}

function getLastNLines(content: string, n: number): string {
  const lines = content.split('\n').filter(Boolean)
  if (lines.length <= n) return content
  return lines.slice(-n).join('\n')
}

async function refreshLogs(): Promise<void> {
  try {
    logFiles.value = await GetLogList()
    logDir.value = await GetLogDir()
    const today = new Date().toISOString().split('T')[0]
    const todayLog = `cczj-${today}.log`
    if (logFiles.value.includes(todayLog)) {
      selectedLogFile.value = todayLog
    } else if (!selectedLogFile.value || !logFiles.value.includes(selectedLogFile.value)) {
      selectedLogFile.value = logFiles.value[0] || ''
    }
    if (selectedLogFile.value) {
      const content = await GetLogContent(selectedLogFile.value)
      logContent.value = getLastNLines(content, 200)
    } else {
      logContent.value = ''
    }
  } catch (e: any) {
    errorStore.fromError('加载日志失败', e, 'AdminLogs')
  }
}

async function openLogFile(filename: string): Promise<void> {
  selectedLogFile.value = filename
  logLoading.value = true
  try {
    const content = await GetLogContent(filename)
    logContent.value = getLastNLines(content, 200)
  } catch {
    logContent.value = ''
  } finally {
    logLoading.value = false
  }
}

async function clearAllLogs(): Promise<void> {
  const ok = await confirmStore.confirm({
    title: '清空日志', message: '确认清空所有日志文件？', okText: '清空', level: 'warn',
  })
  if (!ok) return
  try {
    await ClearLogs()
    errorStore.info('日志已清空', '', '', 'AdminLogs')
    selectedLogFile.value = ''
    logContent.value = ''
    await refreshLogs()
  } catch (e: any) {
    errorStore.fromError('清空失败', e, 'AdminLogs')
  }
}

onMounted(async () => {
  await refreshLogs()
})
</script>

<template>
  <div>
    <!-- 日志文件选择 -->
    <div class="a-card">
      <div class="a-card-hd">
        <h3>日志文件</h3>
        <div class="a-card-hd-acts">
          <Button variant="secondary" size="sm" @click="refreshLogs">
            <Icon name="refresh" :size="12" /> 刷新
          </Button>
          <Button variant="danger" size="sm" @click="clearAllLogs">
            <Icon name="trash" :size="12" /> 清空全部
          </Button>
        </div>
      </div>
      <p class="a-desc" style="margin-bottom:8px">目录：<code style="background:var(--bg-hover);padding:2px 6px;border-radius:4px;font-size:12px">{{ logDir }}</code></p>
      <div class="a-row">
        <select v-model="selectedLogFile" class="a-sel" @change="openLogFile(($event.target as HTMLSelectElement).value)">
          <option value="">-- 选择文件 --</option>
          <option v-for="f in logFiles" :key="f" :value="f">{{ f }}</option>
        </select>
      </div>
    </div>

    <!-- 日志内容 -->
    <div class="a-card">
      <div class="a-row" style="margin-bottom:10px">
        <input v-model="logSearch" type="text" placeholder="搜索关键词..." class="a-inp" style="max-width:220px" />
        <select v-model="logLevelFilter" class="a-sel">
          <option value="">全部级别</option>
          <option value="ERROR">ERROR</option>
          <option value="WARN">WARN</option>
          <option value="INFO">INFO</option>
          <option value="DEBUG">DEBUG</option>
        </select>
        <span class="a-desc">匹配 {{ filteredLogLines.length }} / 共 {{ logLineCount }} 条</span>
      </div>

      <Spinner v-if="logLoading" size="sm" label="加载中..." />
      <div v-else class="a-log-view">
        <div v-if="filteredLogLines.length" style="display:flex;flex-direction:column">
          <div v-for="(l, i) in filteredLogLines" :key="i" class="a-log-line" :class="logLineClass(l)" v-html="highlightSearch(l)"></div>
        </div>
        <Empty v-else :title="logContent ? '无匹配日志' : '选择日志文件查看'" />
      </div>
    </div>

    <!-- 会话错误 -->
    <div class="a-card" v-if="errorStore.history.length">
      <div class="a-card-hd">
        <h3>会话错误/提示 ({{ errorStore.history.length }})</h3>
        <Button variant="secondary" size="sm" @click="errorStore.clearToasts()">关闭弹窗</Button>
      </div>
      <div class="a-log-view">
        <pre v-for="h in errorStore.history" :key="h.id"
          style="color:var(--text-primary);margin:0;padding:4px 0">{{ `[${new Date(h.time).toLocaleString()}] [${h.level.toUpperCase()}] ${h.title} - ${h.message}${h.detail ? '\n' + h.detail : ''}` }}</pre>
      </div>
    </div>
  </div>
</template>
