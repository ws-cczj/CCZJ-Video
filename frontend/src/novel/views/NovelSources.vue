<script setup lang="ts">
import { isMobileRuntime } from '../../api/runtime'
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useBuiltinSources } from '../../reading/useBuiltinSources'
import { openExternalURL } from '../../api/runtime'
import { normalizeApiError } from '../../api/app'
import Icon from '../../components/Icon.vue'
import { Button, Select, Spinner } from '../../components/ui'

const { t }=useI18n()
const { query,rows,status,error,data,changing,actionError,toggle,retry,run,stateLabel,inspection,inspecting,inspect,cancelInspection }=useBuiltinSources('novel')
const section=ref<string|number>('active'),capability=ref<string|number>('all'),linkError=ref('')
const sections=computed(()=>['all','active','reserve','attention'].map(value=>({value,label:t(`sourceSupply.sections.${value}`,{count:rows.value.filter(row=>value==='all'||row.section===value).length})})))
const capabilities=computed(()=>['all','discover','content'].map(value=>({value,label:t(`sourceSupply.filters.${value}`)})))
const filtered=computed(()=>rows.value.filter(row=>(section.value==='all'||row.section===section.value)&&(capability.value==='all'||row.capabilities.includes(String(capability.value)))))
const page=ref(1)
const pageCount=computed(()=>Math.max(1,Math.ceil(filtered.value.length/10)))
const visible=computed(()=>filtered.value.slice((page.value-1)*10,page.value*10))
watch([query,section,capability],()=>{page.value=1})
watch(pageCount,count=>{page.value=Math.min(page.value,count)})
function turnPage(next:number):void {page.value=Math.max(1,Math.min(pageCount.value,next));document.querySelector('.cczj-novel-shell .main-content')?.scrollTo({top:0})}
const time=(stamp:number)=>new Date(stamp*1000).toLocaleString()
async function open(url:string):Promise<void>{linkError.value='';try{await openExternalURL(url)}catch(error){linkError.value=normalizeApiError(error).message}}
</script>
<template>
  <section class="novel-page novel-source-directory">
    <header v-if="!isMobileRuntime()"><h1>{{ t('sourceSupply.title') }}</h1></header>
    <div class="room-search"><Icon name="search" :size="20"/><input v-model="query" type="search" :aria-label="t('sourceRouting.findNovel')" :placeholder="t('sourceSupply.find')"/><Button variant="text" :loading="status==='loading'" @click="run()">{{ t('sourceSupply.refreshRecords') }}</Button></div>
    <nav class="source-tabs cczj-flex cczj-gap-6" :aria-label="t('sourceSupply.title')"><Button v-for="option in sections" :key="option.value" variant="text" :aria-pressed="section===option.value" @click="section=option.value">{{ option.label }}</Button></nav>
    <div class="source-filters cczj-flex cczj-items-center cczj-justify-between cczj-gap-6"><span>{{ t('sourceSupply.ranking') }}</span><Select v-model="capability" :options="capabilities" :aria-label="t('sourceSupply.filterCapability')" size="sm" inline/></div>
    <p v-if="actionError||linkError" class="novel-alert" role="alert">{{ actionError||linkError }}</p>
    <div v-if="inspecting" class="source-inspection cczj-flex cczj-items-center cczj-flex-wrap cczj-gap-6" role="status"><Spinner inline size="sm"/>{{ t('sourceSupply.checking') }}<Button variant="text" @click="cancelInspection">{{ t('common.cancel') }}</Button></div>
    <p v-if="inspection.error.value" class="novel-alert" role="alert">{{ inspection.error.value.message }}</p>
    <details v-if="inspection.data.value&&!inspecting" class="source-inspection" open><summary>{{ t('sourceSupply.checkResult',{name:rows.find(row=>row.sourceKey===inspection.data.value?.sourceKey)?.name??''}) }}</summary><p>{{ t('sourceSupply.sampleScope') }}</p><ul><li v-for="stage in inspection.data.value.stages" :key="stage?.name">{{ t(`librarySources.stage.${stage?.name}`) }} · {{ t(`sourceSupply.operationStates.${stage?.status}`) }}<span v-if="stage?.message"> · {{ stage.message }}</span></li></ul></details>
    <Spinner v-if="status==='loading'&&!data"/>
    <div v-else-if="status==='error'" class="novel-alert" role="alert"><span>{{ error?.message }}</span><Button variant="text" @click="retry()">{{ t('common.retry') }}</Button></div>
    <ol v-if="data" class="source-ledger">
      <li v-for="row in visible" :key="row.sourceKey">
        <div class="source-row-heading cczj-flex cczj-items-center cczj-justify-between cczj-gap-6"><div class="cczj-min-w-0"><h2>{{ row.name }}</h2><span class="source-score">{{ t(row.score>=0?'sourceRouting.novelScore':'sourceSupply.unscored',{score:row.score}) }}</span><small>{{ t(`sourceSupply.supply.${row.supply}`) }} · {{ row.family }}</small></div><Button variant="text" :aria-label="t(row.enabled?'sourceRouting.pause':'sourceRouting.enable',{name:row.name})" :aria-pressed="row.enabled" :disabled="!!changing || row.state==='source_unavailable'" @click="toggle(row)"><Icon :name="row.enabled?'check':'plus'" :size="16"/>{{ t(row.enabled?'sourceRouting.enabled':'sourceRouting.paused') }}</Button></div>
        <p>{{ t(stateLabel(row)) }}<span v-if="!row.enabled"> · {{ t('sourceRouting.paused') }}</span></p>
        <p v-if="row.reading?.checkedAt">{{ t('sourceSupply.chapterCounts',{readable:row.reading.readable,restricted:row.reading.restricted,failed:row.reading.failed+row.reading.warnings}) }} · {{ time(row.reading.checkedAt) }}</p>
        <details class="source-details"><summary>{{ t('sourceSupply.details') }}</summary><div class="source-detail-body">
          <p>{{ t('sourceSupply.capabilities') }}：{{ row.capabilities.map(value=>t(`sourceSupply.capabilitiesList.${value}`)).join(' · ')||t('sourceSupply.invalidConfig') }}</p>
          <p>{{ t('sourceSupply.protocol') }}：{{ row.protocol||'—' }} · {{ t('sourceSupply.ruleVersion') }} {{ row.revision.slice(0,12) }}</p>
          <p>{{ t('sourceSupply.familyHint',{family:row.family}) }}</p>
          <p v-if="row.reading?.stale">{{ t('sourceSupply.staleChapters',{count:row.reading.stale}) }}</p>
          <p v-if="row.protocol==='fanqie-web'">{{ t('sourceSupply.fanqieLimit') }}</p>
          <div class="cczj-flex cczj-flex-wrap cczj-gap-4"><Button variant="text" @click="open(row.site)">{{ t('sourceSupply.site') }}</Button><Button v-if="row.origin" variant="text" @click="open(row.origin)">{{ t('sourceSupply.origin') }} · {{ row.project }}</Button><span v-else>{{ t('sourceSupply.originUnknown') }}</span></div>
          <ul v-if="row.operations.length" class="source-operations"><li v-for="operation in row.operations" :key="operation?.operation"><span>{{ t(`librarySources.stage.${operation?.operation}`) }} · {{ t(`sourceSupply.operationStates.${operation?.status}`) }}<span v-if="operation?.code"> · {{ operation.code }}</span></span><small>{{ operation?.checkedAt?time(operation.checkedAt):'—' }}</small></li></ul>
          <p v-if="row.deepAt">{{ t('sourceSupply.deepAt',{time:time(row.deepAt)}) }}</p>
          <p>{{ t('sourceSupply.sampleScope') }}</p>
          <Button :disabled="!!inspecting" @click="inspect(row)">{{ t('sourceSupply.inspect') }}</Button>
        </div></details>
      </li>
    </ol>
    <p v-if="status!=='loading'&&!visible.length" class="novel-empty">{{ t('sourceRouting.noMatch') }}</p>
    <nav v-if="pageCount>1" class="source-pagination cczj-flex cczj-items-center cczj-justify-between cczj-gap-6" :aria-label="t('sourceSupply.pages')"><Button variant="text" :disabled="page===1" @click="turnPage(page-1)">{{ t('novelUpgrade.previousPage') }}</Button><span>{{ page }} / {{ pageCount }}</span><Button variant="text" :disabled="page===pageCount" @click="turnPage(page+1)">{{ t('novelUpgrade.nextPage') }}</Button></nav>
    <footer v-if="data"><p>{{ t('sourceSupply.subscription') }}</p><p>{{ t('sourceSupply.inventory',{adapted:data.rows.length,pending:data.awaitingAdapter}) }}</p><p>{{ t('sourceSupply.foot') }}</p></footer>
  </section>
