<script setup lang="ts">
import {useViewActivity} from '../../composables/useViewActivity'
import {useSearchReturn} from '../../reading/useSearchReturn'
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useCatalog } from '../../reading/useCatalog'
import { useAsyncResource } from '../../reading/asyncResource'
import Icon from '../../components/Icon.vue'
import ComicCard from '../components/ComicCard.vue'
import ComicLoadState from '../components/ComicLoadState.vue'
import { listComicLibrary, listComicSources, searchComics, type ComicSearchResult } from '../data/repository'
import { isCompletedStatus, splitGenres, type ComicSeries } from '../domain'
import { useModeViewState } from '../../modes/useModeViewState'
import { startupTask } from '../../diagnostics/startup'
import { isEmptyList } from '../../reading/asyncResource'
import ReadingSearchStatus from '../../reading/ReadingSearchStatus.vue'
import { rankSearchItems, searchRelevance } from '../../reading/searchRelevance'

defineOptions({name:'ComicCatalog'})
const active=useViewActivity()
const searchRoot=ref<HTMLElement|null>(null)
const route = useRoute(), router = useRouter(), { t } = useI18n()
const keyword = ref(String(route.query.q || ''))
useModeViewState('comic','search',{keyword})
if(typeof route.query.q==='string')keyword.value=route.query.q
const searching = computed(() => route.path === '/comic/search')
const genre = computed(() => String(route.query.genre || ''))
const complete = computed(() => route.query.complete === '1')
const sourceInventory = useAsyncResource({ load: listComicSources, isEmpty: isEmptyList })
watch(searching, active => {
  if (active && sourceInventory.status.value === 'idle') void sourceInventory.run()
}, { immediate: true })

const browse = useCatalog('comic', () => ({ genre: genre.value, complete: complete.value }), () => active.value&&!searching.value)
const browseStatus = browse.status, browseError = browse.error
const retryBrowse = browse.retry
let searchGeneration=0,requestedKeyword=''
const { status: searchStatus, data: searchData, error: searchError, retry: retrySearch, run: runSearch, cancel: cancelSearch } = useAsyncResource<ComicSearchResult, [string, number]>({
  diagnosticLabel:'comic.search',
  load: (word, page): ReturnType<typeof searchComics> => {
    const generation=++searchGeneration
    const sameQuery=requestedKeyword===word;requestedKeyword=word
    const previous: ComicSearchResult | null = searchData.value
    if(page===1){searchData.value=null;searchReturn.capture()}
    return searchComics(word, page, page > 1 && previous && sameQuery ? previous.nextCursor : '',partial=>{
      if(generation!==searchGeneration)return
      searchData.value=page>1&&previous&&sameQuery?{...partial,searchedSources:previous.searchedSources+partial.searchedSources,items:mergeItems(previous.items,partial.items,partial.keyword || word)}:partial
    }).then(result => {
    // 分页是"再取一页"：第 2 页往后要接在已有结果后面，而不是换掉它。
    return page > 1 && previous && sameQuery && previous.page === page - 1
      ? { ...result,searchedSources:previous.searchedSources+result.searchedSources, items: mergeItems(previous.items,result.items,result.keyword || word) }
      : result
    })
  },
  // 有源失败时不算 empty：那不是一个成功的"没有结果"，界面要指出哪一个源失败了。
  isEmpty: result => result.items.length === 0 && result.failedCount === 0,
})

