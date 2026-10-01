<script setup lang="ts">
defineOptions({ name: 'Favorites' })
import { ref, onMounted, computed, watch, onActivated, onDeactivated } from 'vue'
import { storeToRefs } from 'pinia'
import { tr } from '../locales'
import { favRefreshTick } from '../stores/favoritesSync'
import { useRouter } from 'vue-router'
import { GetFavorites, RemoveFavorite, normalizeApiError } from '../api/app'
import { useLayoutStore } from '../stores/layout'
import VideoCard from '../components/VideoCard.vue'
import Icon from '../components/Icon.vue'
import { Button, Modal, Spinner as LoadingSpinner, Empty as EmptyState } from '../components/ui'
import { getDetailPath } from '../utils'
import { useConfirmStore } from '../stores/confirm'
import { useErrorStore } from '../stores/error'
import type { Video, Favorite } from '../types'
import { readStorage, writeStorage } from '../platform/storage'

const router = useRouter()

const confirmStore = useConfirmStore()
const errorStore = useErrorStore()
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

// 默认夹的名字不落盘：落的是译文就会把当前语言冻进 localStorage，切语言后名字不变。
const folders = ref<FavFolder[]>([{ id: 'default', name: '', default: true }])
const activeFolderId = ref<string>('default')
const mapping = ref<Record<string, string>>({}) // favKey -> folderId

function loadFoldersFromStorage(): void {
  try {
    const savedFolders = readStorage<FavFolder[] | null>(FOLDERS_KEY, null)
    if (savedFolders) folders.value = savedFolders
    // 确保至少有默认夹
    if (!folders.value.some(f => f.default)) {
      folders.value.unshift({ id: 'default', name: '', default: true })
    }
  } catch { /* ignore */ }
  mapping.value = readStorage<Record<string, string>>(MAPPING_KEY, {})
}

function persistFolders(): void {
  writeStorage(FOLDERS_KEY, folders.value)
}

function persistMapping(): void {
  writeStorage(MAPPING_KEY, mapping.value)
}

// 根据后端返回的 fav 推断目标 folderId；若不存在映射，归到默认夹
function resolveFolderId(f: { source_key: string; vod_id: string }): string {
  const key = `${f.source_key}-${f.vod_id}`
  const fid = mapping.value[key]
  return fid && folders.value.some(x => x.id === fid) ? fid : 'default'
}

// 默认夹的名字只在看的时候现取译文，切语言才会跟着变。
function folderLabel(folder: FavFolder): string {
  return folder.default ? tr('favorites.defaultFolder') : folder.name
}

function getFolderName(id: string): string {
  const folder = folders.value.find(f => f.id === id)
  return folder ? folderLabel(folder) : tr('favorites.defaultFolder')
}

const favorites = ref<FavItem[]>([])
const loading = ref(false)
const removingKey = ref<string>('')

// 分页状态：favPage 是已经取到的最后一页，lastFavPageCount 装满一页才算还有下一页。
const FAV_PAGE_SIZE = 24
const favPage = ref(1)
const lastFavPageCount = ref(0)
const loadingMore = ref(false)
const hasMoreFavorites = computed(() => lastFavPageCount.value >= FAV_PAGE_SIZE)

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

// 列数与密度走 layout store：这一页在 KeepAlive 下不会重挂，跟着同一份状态
// 才能在设置页改完立刻看见。
const layoutStore = useLayoutStore()
const { gridStyle } = storeToRefs(layoutStore)

let wasDeactivated = false

onMounted(async () => {
  loadFoldersFromStorage()
  await layoutStore.load()
  await loadFavorites()
})

// KeepAlive runs onActivated right after onMounted, so without this gate the
// favorites list is fetched twice on first visit.
onActivated(() => {
  if (!wasDeactivated) return
  wasDeactivated = false
  loadFavorites()
})

onDeactivated(() => { wasDeactivated = true })

watch(favRefreshTick, () => { loadFavorites() })

