<script setup lang="ts">
defineOptions({ name: 'DevAdmin' })
import { ref, onMounted, computed, watch } from 'vue'
import {
  GetSourceStats, GetSetting, SetSetting, GetLogList, GetLogContent, GetLogDir,
  GetVideoList, DeleteVideo as DeleteVideoApi, GetAllSources, GetVideoDetail,
  RunSourceAction, ClearLogs, ExportSource, ImportSource,
  AddSource as AddSourceApi, UpdateSource as UpdateSourceApi, DeleteSource as DeleteSourceApi,
  StartCollect, StopCollect, GetCollectStatus,
  GetTypes,
} from '../../bindings/cczjVideo/app'
import { useErrorStore } from '../stores/error'
import { useDownloadStore } from '../stores/download'
import { useThemeStore } from '../stores/theme'
import { useDevMode } from '../stores/devMode'
import { useConfirmStore } from '../stores/confirm'
import { useVideoStore } from '../stores/video'
import { useSourceStore } from '../stores/source'
import Icon from '../components/Icon.vue'
import { Button, Modal } from '../components/ui'

const errorStore = useErrorStore()
const downloadStore = useDownloadStore()
const themeStore = useThemeStore()
const devMode = useDevMode()
const confirmStore = useConfirmStore()
const videoStore = useVideoStore()
const sourceStore = useSourceStore()

// ========== 标签页切换 ==========
type TabId = 'dashboard' | 'sources' | 'videos' | 'types' | 'settings' | 'data' | 'logs'
const tabs: { id: TabId; label: string; icon: string }[] = [
  { id: 'dashboard', label: '仪表盘', icon: 'monitor' },
  { id: 'sources', label: '采集源', icon: 'source' },
  { id: 'videos', label: '视频数据', icon: 'film' },
  { id: 'types', label: '分类管理', icon: 'folder' },
  { id: 'data', label: '数据管理', icon: 'database' },
  { id: 'logs', label: '系统日志', icon: 'code' },
  { id: 'settings', label: '系统设置', icon: 'settings' },
]
const activeTab = ref<TabId>('dashboard')

// ========== 仪表盘 ==========
interface SourceStat { source_key: string; name: string; video_count: number; episode_count: number }
const sourceStats = ref<SourceStat[]>([])
const totalVideos = computed(() => sourceStats.value.reduce((s, st) => s + st.video_count, 0))
const totalEpisodes = computed(() => sourceStats.value.reduce((s, st) => s + st.episode_count, 0))
const collectStatusMap = ref<Record<string, any>>({})

async function loadDashboard(): Promise<void> {
  try { sourceStats.value = await GetSourceStats() } catch (e: any) { errorStore.fromError('加载统计失败', e, 'DevAdmin') }
  try {
    const sources = await GetAllSources()
    for (const s of sources as any[]) {
      try { collectStatusMap.value[s.source_key || s.key] = await GetCollectStatus(s.source_key || s.key) } catch { /* */ }
    }
  } catch { /* */ }
}

// ========== 采集源管理 ==========
interface SourceInfo {
  source_key: string; name: string; api_url: string; enabled: number; id: number
  adv_config?: any; collect_limit?: number; collect_hours?: number
}
const allSources = ref<SourceInfo[]>([])
const sourceSearch = ref('')
const filteredSources = computed(() => {
  if (!sourceSearch.value) return allSources.value
  const q = sourceSearch.value.toLowerCase()
  return allSources.value.filter(s => s.name.toLowerCase().includes(q) || s.source_key.toLowerCase().includes(q))
})

const sourceModalOpen = ref(false)
const sourceModalMode = ref<'add' | 'edit'>('add')
const sourceForm = ref({ source_key: '', name: '', api_url: '', enabled: true, adv_config: '', collect_limit: 0, collect_hours: 0 })

function openAddSource(): void {
  sourceModalMode.value = 'add'
  sourceForm.value = { source_key: '', name: '', api_url: '', enabled: true, adv_config: '', collect_limit: 0, collect_hours: 0 }
  sourceModalOpen.value = true
}
function openEditSource(s: SourceInfo): void {
  sourceModalMode.value = 'edit'
  sourceForm.value = {
    source_key: s.source_key, name: s.name, api_url: s.api_url,
    enabled: s.enabled === 1,
    adv_config: s.adv_config ? (typeof s.adv_config === 'string' ? s.adv_config : JSON.stringify(s.adv_config, null, 2)) : '',
    collect_limit: s.collect_limit || 0,
    collect_hours: s.collect_hours || 0,
  }
  sourceModalOpen.value = true
}
async function saveSource(): Promise<void> {
  try {
    const payload: any = {
      source_key: sourceForm.value.source_key.trim(),
      name: sourceForm.value.name.trim(),
      api_url: sourceForm.value.api_url.trim(),
      enabled: sourceForm.value.enabled ? 1 : 0,
      collect_limit: sourceForm.value.collect_limit,
      collect_hours: sourceForm.value.collect_hours,
    }
    if (sourceForm.value.adv_config.trim()) {
      try { payload.adv_config = JSON.parse(sourceForm.value.adv_config.trim()) } catch { payload.adv_config = sourceForm.value.adv_config.trim() }
    }
    if (sourceModalMode.value === 'add') {
      await AddSourceApi(payload)
      errorStore.info('添加成功', `源 "${payload.name}" 已添加`, '', 'DevAdmin')
    } else {
      await UpdateSourceApi(payload)
      errorStore.info('修改成功', `源 "${payload.name}" 已更新`, '', 'DevAdmin')
    }
    sourceModalOpen.value = false
    await loadSources()
    await loadDashboard()
  } catch (e: any) { errorStore.fromError('保存失败', e, 'DevAdmin.saveSource') }
}

