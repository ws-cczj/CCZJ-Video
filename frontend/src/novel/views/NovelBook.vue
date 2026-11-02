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
import { Button, Spinner, MotionTransition } from '../../components/ui'
import ReaderSheet from '../../reading/ReaderSheet.vue'
import NovelSourceSwitch from '../components/NovelSourceSwitch.vue'
import { useNovelSourceCache } from '../../reading/useNovelSourceCache'
const route = useRoute(), { t } = useI18n(), reading = useNovelReading()
const itemId = Number(route.params.bookId)
const sourceCache=useNovelSourceCache(()=>itemId)
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
const choosingSource=ref(false)
watch(choosingSource,(value,previous)=>{if(previous&&!value)void sourceCache.cached.run(itemId)})
const sourceChapter=computed(()=>currentChapter.value?.id??entries.value.find(row=>!row.chapter.isVolume&&row.chapter.available)?.chapter.id??0)
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
    <section class="novel-book-hero"><div class="novel-detail-cover"><NovelCover :book="item" /></div><div><span class="novel-eyebrow">{{ genreText || item.sourceName }}</span><h1>{{ item.title }}</h1><p v-if="item.author">{{ item.author }}<span class="novel-author-label">{{ t('novel.author') }}</span></p><div class="novel-book-badges"><span v-if="item.status">{{ item.status }}</span><span v-if="item.words">{{ item.words }}</span><span v-if="!item.available" class="novel-badge-unavailable">{{ t('novel.sourceUnavailable') }}</span></div></div></section>
    <div class="novel-detail-tabs" role="tablist"><button role="tab" :aria-selected="tab==='intro'" @click="tab='intro'">{{ t('novel.intro') }}</button><button role="tab" :aria-selected="tab==='chapters'" @click="tab='chapters'">{{ t('novel.chapters') }} <small>{{ readableCount }}</small></button></div>
    <section v-if="tab==='intro' && (item.summary || item.updatedAt || item.latest)" class="novel-description"><p v-if="item.summary">{{ item.summary }}</p><p v-if="item.updatedAt" class="novel-demo-note">{{ t('novel.updatedAt',{time:item.updatedAt}) }}</p><button v-if="item.latest" class="novel-latest-chapter" @click="jumpToLatest"><span>{{ t('novel.latestChapter') }}</span><strong>{{ item.latest }}</strong><Icon name="chevron-right" :size="18" /></button></section>
    <details v-if="tab==='intro'" class="novel-book-source"><summary>{{ t('sourceSupply.currentSource',{name:item.sourceName}) }}</summary><p v-if="detail.data.value?.reading?.checkedAt">{{ t('sourceSupply.bookChapterCounts',{readable:detail.data.value.reading.readable,restricted:detail.data.value.reading.restricted,failed:detail.data.value.reading.failed+detail.data.value.reading.warnings}) }} · {{ new Date(detail.data.value.reading.checkedAt*1000).toLocaleString() }}</p><p>{{ t('sourceSupply.unreadUnknown') }}</p><div class="novel-book-source-cache"><p v-if="sourceCache.cached.data.value">{{ t('bookSourceCache.summary',{count:sourceCache.cached.data.value.candidates.length,available:sourceCache.cached.data.value.available}) }}<span v-if="sourceCache.cached.data.value.checkedAt"> · {{ t('bookSourceCache.checked',{time:new Date(sourceCache.cached.data.value.checkedAt*1000).toLocaleString()}) }}</span></p><p v-if="sourceCache.cached.error.value" role="status">{{ sourceCache.cached.error.value.message }}</p><p v-if="sourceCache.probe.error.value" role="status">{{ t('bookSourceCache.updateFailed') }}</p><div class="cczj-flex cczj-flex-wrap cczj-gap-4"><Button v-for="candidate in sourceCache.cached.data.value?.candidates.filter(row=>row?.item?.id!==itemId).slice(0,3)" :key="candidate?.item?.id" variant="text" @click="choosingSource=true">{{ candidate?.item?.sourceName }} · {{ t(`bookSourceCache.states.${candidate?.state}`) }}</Button></div><p>{{ t('bookSourceCache.scope') }}</p></div><p v-if="detail.data.value?.reading?.stale">{{ t('sourceSupply.staleChapters',{count:detail.data.value.reading.stale}) }}</p><Button variant="text" @click="choosingSource=true">{{ t('sourceSupply.otherSources') }}</Button><router-link to="/novel/sources">{{ t('sourceSupply.manageSources') }}</router-link></details>
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
    <MotionTransition preset="sheet" appear><ReaderSheet v-if="choosingSource" :title="t('sourceSupply.otherSources')" @close="choosingSource=false"><NovelSourceSwitch :item-id="itemId" :chapter-id="sourceChapter" @leaving="choosingSource=false"/></ReaderSheet></MotionTransition>
  </div>
  <div v-else-if="detail.status.value === 'loading' || detail.status.value === 'idle'" class="novel-page"><Spinner :label="t('common.loading')" /></div>
  <div v-else-if="detail.status.value === 'error'" class="novel-page"><div class="novel-empty"><Icon name="warning" :size="36" /><h1>{{ detail.error.value?.message || t('common.unknownError') }}</h1><div class="novel-empty-actions"><button class="novel-button" @click="detail.retry()">{{ t('common.retry') }}</button><router-link class="novel-button" to="/novel">{{ t('novel.explore') }}</router-link></div></div></div>
  <div v-else class="novel-page"><div class="novel-empty"><h1>{{ t('novel.bookMissing') }}</h1><router-link class="novel-button" to="/novel">{{ t('novel.explore') }}</router-link></div></div>
</template>
<style scoped>
.novel-book-source{padding:16px 0;border-top:1px solid var(--novel-line);margin-bottom:20px}
.novel-book-source summary{font-size:calc(14rem / 14);padding:12px 0;cursor:pointer}
.novel-book-source p,.novel-book-source>a{font-size:calc(12rem / 14);line-height:1.8;color:var(--novel-muted);overflow-wrap:anywhere}
.novel-book-source>a{margin-left:14px}
</style>