async function fetchFavoritePage(page: number): Promise<void> {
  const raw = await GetFavorites(page, FAV_PAGE_SIZE)
  const favs: Favorite[] = Array.isArray(raw) ? (raw as Favorite[]) : []
  lastFavPageCount.value = favs.length
  // 卡片字段直接来自 GetFavorites 的那一条 JOIN：以前这里对每条收藏串行 await
  // GetVideoDetail，24 条就是一串远程请求，整页 loading 要等最后一条回来。
  const result: FavItem[] = favs.map((f) => ({
    ...f,
    video: {
      vod_id: f.vod_id,
      global_id: f.global_id,
      vod_name: f.vod_name || '',
      vod_pic: f.vod_pic || '',
      type_name: f.type_name || '',
      vod_remarks: f.vod_remarks || '',
      vod_year: f.vod_year || '',
      vod_area: f.vod_area || '',
    },
    folderId: resolveFolderId(f),
  }))
  favorites.value = page === 1 ? result : favorites.value.concat(result)
  favPage.value = page
}

async function loadFavorites(): Promise<void> {
  loading.value = true
  try {
    await fetchFavoritePage(1)
  } catch (e) {
    const err = normalizeApiError(e)
    console.error('加载收藏失败:', e)
    errorStore.error(tr('favorites.loadFailed'), err.message, '', 'Favorites')
  } finally {
    loading.value = false
  }
}

// 收藏过去固定取前 100 条，超出部分既不显示也不提示。
// 现在一页 24 条，取满就承认还有下一页，把展开交给用户。
async function loadMoreFavorites(): Promise<void> {
  if (loading.value || loadingMore.value || !hasMoreFavorites.value) return
  loadingMore.value = true
  try {
    await fetchFavoritePage(favPage.value + 1)
  } catch (e) {
    const err = normalizeApiError(e)
    console.error('加载更多收藏失败:', e)
    errorStore.error(tr('favorites.loadFailed'), err.message, '', 'Favorites')
  } finally {
    loadingMore.value = false
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
    await RemoveFavorite({ source_key: fav.source_key, vod_id: fav.vod_id, global_id: 0 })
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
    const err = normalizeApiError(e)
    console.error('取消收藏失败:', e)
    errorStore.error(tr('favorites.removeFailed'), err.message, '', 'Favorites')
  } finally {
    removingKey.value = ''
  }
}

