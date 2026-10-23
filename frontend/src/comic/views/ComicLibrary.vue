<script setup lang="ts">
import { computed, onActivated, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { parseRefKey, refKey } from '../../api/library'
import { useAsyncResource, isEmptyList } from '../../reading/asyncResource'
import CczjMobileEditActions from '../../mobile/CczjMobileEditActions.vue'
import ComicCover from '../components/ComicCover.vue'
import Icon from '../../components/Icon.vue'
import { isMobileRuntime } from '../../api/runtime'
import { useHoldToEdit } from '../../mobile/useHoldToEdit'
import { useConfirmStore } from '../../stores/confirm'
import { useComicState } from '../state'
import { resolveShelfEntries, snapshotItem, listComicSources } from '../data/repository'
import type { ComicShelfEntry } from '../domain'
import { useModeViewState } from '../../modes/useModeViewState'

const { t } = useI18n(), state = useComicState()
const tab = ref<'follow' | 'history'>('follow'), query = ref(''), sort = ref('recent')
useModeViewState('comic', 'library', { tab, query, sort })
const mobile = isMobileRuntime(), confirm = useConfirmStore()
const { editing: manage, vHoldToEdit, finish } = useHoldToEdit(mobile)
const entries = computed(() => {
  const list: ComicShelfEntry[] = [...state.followed], known = new Set(list.map(refKey))
  for (const saved of state.recent) {
    const ref = parseRefKey(saved.key)
    if (!ref || ref.kind !== 'comic' || known.has(saved.key)) continue
    list.push({ ...ref, itemId: saved.itemId, title: saved.title, cover: saved.cover, addedAt: 0 })
  }
  return list
})
const resolved = useAsyncResource({ load: () => resolveShelfEntries(entries.value) })
const sources = useAsyncResource({ load: listComicSources, isEmpty: isEmptyList })
const rows = computed(() => {
  const word = query.value.trim().toLocaleLowerCase()
  return entries.value.map(entry => {
    const key = refKey(entry), found = resolved.data.value?.get(key)
    return { key, item: found?.item ?? snapshotItem(entry), available: found ? found.available : true, addedAt: entry.addedAt, progress: state.progress[key] }
  }).filter(row => (tab.value === 'follow' ? state.isFollowed(row.key) : !!row.progress) && (!word || `${row.item.title} ${row.item.author}`.toLocaleLowerCase().includes(word)))
    .sort((a, b) => sort.value === 'title' ? a.item.title.localeCompare(b.item.title) : sort.value === 'added' ? b.addedAt - a.addedAt : (b.progress?.updatedAt ?? 0) - (a.progress?.updatedAt ?? 0) || b.addedAt - a.addedAt)
})
const resuming = computed(() => state.recent[0])
watch(() => entries.value.map(refKey).join('|'), () => { if (entries.value.length) void resolved.run() }, { immediate: true })
void sources.run()
let activated = false
onActivated(() => { if (activated) { void sources.run(); if (entries.value.length) void resolved.run() }; activated = true })
async function remove(key: string, name: string): Promise<void> {
  if (!await confirm.confirm({ message: t('mobile.confirmItemRemoval', { name }), level: 'danger' })) return
  await state.unfollow(key)
}
</script>

<template>
  <div class="comic-page comic-library">
    <header class="reading-page-heading"><div><p class="reading-eyebrow">{{ t('readingUI.savedComics', { n: state.followed.length }) }}</p><h1>{{ t('comicRoom.following') }}</h1></div><router-link class="comic-button comic-primary" to="/comic"><Icon name="plus" :size="18" />{{ t('readingUI.findComics') }}</router-link></header>
    <router-link v-if="resuming && !manage" :to="state.resumePath(resuming)" class="comic-resume"><div class="comic-resume-cover"><ComicCover :src="resuming.cover" :title="resuming.title" /></div><div><small>{{ t('comic.continueReading') }}</small><strong>{{ resuming.title }}</strong><span>{{ t('readingUI.pagePosition', { episode: resuming.episodeOrd + 1, page: resuming.page + 1 }) }}</span></div><Icon name="chevron-right" :size="22" /></router-link>
    <div class="reading-library-toolbar"><div class="reading-tabs"><button :aria-pressed="tab === 'follow'" @click="tab = 'follow'; finish()">{{ t('comic.myFollowing') }}<small>{{ state.followed.length }}</small></button><button :aria-pressed="tab === 'history'" @click="tab = 'history'; finish()">{{ t('comic.recentReading') }}</button></div><button v-if="tab === 'follow' && state.followed.length" class="comic-text-button" :aria-pressed="manage" @click="manage ? finish() : manage = true">{{ t(manage ? 'common.done' : 'comic.manage') }}</button></div>
    <div v-if="entries.length" class="reading-library-filters"><label class="reading-inline-search"><Icon name="search" :size="17" /><input v-model="query" type="search" :aria-label="t('readingUI.filterComics')" :placeholder="t('readingUI.filterComics')" /></label><select v-model="sort" :aria-label="t('readingUI.sort')"><option value="recent">{{ t('readingUI.recentFirst') }}</option><option value="added">{{ t('readingUI.addedFirst') }}</option><option value="title">{{ t('readingUI.title') }}</option></select></div>
    <div v-if="resolved.error.value" class="comic-notice" role="status"><span>{{ resolved.error.value.message }}</span><button class="comic-text-button" @click="resolved.retry()">{{ t('common.retry') }}</button></div>
    <div v-if="rows.length" class="comic-grid" :data-cczj-editing="mobile && manage || undefined">
      <article v-for="row in rows" :key="row.key" class="comic-card" v-hold-to-edit="tab === 'follow'">
        <router-link :to="state.resumePath({ itemId: row.item.id, key: row.key })" @click="mobile && manage && $event.preventDefault()"><div class="comic-cover"><ComicCover :src="row.item.cover" :title="row.item.title" /><span v-if="!row.available">{{ t('comic.source.unavailable') }}</span></div><h3>{{ row.item.title }}</h3><p>{{ row.progress ? t('readingUI.pagePosition', { episode: row.progress.episodeOrd + 1, page: row.progress.page + 1 }) : t('comic.notStarted') }}</p></router-link>
        <button v-if="manage && tab === 'follow'" :class="mobile ? 'cczj-mobile-item-remove' : 'comic-remove'" data-cczj-remove :aria-label="t('comic.unfollowNamed', { name: row.item.title })" @click.stop="remove(row.key, row.item.title)"><Icon name="close" :size="17" /><span v-if="!mobile">{{ t('comic.unfollow') }}</span></button>
        <router-link v-else class="comic-card-details reading-icon-button" :to="`/comic/series/${row.item.id}`" :aria-label="`${t('readingUI.openDetails')}: ${row.item.title}`"><Icon name="more" :size="19" /></router-link>
      </article>
    </div>
    <div v-else class="comic-empty reading-empty"><span class="reading-empty-icon"><Icon :name="query ? 'search' : 'panels'" :size="32" /></span><h2>{{ t(query ? 'readingUI.noShelfMatch' : tab === 'history' ? 'comic.emptyHistory' : 'comic.emptyFollowing') }}</h2><p>{{ t(query ? 'comic.tryAnother' : tab === 'history' ? 'readingUI.noHistoryHint' : sources.status.value === 'empty' ? 'readingUI.sourceEmptyHint' : 'comic.followHint') }}</p><router-link v-if="!query" class="comic-button comic-primary" :to="sources.status.value === 'empty' ? '/comic/sources' : '/comic/search'">{{ t(sources.status.value === 'empty' ? 'readingUI.addSource' : 'readingUI.findComics') }}</router-link></div>
    <CczjMobileEditActions v-if="mobile" :open="manage" @done="finish" @cancel="finish" />
  </div>
</template>