function mergeItems(previous:ComicSeries[],next:ComicSeries[],word:string):ComicSeries[] { return rankSearchItems([...new Map([...previous,...next].map(item=>[item.id,item])).values()],word) }
const found = computed<ComicSeries[]>(() => (searching.value ? (searchData.value?.items ?? []).filter(item=>searchRelevance(item,searchData.value?.keyword??'')>0) : browse.items.value))
const related = computed(()=>(searchData.value?.items??[]).filter(item=>searchRelevance(item,searchData.value?.keyword??'')===0))
const onlyPartialMatch = computed(()=>found.value.length>0&&!found.value.some(item=>searchRelevance(item,searchData.value?.keyword??'')>=3))
const failedSources = computed(() => (searchData.value?.sources ?? []).filter(entry => entry.failed))
const matches = computed(() => found.value.filter(item => (!genre.value || splitGenres(item.genres).includes(genre.value)) && (!complete.value || isCompletedStatus(item.status))))
// genres 是源给的自由文本，所以筛选项从已入库的作品里收集，不再是写死的枚举。
const genreOptions = computed(() => {
  if (!searching.value) return browse.genres.value
  const counts = new Map<string, number>()
  for (const item of found.value) for (const name of splitGenres(item.genres)) counts.set(name, (counts.get(name) ?? 0) + 1)
  return [...counts].sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0])).map(([name]) => name)
})
const status = computed(() => (searching.value ? searchStatus.value : browseStatus.value))
const loadError = computed(() => (searching.value ? searchError.value : browseError.value))
const noSources = computed(() => (searching.value
  ? !!searchData.value?.noSources || sourceInventory.status.value === 'empty'
  : browseStatus.value === 'empty'))
/** 空屏幕分五种：还没搜、正在加载、加载失败、还没加源、真的没匹配上。处置各不相同。 */
const notice = computed<'none' | 'idle' | 'loading' | 'error' | 'guide' | 'noMatches'>(() => {
  if (matches.value.length) return 'none'
  if (searching.value && sourceInventory.status.value === 'empty') return 'guide'
  if (searching.value && searchStatus.value === 'idle') return 'idle'
  if (status.value === 'loading') return 'loading'
  if (status.value === 'error') return 'error'
  return noSources.value ? 'guide' : 'noMatches'
})

function retryCurrent(): void {
  if (searching.value && searchError.value?.code === 'CONFLICT') void runSearch(keyword.value.trim(), 1)
  else if (searching.value) void retrySearch()
  else void retryBrowse()
}

function filter(key: string, value: string): void {
  void router.replace({ path: route.path, query: { ...route.query, [key]: value || undefined } })
}

function submit(): void {
  const word = keyword.value.trim()
  if (!word) return
  // 同一个词再搜一次时路由不会变，watch 不会触发，所以这里自己发起。
  if (word === String(route.query.q || '')) { void runSearch(word, 1); return }
  filter('q', word)
}

function more(): void {
  const result = searchData.value
  if (result?.nextCursor && searchStatus.value !== 'loading') void runSearch(result.keyword, result.page + 1)
}
function stopSearch(): void { searchGeneration++;cancelSearch();searchStatus.value='ready' }

async function reload(): Promise<void> {
  if (searching.value) {
    const word = keyword.value.trim()
    if (word) await runSearch(word, 1)
    return
  }
}