async function onRemoveSelected(): Promise<void> {
  if (selectedKeys.value.size === 0 || batchRemoving.value) return
  batchRemoving.value = true
  const failed: string[] = []
  try {
    const toRemove = favorites.value.filter((f) => selectedKeys.value.has(favKey(f)))
    for (const fav of toRemove) {
      try {
        await RemoveFavorite({ source_key: fav.source_key, vod_id: fav.vod_id, global_id: 0 })
        const idx = favorites.value.findIndex(
          (f) => f.source_key === fav.source_key && f.vod_id === fav.vod_id
        )
        if (idx >= 0) favorites.value.splice(idx, 1)
        const k = favKey(fav)
        if (mapping.value[k]) { delete mapping.value[k] }
      } catch (e) {
        console.error('取消收藏失败:', e)
        failed.push(fav.vod_name || fav.vod_id)
      }
    }
    if (failed.length > 0) {
      errorStore.error(
        tr('favorites.removeFailed'),
        tr('favorites.removeFailedDetail', { count: failed.length, names: failed.slice(0, 3).join('、') }),
        '',
        'Favorites',
      )
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

async function deleteFolder(folder: FavFolder): Promise<void> {
  if (folder.default) return
  const yes = await confirmStore.confirm({
    title: tr('favorites.confirmDeleteTitle'),
    message: tr('favorites.confirmDeleteMsg'),
    okText: tr('common.delete'),
    level: 'danger',
  })
  if (!yes) return
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
    <div class="page-header cczj-flex cczj-items-center cczj-justify-between cczj-gap-4 cczj-mb-6">
      <div>
        <h2 class="cczj-flex cczj-items-center cczj-gap-2 cczj-text-accent">
          <Icon name="star" :size="20" /> {{ tr('favorites.title') }}
        </h2>
        <p class="desc cczj-text-muted cczj-mt-1" v-if="manageMode && hasSelection">
          {{ tr('favorites.selected', { count: selectedKeys.size }) }}
        </p>
        <p class="desc cczj-text-muted cczj-mt-1" v-else-if="manageMode">
          {{ tr('favorites.manageHint') }}
        </p>
        <p class="desc cczj-text-muted cczj-mt-1" v-else-if="favorites.length > 0">{{ tr('favorites.folderSummary', { total: favorites.length, name: getFolderName(activeFolderId), count: displayedFavorites.length }) }}<template v-if="hasMoreFavorites"> · {{ tr('favorites.moreAvailable') }}</template>
          </p>
        <p class="desc cczj-text-muted cczj-mt-1" v-else>{{ tr('favorites.emptyHint') }}</p>
      </div>
      <div class="manage-actions cczj-flex cczj-items-center cczj-gap-2 cczj-flex-shrink-0">
        <template v-if="!manageMode">
          <Button variant="ghost" size="sm" @click="openCreateFolder">
            <Icon name="plus" :size="14" /> {{ tr('favorites.newFolder') }}
          </Button>
          <Button v-if="favorites.length > 0" variant="ghost" size="sm" @click="enterManageMode">
            <Icon name="check" :size="14" /> {{ tr('common.manage') }}
          </Button>
        </template>
        <template v-else>
          <Button variant="secondary" size="sm" @click="toggleSelectAll">
            {{ isAllSelected ? tr('common.cancelSelectAll') : tr('common.selectAll') }}
          </Button>
          <Button variant="secondary" size="sm" :disabled="!hasSelection" @click="openMoveSelected">
            <Icon name="move" :size="14" /> {{ tr('favorites.moveTo') }}
          </Button>
          <Button variant="danger" size="sm" :disabled="!hasSelection || batchRemoving" @click="onRemoveSelected">
            <Icon name="trash" :size="14" /> {{ tr('common.deleteSelected') }}
          </Button>
          <Button variant="primary" size="sm" @click="exitManageMode">
            {{ tr('favorites.done') }}
          </Button>
        </template>
      </div>
    </div>

    <!-- 收藏夹侧边栏 + 主区域 -->
    <div class="fav-layout cczj-flex cczj-gap-6">
      <aside class="fav-folders cczj-flex cczj-flex-col cczj-gap-2 cczj-w-64">
        <div v-for="folder in folders" :key="folder.id" class="folder-row cczj-flex cczj-items-center cczj-justify-between cczj-p-2 cczj-rounded cczj-transition cczj-cursor-pointer"
          :class="{ active: folder.id === activeFolderId }" @click="activeFolderId = folder.id">
          <div class="folder-name cczj-flex cczj-items-center cczj-gap-2 cczj-flex-1">
            <Icon :name="folder.default ? 'star' : 'list'" :size="14" />
            <span>{{ folderLabel(folder) }}</span>
            <small class="count cczj-text-muted cczj-text-xs">{{
              favorites.filter((f) => f.folderId === folder.id).length
            }}</small>
          </div>
          <div v-if="!folder.default" class="folder-actions cczj-flex cczj-gap-1" @click.stop>
            <Button variant="text" size="sm" icon @click="openRenameFolder(folder)" :title="tr('common.rename')">
              <Icon name="edit" :size="12" />
            </Button>
            <Button variant="text" size="sm" icon @click="deleteFolder(folder)" :title="tr('common.delete')" class="mini-btn-danger">
              <Icon name="trash" :size="12" />
            </Button>
          </div>
        </div>
      </aside>

      <main class="fav-main cczj-flex-1">
        <!-- 加载状态 -->
        <div v-if="loading">
          <LoadingSpinner :label="tr('favorites.loading')" />
        </div>

        <!-- 空状态 -->
        <div v-else-if="displayedFavorites.length === 0">
          <EmptyState icon="⭐" :title="tr('favorites.folderEmpty', { name: getFolderName(activeFolderId) })" :description="tr('favorites.folderEmptyDesc')">
            <Button variant="primary" @click="router.push('/')">{{ tr('favorites.discover') }}</Button>
          </EmptyState>
        </div>

        <!-- 视频网格 -->
        <div v-else class="fav-grid cczj-grid" :style="gridStyle">
          <div v-for="fav in displayedFavorites" :key="favKey(fav)" class="fav-card cczj-relative cczj-transition cczj-rounded"
            :class="{ 'is-selected': selectedKeys.has(favKey(fav)), 'is-manage': manageMode, 'is-removing': removingKey === favKey(fav) }">
            <!-- 卡片字段现在由 GetFavorites 一次给齐，占位卡留给"全局库里连名字和封面都没有"的残缺条目。 -->
            <VideoCard v-if="fav.video?.vod_name || fav.video?.vod_pic" :video="fav.video" @click="goDetail(fav)" />
            <div v-else class="placeholder-card cczj-flex cczj-items-center cczj-justify-center cczj-rounded cczj-border cczj-border-dashed cczj-bg-card cczj-cursor-pointer cczj-transition" @click="goDetail(fav)">
              <div class="placeholder-inner cczj-flex cczj-flex-col cczj-items-center cczj-gap-2 cczj-text-muted">
                <Icon name="film" :size="32" />
                <span>{{ tr('favorites.videoIdLabel', { id: fav.vod_id }) }}</span>
                <small class="source-tag cczj-text-xs">{{ fav.source_key }}</small>
              </div>
            </div>

            <label v-if="manageMode" class="fav-checkbox cczj-absolute cczj-top-2 cczj-right-2 cczj-z-10" @click.stop>
              <input type="checkbox" :checked="selectedKeys.has(favKey(fav))" :disabled="batchRemoving"
                @change="toggleSelect(fav)" />
              <span class="check-mark" />
            </label>
            <span v-if="removingKey === favKey(fav)" class="fav-loading cczj-absolute cczj-top-2 cczj-right-2 cczj-z-20">
              <LoadingSpinner size="sm" />
            </span>
          </div>
        </div>

        <div v-if="hasMoreFavorites && !loading && !manageMode" class="load-more cczj-flex cczj-justify-center cczj-pt-4">
          <Button variant="secondary" size="sm" :disabled="loadingMore" @click="loadMoreFavorites">
            <Icon name="chevron-down" :size="14" />
            <span>{{ loadingMore ? tr('favorites.loadingMore') : tr('favorites.loadMore') }}</span>
          </Button>
        </div>
      </main>
    </div>

    <!-- 新建/重命名收藏夹弹窗 -->
    <Modal v-model="showFolderModal" :title="folderEditTarget ? tr('favorites.renameFolder') : tr('favorites.newFolder')" :show-footer="true"
      :ok-text="folderEditTarget ? tr('common.save') : tr('favorites.create')" :ok-disabled="!folderEditName.trim()" @ok="saveFolder"
      @cancel="showFolderModal = false" width="420px">
      <input v-model="folderEditName" type="text" class="folder-input cczj-w-full cczj-p-3 cczj-rounded cczj-border cczj-bg-secondary" :placeholder="tr('favorites.folderNamePlaceholder')" @keyup.enter="saveFolder"
        autofocus />
    </Modal>

    <!-- 移动到收藏夹弹窗 -->
    <Modal v-model="showMoveModal" :title="tr('favorites.moveTitle', { count: movePendingKeys.length })" :show-footer="true" :ok-text="tr('favorites.move')"
      @ok="moveSelectedToFolder" @cancel="showMoveModal = false" width="420px">
      <div class="folder-select-list cczj-flex cczj-flex-col cczj-gap-2">
        <label v-for="folder in folders" :key="folder.id" class="folder-select-item cczj-flex cczj-items-center cczj-gap-2 cczj-p-2 cczj-rounded cczj-cursor-pointer cczj-transition"
          :class="{ active: moveTargetFolderId === folder.id }" @click="moveTargetFolderId = folder.id">
          <input type="radio" v-model="moveTargetFolderId" :value="folder.id" />
          <span class="folder-radio" />
          <Icon :name="folder.default ? 'star' : 'list'" :size="14" />
          <span class="cczj-flex-1">{{ folderLabel(folder) }}</span>
          <small class="cczj-text-muted cczj-text-xs">{{ tr('favorites.videoCount', { count: favorites.filter((f) => f.folderId === folder.id).length }) }}</small>
        </label>
      </div>
    </Modal>
  </div>
</template>

<style scoped src="../styles/views/favorites.css"></style>
