<script setup lang="ts">
import { computed, nextTick, onMounted, onBeforeUnmount, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAsyncResource } from '../../reading/asyncResource'
import { refKey, refOf } from '../../api/library'
import { loadNovelBook, refreshNovelToc, splitGenres, type NovelBookView } from '../data/novelLibrary'
import { useNovelReading } from '../readingState'
import { progressChapter } from '../reader/progress'
import { orderChapterEntries } from '../reader/catalog'
import { readNovelBookTab, rememberNovelBookTab, type NovelBookTab } from '../bookViewState'
import NovelCover from '../components/NovelCover.vue'
import Icon from '../../components/Icon.vue'
import NovelChapterTools from '../../reading/ChapterTools.vue'
import { normalizeApiError } from '../../api/app'
import { Spinner } from '../../components/ui'
const route = useRoute(), { t } = useI18n(), reading = useNovelReading()
const itemId = Number(route.params.bookId)
const detail = useAsyncResource<NovelBookView | null, [boolean]>({
  load: refresh => (refresh ? refreshNovelToc(itemId) : loadNovelBook(itemId)),
  isEmpty: view => view === null,
})
onMounted(() => { void detail.run(false) })
const refreshing=ref(false),refreshError=ref('')
let refreshRequest:ReturnType<typeof refreshNovelToc>|undefined,disposed=false
async function refreshToc():Promise<void>{
 if(refreshing.value)return
 refreshing.value=true;refreshError.value=''
 try{refreshRequest=refreshNovelToc(itemId);const result=await refreshRequest;if(!disposed&&result)detail.data.value=result}
 catch(error){if(!disposed)refreshError.value=normalizeApiError(error).message}
 finally{if(!disposed)refreshing.value=false;refreshRequest=undefined}
}
onBeforeUnmount(()=>{disposed=true;void refreshRequest?.cancel?.()})
const item = computed(() => detail.data.value?.item ?? null)
const shelfKey = computed(() => item.value ? refKey(refOf(item.value)) : '')
const chapters = computed(() => detail.data.value?.chapters ?? [])
// 目录行：卷标记不占章号，可用章按顺序编号（不依赖 ord 的基准）。
const entries = computed(() => {
  let number = 0
  return chapters.value.map(chapter => ({ chapter, number: chapter.isVolume ? 0 : ++number }))
})
const readableCount = computed(() => entries.value.reduce((count, entry) => count + (entry.chapter.isVolume ? 0 : 1), 0))
// 取不到的章 = 行在目录里但来源已下架；卷标记是分组标题，不算一章。
const missingCount = computed(() => chapters.value.filter(chapter => !chapter.isVolume && !chapter.available).length)
const progress = computed(() => shelfKey.value ? reading.progress[shelfKey.value] : undefined)
const currentChapter = computed(() => progressChapter(chapters.value, progress.value))
const firstPath = computed(() => {
  if (!item.value) return null
  const saved=currentChapter.value
  // An unavailable/missing saved chapter must reach the reader's explicit
  // recovery state instead of silently replacing the user's place with page 1.
  if(progress.value)return `/novel/read/${item.value.id}/${saved?.id??progress.value.chapterId}`
  const first = entries.value.find(entry => !entry.chapter.isVolume && entry.chapter.available)
  return first ? `/novel/read/${item.value.id}/${first.chapter.id}` : null
})
const genreText = computed(() => item.value ? splitGenres(item.value.genres).join(' · ') : '')
const tab = ref<NovelBookTab>(readNovelBookTab(itemId))
const chapterSearch = ref('')
const descending=ref(false)
const chapterList = ref<HTMLElement | null>(null)
watch(tab, value => rememberNovelBookTab(itemId, value), { flush: 'sync' })
const visibleEntries = computed(() => {
  const query = chapterSearch.value.trim().toLocaleLowerCase()
  const ordered=orderChapterEntries(entries.value,descending.value)
  if (!query) return ordered
  return ordered.filter(entry => entry.chapter.isVolume
    ? entry.chapter.title.toLocaleLowerCase().includes(query)
    : entry.chapter.title.toLocaleLowerCase().includes(query) || String(entry.number) === query)
})

function revealChapter(chapterId: number): void {
  tab.value = 'chapters'
  chapterSearch.value = ''
  void nextTick().then(() => {
    const element = chapterList.value?.querySelector<HTMLElement>(`[data-chapter-id="${chapterId}"]`)
    element?.scrollIntoView({ behavior: 'smooth', block: 'center' })
  })
}

function jumpToNumber(number:number): void {
  if (!Number.isInteger(number) || number < 1) return
  const entry = entries.value.find(candidate => !candidate.chapter.isVolume && candidate.number === number)
  if (entry) revealChapter(entry.chapter.id)
}

