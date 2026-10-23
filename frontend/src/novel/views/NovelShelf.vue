<script setup lang="ts">
import { computed, onActivated, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import CczjMobileEditActions from '../../mobile/CczjMobileEditActions.vue'
import { useAsyncResource, isEmptyList } from '../../reading/asyncResource'
import { resolveShelfEntries, listNovelSources } from '../data/novelLibrary'
import { useNovelReading, type ShelfEntry } from '../readingState'
import { parseRefKey } from '../../api/library'
import NovelCover from '../components/NovelCover.vue'
import Icon from '../../components/Icon.vue'
import { isMobileRuntime } from '../../api/runtime'
import { useHoldToEdit } from '../../mobile/useHoldToEdit'
import { useConfirmStore } from '../../stores/confirm'
import { useModeViewState } from '../../modes/useModeViewState'

const reading = useNovelReading(), { t } = useI18n()
const mobile = isMobileRuntime(), confirm = useConfirmStore()
const { editing: managing, vHoldToEdit, finish } = useHoldToEdit(mobile)
const tab = ref<'shelf' | 'recent'>('shelf'), query = ref(''), sort = ref('recent')
useModeViewState('novel', 'shelf', { tab, query, sort })
const sources = useAsyncResource({ load: listNovelSources, isEmpty: isEmptyList })
const resolved = useAsyncResource({
  load: () => resolveShelfEntries(allEntries.value),
  onData: results => {
    for (const result of results) {
      const saved = reading.shelf.find(entry => entry.key === result.key)
      if (!saved) continue
      const available = !!result.item && result.item.available !== false
      if (saved.available !== available || result.item && JSON.stringify(saved.item) !== JSON.stringify(result.item)) {
        void reading.updateShelfEntry(result.key, { ...(result.item ? { item: result.item } : {}), available })
      }
    }
  },
})
// Saved snapshots stay visible while the local catalog is loading or unavailable.
const allEntries = computed<ShelfEntry[]>(() => {
  const entries = [...reading.shelf], known = new Set(entries.map(entry => entry.key))
  for (const [key, progress] of Object.entries(reading.progress)) {
    const ref = parseRefKey(key)
    if (known.has(key) || !ref || ref.kind !== 'novel') continue
    entries.push({ key, itemId: progress.itemId, addedAt: progress.updatedAt, available: true,
      item: { ...ref, id: progress.itemId, sourceName: '', title: progress.title, author: '', cover: '', summary: '', genres: '', status: '', latest: '', words: '', updatedAt: '', available: true } })
  }
  return entries
})
const rows = computed(() => {
  const found = new Map(resolved.data.value?.map(row => [row.key, row]))
  const word = query.value.trim().toLocaleLowerCase()
  return allEntries.value.filter(entry => tab.value === 'shelf' ? reading.inShelf(entry.key) : !!reading.progress[entry.key])
    .map(entry => {
      const result = found.get(entry.key)
      return { ...entry, itemId: result?.itemId ?? entry.itemId, item: result?.item ?? entry.item,
        available: result ? !!result.item && result.item.available !== false : entry.available }
    })
    .filter(entry => !word || `${entry.item.title} ${entry.item.author}`.toLocaleLowerCase().includes(word))
    .sort((a, b) => sort.value === 'title' ? a.item.title.localeCompare(b.item.title) : sort.value === 'added' ? b.addedAt - a.addedAt : (reading.progress[b.key]?.updatedAt ?? 0) - (reading.progress[a.key]?.updatedAt ?? 0) || b.addedAt - a.addedAt)
})
const last = computed(() => reading.lastRead)
const lastPath = computed(() => {
  if (!last.value) return null
  const current = resolved.data.value?.find(row => row.key === last.value?.key)
  return readPath(last.value.key, current?.itemId ?? last.value.progress.itemId)
})
const noSources = computed(() => sources.status.value === 'empty')
watch(() => allEntries.value.map(entry => entry.key).join('|'), () => { if (allEntries.value.length) void resolved.run() }, { immediate: true })
void sources.run()
let activated = false
onActivated(() => { if (activated) { void sources.run(); if (allEntries.value.length) void resolved.run() }; activated = true })
function readPath(key: string, itemId: number): string {
  return `/novel/read/${itemId}/${reading.progress[key]?.chapterId ?? 'first'}`
}
function path(entry: ShelfEntry): string {
  return managing.value || !entry.available ? `/novel/book/${entry.itemId}` : readPath(entry.key, entry.itemId)
}
async function remove(key: string, name: string): Promise<void> {
  if (!await confirm.confirm({ message: t('mobile.confirmItemRemoval', { name }), level: 'danger' })) return
  await reading.removeShelf(key)
}
</script>

<template>
  <div class="novel-page novel-shelf">
    <header class="reading-page-heading"><div><p class="reading-eyebrow">{{ t('readingUI.savedBooks', { n: reading.shelf.length }) }}</p><h1>{{ t('novel.myShelf') }}</h1><p>{{ t('readingUI.shelfSubtitle') }}</p></div><router-link class="novel-button novel-button-primary" to="/novel/search"><Icon name="plus" :size="18" />{{ t('readingUI.findBooks') }}</router-link></header>
    <router-link v-if="last && lastPath && !managing" class="novel-resume" :to="lastPath"><span class="novel-resume-icon"><Icon name="book-open" :size="24" /></span><div><small>{{ t('novel.continueReading') }}</small><strong>{{ last.progress.title }}</strong><span>{{ t('readingUI.chapterPosition', { chapter: last.progress.chapterOrd + 1, percent: Math.round(last.progress.ratio * 100) }) }}</span></div><span class="novel-resume-arrow"><Icon name="chevron-right" :size="22" /></span></router-link>
    <div class="reading-library-toolbar"><div class="reading-tabs"><button :aria-pressed="tab === 'shelf'" @click="tab = 'shelf'; finish()">{{ t('readingUI.allBooks') }}<small>{{ reading.shelf.length }}</small></button><button :aria-pressed="tab === 'recent'" @click="tab = 'recent'; finish()">{{ t('readingUI.recent') }}</button></div><button v-if="tab === 'shelf' && reading.shelf.length" class="novel-text-button" :aria-pressed="managing" @click="managing ? finish() : managing = true">{{ t(managing ? 'common.done' : 'common.manage') }}</button></div>
    <div v-if="allEntries.length" class="reading-library-filters"><label class="reading-inline-search"><Icon name="search" :size="17" /><input v-model="query" type="search" :aria-label="t('readingUI.filterShelf')" :placeholder="t('readingUI.filterShelf')" /></label><select v-model="sort" :aria-label="t('readingUI.sort')"><option value="recent">{{ t('readingUI.recentFirst') }}</option><option value="added">{{ t('readingUI.addedFirst') }}</option><option value="title">{{ t('readingUI.title') }}</option></select></div>
    <div v-if="resolved.error.value" class="novel-alert" role="status"><span>{{ t('novel.shelfCheckFailed') }}</span><button class="novel-text-button" @click="resolved.retry()">{{ t('common.retry') }}</button></div>
    <section v-if="rows.length" class="novel-shelf-list" :data-cczj-editing="mobile && managing || undefined">
      <article v-for="entry in rows" :key="entry.key" class="novel-shelf-item" :class="{ 'novel-shelf-unavailable': !entry.available }" v-hold-to-edit="tab === 'shelf'">
        <router-link class="novel-shelf-entry" :to="path(entry)" @click="mobile && managing && $event.preventDefault()"><div class="novel-shelf-cover"><NovelCover :book="entry.item" /></div><div class="novel-shelf-copy"><h2>{{ entry.item.title }}</h2><p>{{ entry.item.author || entry.item.sourceName }}</p><span v-if="!entry.available" class="novel-shelf-status">{{ t('novel.sourceUnavailable') }}</span><span v-else class="novel-shelf-status">{{ reading.progress[entry.key] ? t('readingUI.chapterPosition', { chapter: reading.progress[entry.key].chapterOrd + 1, percent: Math.round(reading.progress[entry.key].ratio * 100) }) : t('novel.notStarted') }}</span></div><Icon class="novel-shelf-chevron" name="chevron-right" :size="18" /></router-link>
        <button v-if="managing && tab === 'shelf'" :class="mobile ? 'cczj-mobile-item-remove' : 'novel-shelf-remove'" data-cczj-remove :aria-label="`${t('novel.removeShelf')}: ${entry.item.title}`" @click.stop="remove(entry.key, entry.item.title)"><Icon name="close" :size="17" /></button>
        <router-link v-else class="novel-shelf-details reading-icon-button" :to="`/novel/book/${entry.itemId}`" :aria-label="`${t('readingUI.openDetails')}: ${entry.item.title}`"><Icon name="more" :size="20" /></router-link>
      </article>
    </section>
    <div v-else class="novel-empty reading-empty"><span class="reading-empty-icon"><Icon :name="query ? 'search' : 'book-open'" :size="32" /></span><h2>{{ t(query ? 'readingUI.noShelfMatch' : tab === 'recent' ? 'readingUI.noHistory' : 'novel.emptyShelf') }}</h2><p>{{ t(query ? 'novel.tryKeyword' : tab === 'recent' ? 'readingUI.noHistoryHint' : noSources ? 'readingUI.sourceEmptyHint' : 'readingUI.shelfEmptyHint') }}</p><router-link v-if="!query" class="novel-button novel-button-primary" :to="noSources ? '/novel/sources' : '/novel/search'">{{ t(noSources ? 'readingUI.addSource' : 'readingUI.findBooks') }}</router-link></div>
    <CczjMobileEditActions v-if="mobile" :open="managing" @done="finish" @cancel="finish" />
  </div>
</template>