async function loadSources(): Promise<void> {
  try {
    const sources = await GetAllSources() as any[]
    allSources.value = sources.map((s: any) => ({
      id: s.id || 0, source_key: s.source_key || '', name: s.name || '',
      api_url: s.api_url || '', enabled: s.enabled ?? 1,
      adv_config: s.adv_config || null,
      collect_limit: s.collect_limit || 0,
      collect_hours: s.collect_hours || 0,
    }))
  } catch (e: any) { errorStore.fromError('加载采集源失败', e, 'DevAdmin.loadSources'); allSources.value = [] }
}

async function handleDeleteSource(sk: string): Promise<void> {
  const ok = await confirmStore.confirm({ title: '删除源', message: `确认删除源「${sk}」及所有视频数据？不可恢复。`, okText: '删除', level: 'danger' })
  if (!ok) return
  try { await DeleteSourceApi(sk); errorStore.info('删除成功', `源 ${sk} 已删除`, '', 'DevAdmin'); await loadDashboard(); await loadSources() }
  catch (e: any) { errorStore.fromError('删除失败', e, 'DevAdmin.deleteSource') }
}

async function handleTruncateSource(sk: string): Promise<void> {
  const ok = await confirmStore.confirm({ title: '清空数据', message: `确认清空源「${sk}」的视频数据？源配置保留。`, okText: '清空', level: 'warn' })
  if (!ok) return
  try { await RunSourceAction({ source_key: sk, action: 'truncate', vod_id: '' }); errorStore.info('已清空', `源 ${sk} 数据已清空`, '', 'DevAdmin'); await loadDashboard() }
  catch (e: any) { errorStore.fromError('清空失败', e, 'DevAdmin.truncate') }
}

async function exportSource(sk: string): Promise<void> {
  try { const p = await ExportSource(sk); errorStore.info('导出成功', `文件: ${p}`, '', 'DevAdmin') }
  catch (e: any) { errorStore.fromError('导出失败', e, 'DevAdmin.export') }
}

async function startCollect(sk: string, mode: string): Promise<void> {
  try { await StartCollect({ source_key: sk, mode, hours: 24 }); errorStore.info('开始采集', `${sk} ${mode}采集已启动`, '', 'DevAdmin'); await refreshCollectStatus(sk) }
  catch (e: any) { errorStore.fromError('采集失败', e, 'DevAdmin.startCollect') }
}
async function stopCollect(sk: string): Promise<void> {
  try { await StopCollect({ source_key: sk, mode: '', hours: 0 }); errorStore.info('已停止', `${sk} 采集已停止`, '', 'DevAdmin'); await refreshCollectStatus(sk) }
  catch (e: any) { errorStore.fromError('停止失败', e, 'DevAdmin.stopCollect') }
}
async function refreshCollectStatus(sk: string): Promise<void> {
  try { collectStatusMap.value[sk] = await GetCollectStatus(sk) } catch { /* */ }
}

// ========== 视频数据 ==========
interface VideoItem {
  source_key: string; vod_id: string; vod_name: string; type_name: string
  vod_remarks: string; vod_year: string; vod_area: string; vod_lang: string
  vod_actor: string; vod_director: string; vod_hits: string; vod_score: string
  vod_pic: string; vod_time: string
}
const videoList = ref<VideoItem[]>([])
const videoPage = ref(1)
const videoTotal = ref(0)
const videoPageSize = 20
const videoSourceKey = ref('')
const videoSearch = ref('')
const videoLoading = ref(false)
const sourceKeys = ref<string[]>([])
const selectedVideos = ref<Set<string>>(new Set())
const selectAll = ref(false)

const videoDetailOpen = ref(false)
const videoDetail = ref<VideoItem | null>(null)
const videoDetailRaw = ref<any>(null)

async function loadSourceKeys(): Promise<void> {
  try { const s = await GetAllSources(); sourceKeys.value = (s as any[]).map((x: any) => x.source_key || '') } catch { /* */ }
}

async function loadVideoList(): Promise<void> {
  videoLoading.value = true; selectedVideos.value.clear(); selectAll.value = false
  try {
    const req: any = { source_key: videoSourceKey.value || '', page: videoPage.value, page_size: videoPageSize, keyword: videoSearch.value || '', type_id: '', year: '', area: '', sort: '' }
    const resp = await GetVideoList(req)
    videoList.value = ((resp as any)?.videos || []).map((v: any) => ({
      source_key: v.source_key || v.vod_source || '', vod_id: v.vod_id || '', vod_name: v.vod_name || '',
      type_name: v.type_name || '', vod_remarks: v.vod_remarks || '', vod_year: v.vod_year || '',
      vod_area: v.vod_area || '', vod_lang: v.vod_lang || '', vod_actor: v.vod_actor || '',
      vod_director: v.vod_director || '', vod_hits: v.vod_hits || '', vod_score: v.vod_score || '',
      vod_pic: v.vod_pic || '', vod_time: v.vod_time || '',
    }))
    videoTotal.value = (resp as any)?.total || 0
  } catch { videoList.value = [] }
  finally { videoLoading.value = false }
}

