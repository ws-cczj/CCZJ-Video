<script setup lang="ts">
defineOptions({ name: 'Favorites' })
import { ref, onMounted, computed, watch, onActivated } from 'vue'
import { favRefreshTick } from '../stores/favoritesSync'
import { useRouter } from 'vue-router'
import { GetSetting, GetFavorites, GetVideoDetail, RemoveFavorite } from '../../bindings/cczjVideo/app'
import VideoCard from '../components/VideoCard.vue'
import Icon from '../components/Icon.vue'
import { Button, Modal, Spinner as LoadingSpinner, Empty as EmptyState } from '../components/ui'
import { getDetailPath } from '../utils'
import type { Video, Favorite } from '../types'

const router = useRouter()

type FavItem = Omit<Favorite, 'video'> & {
  video?: Video | null
  folderId: string
}

interface FavFolder {
  id: string
  name: string
  default: boolean
}

const FOLDERS_KEY = 'cczj_fav_folders'
const MAPPING_KEY = 'cczj_fav_mapping' // key(source-vod_id) -> folderId

const folders = ref<FavFolder[]>([{ id: 'default', name: '默认收藏夹', default: true }])
const activeFolderId = ref<string>('default')
const mapping = ref<Record<string, string>>({}) // favKey -> folderId

function loadFoldersFromStorage(): void {
  try {
    const raw = localStorage.getItem(FOLDERS_KEY)
    if (raw) folders.value = JSON.parse(raw) as FavFolder[]
    // 确保至少有默认夹
    if (!folders.value.some(f => f.default)) {
      folders.value.unshift({ id: 'default', name: '默认收藏夹', default: true })
    }
  } catch { /* ignore */ }
  try {
    const raw2 = localStorage.getItem(MAPPING_KEY)
    if (raw2) mapping.value = JSON.parse(raw2) as Record<string, string>
  } catch { /* ignore */ }
}

function persistFolders(): void {
  try { localStorage.setItem(FOLDERS_KEY, JSON.stringify(folders.value)) } catch { /* ignore */ }
}

function persistMapping(): void {
  try { localStorage.setItem(MAPPING_KEY, JSON.stringify(mapping.value)) } catch { /* ignore */ }
}

// 根据后端返回的 fav 推断目标 folderId；若不存在映射，归到默认夹
function resolveFolderId(f: { source_key: string; vod_id: string }): string {
  const key = `${f.source_key}-${f.vod_id}`
  const fid = mapping.value[key]
  return fid && folders.value.some(x => x.id === fid) ? fid : 'default'
}

function getFolderName(id: string): string {
  return folders.value.find(f => f.id === id)?.name || '默认收藏夹'
}

const favorites = ref<FavItem[]>([])
const loading = ref(false)
const removingKey = ref<string>('')

const manageMode = ref(false)
const selectedKeys = ref<Set<string>>(new Set<string>())
const batchRemoving = ref(false)

// 新建/重命名/删除 文件夹相关状态
const showFolderModal = ref(false)
const folderEditTarget = ref<FavFolder | null>(null) // null 表示新建
const folderEditName = ref('')
const showMoveModal = ref(false)
const movePendingKeys = ref<string[]>([])
const moveTargetFolderId = ref<string>('default')

function favKey(fav: { source_key: string; vod_id: string }): string {
  return `${fav.source_key}-${fav.vod_id}`
}

const displayedFavorites = computed(() =>
  favorites.value.filter(f => f.folderId === activeFolderId.value)
)

const isAllSelected = computed(() => {
  if (displayedFavorites.value.length === 0) return false
  return displayedFavorites.value.every((f) => selectedKeys.value.has(favKey(f)))
})

const hasSelection = computed(() => selectedKeys.value.size > 0)

const gridColumns = ref(5)
const gridStyle = computed(() => ({
  gridTemplateColumns: `repeat(${gridColumns.value}, minmax(0, 1fr))`,
}))

onMounted(async () => {
  loadFoldersFromStorage()
  try {
    const col = await GetSetting('grid_columns')
    if (col) gridColumns.value = parseInt(col as string, 10) || 5
  } catch {
    // 忽略
  }
  await loadFavorites()
})