const searchReturn=useSearchReturn(searchRoot,{loading:()=>searchStatus.value==='loading',cancel:stopSearch,refresh:reload})
watch(() => [route.query.q,route.path,active.value],()=>{
 if(!active.value||!searching.value)return
 const word=String(route.query.q||'')
 if(word===requestedKeyword&&searchReturn.fresh())return
 keyword.value=word;void reload()
})
onMounted(()=>{if(searching.value)void startupTask('first_screen',reload)})
</script>
<template>
  <div ref="searchRoot" class="comic-page comic-catalog" :class="{ 'reading-search-page': searching }">
    <header v-if="searching" class="reading-page-heading"><div><h1>{{ t('readingUI.findComics') }}</h1><p>{{ t('readingUI.comicSearchSubtitle') }}</p></div></header>
    <form v-if="searching" class="comic-search" role="search" @submit.prevent="submit">
      <Icon name="search" :size="21" />
      <input v-model="keyword" type="search" enterkeyhint="search" :aria-label="t('comic.search')" :placeholder="t('readingUI.searchComics')" />
      <button type="submit" :disabled="!keyword.trim()">{{ t('readingUI.searchAction') }}</button>
    </form>
    <div v-else class="comic-section-heading">
      <h1>{{ t('comic.findStory') }}</h1>
      <span>{{ t('common.loadedCount', { count: matches.length }) }}</span>
    </div>
    <div v-if="searching" class="reading-search-links"><router-link to="/comic/sources"><Icon name="database" :size="16" />{{ t('readingUI.sources') }}<Icon name="chevron-right" :size="14" /></router-link><router-link to="/comic/categories"><Icon name="panels" :size="16" />{{ t('readingUI.localBooks') }}<Icon name="chevron-right" :size="14" /></router-link></div>
    <div v-if="!searching || found.length" class="comic-filter-chips" :aria-label="t('comic.categories')">
      <button :aria-pressed="!genre" @click="filter('genre', '')">{{ t('common.all') }}</button>
      <button v-for="name in genreOptions" :key="name" :aria-pressed="genre === name" @click="filter('genre', name)">{{ name }}</button>
    </div>
    <div v-if="!searching || found.length" class="comic-filter-status">
      <button :aria-pressed="!complete" @click="filter('complete', '')">{{ t('comic.allStatus') }}</button>
      <button :aria-pressed="complete" @click="filter('complete', '1')">{{ t('comic.complete') }}</button>
      <span v-if="searching">{{ t('common.loadedCount', { count: matches.length }) }}</span>
    </div>
    <ReadingSearchStatus v-if="searching && (searchData||searchStatus==='loading')" :searched="searchData?.searchedSources??0" :pending="searchData?.pendingSources??0" :loading="searchStatus==='loading'" :partial-match="onlyPartialMatch" :failures="failedSources" @stop="stopSearch" />
    <template v-if="notice === 'none'">
      <div class="comic-grid"><ComicCard v-for="item in matches" :key="`${item.sourceKey}:${item.remoteId}`" :series="item" /></div>
      <button v-if="searching && searchData?.hasNext" type="button" class="comic-button comic-load-more" :disabled="searchStatus === 'loading'" @click="more">{{ t(searchData.pendingSources?'sourceRouting.more':'comic.loadMore') }}</button>
    </template>
    <div v-else-if="notice === 'idle'" class="comic-empty">
      <Icon name="search" :size="36" />
      <h2>{{ t('comic.search') }}</h2>
      <p>{{ t('comic.searchHint') }}</p>
    </div>
    <ComicLoadState v-else-if="notice === 'loading' || notice === 'error'" :status="status" :error="loadError" icon="search" :title="t('comic.loadFailed')" @retry="retryCurrent" />
    <ComicLoadState v-else-if="notice === 'guide'" status="empty" icon="source" :title="t('comic.source.emptyTitle')" :hint="t('comic.source.emptyHint')">
      <template #actions>
        <div class="comic-empty-actions">
          <router-link to="/comic/sources" class="comic-button comic-primary">{{ t('comic.source.manage') }}</router-link>
          <router-link v-if="!searching" to="/comic/search" class="comic-button">{{ t('comic.search') }}</router-link>
        </div>
      </template>
    </ComicLoadState>
    <div v-else class="comic-empty">
      <Icon name="search" :size="36" />
      <h2>{{ t(searching?'sourceRouting.noTitleMatch':'comic.noMatches') }}</h2>
      <p>{{ t(searching&&searchData?.pendingSources?'sourceRouting.searchScope':searching&&failedSources.length?'sourceRouting.incompleteSearch':'comic.tryAnother') }}</p>
    </div>
    <details v-if="searching&&related.length" class="reading-search-related"><summary class="cczj-cursor-pointer">{{ t('sourceRouting.otherResults',{count:related.length}) }}</summary><div class="comic-grid"><ComicCard v-for="item in related" :key="item.id" :series="item" /></div></details>
    <p v-if="!searching && browse.nextError.value" class="comic-error" role="alert">{{ browse.nextError.value.message }}</p>
    <button v-if="searching && notice!=='none' && searchData?.hasNext" class="comic-button" :disabled="searchStatus==='loading'" @click="more">{{ t('sourceRouting.more') }}</button>
    <button v-if="!searching && browse.hasNext.value" class="comic-button" :disabled="browse.loading.value" @click="browse.more()">{{ t(browse.loading.value ? 'common.loading' : 'common.loadMore') }}</button>

  </div>
</template>
