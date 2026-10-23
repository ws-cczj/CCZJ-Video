<script setup lang="ts">
import { computed, nextTick, ref } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAsyncResource } from '../../reading/asyncResource'
import ComicCover from '../components/ComicCover.vue'
import ChapterTools from '../../reading/ChapterTools.vue'
import BackToTop from '../../components/BackToTop.vue'
import Icon from '../../components/Icon.vue'
import ComicLoadState from '../components/ComicLoadState.vue'
import { useComicState } from '../state'
import { loadComicSeries, refreshComicToc, type ComicSeriesView } from '../data/repository'
import { splitGenres, type ComicChapter } from '../domain'
import { progressChapter } from '../reader/progress'

const route = useRoute(), { t } = useI18n(), state = useComicState()
const itemId = Number(route.params.seriesId)
const descending = ref(false)
const chapterQuery = ref('')

const { status, data, error, retry, run } = useAsyncResource<ComicSeriesView, [boolean]>({
  load: refresh => (refresh ? refreshComicToc(itemId) : loadComicSeries(itemId)),
})
void run(false)

const item = computed(() => data.value?.item ?? null)
const key = computed(() => item.value ? state.keyOf(item.value) : '')
const following = computed(() => !!key.value && state.isFollowed(key.value))
const progress = computed(() => key.value ? state.progress[key.value] : undefined)
const ordered=computed(()=>[...(data.value?.chapters??[])].filter(row=>!row.isVolume).sort((a,b)=>a.ord-b.ord))
const chapters = computed<ComicChapter[]>(() => [...(data.value?.chapters ?? [])].filter(chapter => !chapterQuery.value.trim() || `${chapter.ord + 1} ${chapter.title}`.toLocaleLowerCase().includes(chapterQuery.value.trim().toLocaleLowerCase())).sort((a, b) => descending.value ? b.ord - a.ord : a.ord - b.ord))
const savedChapter = computed(() => progressChapter(data.value?.chapters ?? [], progress.value))
const heading = computed(() => item.value ? (splitGenres(item.value.genres).join(' · ') || item.value.sourceName) : '')
const tags = computed(() => {
  const list = item.value ? [item.value.status, item.value.latest ? t('comic.latestEpisode', { name: item.value.latest }) : ''].filter(Boolean) : []
  if (data.value) list.push(t('comic.episodeCount', { n: data.value.chapters.length }))
  if (item.value && !item.value.available) list.push(t('comic.source.unavailable'))
  return list
})
const busy = computed(() => status.value === 'loading')
const startChapter = computed(() => {
  const ordered = [...(data.value?.chapters ?? [])].sort((a, b) => a.ord - b.ord)
  if (progress.value) return savedChapter.value ?? undefined
  return ordered.find(chapter => chapter.available) ?? ordered[0]
})
const startPath = computed(() => {
  if (!item.value) return '/comic'
  if (!data.value?.chapters.length) return null
  if (progress.value) return state.readPath(item.value.id, progress.value.episodeOrd, progress.value.episodeRemoteId)
  const first = startChapter.value
  return first ? state.readPath(item.value.id, first.ord, first.remoteId) : null
})
const startLabel = computed(() => {
  if (progress.value) return item.value?.available && savedChapter.value?.available === true ? 'comic.continueReading' : 'comic.tryCachedReading'
  return item.value?.available && startChapter.value?.available === true ? 'comic.readNow' : 'comic.tryCachedReading'
})

function refresh(): void {
  void run(true)
}
async function revealCurrent(): Promise<void> {
  chapterQuery.value = ''
  await nextTick()
  document.querySelector('.comic-episode-grid [aria-current="page"]')?.scrollIntoView({ block: 'center', behavior: 'auto' })
}

async function locate(id:number){chapterQuery.value='';await nextTick();document.querySelector(`.comic-episode-grid [data-chapter-id="${id}"]`)?.scrollIntoView({block:'center',behavior:'auto'})}
function locateNumber(n:number){const row=ordered.value[n-1];if(row)void locate(row.id)}