</template>
<style scoped>
.novel-source-directory{max-width:900px;margin:auto}
.novel-source-directory header h1{font-family:var(--room-serif,serif);font-size:calc(28rem / 14);margin:8px 0 12px}
.novel-source-directory p,.novel-source-directory small{color:var(--novel-muted);font-size:calc(12rem / 14);line-height:1.8;overflow-wrap:anywhere}
.source-tabs{@apply cczj-overflow-x-auto;white-space:nowrap;border-bottom:1px solid var(--novel-line);padding:10px 0 0}
.source-tabs>.ui-btn{@apply cczj-flex-shrink-0;border-radius:0}
.source-tabs>[aria-pressed="true"]{color:var(--novel-red);box-shadow:inset 0 -2px var(--novel-red)}
.source-filters{margin:12px 0;color:var(--novel-muted);font-size:calc(12rem / 14)}
.source-score{@apply cczj-block;color:var(--novel-red);font-size:calc(12rem / 14)}
.source-pagination{padding:16px 0;font-size:calc(12rem / 14)}
.source-ledger{list-style:none;margin:0;padding:0}
.source-ledger>li{padding:20px 0;border-bottom:1px solid var(--novel-line)}
.source-ledger h2{font-size:calc(16rem / 14);margin:0 0 6px;overflow-wrap:anywhere}
.source-details summary{color:var(--novel-red);font-size:calc(12rem / 14);padding:8px 0;@apply cczj-cursor-pointer}
.source-details summary:focus-visible{outline:2px solid var(--novel-red);outline-offset:2px}
.source-detail-body{padding:8px 12px;background:var(--novel-surface);border-radius:8px}
.source-operations{list-style:none;padding:0}.source-operations li{@apply cczj-grid;gap:4px;margin:12px 0;font-size:calc(12rem / 14);overflow-wrap:anywhere}
.source-inspection{padding:12px 0;font-size:calc(12rem / 14);line-height:1.8;overflow-wrap:anywhere}
.novel-source-directory footer{margin-top:24px}
</style>
