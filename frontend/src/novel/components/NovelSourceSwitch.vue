<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { useSourceSwitch } from '../../reading/useSourceSwitch'
import { Spinner } from '../../components/ui'
const props=defineProps<{itemId:number;chapterId:number}>()
const emit=defineEmits<{leaving:[]}>()
const {t}=useI18n()
const {sources,match,candidates,targets,failed,more,choose,catalog}=useSourceSwitch('novel',()=>props.itemId,()=>props.chapterId,()=>emit('leaving'))
</script>
<template>
  <div class="novel-source-switch"><p>{{ t('sourceRouting.startNovel') }}</p><Spinner v-if="sources.status.value==='loading'&&!candidates.length" :label="t('sourceRouting.finding')"/><p v-if="sources.error.value" role="alert">{{ sources.error.value.message }}</p>
    <button v-for="book in candidates" :key="book.id" class="novel-switch-book" :disabled="match.status.value==='loading'" @click="match.run(book.id)"><span>{{ book.sourceName }}</span><strong>{{ book.title }}</strong><small>{{ t('sourceRouting.compareNovel') }} →</small></button>
    <p v-if="failed" role="status">{{ t('sourceRouting.failedSources',{count:failed}) }} <button @click="sources.retry()">{{ t('common.retry') }}</button></p><p v-if="sources.status.value!=='loading'&&!candidates.length">{{ t('sourceRouting.nothing') }}</p><button v-if="sources.data.value?.nextCursor" class="novel-text-button" :disabled="sources.status.value==='loading'" @click="more">{{ t('sourceRouting.more') }}</button>
    <Spinner v-if="match.status.value==='loading'"/><p v-if="match.error.value" role="alert">{{ match.error.value.message }}</p><section v-if="match.data.value" class="novel-switch-result"><h3>{{ t(`sourceRouting.${match.data.value.status}`) }}</h3><p>{{ match.data.value.evidence }}</p><button v-for="target in targets" :key="target.id" class="novel-button" @click="choose(target)">{{ target.title }}</button><button class="novel-text-button" @click="catalog">{{ t('sourceRouting.openCatalog') }}</button></section>
  </div>
</template>
<style scoped>
.novel-source-switch>p,.novel-switch-result p{font-size:12px;opacity:.65;line-height:1.7}.novel-switch-book{@apply cczj-grid;grid-template-columns:1fr auto;width:100%;text-align:left;gap:7px 12px;border:0;border-bottom:1px solid #92826a18;background:transparent;color:inherit;padding:17px 0;cursor:pointer}.novel-switch-book span{font-size:11px;opacity:.55}.novel-switch-book strong{font-size:14px;font-weight:500;grid-column:1}.novel-switch-book small{font-size:11px;grid-column:2;grid-row:1/3;align-self:center;color:var(--novel-red,#986344)}.novel-switch-result{background:#b7996c0d;padding:18px;margin-top:15px;border-radius:10px}.novel-switch-result h3{font-size:14px}.novel-switch-result button{@apply cczj-block;margin-top:12px}
</style>