onActivated(() => { loadFavorites() })

watch(favRefreshTick, () => { loadFavorites() })

async function loadFavorites(): Promise<void> {
  loading.value = true
  try {
    const raw = await GetFavorites(1, 100)
    const favs: Favorite[] = Array.isArray(raw) ? (raw as Favorite[]) : []
    const result: FavItem[] = []
    for (const f of favs) {
      try {
        const detail = (await GetVideoDetail({
          source_key: f.source_key,
          vod_id: String(f.vod_id),
        })) as { video: Video | null }
        const fav: FavItem = { ...f, video: detail?.video || null, folderId: resolveFolderId(f) }
        result.push(fav)
      } catch {
        const fav: FavItem = { ...f, video: null, folderId: resolveFolderId(f) }
        result.push(fav)
      }
    }
    favorites.value = result
  } catch (e) {
    console.error('加载收藏失败:', e)
  } finally {
    loading.value = false
  }
}

function goDetail(fav: FavItem): void {
  if (manageMode.value) {
    toggleSelect(fav)
    return
  }
  router.push(getDetailPath(fav.source_key, fav.video || { vod_id: fav.vod_id }))
}

function toggleSelect(fav: FavItem): void {
  const key = favKey(fav)
  const next = new Set(selectedKeys.value)
  if (next.has(key)) {
    next.delete(key)
  } else {
    next.add(key)
  }
  selectedKeys.value = next
}

function toggleSelectAll(): void {
  if (isAllSelected.value) {
    // 只清空当前夹的选择
    const curr = new Set(selectedKeys.value)
    for (const f of displayedFavorites.value) curr.delete(favKey(f))
    selectedKeys.value = curr
  } else {
    const merged = new Set(selectedKeys.value)
    for (const f of displayedFavorites.value) merged.add(favKey(f))
    selectedKeys.value = merged
  }
}

function enterManageMode(): void {
  selectedKeys.value = new Set()
  manageMode.value = true
}

function exitManageMode(): void {
  manageMode.value = false
  selectedKeys.value = new Set()
}

async function onRemove(fav: FavItem, evt: Event): Promise<void> {
  evt.stopPropagation()
  removingKey.value = favKey(fav)
  try {
    await RemoveFavorite({ source_key: fav.source_key, vod_id: fav.vod_id })
    const idx = favorites.value.findIndex(
      (f) => f.source_key === fav.source_key && f.vod_id === fav.vod_id
    )
    if (idx >= 0) favorites.value.splice(idx, 1)
    const k = favKey(fav)
    if (mapping.value[k]) {
      delete mapping.value[k]
      persistMapping()
    }
  } catch (e) {
    console.error('取消收藏失败:', e)
  } finally {
    removingKey.value = ''
  }
}

async function onRemoveSelected(): Promise<void> {
  if (selectedKeys.value.size === 0 || batchRemoving.value) return
  batchRemoving.value = true
  try {
    const toRemove = favorites.value.filter((f) => selectedKeys.value.has(favKey(f)))
    for (const fav of toRemove) {
      try {
        await RemoveFavorite({ source_key: fav.source_key, vod_id: fav.vod_id })
        const idx = favorites.value.findIndex(
          (f) => f.source_key === fav.source_key && f.vod_id === fav.vod_id
        )
        if (idx >= 0) favorites.value.splice(idx, 1)
        const k = favKey(fav)
        if (mapping.value[k]) { delete mapping.value[k] }
      } catch (e) {
        console.error('取消收藏失败:', e)
      }
    }
    persistMapping()
    selectedKeys.value = new Set()
    manageMode.value = false
  } finally {
    batchRemoving.value = false
  }
}

// ============== 文件夹管理 ==============
function openCreateFolder(): void {
  folderEditTarget.value = null
  folderEditName.value = ''
  showFolderModal.value = true
}

function openRenameFolder(folder: FavFolder): void {
  folderEditTarget.value = folder
  folderEditName.value = folder.name
  showFolderModal.value = true
}