function accessLabel(chapter: ComicChapter): string {
  if (!chapter.available) return t('comic.chapterUnavailable')
  if (chapter.access === 'login') return t('comic.access.login')
  if (chapter.access === 'subscription') return t('comic.access.subscription')
  return ''
}
</script>
<template>
  <div v-if="item" class="comic-page comic-detail">
    <div class="comic-detail-hero">
      <div class="comic-cover"><ComicCover :src="item.cover" :title="item.title" /></div>
      <div>
        <p class="comic-eyebrow">{{ heading }}</p>
        <h1>{{ item.title }}</h1>
        <p class="comic-author">{{ item.author || item.sourceName }}</p>
        <div class="comic-detail-tags"><span v-for="tag in tags" :key="tag">{{ tag }}</span></div>
      </div>
    </div>
    <p v-if="!item.available" class="comic-notice">{{ t('comic.source.unavailableHint') }}</p>
    <p v-if="error" class="comic-notice">{{ error.message }}<button type="button" class="comic-text-button" @click="retry">{{ t('common.retry') }}</button></p>
    <p v-else-if="data?.tocError" class="comic-notice">{{ t('comic.tocRefreshFailed') }}<button type="button" class="comic-text-button" :disabled="busy" @click="refresh">{{ t('common.retry') }}</button></p>
    <section class="comic-intro"><h2>{{ t('comic.aboutStory') }}</h2><p>{{ item.summary || t('comic.noSummary') }}</p></section>
    <div class="comic-section-heading"><h2>{{t('comic.episodes')}}<small>{{ordered.length}}</small></h2></div>
    <ChapterTools mode="comic" v-model:query="chapterQuery" v-model:descending="descending" :count="ordered.length" :current="!!savedChapter" refreshable :refreshing="busy" @current="revealCurrent" @locate="locateNumber" @latest="ordered.length&&locate(ordered[ordered.length-1].id)" @refresh="refresh"/>
    <ol v-if="chapters.length" class="comic-episode-grid">
      <li v-for="chapter in chapters" :key="chapter.id" :data-chapter-id="chapter.id">
          <router-link v-if="chapter.available" :to="state.readPath(itemId, chapter.ord, chapter.remoteId)" :class="{ selected: savedChapter?.remoteId === chapter.remoteId }" :aria-current="savedChapter?.remoteId === chapter.remoteId ? 'page' : undefined">
          <span v-if="chapter.volume||accessLabel(chapter)"><template v-if="chapter.volume">{{chapter.volume}}</template><template v-if="accessLabel(chapter)"> {{accessLabel(chapter)}}</template></span>
          <strong>{{ chapter.title }}</strong>
          <Icon name="chevron-right" :size="16" />
        </router-link>
          <span v-else class="comic-episode-row unavailable">
          <span v-if="chapter.volume">{{chapter.volume}}</span>
          <strong>{{ chapter.title }}</strong>
          <em>{{ t('comic.chapterUnavailable') }}</em>
        </span>
      </li>
    </ol>
    <div v-else-if="chapterQuery && data?.chapters.length" class="comic-empty"><h2>{{ t('novel.noChapterMatches') }}</h2><button class="comic-text-button" @click="chapterQuery = ''">{{ t('common.clear') }}</button></div>
    <div v-else-if="!busy" class="comic-empty">
      <Icon name="list" :size="34" />
      <h2>{{ t('comic.tocEmpty') }}</h2>
      <p>{{ t('comic.tocEmptyHint') }}</p>
      <button type="button" class="comic-button" @click="refresh"><Icon name="refresh" :size="17" />{{ t('comic.refreshToc') }}</button>
    </div>
    <BackToTop compact target-selector=".cczj-comic-shell .main-content"/>
    <footer class="comic-detail-actions">
      <button class="comic-button" type="button" :aria-pressed="following" :disabled="!item.available" @click="state.toggle(item)"><Icon :name="following ? 'check' : 'plus'" :size="19" />{{ t(following ? 'comic.followed' : 'comic.follow') }}</button>
      <router-link v-if="startPath" :to="startPath" class="comic-button comic-primary">{{ t(startLabel) }}<Icon name="chevron-right" :size="18" /></router-link>
      <button v-else class="comic-button comic-primary" disabled>{{ t('comic.noReadableEpisode') }}</button>
    </footer>
  </div>
  <ComicLoadState v-else :status="status" :error="error" icon="panels" :title="t('comic.seriesMissing')" :not-found-text="t('comic.seriesMissing')" @retry="retry">
    <template #actions><router-link to="/comic" class="comic-button">{{ t('comic.libraryHome') }}</router-link></template>
  </ComicLoadState>
</template>