function jumpToLatest(): void {
  const entry = [...entries.value].reverse().find(candidate => !candidate.chapter.isVolume)
  if (entry) revealChapter(entry.chapter.id)
}
</script>
<template>
  <div v-if="detail.status.value === 'ready' && item" class="novel-page novel-book-detail">
    <section class="novel-book-hero"><div class="novel-detail-cover"><NovelCover :book="item" /></div><div><span class="novel-eyebrow">{{ genreText || item.sourceName }}</span><h1>{{ item.title }}</h1><p v-if="item.author">{{ item.author }}<span class="novel-author-label">{{ t('novel.author') }}</span></p><div class="novel-book-badges"><span v-if="item.status">{{ item.status }}</span><span v-if="item.words">{{ item.words }}</span><span v-if="item.sourceName">{{ item.sourceName }}</span><span v-if="!item.available" class="novel-badge-unavailable">{{ t('novel.sourceUnavailable') }}</span></div></div></section>
    <div class="novel-detail-tabs" role="tablist"><button role="tab" :aria-selected="tab==='intro'" @click="tab='intro'">{{ t('novel.intro') }}</button><button role="tab" :aria-selected="tab==='chapters'" @click="tab='chapters'">{{ t('novel.chapters') }} <small>{{ readableCount }}</small></button></div>
    <section v-if="tab==='intro'" class="novel-description"><p v-if="item.summary">{{ item.summary }}</p><p v-else class="novel-demo-note">{{ t('novel.noSummary') }}</p><p v-if="item.updatedAt" class="novel-demo-note">{{ t('novel.updatedAt',{time:item.updatedAt}) }}</p><button v-if="item.latest" class="novel-latest-chapter" @click="tab='chapters'"><span>{{ t('novel.latestChapter') }}</span><strong>{{ item.latest }}</strong><Icon name="chevron-right" :size="18" /></button></section>
    <template v-else>
      <NovelChapterTools v-model:query="chapterSearch" v-model:descending="descending" :count="readableCount" :current="!!currentChapter" :refreshable="item.available" :refreshing="refreshing" @locate="jumpToNumber" @current="currentChapter && revealChapter(currentChapter.id)" @latest="jumpToLatest" @refresh="refreshToc"/>
      <p v-if="missingCount" class="novel-toc-missing">{{t('novel.chaptersUnavailable',{n:missingCount})}}</p>
      <p v-if="refreshError" class="novel-alert" role="status">{{refreshError}} <button class="novel-text-button" @click="refreshToc">{{t('common.retry')}}</button></p>
      <p v-if="detail.data.value?.tocError" class="novel-alert" role="status"><span>{{ t('novel.tocRefreshFailed') }} · {{ detail.data.value.tocError }}</span><button class="novel-text-button" @click="refreshToc">{{ t('common.retry') }}</button></p>
      <ol v-if="visibleEntries.length" ref="chapterList" class="novel-chapter-list">
        <li v-for="entry in visibleEntries" :key="entry.chapter.isVolume ? `v-${entry.chapter.id}` : entry.chapter.id" :data-chapter-id="entry.chapter.id" :class="{'novel-chapter-volume':entry.chapter.isVolume,'novel-chapter-gone':!entry.chapter.isVolume && !entry.chapter.available}">
          <strong v-if="entry.chapter.isVolume" class="novel-volume-title">{{ entry.chapter.title }}</strong>
          <router-link v-else-if="entry.chapter.available" :to="`/novel/read/${item.id}/${entry.chapter.id}`"><strong>{{ entry.chapter.title }}</strong><Icon v-if="currentChapter?.id === entry.chapter.id" name="bookmark" :size="16" /></router-link>
          <div v-else class="novel-chapter-dead"><strong>{{ entry.chapter.title }}</strong><small>{{ t('novel.chapterUnavailable') }}</small></div>
        </li>
      </ol>
      <div v-else-if="entries.length" class="novel-empty"><h2>{{ t('novel.noChapterMatches') }}</h2><button class="novel-text-button" @click="chapterSearch = ''">{{ t('common.clear') }}</button></div>
      <div v-if="!entries.length" class="novel-empty"><h2>{{ t('novel.noChapters') }}</h2><p>{{ t('novel.noChaptersHint') }}</p><button class="novel-button" :disabled="!item.available" @click="refreshToc">{{ t('common.refresh') }}</button></div>
    </template>
    <footer class="novel-detail-actions"><button class="novel-button" :aria-pressed="reading.inShelf(shelfKey)" @click="reading.toggleShelf(item)"><Icon :name="reading.inShelf(shelfKey)?'check':'plus'" :size="18" />{{ t(reading.inShelf(shelfKey)?'novel.inShelf':'novel.addShelf') }}</button><router-link v-if="firstPath && item.available" class="novel-button novel-button-primary" :to="firstPath">{{ t(progress?'novel.continueReading':'novel.readNow') }}</router-link><button v-else class="novel-button novel-button-primary" disabled>{{ t('novel.noReadableChapter') }}</button></footer>
  </div>
  <div v-else-if="detail.status.value === 'loading' || detail.status.value === 'idle'" class="novel-page"><Spinner :label="t('common.loading')" /></div>
  <div v-else-if="detail.status.value === 'error'" class="novel-page"><div class="novel-empty"><Icon name="warning" :size="36" /><h1>{{ detail.error.value?.message || t('common.unknownError') }}</h1><div class="novel-empty-actions"><button class="novel-button" @click="detail.retry()">{{ t('common.retry') }}</button><router-link class="novel-button" to="/novel">{{ t('novel.explore') }}</router-link></div></div></div>
  <div v-else class="novel-page"><div class="novel-empty"><h1>{{ t('novel.bookMissing') }}</h1><router-link class="novel-button" to="/novel">{{ t('novel.explore') }}</router-link></div></div>
</template>
