<script setup lang="ts">
import { computed, nextTick, onActivated, onBeforeUnmount, onDeactivated, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { novelBrowseChannels, novelBrowsePage, type BrowseChannel, type BrowsePage } from '../../api/novelBrowse'
import { normalizeApiError } from '../../api/library'
import NovelCover from '../components/NovelCover.vue'
import NovelDiscoveryDeck from '../components/NovelDiscoveryDeck.vue'
import Icon from '../../components/Icon.vue'
import { lastBookcityChannel } from '../browseLocation'

defineOptions({ name: 'NovelHome' })
const route = useRoute(), router = useRouter(), { t } = useI18n()
const channels = ref<BrowseChannel[]>([]), startupError = ref(''), query = ref('')
interface Shelf { page?: BrowsePage; loading: boolean; error: string; revision: number }
const shelves = reactive<Record<string, Shelf>>({})
const pending = new Map<string, ReturnType<typeof novelBrowsePage>>()
let disposed = false, active = true
const selected = computed(() => channels.value.find(channel => channel.id === route.query.shelf))
watch(() => selected.value?.id, value => { lastBookcityChannel.value = value ?? '' }, { immediate: true })
const visible = computed(() => selected.value ? [selected.value] : channels.value.filter(channel => ['popular', 'publication', 'history'].includes(channel.id)))
const deskBooks = computed(() => shelves.publication?.page?.items.slice(0, 5) ?? [])
const wayfinding = ref<HTMLElement>(), marker = ref({ left: 0, width: 0 })
let navObserver: ResizeObserver | undefined
const channelRequest = novelBrowseChannels()

function measureMarker(): void {
  const button = wayfinding.value?.querySelector<HTMLElement>('[aria-pressed="true"]')
  if (button) marker.value = { left: button.offsetLeft, width: button.offsetWidth }
}
function observeNavigation(): void {
  navObserver?.disconnect()
  if (!wayfinding.value || !active) return
  navObserver ??= new ResizeObserver(measureMarker)
  navObserver.observe(wayfinding.value)
  measureMarker()
}
onMounted(observeNavigation)
watch([channels, () => route.query.shelf], () => { void nextTick(measureMarker) })

function rows(channel: BrowseChannel) {
  const items = shelves[channel.id]?.page?.items ?? []
  const start = channel.id === 'publication' ? deskBooks.value.length : 0
  return selected.value ? items : items.slice(start, start + 6)
}
async function load(id: string, more = false, refresh = false): Promise<void> {
  if (!shelves[id]) shelves[id] = { loading: false, error: '', revision: 0 }
  const state = shelves[id]!
  if (state.loading || more && !state.page?.nextCursor) return
  state.loading = true; state.error = ''
  const revision = ++state.revision
  const request = novelBrowsePage(id, more ? state.page?.nextCursor : '', refresh)
  pending.set(id, request)
  try {
    const page = await request
    if (disposed || revision !== state.revision) return
    if (more && state.page) {
      const known = new Set(state.page.items.map(book => `${book.sourceKey}:${book.remoteId}`))
      state.page = { ...page, items: [...state.page.items, ...page.items.filter(book => !known.has(`${book.sourceKey}:${book.remoteId}`))] }
    } else state.page = page
  } catch (error) {
    if (!disposed && revision === state.revision) {
      const failure = normalizeApiError(error)
      if (failure.code !== 'CANCELLED') state.error = failure.message
    }
  } finally {
    if (revision === state.revision) { state.loading = false; pending.delete(id) }
  }
}
function ensureVisible(): void {
  if (!active || disposed) return
  const wanted = new Set(visible.value.map(channel => channel.id))
  for (const [id, request] of pending) if (!wanted.has(id)) {
    shelves[id]!.revision++; shelves[id]!.loading = false; pending.delete(id); request.cancel()
  }
  for (const channel of visible.value) if (!shelves[channel.id]?.page) void load(channel.id)
}
async function initialize(): Promise<void> {
  try { channels.value = await channelRequest; if (!disposed) ensureVisible() }
  catch (error) { if (!disposed) startupError.value = normalizeApiError(error).message }
}
void initialize()
watch(() => route.query.shelf, ensureVisible)
onActivated(() => { active = true; ensureVisible(); observeNavigation() })
function choose(id = ''): void {
  void router.replace({ path: '/novel', query: id ? { shelf: id } : {} })
  document.querySelector('.cczj-novel-shell .main-content')?.scrollTo({ top: 0 })
}
function search(): void {
  void router.push({ path: '/novel/search', query: query.value.trim() ? { q: query.value.trim() } : {} })
}
function cancel(): void {
  for (const [id, request] of pending) {
    shelves[id]!.revision++; shelves[id]!.loading = false; request.cancel()
  }
  pending.clear()
}
onDeactivated(() => { active = false; cancel(); navObserver?.disconnect() })
onBeforeUnmount(() => { disposed = true; cancel(); channelRequest.cancel(); navObserver?.disconnect() })
</script>

<template>
  <div class="novel-page novel-home library-room-page">
    <header class="room-welcome">
      <h1>{{ t('novelRoom.room') }}</h1>
      <form class="room-search" role="search" @submit.prevent="search"><Icon name="search" :size="20" /><input v-model="query" type="search" :placeholder="t('novelRoom.search')" :aria-label="t('novelRoom.search')" /><button :aria-label="t('readingUI.searchAction')"><Icon name="chevron-right" :size="20" /></button></form>
    </header>
    <nav ref="wayfinding" class="room-wayfinding" :aria-label="t('novelRoom.categories')"><span class="room-shelf-marker" aria-hidden="true" :style="{ width: `${marker.width}px`, transform: `translateX(${marker.left}px)` }"></span><button :aria-pressed="!selected" @click="choose()">{{ t('novelRoom.all') }}</button><button v-for="channel in channels" :key="channel.id" :aria-pressed="selected?.id === channel.id" @click="choose(channel.id)">{{ channel.title }}</button></nav>
    <p v-if="startupError" class="room-notice" role="alert">{{ startupError }} <button @click="router.go(0)">{{ t('common.retry') }}</button></p>
    <div class="room-browse-layout" :class="{ 'room-browse-overview': !selected }">
    <NovelDiscoveryDeck v-if="!selected" :books="deskBooks" />
    <div class="room-shelves">
    <section v-for="(channel, index) in visible" :key="`${selected?.id || 'overview'}:${channel.id}`" class="room-shelf" :data-channel="channel.id" :aria-busy="shelves[channel.id]?.loading">
      <header class="room-shelf-heading"><div><span class="room-shelf-number">{{ String(index + 1).padStart(2, '0') }}</span><h2>{{ channel.title }}</h2></div><button v-if="!selected" class="room-shelf-action" @click="choose(channel.id)">{{ t('novelRoom.more') }} <Icon name="chevron-right" :size="16" /></button><button v-else class="room-shelf-action" :disabled="shelves[channel.id]?.loading" @click="load(channel.id, false, true)"><Icon name="refresh" :size="16" />{{ t('novelRoom.refresh') }}</button></header>
      <p v-if="shelves[channel.id]?.error" class="room-notice" role="alert">{{ shelves[channel.id]?.error }} <button @click="load(channel.id, false, true)">{{ t('common.retry') }}</button><router-link to="/novel/sources">{{ t('novel.manageSources') }}</router-link></p>
      <div v-if="!shelves[channel.id]?.page && shelves[channel.id]?.loading" class="room-book-grid room-skeleton" aria-hidden="true"><div v-for="n in 6" :key="n"><span></span><i></i><i></i></div></div>
      <div v-else class="room-book-grid"><router-link v-for="(book, bookIndex) in rows(channel)" :key="`${book.sourceKey}:${book.remoteId}`" :to="`/novel/book/${book.id}`" class="room-book" :style="{ '--book-order': bookIndex % 6 }"><div class="room-book-cover"><div class="room-book-volume"><NovelCover :book="book" /></div></div><h3>{{ book.title }}</h3><p>{{ book.author || book.sourceName }}</p><small>{{ [book.status, book.words].filter(Boolean).join(' · ') || book.genres }}</small></router-link></div>
      <p v-if="shelves[channel.id]?.page && !rows(channel).length" class="room-empty">{{ t('novelRoom.empty') }}</p>
      <footer v-if="shelves[channel.id]?.page" class="room-shelf-footer"><span>{{ shelves[channel.id]!.page!.sourceName }} · {{ t(shelves[channel.id]!.page!.fromCache ? 'novelRoom.cached' : 'novelRoom.updated') }} {{ new Date(shelves[channel.id]!.page!.fetchedAt * 1000).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }) }}</span><span v-if="shelves[channel.id]!.page!.warning" role="status">{{ shelves[channel.id]!.page!.warning }}</span></footer>
      <button v-if="selected && shelves[channel.id]?.page?.nextCursor" class="room-load-more" :disabled="shelves[channel.id]?.loading" @click="load(channel.id, true)">{{ t(shelves[channel.id]?.loading ? 'common.loading' : 'novelRoom.nextShelf') }}<Icon name="chevron-down" :size="16" /></button>
    </section>
    </div>
    </div>
  </div>
</template>
