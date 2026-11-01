<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, reactive, ref, watch, type ComponentPublicInstance } from 'vue'
import { useViewActivity } from '../../composables/useViewActivity'
import { useGeneration } from '../../composables/useGeneration'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { comicBrowseChannels, comicBrowsePage, type ComicBrowseShelf, type ComicBrowseChannel } from '../../api/comicBrowse'
import { normalizeApiError } from '../../api/errors'
import { useComicState } from '../state'
import ComicCard from '../components/ComicCard.vue'
import ComicTrending from '../components/ComicTrending.vue'
import ComicSpotlight from '../components/ComicSpotlight.vue'
import Icon from '../../components/Icon.vue'

const route = useRoute(), router = useRouter(), { t } = useI18n(), state = useComicState()
const query = ref(''), selected = ref(''), channels = ref<ComicBrowseChannel[]>([]), startupError = ref('')
const shelves = reactive<Record<string, { page?: ComicBrowseShelf; loading: boolean; error: string; revision: number }>>({})
const pending = new Map<string, ReturnType<typeof comicBrowsePage>>()
let channelRequest: ReturnType<typeof comicBrowseChannels> | null = null
const channelGeneration = useGeneration()
const requestedChannels = ref(new Set<string>())
const sections = new Map<string, Element>()
let observer: IntersectionObserver | null = null
let disposed = false
const active = useViewActivity(() => true, 'committed')
const visible = computed(() => channels.value.filter(channel => !selected.value || channel.id === selected.value))
const spotlights = computed(() => {
  const picks = channels.value.filter(channel=>!['honkai','terra'].includes(channel.id)).flatMap(channel=>shelves[channel.id]?.page?.items.slice(0,3)??[])
  return picks.filter((item): item is NonNullable<typeof item> => !!item)
})
const recent = computed(() => state.recent[0])
const spotlightVisible = computed(() => !selected.value && !startupError.value && (spotlights.value.length || !channels.value.length || channels.value.some(channel => shelves[channel.id]?.loading)))
function observeSection(el: Element | ComponentPublicInstance | null, id: string): void {
  const old = sections.get(id)
  if (old === el) return
  if (old) observer?.unobserve(old)
  if (!(el instanceof Element)) { sections.delete(id); return }
  sections.set(id, el)
  if (typeof IntersectionObserver === 'undefined') { requestedChannels.value.add(id); return }
  if (!observer) observer = new IntersectionObserver(entries => {
    if (!active.value || disposed) return
    for (const entry of entries) if (entry.isIntersecting) {
      const channel = entry.target.getAttribute('data-channel')
      if (channel) requestedChannels.value.add(channel)
      observer?.unobserve(entry.target)
    }
  }, { root: el.closest('.main-content'), rootMargin: '320px', threshold: 0 })
  if (active.value) observer.observe(el)
}
async function loadChannels(): Promise<void> {
  if (!active.value || disposed || channelRequest) return
  startupError.value = ''
  const token = channelGeneration.begin(), request = comicBrowseChannels()
  channelRequest = request
  try {
    const value = await request
    if (channelGeneration.isCurrent(token) && !disposed) { channels.value = value; ensure() }
  } catch (error) {
    if (channelGeneration.isCurrent(token) && !disposed) {
      const failure = normalizeApiError(error)
      if (failure.code !== 'CANCELLED') startupError.value = failure.message
    }
  } finally { if (channelRequest === request) channelRequest = null }
}
async function load(id: string, refresh = false): Promise<void> {
  if (!active.value || disposed) return
  if (!shelves[id]) shelves[id] = { loading: false, error: '', revision: 0 }
  const shelf = shelves[id]!
  if (shelf.loading) return
  shelf.loading = true; shelf.error = ''
  const generation = ++shelf.revision, request = comicBrowsePage(id, refresh)
  pending.set(id, request)
  try {
    const page = await request
    if (!disposed && generation === shelf.revision) shelf.page = page
  } catch (error) {
    if (!disposed && generation === shelf.revision) {
      const failure = normalizeApiError(error)
      if (failure.code !== 'CANCELLED') shelf.error = failure.message
    }
  } finally {
    if (generation === shelf.revision) { shelf.loading = false; pending.delete(id) }
  }
}
function ensure(): void {
  if (!active.value || disposed) return
  // Prime the first catalog for the spotlight; other catalogs wait until their
  // own section approaches the viewport instead of competing at startup.
  const wanted = new Set(visible.value.filter((channel, index) => selected.value || index === 0 || requestedChannels.value.has(channel.id)).map(channel => channel.id))
  for (const [id, request] of pending) if (!wanted.has(id)) { shelves[id]!.revision++; shelves[id]!.loading = false; pending.delete(id); request.cancel() }
  for (const id of wanted) if (!shelves[id]?.page||Date.now()/1000-shelves[id]!.page!.fetchedAt>30*60) void load(id)
}
function choose(id: string): void { selected.value = id }
function search(): void { void router.push({ path: '/comic/search', query: query.value.trim() ? { q: query.value.trim() } : {} }) }
watch(() => route.query.channel, value => { selected.value = typeof value === 'string' ? value : '' }, { immediate: true })
watch(active, show => {
  if (show) {
    for (const [id, element] of sections) if (!requestedChannels.value.has(id)) observer?.observe(element)
    if (!channels.value.length && !startupError.value) void loadChannels()
    void nextTick(ensure)
  } else { cancel(); observer?.disconnect() }
}, {flush:'sync', immediate:true})
watch([visible, () => [...requestedChannels.value]], ensure)
function cancel(): void {
  channelGeneration.invalidate(); void channelRequest?.cancel(); channelRequest = null
  for (const [id, request] of pending) { shelves[id]!.revision++; shelves[id]!.loading = false; request.cancel() }
  pending.clear()
}
onBeforeUnmount(() => { disposed = true; cancel(); observer?.disconnect(); sections.clear() })
</script>

