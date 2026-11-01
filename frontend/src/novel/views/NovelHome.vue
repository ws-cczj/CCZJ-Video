<script setup lang="ts">
import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue'
import { useViewActivity } from '../../composables/useViewActivity'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { novelBrowseChannels, novelBrowsePage, type BrowseChannel, type BrowsePage } from '../../api/novelBrowse'
import { normalizeApiError } from '../../api/library'
import NovelBookCard from '../components/NovelBookCard.vue'
import { Button, Spinner } from '../../components/ui'
import { useNovelReading } from '../readingState'
import Icon from '../../components/Icon.vue'
import { lastBookcityChannel } from '../browseLocation'
import { takeNovelHomePreparation } from '../startup'

defineOptions({ name: 'NovelHome' })
const route = useRoute(), router = useRouter(), { t } = useI18n()
const reading = useNovelReading()
const channels = ref<BrowseChannel[]>([]), startupError = ref(''), query = ref('')
interface Shelf { page?: BrowsePage; loading: boolean; error: string; revision: number }
const shelves = reactive<Record<string, Shelf>>({})
const pending = new Map<string, ReturnType<typeof novelBrowsePage>>()
const prepared = takeNovelHomePreparation()
let disposed = false
const active = useViewActivity(() => true, 'committed')
const selected = computed(() => channels.value.find(channel => channel.id === route.query.shelf))
watch(() => selected.value?.id, value => { lastBookcityChannel.value = value ?? '' }, { immediate: true })
const visible = computed(() => selected.value ? [selected.value] : channels.value.filter(channel => ['popular', 'publication', 'history'].includes(channel.id)))
const last = computed(() => reading.lastRead)
const resumePath = computed(() => last.value ? reading.readingPath(last.value.key) : null)
const initializing = ref(false)
let channelRequest: ReturnType<typeof novelBrowseChannels> | null = null

