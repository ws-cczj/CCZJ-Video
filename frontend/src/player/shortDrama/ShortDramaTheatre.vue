<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { storeToRefs } from 'pinia'
import { useI18n } from 'vue-i18n'
import { Button, Empty, Spinner } from '../../components/ui'
import Icon from '../../components/Icon.vue'
import VideoCard from '../../components/VideoCard.vue'
import { useSourceStore } from '../../stores/source'
import { useVideoList } from '../../composables/useVideoList'
import type { Video } from '../../types'

const props = defineProps<{active:boolean;mobile:boolean}>()
const emit = defineEmits<{play:[video:Video];random:[]}>()
const {t}=useI18n()
const {currentSourceKey}=storeToRefs(useSourceStore())
const list=useVideoList({enabled:()=>props.active})
const draft=ref(''), keyword=ref(''), recent=ref(false)
const filters=computed(()=>({short_drama:true,type_id:'',year:'',area:'',keyword:keyword.value,recent_days:recent.value?7:0}))
let signature='', pendingPage=1
function load(page=1):void {
  if(!props.active || !currentSourceKey.value)return
  const next=JSON.stringify([currentSourceKey.value,filters.value])
  if(next!==signature){list.reset();signature=next}
  pendingPage=page
  void list.load(currentSourceKey.value,filters.value,page,24)
}
function search():void {
  const value=draft.value.trim()
  if(value===keyword.value)load()
  else keyword.value=value
}
watch([currentSourceKey,()=>props.active],([key,active])=>{
  if(!key){list.reset();signature='';return}
  if(active && (list.interrupted || !list.videos.length || signature!==JSON.stringify([key,filters.value])))load()
},{immediate:true,flush:'post'})
watch([keyword,recent],()=>load())
</script>

<template>
  <div class="drama-theatre cczj-h-full cczj-overflow-y-auto" :class="{'drama-theatre-mobile':mobile}">
    <header class="theatre-toolbar cczj-flex cczj-items-center cczj-justify-between cczj-flex-wrap cczj-gap-8">
      <nav v-if="mobile" class="theatre-mobile-tabs cczj-flex cczj-items-center cczj-gap-6" :aria-label="t('shortDrama.title')">
        <Button variant="text" :aria-pressed="false" @click="emit('random')">{{t('shortDrama.recommendTab')}}</Button>
        <Button variant="text" class="theatre-tab-active" :aria-pressed="true">{{t('shortDrama.theatre')}}</Button>
      </nav>
      <div v-else class="cczj-flex cczj-items-baseline cczj-gap-6"><h1>{{t('shortDrama.title')}}</h1><span class="theatre-count">{{t('shortDrama.titleCount',{count:list.total})}}</span></div>
      <div class="theatre-actions cczj-flex cczj-items-center cczj-gap-6">
        <form class="theatre-search cczj-flex cczj-items-center cczj-gap-4" role="search" @submit.prevent="search">
          <Icon name="search" :size="17"/>
          <input v-model="draft" type="search" :placeholder="t('shortDrama.searchPlaceholder')" :aria-label="t('shortDrama.searchPlaceholder')"/>
          <Button variant="text" type="submit">{{t('sidebar.search')}}</Button>
        </form>
        <Button class="theatre-random" variant="primary" :aria-label="t('shortDrama.randomPlay')" :disabled="!currentSourceKey" @click="emit('random')"><Icon name="play" :size="16"/><span>{{t('shortDrama.randomPlay')}}</span></Button>
      </div>
    </header>
    <nav class="theatre-filters cczj-flex cczj-items-center cczj-gap-8" :aria-label="t('shortDrama.catalogFilter')">
      <Button variant="text" :aria-pressed="!recent" @click="recent=false">{{t('shortDrama.allDramas')}}</Button>
      <Button variant="text" :aria-pressed="recent" @click="recent=true">{{t('shortDrama.recentDramas')}}</Button>
    </nav>
    <div v-if="list.loading && !list.videos.length" class="theatre-empty cczj-flex cczj-items-center cczj-justify-center" role="status"><Spinner :label="t('common.loading')"/></div>
    <Empty v-else-if="!list.videos.length" class="theatre-empty" :title="list.error ? t('shortDrama.loadFailed') : t('shortDrama.empty')" :description="list.error || t(!currentSourceKey ? 'shortDrama.noSource' : keyword ? 'shortDrama.noMatches' : 'shortDrama.emptyHint')">
      <Button v-if="currentSourceKey" @click="load()">{{t('common.retry')}}</Button>
      <router-link v-else to="/sources">{{t('sidebar.sources')}}</router-link>
    </Empty>
    <div v-else class="theatre-grid cczj-grid cczj-gap-8">
      <VideoCard v-for="item in list.videos" :key="item.vod_id" :video="item" :data-vod-id="item.vod_id" @click="emit('play',item)">
        <template #meta><span class="theatre-meta cczj-truncate">{{[item.type_name,item.vod_year].filter(Boolean).join(' · ')}}</span></template>
      </VideoCard>
    </div>
    <footer v-if="list.videos.length" class="theatre-footer cczj-flex cczj-flex-col cczj-items-center cczj-gap-6">
      <p v-if="list.error" role="alert">{{list.error}}</p>
      <Button v-if="list.error || list.nextCursor" :loading="list.loading" @click="load(list.error ? pendingPage : list.page+1)">{{t(list.error?'common.retry':'shortDrama.loadMore')}}</Button>
      <span v-else class="theatre-count">{{t('shortDrama.titleCount',{count:list.total})}}</span>
    </footer>
  </div>