function saveFolder(): void {
  const name = folderEditName.value.trim()
  if (!name) return
  if (folderEditTarget.value) {
    // 重命名
    const idx = folders.value.findIndex(f => f.id === folderEditTarget.value!.id)
    if (idx >= 0) {
      folders.value[idx].name = name
      persistFolders()
    }
  } else {
    // 新建
    const id = 'folder_' + Date.now()
    folders.value.push({ id, name, default: false })
    persistFolders()
  }
  showFolderModal.value = false
}

function deleteFolder(folder: FavFolder): void {
  if (folder.default) return
  if (!window.confirm(`确定要删除收藏夹「${folder.name}」吗？夹内的视频会移动到默认收藏夹。`)) return
  // 将该夹中所有映射改到 default
  for (const fav of favorites.value) {
    if (fav.folderId === folder.id) fav.folderId = 'default'
  }
  for (const k of Object.keys(mapping.value)) {
    if (mapping.value[k] === folder.id) mapping.value[k] = 'default'
  }
  folders.value = folders.value.filter(f => f.id !== folder.id)
  if (activeFolderId.value === folder.id) activeFolderId.value = 'default'
  persistFolders()
  persistMapping()
}

function moveSelectedToFolder(): void {
  const target = moveTargetFolderId.value || 'default'
  const keys = Array.from(selectedKeys.value)
  for (const fav of favorites.value) {
    if (selectedKeys.value.has(favKey(fav))) fav.folderId = target
  }
  for (const k of keys) mapping.value[k] = target
  persistMapping()
  showMoveModal.value = false
  exitManageMode()
}

function openMoveSelected(): void {
  if (selectedKeys.value.size === 0) return
  movePendingKeys.value = Array.from(selectedKeys.value)
  moveTargetFolderId.value = activeFolderId.value === 'default' ? 'default' : 'default'
  showMoveModal.value = true
}

// 当映射或收藏夹列表变化时，同步 favorites 上的 folderId 派生值
watch([mapping, folders], () => {
  for (const fav of favorites.value) {
    fav.folderId = resolveFolderId({ source_key: fav.source_key, vod_id: fav.vod_id })
  }
}, { deep: true })
</script>