function rows(channel: BrowseChannel) {
  const items = shelves[channel.id]?.page?.items ?? []
  return selected.value ? items : items.slice(0, 3)
}
async function load(id: string, more = false, refresh = false): Promise<void> {
  if (!active.value || disposed) return
  if (!shelves[id]) shelves[id] = { loading: false, error: '', revision: 0 }
  const state = shelves[id]!
  if (state.loading || more && !state.page?.nextCursor) return
  state.loading = true; state.error = ''
  const revision = ++state.revision
  const request = (!more && !refresh ? prepared?.pages.get(id) : undefined) ?? novelBrowsePage(id, more ? state.page?.nextCursor : '', refresh)
  prepared?.pages.delete(id)
  pending.set(id, request)
  try {
    const page = await request
    if (disposed || revision !== state.revision) return
    if (more && state.page) {
      const known = new Set(state.page.items.map(book => `${book.sourceKey}:${book.remoteId}`))
      state.page = { ...page, items: [...state.page.items, ...page.items.filter(book => !known.has(`${book.sourceKey}:${book.remoteId}`))] }
    } else state.page = page
    if (!more && !refresh && page.stale) {
      state.loading = false
      void load(id, false, true)
    }
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
  if (!active.value || disposed) return
  const wanted = new Set(visible.value.map(channel => channel.id))
  for (const [id, request] of pending) if (!wanted.has(id)) {
    shelves[id]!.revision++; shelves[id]!.loading = false; pending.delete(id); request.cancel()
  }
  for (const channel of visible.value) if (!shelves[channel.id]?.page) void load(channel.id)
}
async function initialize(): Promise<void> {
  if (initializing.value || disposed) return
  initializing.value = true; startupError.value = ''
  const request = prepared?.channels ?? novelBrowseChannels(); channelRequest = request
  try { const result = await request; if (!disposed) { channels.value = result; ensureVisible() } }
  catch (error) { if (!disposed && normalizeApiError(error).code !== 'CANCELLED') startupError.value = normalizeApiError(error).message }
  finally { if (channelRequest === request) { initializing.value = false; channelRequest = null } }
}
void initialize()
watch(() => route.query.shelf, ensureVisible)
watch(active, show => {
  if (show) ensureVisible()
  else cancel()
}, {flush:'sync'})
function choose(id = ''): void {
  void router.replace({ path: '/novel', query: id ? { shelf: id } : {} })
  document.querySelector('.cczj-novel-shell .main-content')?.scrollTo({ top: 0 })
}
function search(): void {
  void router.push({ path: '/novel/search', query: query.value.trim() ? { q: query.value.trim() } : {} })
}
function cancel(): void {
  for (const request of prepared?.pages.values() ?? []) request.cancel()
  prepared?.pages.clear()
  for (const [id, request] of pending) {
    shelves[id]!.revision++; shelves[id]!.loading = false; request.cancel()
  }
  pending.clear()
}
onBeforeUnmount(() => { disposed = true; cancel(); channelRequest?.cancel() })
</script>

<template>
  <div class="novel-page novel-home library-room-page">
    <header class="room-welcome"><div><h1>{{ t('novelRoom.bookcity') }}</h1><p>{{ t('novelUpgrade.discoverHint') }}</p></div><router-link class="novel-small-link" to="/novel/shelf"><Icon name="bookmark" :size="18" />{{ t('novel.myShelf') }}</router-link></header>
    <form class="room-search" role="search" @submit.prevent="search"><Icon name="search" :size="20" /><input v-model="query" type="search" enterkeyhint="search" :placeholder="t('novelRoom.search')" :aria-label="t('novelRoom.search')" /><Button type="submit" variant="text">{{ t('readingUI.searchAction') }}</Button></form>
    <router-link v-if="last && resumePath" class="room-resume" :to="resumePath"><Icon name="book-open" :size="22" /><div><small>{{ t('novel.continueReading') }}</small><strong>{{ last.progress.title }}</strong><span>{{ t('readingUI.chapterPosition', { chapter: last.progress.chapterOrd + 1, percent: Math.round(last.progress.ratio * 100) }) }}</span></div><Icon name="chevron-right" :size="20" /></router-link>
    <nav class="room-wayfinding" :aria-label="t('novelRoom.categories')"><Button :variant="!selected ? 'secondary' : 'text'" :aria-pressed="!selected" @click="choose()">{{ t('novelRoom.all') }}</Button><Button v-for="channel in channels" :key="channel.id" :variant="selected?.id === channel.id ? 'secondary' : 'text'" :aria-pressed="selected?.id === channel.id" @click="choose(channel.id)">{{ channel.title }}</Button></nav>
    <Spinner v-if="initializing" :label="t('common.loading')" />
    <p v-if="startupError" class="room-notice" role="alert">{{ startupError }} <Button variant="text" @click="initialize">{{ t('common.retry') }}</Button></p>
    <div class="room-shelves">
    <section v-for="channel in visible" :key="channel.id" class="room-shelf" :data-channel="channel.id" :aria-busy="shelves[channel.id]?.loading">
      <header class="room-shelf-heading"><div><h2>{{ channel.title }}</h2></div><Button v-if="!selected" variant="text" @click="choose(channel.id)">{{ t('novelRoom.more') }} <Icon name="chevron-right" :size="16" /></Button><Button v-else variant="text" :loading="shelves[channel.id]?.loading" @click="load(channel.id, false, true)"><Icon name="refresh" :size="16" />{{ t('novelRoom.refresh') }}</Button></header>
      <p v-if="shelves[channel.id]?.error" class="room-notice" role="alert">{{ shelves[channel.id]?.error }} <Button variant="text" @click="load(channel.id, false, true)">{{ t('common.retry') }}</Button><router-link to="/novel/sources">{{ t('novel.manageSources') }}</router-link></p>
      <Spinner v-if="!shelves[channel.id]?.page && shelves[channel.id]?.loading" :label="t('common.loading')" />
      <div v-else class="novel-row-grid"><NovelBookCard v-for="book in rows(channel)" :key="`${book.sourceKey}:${book.remoteId}`" :book="book" compact /></div>
      <p v-if="shelves[channel.id]?.page && !rows(channel).length" class="room-empty">{{ t('novelRoom.empty') }}</p>
      <footer v-if="shelves[channel.id]?.page" class="room-shelf-footer"><span>{{ shelves[channel.id]!.page!.sourceName }} · {{ t(shelves[channel.id]!.page!.fromCache ? 'novelRoom.cached' : 'novelRoom.updated') }} {{ new Date(shelves[channel.id]!.page!.fetchedAt * 1000).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }) }}</span><span v-if="shelves[channel.id]!.page!.warning" role="status">{{ shelves[channel.id]!.page!.warning }}</span></footer>
      <div v-if="selected && shelves[channel.id]?.page?.nextCursor" class="novel-load-more"><Button :loading="shelves[channel.id]?.loading" @click="load(channel.id, true)">{{ t('novel.loadMore') }}<Icon name="chevron-down" :size="16" /></Button></div>
    </section>
    </div>
  </div>
</template>