function toggleSelectVideo(v: VideoItem): void {
  const key = `${v.source_key}:${v.vod_id}`
  if (selectedVideos.value.has(key)) selectedVideos.value.delete(key); else selectedVideos.value.add(key)
}
function toggleSelectAll(): void {
  if (selectAll.value) videoList.value.forEach(v => selectedVideos.value.add(`${v.source_key}:${v.vod_id}`))
  else selectedVideos.value.clear()
}

async function batchDelete(): Promise<void> {
  if (selectedVideos.value.size === 0) return
  const ok = await confirmStore.confirm({ title: '批量删除', message: `确认删除选中的 ${selectedVideos.value.size} 个视频？不可恢复。`, okText: '删除', level: 'warn' })
  if (!ok) return
  let failed = 0
  for (const key of selectedVideos.value) {
    const [sk, vid] = key.split(':')
    try { 
      await DeleteVideoApi({ source_key: sk, vod_id: vid })
      videoStore.notifyDeletion(sk, vid)
    } catch { failed++ }
  }
  errorStore.info('批量删除', `成功 ${selectedVideos.value.size - failed}，失败 ${failed}`, '', 'DevAdmin')
  await loadVideoList(); await loadDashboard()
}

async function deleteVideo(v: VideoItem): Promise<void> {
  const ok = await confirmStore.confirm({ title: '删除视频', message: `确认删除「${v.vod_name}」？`, okText: '删除', level: 'warn' })
  if (!ok) return
  try { 
    await DeleteVideoApi({ source_key: v.source_key, vod_id: v.vod_id })
    videoStore.notifyDeletion(v.source_key, String(v.vod_id))
    errorStore.info('删除成功', `已删除: ${v.vod_name}`, '', 'DevAdmin')
    await loadVideoList(); await loadDashboard() 
  }
  catch (e: any) { errorStore.fromError('删除失败', e, 'DevAdmin.deleteVideo') }
}

async function openVideoDetail(v: VideoItem): Promise<void> {
  try { videoDetailRaw.value = await GetVideoDetail({ source_key: v.source_key, vod_id: v.vod_id }) } catch { videoDetailRaw.value = null }
  videoDetail.value = v; videoDetailOpen.value = true
}

// ========== 分类管理 ==========
interface VideoType { id: number; type_name: string; parent_id: number; sort: number; source_key: string }
const allTypes = ref<VideoType[]>([])
const typeSourceKey = ref('')

async function loadTypes(): Promise<void> {
  try {
    const types = await GetTypes({ source_key: typeSourceKey.value || '' }) as any[]
    allTypes.value = types.map((t: any) => ({
      id: t.id || 0, type_name: t.type_name || '', parent_id: t.parent_id || 0,
      sort: t.sort || 0, source_key: t.source_key || '',
    }))
  } catch { allTypes.value = [] }
}

// ========== 数据管理 ==========
const importFilePath = ref('')
const importLoading = ref(false)
async function doImport(): Promise<void> {
  if (!importFilePath.value.trim()) { errorStore.info('提示', '请输入文件路径', '', 'DevAdmin'); return }
  const ok = await confirmStore.confirm({ title: '导入数据', message: '确认导入？同名源数据将合并。', okText: '导入', level: 'warn' })
  if (!ok) return
  importLoading.value = true
  try { const r = await ImportSource(importFilePath.value.trim()); errorStore.info('导入成功', r, '', 'DevAdmin'); importFilePath.value = ''; await loadDashboard(); await loadSources() }
  catch (e: any) { errorStore.fromError('导入失败', e, 'DevAdmin.import') }
  finally { importLoading.value = false }
}

// ========== 系统日志 ==========
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
  if (logSearch.value) { const lower = logSearch.value.toLowerCase(); lines = lines.filter(l => l.toLowerCase().includes(lower)) }
  return lines
})
function logLineClass(line: string): string {
  if (line.includes('[ERROR]')) return 'll-err'
  if (line.includes('[WARN]')) return 'll-warn'
  if (line.includes('[DEBUG]')) return 'll-dbg'
  if (line.includes('[INFO]')) return 'll-info'
  return ''
}
function highlightSearch(line: string): string {
  if (!logSearch.value) return line
  const esc = logSearch.value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  return line.replace(new RegExp(`(${esc})`, 'gi'), '<mark>$1</mark>')
}