<template>
  <div class="favorites-page">
    <div class="page-header">
      <div>
        <h2><Icon name="star" :size="20" /> 我的收藏</h2>
        <p class="desc" v-if="manageMode && hasSelection">
          已选择 {{ selectedKeys.size }} 项
        </p>
        <p class="desc" v-else-if="manageMode">
          请选择要删除或移动的收藏
        </p>
        <p class="desc" v-else-if="favorites.length > 0">共 {{ favorites.length }} 部精彩内容 · 当前夹「{{ getFolderName(activeFolderId) }}」{{ displayedFavorites.length }} 部</p>
        <p class="desc" v-else>还没有收藏，进入视频详情页点击「收藏」即可保存</p>
      </div>
      <div class="manage-actions">
        <template v-if="!manageMode">
          <Button variant="ghost" size="sm" @click="openCreateFolder">
            <Icon name="plus" :size="14" /> 新建收藏夹
          </Button>
          <Button v-if="favorites.length > 0" variant="ghost" size="sm" @click="enterManageMode">
            <Icon name="check" :size="14" /> 管理
          </Button>
        </template>
        <template v-else>
          <Button variant="secondary" size="sm" @click="toggleSelectAll">
            {{ isAllSelected ? '取消全选' : '全选' }}
          </Button>
          <Button variant="secondary" size="sm" :disabled="!hasSelection" @click="openMoveSelected">
            <Icon name="move" :size="14" /> 移动到
          </Button>
          <Button
            variant="danger"
            size="sm"
            :disabled="!hasSelection || batchRemoving"
            @click="onRemoveSelected"
          >
            <Icon name="trash" :size="14" /> 删除所选
          </Button>
          <Button variant="primary" size="sm" @click="exitManageMode">
            完成
          </Button>
        </template>
      </div>
    </div>

    <!-- 收藏夹侧边栏 + 主区域 -->
    <div class="fav-layout">
      <aside class="fav-folders">
        <div
          v-for="folder in folders"
          :key="folder.id"
          class="folder-row"
          :class="{ active: folder.id === activeFolderId }"
          @click="activeFolderId = folder.id"
        >
          <div class="folder-name">
            <Icon :name="folder.default ? 'star' : 'list'" :size="14" />
            <span>{{ folder.name }}</span>
            <small class="count">{{
              favorites.filter((f) => f.folderId === folder.id).length
            }}</small>
          </div>
          <div v-if="!folder.default" class="folder-actions" @click.stop>
            <Button variant="text" size="sm" icon @click="openRenameFolder(folder)" title="重命名">
              <Icon name="edit" :size="12" />
            </Button>
            <Button variant="text" size="sm" icon @click="deleteFolder(folder)" title="删除" class="mini-btn-danger">
              <Icon name="trash" :size="12" />
            </Button>
          </div>
        </div>
      </aside>

      <main class="fav-main">
        <!-- 加载状态 -->
        <div v-if="loading">
          <LoadingSpinner label="加载收藏中..." />
        </div>

        <!-- 空状态 -->
        <div v-else-if="displayedFavorites.length === 0">
          <EmptyState
            icon="⭐"
            :title="`「${getFolderName(activeFolderId)}」暂无内容`"
            description="切换到其他收藏夹或在详情页加入新收藏"
          >
            <Button variant="primary" @click="router.push('/')">去发现视频</Button>
          </EmptyState>
        </div>

        <!-- 视频网格 -->
        <div v-else class="fav-grid" :style="gridStyle">
          <div
            v-for="fav in displayedFavorites"
            :key="favKey(fav)"
            class="fav-card"
            :class="{ 'is-selected': selectedKeys.has(favKey(fav)), 'is-manage': manageMode, 'is-removing': removingKey === favKey(fav) }"
          >
            <VideoCard
              v-if="fav.video"
              :video="fav.video"
              @click="goDetail(fav)"
            />
            <div v-else class="placeholder-card" @click="goDetail(fav)">
              <div class="placeholder-inner">
                <Icon name="film" :size="32" />
                <span>视频 #{{ fav.vod_id }}</span>
                <small class="source-tag">{{ fav.source_key }}</small>
              </div>
            </div>

            <label
              v-if="manageMode"
              class="fav-checkbox"
              @click.stop
            >
              <input
                type="checkbox"
                :checked="selectedKeys.has(favKey(fav))"
                :disabled="batchRemoving"
                @change="toggleSelect(fav)"
              />
              <span class="check-mark" />
            </label>
            <span v-if="removingKey === favKey(fav)" class="fav-loading">
              <LoadingSpinner size="sm" />
            </span>
          </div>
        </div>
      </main>
    </div>

    <!-- 新建/重命名收藏夹弹窗 -->
    <Modal
      v-model="showFolderModal"
      :title="folderEditTarget ? '重命名收藏夹' : '新建收藏夹'"
      :show-footer="true"
      :ok-text="folderEditTarget ? '保存' : '创建'"
      :ok-disabled="!folderEditName.trim()"
      @ok="saveFolder"
      @cancel="showFolderModal = false"
      width="420px"
    >
      <input
        v-model="folderEditName"
        type="text"
        class="folder-input"
        placeholder="请输入收藏夹名称"
        @keyup.enter="saveFolder"
        autofocus
      />
    </Modal>

    <!-- 移动到收藏夹弹窗 -->
    <Modal
      v-model="showMoveModal"
      :title="`移动所选（${movePendingKeys.length}）到：`"
      :show-footer="true"
      ok-text="移动"
      @ok="moveSelectedToFolder"
      @cancel="showMoveModal = false"
      width="420px"
    >
      <div class="folder-select-list">
        <label
          v-for="folder in folders"
          :key="folder.id"
          class="folder-select-item"
          :class="{ active: moveTargetFolderId === folder.id }"
          @click="moveTargetFolderId = folder.id"
        >
          <input type="radio" v-model="moveTargetFolderId" :value="folder.id" />
          <span class="folder-radio" />
          <Icon :name="folder.default ? 'star' : 'list'" :size="14" />
          <span>{{ folder.name }}</span>
          <small>{{ favorites.filter((f) => f.folderId === folder.id).length }} 部</small>
        </label>
      </div>
    </Modal>
  </div>
