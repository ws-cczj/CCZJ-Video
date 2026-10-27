<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { useSourceSwitch } from '../../reading/useSourceSwitch'
import { Spinner } from '../../components/ui'
const props=defineProps<{itemId:number;chapterId:number}>()
const emit=defineEmits<{leaving:[]}>()
const {t}=useI18n()
const {sources,match,candidates,targets,failed,more,choose,catalog}=useSourceSwitch('comic',()=>props.itemId,()=>props.chapterId,()=>emit('leaving'))
</script>
<template>
  <div class="comic-source-switch"><p>{{ t('sourceRouting.startComic') }}</p><Spinner v-if="sources.status.value==='loading'&&!candidates.length" :label="t('sourceRouting.finding')"/><p v-if="sources.error.value" role="alert">{{ sources.error.value.message }}</p>
    <div class="comic-switch-grid"><button v-for="series in candidates" :key="series.id" :disabled="match.status.value==='loading'" @click="match.run(series.id)"><small>{{ series.sourceName }}</small><strong>{{ series.title }}</strong><span>{{ t('sourceRouting.compareComic') }} ✦</span></button></div>
    <p v-if="failed" role="status">{{ t('sourceRouting.failedSources',{count:failed}) }} <button @click="sources.retry()">{{ t('common.retry') }}</button></p><p v-if="sources.status.value!=='loading'&&!candidates.length">{{ t('sourceRouting.nothing') }}</p><button v-if="sources.data.value?.nextCursor" class="comic-button" :disabled="sources.status.value==='loading'" @click="more">{{ t('sourceRouting.more') }}</button>
    <Spinner v-if="match.status.value==='loading'"/><p v-if="match.error.value" role="alert">{{ match.error.value.message }}</p><section v-if="match.data.value" class="comic-switch-result"><h3>{{ t(`sourceRouting.${match.data.value.status}`) }}</h3><p>{{ match.data.value.evidence }}</p><button v-for="target in targets" :key="target.id" class="comic-button" @click="choose(target)">{{ target.title }}</button><button class="comic-button" @click="catalog">{{ t('sourceRouting.openCatalog') }}</button></section>
  </div>
</template>
<style scoped>
.comic-source-switch>p,.comic-switch-result p{font-size:calc(12rem / 14);line-height:1.7;opacity:.65}.comic-switch-grid{@apply cczj-grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:12px;margin:18px 0}.comic-switch-grid button{@apply cczj-flex cczj-flex-col;gap:12px;text-align:left;border:1px solid #ac8cda28;border-radius:12px;padding:17px;background:#ac8cda0b;color:inherit;cursor:pointer}.comic-switch-grid small{font-size:calc(11rem / 14);opacity:.55}.comic-switch-grid strong{font-size:calc(14rem / 14);overflow-wrap:anywhere}.comic-switch-grid span{font-size:calc(11rem / 14);color:var(--comic-accent,#8b65d0)}.comic-switch-result{margin-top:20px;padding:18px;border-radius:12px;background:#ac8cda0a}.comic-switch-result h3{font-size:calc(15rem / 14)}.comic-switch-result button{@apply cczj-block;margin-top:10px}
</style>
