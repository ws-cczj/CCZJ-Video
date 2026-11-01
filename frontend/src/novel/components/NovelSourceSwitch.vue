<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { rememberNovelBookTab } from '../bookViewState'
import { useSourceSwitch } from '../../reading/useSourceSwitch'
import { Button, Spinner } from '../../components/ui'
const props=defineProps<{itemId:number;chapterId:number}>()
const emit=defineEmits<{leaving:[]}>()
const {t}=useI18n()
const router=useRouter()
const {sources,match,candidates,targets,failed,more,choose,preview,committing,commitError,sourceCache,sourceState,expand}=useSourceSwitch('novel',()=>props.itemId,()=>props.chapterId,()=>emit('leaving'))
async function openBook(id:number):Promise<void>{if(!candidates.value.some(book=>book.id===id)||committing.value)return;rememberNovelBookTab(id,'chapters');emit('leaving');await router.push(`/novel/book/${id}`)}
</script>
<template>
  <div class="novel-source-switch"><p>{{ t('sourceRouting.startNovel') }}</p><Spinner v-if="sources.status.value==='loading'&&!candidates.length" :label="t('sourceRouting.finding')"/><p v-if="sources.error.value" role="alert">{{ sources.error.value.message }}</p>
    <p v-if="sourceCache?.cached.data.value" role="status">{{ t('bookSourceCache.summary',{count:sourceCache.cached.data.value.candidates.length,available:sourceCache.cached.data.value.available}) }}</p><p v-if="sourceCache?.probe.error.value">{{ t('bookSourceCache.updateFailed') }}</p><Button v-if="sources.status.value==='loading'" variant="text" @click="sources.cancel()">{{ t('sourceRouting.stopSearch') }}</Button>
    <div v-for="book in candidates" :key="book.id" class="novel-switch-book"><strong>{{ book.title }}</strong><p>{{ book.author }} · {{ book.sourceName }} · {{ t(`bookSourceCache.states.${sourceState(book.id)}`) }}</p><div class="cczj-flex cczj-flex-wrap cczj-gap-6"><Button :disabled="match.status.value==='loading'||committing||sourceState(book.id)==='paused'" @click="match.run(book.id)">{{ t('sourceRouting.compareNovel') }}</Button><Button variant="text" :disabled="committing" @click="openBook(book.id)">{{ t('sourceRouting.openCatalog') }}</Button></div></div>
    <p v-if="failed" role="status">{{ t('sourceRouting.failedSources',{count:failed}) }} <Button variant="text" @click="sources.retry()">{{ t('common.retry') }}</Button></p><p v-if="sources.status.value!=='loading'&&sourceCache?.probe.status.value!=='loading'&&!candidates.length">{{ t('sourceRouting.nothing') }}</p><Button v-if="sources.data.value?.nextCursor" variant="text" :disabled="sources.status.value==='loading'" @click="more">{{ t('sourceRouting.more') }}</Button>
    <Button v-if="!sources.data.value?.nextCursor" variant="text" :disabled="sources.status.value==='loading'" @click="expand">{{ t('sourceRouting.more') }}</Button>
    <Spinner v-if="match.status.value==='loading'"/><p v-if="match.error.value" role="alert">{{ match.error.value.message }}</p>
    <section v-if="match.data.value&&match.status.value!=='loading'" class="novel-switch-result"><h3>{{ t(`sourceRouting.${match.data.value.status}`) }}</h3><p>{{ match.data.value.evidence }}</p><p v-if="match.data.value.identity==='provisional'">{{ t('sourceSupply.reviewIdentity') }}</p>
      <Button v-for="target in targets" :key="target.id" :disabled="!target.available||preview.status.value==='loading'||committing" @click="preview.run(target.id)">{{ t('sourceSupply.verifyChapter',{chapter:target.title}) }}</Button>
      <Spinner v-if="preview.status.value==='loading'" :label="t('sourceSupply.preparing')"/>
      <Button v-if="preview.status.value==='loading'" variant="text" @click="preview.cancel();preview.data.value=null">{{ t('common.cancel') }}</Button>
      <p v-if="preview.error.value" role="alert">{{ preview.error.value.message }} · {{ t('sourceSupply.originalKept') }}</p>
      <div v-if="preview.status.value==='ready'&&preview.data.value?.chapter" class="novel-switch-preview"><h4>{{ preview.data.value.item?.sourceName }} · {{ preview.data.value.chapter.title }}</h4><p>{{ t('sourceSupply.targetVerified') }}</p><blockquote>{{ preview.data.value.text?.paragraphs.slice(0,2).join('\n').slice(0,180) }}</blockquote><Button variant="primary" :loading="committing" @click="choose(preview.data.value!.chapter!)">{{ t('sourceSupply.commitSwitch') }}</Button></div>
      <p v-if="commitError" role="alert">{{ commitError }}</p><Button variant="text" :disabled="committing" @click="openBook(match.data.value.targetItemId)">{{ t('sourceRouting.openCatalog') }}</Button>
    </section>
  </div>
</template>
<style scoped>
.novel-source-switch p,.novel-switch-preview blockquote{font-size:calc(12rem / 14);color:var(--novel-muted);line-height:1.7;overflow-wrap:anywhere}.novel-switch-book{border-bottom:1px solid var(--novel-line);padding:17px 0}.novel-switch-book strong{font-size:calc(14rem / 14);font-weight:500}.novel-switch-result{background:var(--novel-surface);padding:18px;margin-top:15px;border-radius:10px}.novel-switch-result h3,.novel-switch-preview h4{font-size:calc(14rem / 14);overflow-wrap:anywhere}.novel-switch-result .ui-btn{margin:8px 8px 0 0;white-space:normal;text-align:left}.novel-switch-preview blockquote{margin:12px 0;padding-left:12px;border-left:2px solid var(--novel-line)}
</style>