function getLastNLines(content: string, n: number): string {
  const lines = content.split('\n').filter(Boolean)
  if (lines.length <= n) return content
  return lines.slice(-n).join('\n')
}
async function refreshLogs(): Promise<void> {
  try {
    logFiles.value = await GetLogList(); logDir.value = await GetLogDir()
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
    } else logContent.value = ''
  } catch (e: any) { errorStore.fromError('加载日志失败', e, 'DevAdmin.logs') }
}
async function openLogFile(filename: string): Promise<void> {
  selectedLogFile.value = filename; logLoading.value = true
  try {
    const content = await GetLogContent(filename)
    logContent.value = getLastNLines(content, 200)
  } catch { logContent.value = '' }
  finally { logLoading.value = false }
}
async function clearAllLogs(): Promise<void> {
  const ok = await confirmStore.confirm({ title: '清空日志', message: '确认清空所有日志文件？', okText: '清空', level: 'warn' })
  if (!ok) return
  try { await ClearLogs(); errorStore.info('日志已清空', '', '', 'DevAdmin'); selectedLogFile.value = ''; logContent.value = ''; await refreshLogs() }
  catch (e: any) { errorStore.fromError('清空失败', e, 'DevAdmin.clearLogs') }
}

// ========== 系统设置 ==========
const debugMode = ref(false)
const closeToTray = ref(true)
const clearCacheConfirm = ref(false)

async function loadSettings(): Promise<void> {
  try { const v = await GetSetting('debug_mode'); debugMode.value = v === '1' } catch { /* */ }
  try { const v = await GetSetting('close_to_tray'); closeToTray.value = v !== '0' } catch { /* */ }
}
async function saveDebugMode(): Promise<void> {
  try { await SetSetting('debug_mode', debugMode.value ? '1' : '0'); errorStore.info('已保存', 'debug_mode = ' + (debugMode.value ? '1' : '0'), '', 'DevAdmin') }
  catch (e: any) { errorStore.fromError('保存失败', e, 'DevAdmin.debugMode') }
}
async function clearCache(): Promise<void> {
  clearCacheConfirm.value = false
  try { await SetSetting('cache_clear_flag', Date.now().toString()); errorStore.info('缓存已标记清除', '重启后生效', '', 'DevAdmin') }
  catch (e: any) { errorStore.fromError('清除缓存失败', e, 'DevAdmin.clearCache') }
}

// ========== 生命周期 ==========
watch(activeTab, async (tab) => {
  if (tab === 'sources') await loadSources()
  if (tab === 'videos') await loadSourceKeys()
  if (tab === 'types') await loadTypes()
  if (tab === 'logs') await refreshLogs()
})

onMounted(async () => {
  await loadDashboard(); await loadSettings(); await loadSourceKeys()
  await downloadStore.init()
  if (!themeStore.loaded) await themeStore.load()
})
</script>

