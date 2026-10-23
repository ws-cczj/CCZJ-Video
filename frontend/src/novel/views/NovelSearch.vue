<script setup lang="ts">
import {useSearchReturn} from '../../reading/useSearchReturn'
import { ref, computed, onMounted, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useModeViewState } from '../../modes/useModeViewState'
import { startupTask } from '../../diagnostics/startup'
import { useI18n } from 'vue-i18n'
import { useAsyncResource } from '../../reading/asyncResource'
import { searchNovels, listNovelSources, type NovelSearchPage } from '../data/novelLibrary'
import NovelBookCard from '../components/NovelBookCard.vue'
import Icon from '../../components/Icon.vue'
import { Spinner } from '../../components/ui'
import ReadingSearchStatus from '../../reading/ReadingSearchStatus.vue'
import { rankSearchItems, searchRelevance } from '../../reading/searchRelevance'
defineOptions({name:'NovelSearch'})
const { t } = useI18n()
const route = useRoute()
const sources = useAsyncResource({ load: listNovelSources, isEmpty: rows => rows.length === 0 })
void sources.run()
const keyword = ref('')
const submitted=ref(false)
useModeViewState('novel','search',{keyword,submitted})
const page = ref(1)
// 新一轮搜索的代号：迟到的旧响应不能把结果追加进新一轮的累计列表。
let searchId = 0
let accumulated: NovelSearchPage['items'] = []
let cursor = ''
let submittedKeyword = ''
let searchedBefore = 0
const searchedCount = ref(0)
const stopped = ref(false)
function mergeItems(previous:NovelSearchPage['items'],next:NovelSearchPage['items']):NovelSearchPage['items'] {
  return rankSearchItems([...new Map([...previous,...next].map(item=>[item.id,item])).values()],submittedKeyword)
}
const search = useAsyncResource<NovelSearchPage>({
  diagnosticLabel:'novel.search',
  load: () => {
    const id = searchId, current = page.value
    return searchNovels(submittedKeyword, current, current > 1 ? cursor : '',partial=>{
      if(id===searchId){ searchedCount.value=searchedBefore+partial.searchedSources; search.data.value={...partial,items:current<=1?partial.items:mergeItems(accumulated,partial.items)} }
    }).then(result => {
    if (id !== searchId) return result
    searchedCount.value=searchedBefore+result.searchedSources
    accumulated = mergeItems(current <= 1 ? [] : accumulated,result.items)
    return { ...result, items: accumulated }
    })
  },
  onData: result => { cursor = result.nextCursor },
  isEmpty: data => data.items.length === 0 && data.failures.length === 0,
})
const matchedKeyword=computed(()=>search.data.value?.keyword || submittedKeyword)
const searchRoot=ref<HTMLElement|null>(null)
const searchReturn=useSearchReturn(searchRoot,{loading:()=>search.status.value==='loading',cancel:stopSearch,refresh:submit})
const matchingBooks = computed(()=>(search.data.value?.items??[]).filter(item=>searchRelevance(item,matchedKeyword.value)>0))
const relatedBooks = computed(()=>(search.data.value?.items??[]).filter(item=>searchRelevance(item,matchedKeyword.value)===0))
const onlyPartialMatch = computed(()=>matchingBooks.value.length>0&&!matchingBooks.value.some(item=>searchRelevance(item,matchedKeyword.value)>=3))
async function submit(): Promise<void> {
  const value = keyword.value.trim()
  if (!value || (search.status.value === 'loading' && value===submittedKeyword)) return
  submittedKeyword=value
  searchReturn.capture()
  submitted.value=true
  searchId += 1
  page.value = 1
  accumulated = []
  cursor = ''
  searchedBefore=0; searchedCount.value=0; stopped.value=false
  search.data.value=null
  await search.run()
}
function more(): void {
  if (search.status.value === 'loading' || !cursor) return
  page.value += 1
  searchedBefore=searchedCount.value; stopped.value=false
  void search.run()
}
function clear(): void {
  searchId += 1
  page.value = 1
  accumulated = []
  cursor = ''
  keyword.value = ''
  submittedKeyword = ''
  submitted.value=false
  search.cancel()
}
function stopSearch(): void {
  searchId += 1
  search.cancel()
  stopped.value=true
  cursor=search.data.value?.nextCursor??''
  // Partial results survive cancellation; no late response can append to them.
  accumulated=search.data.value?.items??[]
  search.status.value='ready'
}
function retrySearch(): void {
  if (search.error.value?.code === 'CONFLICT') {
    searchId += 1
    page.value = 1
    accumulated = []
    cursor = ''
  }
  void search.run()
}
onMounted(()=>{void startupTask('first_screen',async()=>{if(typeof route.query.q==='string'&&route.query.q.trim()){keyword.value=route.query.q;await submit()}else if(submitted.value&&keyword.value.trim())await submit()})})
watch(()=>route.query.q,value=>{if(typeof value==='string'&&value.trim()&&value!==submittedKeyword){keyword.value=value;void submit()}})
</script>
<template>
  <div ref="searchRoot" class="novel-page reading-search-page"><header class="reading-page-heading"><div><h1>{{ t('readingUI.findBooks') }}</h1><p>{{ t('readingUI.searchSubtitle') }}</p></div></header>
    <form class="novel-search" role="search" @submit.prevent="submit"><Icon name="search" :size="20" /><input v-model="keyword" type="search" autocomplete="off" @keydown.enter="($event.isComposing || $event.keyCode===229) && $event.preventDefault()" enterkeyhint="search" :placeholder="t('readingUI.searchBooks')" :aria-label="t('novel.search')" /><button v-if="keyword" :aria-label="t('common.clearAll')" @click="clear" type="button"><Icon name="close" :size="18" /></button><button type="submit" :disabled="!keyword.trim()">{{ t('readingUI.searchAction') }}</button></form>
    <p v-if="search.data.value?.keyword && search.data.value.keyword!==submittedKeyword" class="novel-search-resolved" role="status">{{t('novel.searchResolved',{keyword:search.data.value.keyword})}}</p>
    <div class="reading-search-links"><router-link to="/novel/sources"><Icon name="database" :size="16" />{{ t('readingUI.sources') }}<Icon name="chevron-right" :size="14" /></router-link><router-link to="/novel/categories"><Icon name="book-open" :size="16" />{{ t('readingUI.localBooks') }}<Icon name="chevron-right" :size="14" /></router-link></div>
    <div v-if="sources.error.value" class="novel-alert" role="status"><span>{{ sources.error.value.message }}</span><button class="novel-text-button" @click="sources.retry()">{{ t('common.retry') }}</button></div>
    <ReadingSearchStatus v-if="search.status.value === 'loading' && !search.data.value" :searched="0" :pending="0" :loading="true" :failures="[]" @stop="stopSearch" />
    <div v-else-if="search.status.value === 'error'" class="novel-empty"><Icon name="warning" :size="36" /><h2>{{ search.error.value?.message || t('common.unknownError') }}</h2><button class="novel-button" @click="retrySearch">{{ t('common.retry') }}</button></div>
    <div v-else-if="search.status.value === 'idle' && sources.status.value === 'empty'" class="novel-empty reading-empty"><span class="reading-empty-icon"><Icon name="database" :size="32" /></span><h2>{{ t('librarySources.emptyTitle') }}</h2><p>{{ t('readingUI.sourceEmptyHint') }}</p><router-link class="novel-button novel-button-primary" to="/novel/sources">{{ t('readingUI.addSource') }}</router-link></div>
    <div v-else-if="search.status.value === 'idle'" class="novel-empty reading-empty"><span class="reading-empty-icon"><Icon name="search" :size="32" /></span><h2>{{ t('readingUI.searchBooks') }}</h2><p>{{ t('novel.searchPrompt') }}</p></div>
    <template v-else>
      <div class="novel-section-heading"><h2>{{ t('novel.searchResults') }}</h2><span>{{ t('novel.bookCount',{n:matchingBooks.length}) }}</span></div>
      <ReadingSearchStatus :searched="searchedCount" :pending="search.data.value?.pendingSources??0" :loading="search.status.value==='loading'" :partial-match="onlyPartialMatch" :failures="search.data.value?.failures??[]" @stop="stopSearch" />
      <p v-if="stopped" role="status">{{ t('sourceRouting.searchStopped') }}</p>
      <div v-if="matchingBooks.length" class="novel-row-grid"><NovelBookCard v-for="book in matchingBooks" :key="book.id" :book="book" compact /></div>
      <div v-else-if="search.status.value!=='loading'" class="novel-empty"><Icon name="search" :size="36" /><h2>{{ t('sourceRouting.noTitleMatch') }}</h2><p>{{ t(search.data.value?.pendingSources?'sourceRouting.searchScope':search.data.value?.failures.length?'sourceRouting.incompleteSearch':'novel.tryKeyword') }}</p></div>
      <details v-if="relatedBooks.length" class="reading-search-related"><summary class="cczj-cursor-pointer">{{ t('sourceRouting.otherResults',{count:relatedBooks.length}) }}</summary><div class="novel-row-grid"><NovelBookCard v-for="book in relatedBooks" :key="book.id" :book="book" compact /></div></details>
      <div v-if="search.status.value === 'loading'" class="novel-load-more"><Spinner size="sm" /></div>
      <div v-else-if="search.data.value && search.data.value.hasNext" class="novel-load-more"><button class="novel-button" @click="more">{{ t(search.data.value.pendingSources?'sourceRouting.more':'novel.loadMore') }}</button></div>
    </template>
  </div>
</template>
