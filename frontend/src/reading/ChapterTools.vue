<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '../components/Icon.vue'
import { Button, NumberField, Segment } from '../components/ui'
const props=withDefaults(defineProps<{ mode?:'novel'|'comic'; query:string; descending:boolean; count:number; current?:boolean; refreshing?:boolean; refreshable?:boolean }>(),{mode:'novel'})
const emit=defineEmits<{(e:'update:query',value:string):void;(e:'update:descending',value:boolean):void;(e:'locate',value:number):void;(e:'current'):void;(e:'latest'):void;(e:'refresh'):void}>()
const {t}=useI18n(),jump=ref(1)
function label(novel:string,comic:string){return t(props.mode==='comic'?comic:novel)}
</script>
<template>
 <div class="novel-directory-tools cczj-grid cczj-gap-5">
  <label class="novel-directory-search cczj-flex cczj-items-center cczj-gap-4"><Icon name="search" :size="17"/><input type="search" :value="query" :aria-label="label('novel.chapterSearch','readingUI.episodeSearch')" :placeholder="label('novel.chapterSearchPlaceholder','readingUI.episodeSearch')" @input="emit('update:query',($event.target as HTMLInputElement).value)"/></label>
  <div class="cczj-flex cczj-items-center cczj-flex-wrap cczj-gap-4">
   <Segment :model-value="descending?'desc':'asc'" :options="[{value:'asc',label:label('novel.ascending','comic.ascending')},{value:'desc',label:label('novel.descending','comic.descending')}]" size="sm" @update:model-value="emit('update:descending',$event==='desc')"/>
   <Button variant="text" :disabled="!current" @click="emit('current')"><Icon name="bookmark" :size="15"/>{{label('novel.currentChapter','readingUI.currentEpisode')}}</Button>
   <Button variant="text" :disabled="!count" @click="emit('latest')">{{label('novel.latestChapter','comic.directoryLatest')}}</Button>
   <div class="novel-directory-jump cczj-flex cczj-items-center cczj-gap-3"><span>{{label('novel.chapterNumberInput','comic.directoryNumber')}}</span><NumberField v-model="jump" :min="1" :max="Math.max(1,count)" :ariaLabel="label('novel.chapterNumberInput','comic.directoryNumber')" @keydown.enter="emit('locate',jump)"/><Button variant="text" :aria-label="label('novel.locateChapter','comic.directoryLocate')" @click="emit('locate',jump)"><Icon name="chevron-right" :size="16"/></Button></div>
   <Button v-if="refreshable" variant="text" :loading="refreshing" :aria-label="t('common.refresh')" @click="emit('refresh')"><Icon name="refresh" :size="16"/>{{t('common.refresh')}}</Button>
  </div>
 </div>
</template>
<style scoped>
.novel-directory-tools{grid-template-columns:minmax(180px,1fr);padding:16px 0 20px;}
.novel-directory-search{padding:0 14px;border:1px solid var(--directory-line,var(--novel-line));background:var(--directory-surface,var(--novel-surface));border-radius:10px;color:var(--directory-muted,var(--novel-muted));max-width:420px;}
.novel-directory-search:focus-within{border-color:var(--directory-accent,var(--novel-red));box-shadow:0 0 0 3px var(--accent-alpha-10);}
.novel-directory-search input{font:inherit;font-size:calc(13rem / 14);color:var(--directory-ink,var(--novel-ink));background:transparent;border:0;min-height:44px;@apply cczj-flex-1 cczj-min-w-0;}

.novel-directory-jump{font-size:calc(12rem / 14);color:var(--directory-muted,var(--novel-muted));}
.novel-directory-tools :deep(.ui-btn){min-height:40px;}
@media(min-width:1000px){.novel-directory-tools{grid-template-columns:minmax(180px,240px) minmax(0,1fr);@apply cczj-items-start;}}
@media(max-width:600px){.novel-directory-search{max-width:none;}.novel-directory-jump{order:2;}}
</style>