</template>

<style scoped>
.drama-theatre { padding: 24px 30px; scrollbar-width: thin; }
.theatre-toolbar h1 { margin: 0; font-size: 24px; font-weight: 600; }
.theatre-count,.theatre-meta { color: var(--text-muted); font-size: 12px; }
.theatre-search { padding-left: 12px; border: 1px solid var(--border-strong); border-radius: 10px; color: var(--text-secondary); background: var(--bg-card); }
.theatre-search:focus-within { border-color: var(--accent); outline: 1px solid var(--accent); outline-offset: 0; }
.theatre-search input:focus-visible { outline: 0; }
.theatre-search input { @apply cczj-flex-1 cczj-min-w-0; width: 220px; border: 0; padding: 10px 0; background: transparent; color: var(--text-primary); font: inherit; font-size: 13px; }
.theatre-search .ui-btn,.theatre-random { min-height: 42px; }
.theatre-search .ui-btn { font-weight: 400; }
.theatre-random { border-radius: 10px; padding: 8px 14px; }
.theatre-filters { margin: 18px 0 24px; border-bottom: 1px solid var(--border); }
.theatre-filters .ui-btn { @apply cczj-relative; padding: 12px 0; min-height: 44px; color: var(--text-secondary); font-weight: 400; border-radius: 0; }
.theatre-filters .ui-btn[aria-pressed=true] { color: var(--text-primary); font-weight: 600; }
.theatre-filters .ui-btn[aria-pressed=true]::after { content: ''; @apply cczj-absolute cczj-left-0 cczj-right-0 cczj-bottom-0; height: 2px; background: currentColor; }
.theatre-grid { grid-template-columns: repeat(auto-fill,minmax(160px,1fr)); }
.theatre-grid :deep(.video-card) { border-radius: 12px; }
.theatre-grid :deep(.video-card:hover) { transform: translateY(-3px); box-shadow: none; }
.theatre-grid :deep(.poster-badge) { font-weight: 400; }
.theatre-grid :deep(.info) { padding: 12px; }
.theatre-grid :deep(.title) { font-weight: 500; font-size: 14px; }
.theatre-empty { min-height: 280px; }
.theatre-footer { padding: 32px 0 12px; }
.theatre-footer p { margin: 0; font-size: 13px; color: var(--text-secondary); }
.drama-theatre-mobile { padding: 8px 16px 20px; }
.drama-theatre-mobile .theatre-toolbar { gap: 12px; }
.drama-theatre-mobile .theatre-toolbar,.drama-theatre-mobile .theatre-actions { @apply cczj-w-full; }
.drama-theatre-mobile .theatre-actions { gap: 8px; }
.drama-theatre-mobile .theatre-search { @apply cczj-flex-1 cczj-min-w-0; }
.drama-theatre-mobile .theatre-search input { width: 80px; }
.drama-theatre-mobile .theatre-random { min-width: 44px; padding: 8px 12px; }
.drama-theatre-mobile .theatre-random span { @apply cczj-hidden; }
.theatre-mobile-tabs .ui-btn { @apply cczj-relative; min-height: 44px; padding: 4px 0; font-size: 17px; font-weight: 400; }
.theatre-mobile-tabs .theatre-tab-active { color: var(--text-primary); font-weight: 600; }
.theatre-mobile-tabs .theatre-tab-active::after { content: ''; @apply cczj-absolute cczj-bottom-0; width: 18px; height: 2px; left: calc(50% - 9px); background: currentColor; }
.drama-theatre-mobile .theatre-filters { margin: 8px 0 16px; }
.drama-theatre-mobile .theatre-grid { grid-template-columns: repeat(2,minmax(0,1fr)); gap: 12px; }
</style>