</template>

<style scoped>
.favorites-page {
  max-width: 100%;
  color: var(--text-primary);
  animation: fadeInUp 0.4s ease;
}

@keyframes fadeInUp {
  from {
    opacity: 0;
    transform: translateY(10px);
  }
  to {
    opacity: 1;
    transform: translateY(0);
  }
}

.page-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 24px;
  gap: 16px;
}

.page-header h2 {
  display: inline-flex;
  align-items: center;
  gap: 10px;
  font-size: 22px;
  font-weight: 700;
  margin: 0 0 4px;
  color: var(--accent);
}

.page-header .desc {
  font-size: 13px;
  color: var(--text-muted);
  margin: 0;
}

.manage-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
}

.fav-grid {
  display: grid;
  gap: 18px;
}

.fav-card {
  position: relative;
  transition: transform 0.2s ease, box-shadow 0.2s ease;
  border-radius: 12px;
}

.fav-card:hover {
  transform: none;
}

.fav-card.is-manage:hover {
  transform: none;
}

.fav-card.is-selected {
  box-shadow: 0 0 0 2px var(--accent);
}

.fav-card.is-manage {
  cursor: pointer;
}

.placeholder-card {
  aspect-ratio: 2 / 3;
  background: var(--bg-card);
  border: 1px dashed var(--border);
  border-radius: 12px;
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  color: var(--text-muted);
  transition: all 0.2s ease;
}

.placeholder-card:hover {
  border-color: var(--accent);
  color: var(--accent);
}

.placeholder-inner {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 8px;
  font-size: 13px;
  opacity: 0.7;
}

.source-tag {
  font-size: 11px;
  padding: 2px 10px;
  border-radius: 10px;
  background: var(--bg-hover);
  color: var(--text-muted);
}

.remove-btn {
  position: absolute;
  top: 8px;
  right: 8px;
  width: 28px;
  height: 28px;
  border-radius: 50%;
  border: none;
  background: rgba(0, 0, 0, 0.6);
  color: #ffffff;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  opacity: 0;
  transform: scale(0.9);
  transition: all 0.2s ease;
  backdrop-filter: blur(4px);
  z-index: 3;
}

.fav-card:hover .remove-btn {
  opacity: 1;
  transform: scale(1);
}

.remove-btn:hover:not(:disabled) {
  background: var(--danger);
  transform: scale(1.1);
}

.remove-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.fav-checkbox {
  position: absolute;
  top: 8px;
  left: 8px;
  width: 24px;
  height: 24px;
  border-radius: 6px;
  z-index: 4;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
}

.fav-checkbox input {
  position: absolute;
  opacity: 0;
  width: 0;
  height: 0;
  pointer-events: none;
}

.check-mark {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 22px;
  height: 22px;
  border-radius: 6px;
  background: rgba(255, 255, 255, 0.9);
  border: 2px solid var(--accent);
  transition: all 0.15s ease;
  position: relative;
}

.check-mark::after {
  content: '';
  display: none;
  width: 6px;
  height: 11px;
  border: solid var(--accent);
  border-width: 0 2.5px 2.5px 0;
  transform: rotate(45deg) translate(-1px, -1px);
}

.fav-checkbox input:checked + .check-mark {
  background: var(--accent);
}

.fav-checkbox input:checked + .check-mark::after {
  display: block;
  border-color: #ffffff;
}

.fav-checkbox input:disabled + .check-mark {
  opacity: 0.5;
  cursor: not-allowed;
}

/* 两栏布局：左侧文件夹列表 + 右侧内容 */
.fav-layout {
  display: flex;
  gap: 20px;
  align-items: flex-start;
}

.fav-folders {
  flex-shrink: 0;
  width: 220px;
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding: 16px;
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 12px;
  position: sticky;
  top: 16px;
}

