<script setup lang="ts">
import { computed,onActivated,onDeactivated,ref,watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { comicTrending } from '../../api/comicBrowse'
import { useAsyncResource } from '../../reading/asyncResource'
import { Button,Segment,Spinner } from '../../components/ui'
import ComicCover from './ComicCover.vue'
import Icon from '../../components/Icon.vue'
const {t}=useI18n(),board=ref<'japan'|'china'>('japan')
const resource=useAsyncResource({diagnosticLabel:'comic.trending',load:()=>comicTrending(board.value)})
const page=computed(()=>resource.data.value?.board===board.value?resource.data.value:null)
const source=computed(()=>`https://manga.bilibili.com/ranking/${board.value==='japan'?0:1}`)
let lastChecked=0
watch(board,()=>{lastChecked=Date.now();void resource.run()},{immediate:true})
onActivated(()=>{if(resource.status.value==='idle'||Date.now()-lastChecked>30*60*1000){lastChecked=Date.now();void resource.run()}})
onDeactivated(()=>resource.cancel())
</script>
<template>
 <section class="manga-trending manga-section" :aria-busy="resource.status.value==='loading'">
  <header class="manga-section-heading"><div><span class="manga-section-index" aria-hidden="true">↗</span><h2>{{t('comicRoom.trending')}}</h2></div><Segment v-model="board" :options="[{value:'japan',label:t('comicRoom.japanRank')},{value:'china',label:t('comicRoom.chinaRank')}]"/></header>
  <p class="manga-trend-note">{{t('comicRoom.trendingHint')}}</p>
  <div v-if="resource.error.value" class="comic-notice" role="status">{{resource.error.value.message}}<Button variant="text" @click="resource.retry()">{{t('common.retry')}}</Button></div>
  <Spinner v-if="!page&&resource.status.value==='loading'" :label="t('common.loading')"/>
  <ol v-if="page" class="manga-trend-grid cczj-grid cczj-gap-8">
   <li v-for="(item,index) in page.items.slice(0,12)" :key="item.url" class="cczj-min-w-0"><router-link :to="{path:'/comic/search',query:{q:item.title}}" class="manga-trend-card cczj-grid cczj-gap-4" :aria-label="t('comicRoom.findTrending',{title:item.title})"><div class="manga-trend-cover cczj-relative"><ComicCover :src="item.cover" :title="item.title"/><span class="manga-trend-rank">{{String(index+1).padStart(2,'0')}}</span></div><strong>{{item.title}}</strong><small>{{item.genres||item.author}}</small><span class="manga-trend-search cczj-flex cczj-items-center cczj-gap-3"><Icon name="search" :size="13"/>{{t('comicRoom.findSources')}}</span></router-link></li>
  </ol>
  <footer class="manga-source-caption cczj-flex cczj-flex-wrap cczj-gap-5"><a :href="source" target="_blank" rel="noopener noreferrer">{{t('comicRoom.rankAttribution')}}</a><span v-if="page">{{t(page.stale?'comicRoom.staleRank':page.fromCache?'comicRoom.cached':'comicRoom.updated')}} {{new Date(page.fetchedAt*1000).toLocaleString()}}</span><span v-if="page?.warning" role="status">{{page.warning}}</span></footer>
 </section>
</template>
<style scoped>
.manga-trend-grid{grid-template-columns:repeat(6,minmax(0,1fr));list-style:none;padding:0;margin:20px 0;}
.manga-trend-note{font-size:12px;color:var(--comic-muted);line-height:1.7;}
.manga-trend-cover{aspect-ratio:3/4;@apply cczj-overflow-hidden;border-radius:6px;background:var(--comic-soft);}
.manga-trend-cover :deep(img){@apply cczj-w-full cczj-h-full;object-fit:cover;}
.manga-trend-rank{position:absolute;bottom:0;left:0;padding:5px 9px;background:var(--comic-ink);color:var(--comic-bg);font:bold 18px Georgia,serif;border-radius:0 6px 0 0;}
.manga-trend-card strong{font-size:14px;line-height:1.5;display:-webkit-box;-webkit-line-clamp:2;-webkit-box-orient:vertical;@apply cczj-overflow-hidden;}
.manga-trend-card small{font-size:11px;color:var(--comic-muted);white-space:nowrap;text-overflow:ellipsis;@apply cczj-overflow-hidden;}
.manga-trend-search{font-size:11px;color:var(--comic-accent);}
.manga-trend-card:focus-visible{outline:2px solid var(--comic-accent);outline-offset:5px;}
@media(max-width:760px){.manga-trend-grid{grid-template-columns:repeat(3,minmax(0,1fr));gap:18px 12px;}.manga-trending .manga-section-heading{@apply cczj-flex-wrap cczj-gap-5;}}
</style>