<template>
  <div class="da">
    <header class="da-hd">
      <h1 class="da-title"><Icon name="code" :size="20" /> 后台管理</h1>
    </header>

    <nav class="da-nav">
      <button v-for="t in tabs" :key="t.id" :class="['da-tab', { active: activeTab === t.id }]" @click="activeTab = t.id">
        <Icon :name="t.icon" :size="14" /> <span>{{ t.label }}</span>
      </button>
    </nav>

    <!-- ===== 仪表盘 ===== -->
    <div v-if="activeTab === 'dashboard'" class="da-body">
      <div class="stat-row">
        <div class="stat"><div class="stat-n">{{ sourceStats.length }}</div><div class="stat-l">采集源</div></div>
        <div class="stat"><div class="stat-n">{{ totalVideos }}</div><div class="stat-l">视频总数</div></div>
        <div class="stat"><div class="stat-n">{{ totalEpisodes }}</div><div class="stat-l">剧集总数</div></div>
        <div class="stat"><div class="stat-n">{{ downloadStore.activeCount }}</div><div class="stat-l">活动下载</div></div>
      </div>

      <div class="block" v-if="sourceStats.length">
        <div class="block-hd"><h3>各源数据统计</h3></div>
        <table class="tb">
          <thead><tr><th>源名称</th><th>Key</th><th>视频数</th><th>剧集数</th><th>采集状态</th></tr></thead>
          <tbody>
            <tr v-for="st in sourceStats" :key="st.source_key">
              <td class="tb-name">{{ st.name }}</td>
              <td class="tb-mono">{{ st.source_key }}</td>
              <td class="tb-num">{{ st.video_count }}</td>
              <td class="tb-num">{{ st.episode_count }}</td>
              <td>
                <span v-if="collectStatusMap[st.source_key]?.running" class="badge badge-running">采集中 ({{ collectStatusMap[st.source_key]?.page || 0 }}页)</span>
                <span v-else class="badge badge-idle">空闲</span>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <!-- ===== 采集源管理 ===== -->
    <div v-else-if="activeTab === 'sources'" class="da-body">
      <div class="block">
        <div class="block-hd">
          <h3>采集源列表 ({{ allSources.length }})</h3>
          <div class="block-acts">
            <input v-model="sourceSearch" type="text" placeholder="搜索源..." class="inp" />
            <Button variant="primary" size="sm" @click="openAddSource"><Icon name="plus" :size="14" /> 添加源</Button>
          </div>
        </div>
        <table class="tb">
          <thead><tr><th>ID</th><th>名称</th><th>Key</th><th>API</th><th>状态</th><th>采集控制</th><th style="width:180px">操作</th></tr></thead>
          <tbody>
            <tr v-for="s in filteredSources" :key="s.source_key">
              <td class="tb-num">{{ s.id }}</td>
              <td class="tb-name">{{ s.name }}</td>
              <td class="tb-mono">{{ s.source_key }}</td>
              <td class="tb-mono" :title="s.api_url">{{ s.api_url?.substring(0, 50) }}{{ s.api_url?.length > 50 ? '...' : '' }}</td>
              <td><span :class="s.enabled ? 'badge badge-ok' : 'badge badge-off'">{{ s.enabled ? '启用' : '禁用' }}</span></td>
              <td class="tb-acts">
                <Button v-if="collectStatusMap[s.source_key]?.running" variant="danger" size="sm" @click="stopCollect(s.source_key)">停止</Button>
                <template v-else>
                  <Button variant="secondary" size="sm" @click="startCollect(s.source_key, 'full')">全量</Button>
                  <Button variant="secondary" size="sm" @click="startCollect(s.source_key, 'incremental')">增量</Button>
                </template>
              </td>
              <td class="tb-acts">
                <Button variant="secondary" size="sm" @click="openEditSource(s)">编辑</Button>
                <Button variant="secondary" size="sm" @click="exportSource(s.source_key)">导出</Button>
                <Button variant="secondary" size="sm" style="background:#f59e0b;color:#fff;border-color:#f59e0b" @click="handleTruncateSource(s.source_key)">清空</Button>
                <Button variant="danger" size="sm" @click="handleDeleteSource(s.source_key)">删除</Button>
              </td>
            </tr>
            <tr v-if="filteredSources.length === 0"><td colspan="7" class="tb-empty">暂无数据</td></tr>
          </tbody>
        </table>
      </div>

      <Modal :model-value="sourceModalOpen" :title="sourceModalMode === 'add' ? '添加采集源' : '编辑采集源'" width="600px" :show-footer="true" ok-text="保存" cancel-text="取消" @update:model-value="(v: boolean) => { if (!v) sourceModalOpen = false }" @ok="saveSource">
        <div class="form-v">
          <label class="fm-l">名称</label>
          <input v-model="sourceForm.name" type="text" class="inp" placeholder="采集源显示名称" />
          <label class="fm-l">Key</label>
          <input v-model="sourceForm.source_key" type="text" class="inp" placeholder="唯一标识符" :disabled="sourceModalMode === 'edit'" />
          <label class="fm-l">API 地址</label>
          <input v-model="sourceForm.api_url" type="text" class="inp" placeholder="http://..." />
          <label class="fm-l">
            <input type="checkbox" v-model="sourceForm.enabled" style="width:auto;margin-right:6px" />
            <span>启用</span>
          </label>
          <label class="fm-l">采集限制（0=不限）</label>
          <input v-model.number="sourceForm.collect_limit" type="number" class="inp" style="width:100px" min="0" />
          <label class="fm-l">增量回溯小时（0=使用源配置）</label>
          <input v-model.number="sourceForm.collect_hours" type="number" class="inp" style="width:100px" min="0" />
          <label class="fm-l">高级配置 (JSON)</label>
          <textarea v-model="sourceForm.adv_config" class="inp fm-ta" rows="5" placeholder='{"field_mapping":{}, "collect_limit": 50}'></textarea>
        </div>
      </Modal>
    </div>

    <!-- ===== 视频数据 ===== -->
    <div v-else-if="activeTab === 'videos'" class="da-body">
      <div class="block">
        <div class="block-hd">
          <h3>视频列表</h3>
          <div class="video-tb">
            <select v-model="videoSourceKey" class="inp-sel" @change="videoPage = 1; loadVideoList()"><option value="">全部源</option><option v-for="sk in sourceKeys" :key="sk" :value="sk">{{ sk }}</option></select>
            <input v-model="videoSearch" type="text" placeholder="搜索关键词..." class="inp" @keyup.enter="videoPage = 1; loadVideoList()" />
            <Button variant="primary" size="sm" @click="videoPage = 1; loadVideoList()"><Icon name="search" :size="12" /> 搜索</Button>
            <Button variant="danger" size="sm" :disabled="selectedVideos.size === 0" @click="batchDelete"><Icon name="trash" :size="12" /> 批量删除 ({{ selectedVideos.size }})</Button>
          </div>
        </div>

        <div v-if="videoLoading" class="ld">加载中...</div>
        <div v-else-if="videoList.length === 0" class="empty">暂无视频数据</div>
        <div v-else>
          <table class="tb">
            <thead>
              <tr>
                <th style="width:36px"><input type="checkbox" v-model="selectAll" @change="toggleSelectAll" /></th>
                <th>源</th><th>名称</th><th>类型</th><th>年份</th><th>地区</th><th>评分</th><th>操作</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="v in videoList" :key="`${v.source_key}-${v.vod_id}`">
                <td><input type="checkbox" :checked="selectedVideos.has(`${v.source_key}:${v.vod_id}`)" @change="toggleSelectVideo(v)" /></td>
                <td class="tb-mono">{{ v.source_key }}</td>
                <td class="tb-name" :title="v.vod_name">{{ v.vod_name }}</td>
                <td>{{ v.type_name }}</td>
                <td>{{ v.vod_year }}</td>
                <td>{{ v.vod_area }}</td>
                <td>{{ v.vod_score || '-' }}</td>
                <td class="tb-acts">
                  <Button variant="secondary" size="sm" @click="openVideoDetail(v)">详情</Button>
                  <Button variant="danger" size="sm" @click="deleteVideo(v)">删除</Button>
                </td>
              </tr>
            </tbody>
          </table>
          <div class="pgn">
            <span>共 {{ videoTotal }} 条</span>
            <div class="pgn-btns">
              <Button variant="secondary" size="sm" :disabled="videoPage <= 1" @click="videoPage--; loadVideoList()">上一页</Button>
              <span class="pgn-num">{{ videoPage }}</span>
              <Button variant="secondary" size="sm" :disabled="videoPage * videoPageSize >= videoTotal" @click="videoPage++; loadVideoList()">下一页</Button>
            </div>
          </div>
        </div>
      </div>

      <Modal :model-value="videoDetailOpen" title="视频详情" width="min(800px, 94vw)" :show-footer="true" @update:model-value="(v: boolean) => { if (!v) videoDetailOpen = false }">
        <div v-if="videoDetail" class="vd">
          <div class="vd-r"><strong>名称：</strong>{{ videoDetail.vod_name }}</div>
          <div class="vd-r"><strong>源Key：</strong>{{ videoDetail.source_key }}</div>
          <div class="vd-r"><strong>VodId：</strong>{{ videoDetail.vod_id }}</div>
          <div class="vd-r"><strong>类型：</strong>{{ videoDetail.type_name }}</div>
          <div class="vd-r"><strong>备注：</strong>{{ videoDetail.vod_remarks }}</div>
          <div class="vd-r"><strong>年份：</strong>{{ videoDetail.vod_year }}</div>
          <div class="vd-r"><strong>地区：</strong>{{ videoDetail.vod_area }}</div>
          <div class="vd-r"><strong>导演：</strong>{{ videoDetail.vod_director }}</div>
          <div class="vd-r"><strong>演员：</strong>{{ videoDetail.vod_actor }}</div>
          <div class="vd-r"><strong>评分：</strong>{{ videoDetail.vod_score }}</div>
          <div class="vd-r"><strong>点击：</strong>{{ videoDetail.vod_hits }}</div>
          <div class="vd-r"><strong>更新：</strong>{{ videoDetail.vod_time }}</div>
          <div v-if="videoDetail.vod_pic" class="vd-r"><strong>封面：</strong><img :src="videoDetail.vod_pic" class="vd-pic" /></div>
          <div v-if="videoDetailRaw" class="vd-r"><strong>完整数据：</strong><pre class="vd-json">{{ JSON.stringify(videoDetailRaw, null, 2) }}</pre></div>
        </div>
        <template #footer><Button variant="secondary" @click="videoDetailOpen = false">关闭</Button></template>
      </Modal>
    </div>

    <!-- ===== 分类管理 ===== -->
    <div v-else-if="activeTab === 'types'" class="da-body">
      <div class="block">
        <div class="block-hd">
          <h3>视频分类</h3>
          <div class="block-acts">
            <select v-model="typeSourceKey" class="inp-sel" @change="loadTypes()"><option value="">全部源</option><option v-for="sk in sourceKeys" :key="sk" :value="sk">{{ sk }}</option></select>
          </div>
        </div>
        <div v-if="allTypes.length === 0" class="empty">暂无分类数据</div>
        <table class="tb" v-else>
          <thead><tr><th>ID</th><th>源</th><th>分类名称</th><th>父分类ID</th><th>排序</th></tr></thead>
          <tbody>
            <tr v-for="t in allTypes" :key="`${t.source_key}-${t.id}`">
              <td class="tb-num">{{ t.id }}</td>
              <td class="tb-mono">{{ t.source_key }}</td>
              <td class="tb-name">{{ t.type_name }}</td>
              <td class="tb-num">{{ t.parent_id }}</td>
              <td class="tb-num">{{ t.sort }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <!-- ===== 数据管理 ===== -->
    <div v-else-if="activeTab === 'data'" class="da-body">
      <div class="block"><div class="block-hd"><h3>导入数据</h3></div>
        <p style="color:var(--text-muted);font-size:13px;margin-bottom:10px">支持 .json.br / .json.gz / .json 格式。同名源数据将合并。</p>
        <div class="act-row"><input v-model="importFilePath" type="text" placeholder="输入文件路径..." class="inp" style="flex:1" /><Button variant="primary" @click="doImport" :disabled="importLoading">导入</Button></div>
      </div>
      <div class="block" v-if="sourceStats.length"><div class="block-hd"><h3>导出数据</h3></div>
        <table class="tb">
          <thead><tr><th>源名称</th><th>Key</th><th>视频数</th><th>操作</th></tr></thead>
          <tbody><tr v-for="st in sourceStats" :key="st.source_key"><td class="tb-name">{{ st.name }}</td><td class="tb-mono">{{ st.source_key }}</td><td class="tb-num">{{ st.video_count }}</td><td><Button variant="secondary" size="sm" @click="exportSource(st.source_key)">导出</Button></td></tr></tbody>
        </table>
      </div>
    </div>

    <!-- ===== 系统日志 ===== -->
    <div v-else-if="activeTab === 'logs'" class="da-body">
      <div class="block"><div class="block-hd"><h3>日志文件</h3></div>
        <p style="color:var(--text-muted);font-size:13px;margin-bottom:8px">目录：<code>{{ logDir }}</code></p>
        <div class="act-row">
          <select v-model="selectedLogFile" class="inp-sel" @change="openLogFile(($event.target as HTMLSelectElement).value)">
            <option value="">-- 选择文件 --</option>
            <option v-for="f in logFiles" :key="f" :value="f">{{ f }}</option>
          </select>
          <Button variant="secondary" size="sm" @click="refreshLogs">刷新</Button>
          <Button variant="danger" size="sm" @click="clearAllLogs">清空全部</Button>
        </div>
      </div>
      <div class="block">
        <div class="act-row" style="margin-bottom:10px">
          <input v-model="logSearch" type="text" placeholder="搜索关键词..." class="inp" />
          <select v-model="logLevelFilter" class="inp-sel"><option value="">全部级别</option><option value="ERROR">ERROR</option><option value="WARN">WARN</option><option value="INFO">INFO</option><option value="DEBUG">DEBUG</option></select>
          <span style="font-size:12px;color:var(--text-muted)">匹配 {{ filteredLogLines.length }} / 共 {{ logLineCount }} 条</span>
        </div>
        <div v-if="logLoading" class="ld">加载中...</div>
        <div v-else class="log-v">
          <div v-if="filteredLogLines.length" class="log-lns">
            <div v-for="(l, i) in filteredLogLines" :key="i" class="log-l" :class="logLineClass(l)" v-html="highlightSearch(l)"></div>
          </div>
          <div v-else class="empty">{{ logContent ? '无匹配日志' : '选择日志文件查看' }}</div>
        </div>
      </div>
      <div class="block" v-if="errorStore.history.length">
        <div class="block-hd"><h3>会话错误/提示 ({{ errorStore.history.length }})</h3><Button variant="secondary" size="sm" @click="errorStore.clearToasts()">关闭弹窗</Button></div>
        <div class="log-v"><pre v-for="h in errorStore.history" :key="h.id" class="log-se">[{{ new Date(h.time).toLocaleString() }}] [{{ h.level.toUpperCase() }}] {{ h.title }} - {{ h.message }}{{ h.detail ? '\n' + h.detail : '' }}</pre></div>
      </div>
    </div>

    <!-- ===== 系统设置 ===== -->
    <div v-else-if="activeTab === 'settings'" class="da-body">
      <div class="block"><div class="block-hd"><h3>调试选项</h3></div>
        <label class="fm-l"><input type="checkbox" v-model="debugMode" @change="saveDebugMode" style="width:auto;margin-right:6px" /><span>调试模式</span></label><small style="color:var(--text-muted);margin-left:6px">输出更详细的运行日志</small>
      </div>
      <div class="block"><div class="block-hd"><h3>关闭行为</h3></div>
        <label class="fm-l"><input type="checkbox" v-model="closeToTray" style="width:auto;margin-right:6px" /><span>关闭时最小化到托盘</span></label>
      </div>
      <div class="block"><div class="block-hd"><h3>缓存管理</h3></div>
        <p style="color:var(--text-muted);font-size:13px;margin-bottom:10px">清除应用缓存（图片缓存等），需重启生效。</p>
        <div v-if="!clearCacheConfirm"><Button variant="secondary" size="sm" @click="clearCacheConfirm = true">清除缓存</Button></div>
        <div v-else class="act-row"><span>确认清除？</span><Button variant="danger" size="sm" @click="clearCache">确认</Button><Button variant="secondary" size="sm" @click="clearCacheConfirm = false">取消</Button></div>
      </div>
      <div class="block"><div class="block-hd"><h3>开发者模式</h3></div>
        <label class="fm-l"><input type="checkbox" :checked="devMode.enabled" @change="(e: any) => devMode.setEnabled(e.target.checked)" style="width:auto;margin-right:6px" /><span>打开开发者模式</span></label><small style="color:var(--text-muted);margin-left:6px">关闭后侧边栏"开发者模式"栏目将隐藏</small>
      </div>
    </div>
  </div>
</template>

<style scoped>
.da { max-width: 1300px; margin: 0 auto; padding-bottom: 40px; }
.da-hd { margin-bottom: 18px; }
.da-title { display: flex; align-items: center; gap: 10px; font-size: 20px; color: var(--text-primary); margin: 0; }

.da-nav { display: flex; gap: 2px; margin-bottom: 20px; border-bottom: 2px solid var(--border); padding-bottom: 0; }
.da-tab { display: flex; align-items: center; gap: 5px; padding: 8px 18px; border: none; background: transparent; color: var(--text-muted); font-size: 13px; font-weight: 600; cursor: pointer; border-bottom: 2px solid transparent; margin-bottom: -2px; transition: all 0.15s; font-family: inherit; }
.da-tab:hover { color: var(--text-primary); }
.da-tab.active { color: var(--accent); border-bottom-color: var(--accent); }
.da-body { animation: fadeIn 0.15s; }
@keyframes fadeIn { from { opacity: 0.6 } to { opacity: 1 } }

.stat-row { display: grid; grid-template-columns: repeat(auto-fill, minmax(140px, 1fr)); gap: 10px; margin-bottom: 18px; }
.stat { background: var(--bg-card); border: 1px solid var(--border); border-radius: 8px; padding: 16px; text-align: center; }
.stat:hover { border-color: var(--accent-alpha-35); }
.stat-n { font-size: 26px; font-weight: 700; color: var(--accent); line-height: 1.2; }
.stat-l { font-size: 11px; color: var(--text-muted); margin-top: 4px; }

.block { background: var(--bg-card); border: 1px solid var(--border); border-radius: 8px; padding: 20px; margin-bottom: 14px; }
.block-hd { display: flex; justify-content: space-between; align-items: center; margin-bottom: 14px; flex-wrap: wrap; gap: 10px; }
.block-hd h3 { margin: 0; font-size: 15px; color: var(--text-primary); }
.block-acts { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; }

.tb { width: 100%; border-collapse: collapse; font-size: 13px; }
.tb th { text-align: left; padding: 10px 10px; border-bottom: 2px solid var(--border); font-weight: 600; color: var(--text-muted); white-space: nowrap; font-size: 11px; text-transform: uppercase; letter-spacing: 0.3px; }
.tb td { padding: 8px 10px; border-bottom: 1px solid var(--border); }
.tb tbody tr:hover { background: var(--bg-hover); }
.tb-name { font-weight: 600; color: var(--text-primary); max-width: 220px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.tb-mono { font-family: 'Cascadia Code', 'Fira Code', Consolas, monospace; font-size: 11px; color: var(--text-muted); max-width: 160px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.tb-num { text-align: center; font-weight: 500; color: var(--accent); }
.tb-acts { display: flex; gap: 4px; flex-wrap: wrap; }
.tb-empty { text-align: center; padding: 30px; color: var(--text-muted); }

.badge { display: inline-block; padding: 2px 8px; border-radius: 10px; font-size: 11px; font-weight: 500; }
.badge-ok { background: var(--success-alpha-10); color: var(--success); }
.badge-off { background: var(--bg-hover); color: var(--text-muted); }
.badge-running { background: var(--accent-alpha-15); color: var(--accent); }
.badge-idle { background: var(--bg-hover); color: var(--text-muted); }

.form-v { display: flex; flex-direction: column; gap: 10px; }
.fm-l { display: flex; align-items: center; gap: 6px; font-size: 13px; font-weight: 500; color: var(--text-primary); }
.fm-ta { resize: vertical; font-family: 'Cascadia Code', 'Fira Code', Consolas, monospace; font-size: 12px; }
.inp { padding: 7px 10px; border: 1px solid var(--border); border-radius: 6px; background: var(--bg-input); color: var(--text-primary); font-size: 13px; font-family: inherit; outline: none; flex: 1; min-width: 120px; }
.inp:focus { border-color: var(--accent); }
.inp-sel { padding: 7px 10px; border: 1px solid var(--border); border-radius: 6px; background: var(--bg-input); color: var(--text-primary); font-size: 13px; font-family: inherit; }
.act-row { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; }
.video-tb { display: flex; gap: 6px; align-items: center; flex-wrap: wrap; }

.pgn { display: flex; justify-content: space-between; align-items: center; margin-top: 10px; padding-top: 8px; border-top: 1px solid var(--border); font-size: 13px; color: var(--text-muted); }
.pgn-btns { display: flex; align-items: center; gap: 6px; }
.pgn-num { font-weight: 600; color: var(--text-primary); }

.empty { text-align: center; padding: 40px 20px; color: var(--text-muted); font-size: 14px; }
.ld { text-align: center; padding: 30px; color: var(--text-muted); }

.vd { display: flex; flex-direction: column; gap: 6px; max-height: 60vh; overflow-y: auto; }
.vd-r { font-size: 13px; line-height: 1.6; display: flex; gap: 6px; align-items: flex-start; }
.vd-r strong { color: var(--text-muted); white-space: nowrap; min-width: 70px; }
.vd-pic { max-width: 180px; max-height: 120px; border-radius: 6px; border: 1px solid var(--border); }
.vd-json { background: var(--bg-secondary); border: 1px solid var(--border); border-radius: 6px; padding: 10px; font-size: 11px; font-family: 'Cascadia Code', 'Fira Code', Consolas, monospace; white-space: pre-wrap; word-break: break-all; max-height: 400px; overflow: auto; }

.log-v { background: var(--bg-secondary); border: 1px solid var(--border); border-radius: 6px; padding: 12px; max-height: 500px; overflow: auto; font-family: 'Cascadia Code', 'Fira Code', Consolas, monospace; font-size: 12px; line-height: 1.8; white-space: pre-wrap; word-break: break-all; }
.log-lns { display: flex; flex-direction: column; }
.log-l { padding: 1px 0; }
.log-l:hover { background: var(--bg-hover); }
.ll-err { color: var(--danger); font-weight: 500; }
.ll-warn { color: var(--warning); }
.ll-dbg { color: var(--text-muted); }
.ll-info { color: var(--info); }
.log-l mark { background: #fde047; color: #000; padding: 0 2px; border-radius: 2px; }
.log-se { color: var(--text-primary); margin: 0; padding: 4px 0; }
.log-se + .log-se { margin-top: 8px; padding-top: 8px; border-top: 1px dashed var(--border); }

code { background: var(--bg-hover); padding: 2px 6px; border-radius: 4px; font-size: 12px; }
</style>