.folder-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 8px 12px;
  border-radius: 8px;
  cursor: pointer;
  transition: all 0.15s ease;
  background: var(--accent-alpha-10);
  border: 1px solid var(--accent);
  color: var(--accent);
  font-weight: 500;
}

.folder-row:hover {
  background: var(--bg-card);
  border-color: var(--border);
  color: var(--text-primary);
  font-weight: 500;
}

.folder-row.active {
  background: var(--accent);
  border-color: var(--accent);
  color: var(--accent-contrast);
  font-weight: 600;
}

.folder-name {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
  color: inherit;
  flex: 1;
  min-width: 0;
}

.folder-row.active .folder-name { color: inherit; }

.folder-name .count {
  margin-left: auto;
  font-size: 11px;
  color: var(--text-muted);
  font-weight: 500;
  padding: 2px 8px;
  border-radius: 10px;
  background: var(--bg-card);
}

.folder-row.active .folder-name .count {
  background: rgba(255, 255, 255, 0.22);
  color: var(--accent-contrast);
}

.folder-actions {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  flex-shrink: 0;
}

.folder-actions :deep(.ui-btn) {
  width: 24px !important;
  height: 24px !important;
  min-width: 24px !important;
  padding: 0 !important;
  border-radius: 6px;
  border: 1px solid var(--border);
  background: var(--bg-card);
  color: var(--text-secondary);
}
.folder-actions :deep(.ui-btn):hover {
  border-color: var(--accent);
  color: var(--accent);
}
.mini-btn-danger:hover {
  border-color: #ff5a5f !important;
  color: #ff5a5f !important;
}

.fav-main {
  flex: 1;
  min-width: 0;
}

/* 选择中的卡片 loading */
.fav-card.is-removing { opacity: 0.5; pointer-events: none; }
.fav-loading { position: absolute; top: 8px; right: 8px; z-index: 3; }

.folder-input {
  width: 100%;
  padding: 10px 14px;
  border-radius: 8px;
  border: 1px solid var(--border);
  background: var(--bg-secondary);
  color: var(--text-primary);
  font-size: 13px;
  font-family: inherit;
  outline: none;
  transition: border-color 0.15s ease;
}
.folder-input:focus { border-color: var(--accent); }

/* 文件夹选择列表 */
.folder-select-list { display: flex; flex-direction: column; gap: 8px; }
.folder-select-item {
  display: flex; align-items: center; gap: 10px;
  padding: 10px 14px; border-radius: 8px;
  border: 1px solid var(--border);
  cursor: pointer;
  font-size: 13px; color: var(--text-primary);
  transition: all 0.15s ease;
  position: relative;
}
.folder-select-item small {
  margin-left: auto;
  font-size: 11px;
  color: var(--text-muted);
  font-weight: 500;
  padding: 2px 8px;
  border-radius: 10px;
  background: var(--bg-secondary);
}
.folder-select-item:hover {
  border-color: var(--accent);
  background: var(--accent-alpha-10);
}
.folder-select-item.active {
  border-color: var(--accent);
  background: var(--accent);
  color: var(--accent-contrast);
  font-weight: 600;
}
.folder-select-item.active small {
  color: var(--accent-contrast);
  background: rgba(255, 255, 255, 0.22);
}
.folder-select-item input {
  position: absolute;
  opacity: 0;
  width: 0;
  height: 0;
  pointer-events: none;
}
.folder-radio {
  flex-shrink: 0;
  width: 16px;
  height: 16px;
  border-radius: 50%;
  border: 2px solid var(--border-strong);
  background: var(--bg-card);
  transition: all 0.15s ease;
  position: relative;
}
.folder-select-item.active .folder-radio {
  border-color: var(--accent-contrast);
  background: var(--accent-contrast);
}
.folder-select-item.active .folder-radio::after {
  content: '';
  position: absolute;
  inset: 3px;
  border-radius: 50%;
  background: var(--accent);
}

@media (max-width: 720px) {
  .fav-layout { flex-direction: column; }
  .fav-folders { width: 100%; position: static; }
}
</style>