<template>
  <div class="comic-page comic-home manga-home">
    <header class="manga-heading"><h1>{{ t('comicRoom.browse') }}</h1><form class="manga-search" role="search" @submit.prevent="search"><Icon name="search" :size="19" /><input v-model="query" type="search" :aria-label="t('comicRoom.search')" :placeholder="t('comicRoom.search')" /><button :aria-label="t('readingUI.searchAction')"><Icon name="chevron-right" :size="20" /></button></form></header>
    <nav class="manga-channels" :aria-label="t('comicRoom.channels')"><button :aria-pressed="!selected" @click="choose('')">{{ t('comicRoom.all') }}</button><button v-for="channel in channels" :key="channel.id" :aria-pressed="selected === channel.id" @click="choose(channel.id)">{{ channel.title }}<small v-if="shelves[channel.id]?.page">{{ shelves[channel.id]!.page!.items.length }}</small></button></nav>
    <p v-if="startupError" class="comic-notice" role="alert">{{ startupError }}<button @click="loadChannels">{{ t('common.retry') }}</button></p>
    <router-link v-if="recent" :to="state.resumePath(recent)" class="manga-continue"><span class="manga-continue-mark" aria-hidden="true"><Icon name="bookmark" :size="18" /></span><div><small>{{ t('comic.continueReading') }}</small><strong>{{ recent.title }}</strong></div><span>{{ t('readingUI.pagePosition', { episode: recent.episodeOrd + 1, page: recent.page + 1 }) }}</span><Icon name="chevron-right" :size="18" /></router-link>
    <ComicSpotlight v-if="spotlightVisible" :series="spotlights" />
    <ComicTrending v-if="!selected"/>
    <section v-for="(channel, index) in visible" :ref="el => observeSection(el, channel.id)" :key="channel.id" class="manga-section" :data-channel="channel.id" :aria-busy="shelves[channel.id]?.loading">
      <header class="manga-section-heading"><div><span class="manga-section-index" aria-hidden="true">{{ String(index + 1).padStart(2, '0') }}</span><h2>{{ channel.title }}</h2><small>{{ channel.description }}</small></div><button v-if="!selected" @click="choose(channel.id)">{{ t('comicRoom.viewAll') }}<Icon name="chevron-right" :size="17" /></button><button v-else :disabled="shelves[channel.id]?.loading" @click="load(channel.id, true)"><Icon name="refresh" :size="16" />{{ t('common.refresh') }}</button></header>
      <div v-if="shelves[channel.id]?.error" class="comic-notice" role="alert">{{ shelves[channel.id]!.error }}<button class="comic-text-button" @click="load(channel.id, true)">{{ t('common.retry') }}</button><router-link to="/comic/sources">{{ t('readingUI.sources') }}</router-link></div>
      <div v-if="!shelves[channel.id]?.page && !shelves[channel.id]?.error" class="manga-grid manga-skeleton" aria-hidden="true"><span v-for="n in 6" :key="n"></span></div>
      <div v-else class="manga-grid"><ComicCard v-for="(item, order) in (selected ? shelves[channel.id]?.page?.items : shelves[channel.id]?.page?.items.slice(0, 6))" :key="`${item.sourceKey}:${item.remoteId}`" :series="item" :style="{ '--panel-order': order % 6 }" /></div>
      <footer v-if="shelves[channel.id]?.page" class="manga-source-caption"><span>{{ shelves[channel.id]!.page!.sourceName }} · {{ t(shelves[channel.id]!.page!.fromCache ? 'comicRoom.cached' : 'comicRoom.updated') }} {{ new Date(shelves[channel.id]!.page!.fetchedAt * 1000).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }) }}</span><span v-if="shelves[channel.id]!.page!.warning" role="status">{{ shelves[channel.id]!.page!.warning }}</span></footer>
    </section>
  </div>
</template>
