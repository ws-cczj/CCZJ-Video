<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, shallowRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { LibraryKind } from '../api/library'
import { previewSources, checkSourceCandidate, importSources, sourceStatus, compareSources, visibleSourceCandidates, type SourceCandidate, type SourcePreview, type SourceImportResult, type SourceCatalogMode } from '../api/sourceCatalog'
import { normalizeApiError } from '../api/app'
import { Button, Spinner } from './ui'

const props=defineProps<{kind:LibraryKind}>()
const emit=defineEmits<{ imported: [] }>()
const {t}=useI18n()
const preview=shallowRef<SourcePreview|null>(null),loading=ref(false),error=ref(''),message=ref('')
const address=ref(''),query=ref(''),project=ref(''),status=ref(''),sort=ref('quality'),keyword=ref(''),limit=ref(30)
const selected=ref(new Set<string>()),checking=ref(new Set<string>()),importing=ref(false),expanded=ref(new Set<string>())
const fileInput=ref<HTMLInputElement|null>(null)
const advanced=ref(false)
const community=ref(false)
const catalogMode=ref<SourceCatalogMode>('bundled')
type Pending=Promise<unknown>&{cancel?:()=>unknown}
const pending=new Set<Pending>()
let generation=0,disposed=false
async function track<T>(request:Promise<T>&{cancel?:()=>unknown}):Promise<T>{pending.add(request);try{return await request}finally{pending.delete(request)}}
function cancel(){generation++;for(const request of pending){try{void Promise.resolve(request.cancel?.()).catch(()=>{})}catch{}};pending.clear();checking.value=new Set()}
async function load(input='',origin=''){
  if(importing.value)return
  catalogMode.value = input === 'community' || input === 'community:refresh' ? 'community' : origin || input ? 'custom' : 'bundled'
  community.value=input==='community'||input==='community:refresh'
  cancel();const token=generation;loading.value=true;error.value='';message.value='';selected.value=new Set()
  try{const result=await track(previewSources(props.kind,input,origin));if(token===generation&&!disposed){preview.value=result;limit.value=30;query.value='';project.value='';status.value='';expanded.value=new Set()}}
  catch(raw){if(token===generation&&!disposed)error.value=normalizeApiError(raw).message}
  finally{if(token===generation&&!disposed)loading.value=false}
}
async function loadCommunity(){community.value=true;await load('community')}
async function readFile(event:Event){
  const file=(event.target as HTMLInputElement).files?.[0];if(!file)return
  try{if(file.size>32*1024*1024){error.value=t('sourceCatalog.tooLarge');return}community.value=false;await load(await file.text(),file.name)}catch(raw){error.value=normalizeApiError(raw).message}
  if(fileInput.value)fileInput.value.value=''
}
const candidates=computed(()=>preview.value?.candidates??[])
const displayCandidates=computed(()=>visibleSourceCandidates(candidates.value,catalogMode.value))
const projects=computed(()=>[...new Set(displayCandidates.value.map(row=>row.project))].sort())
const filtered=computed(()=>displayCandidates.value.filter(row=>(!query.value||`${row.name} ${row.apiUrl}`.toLowerCase().includes(query.value.toLowerCase()))&&(!project.value||row.project===project.value)&&(!status.value||sourceStatus(row)===status.value)).sort((a,b)=>compareSources(a,b,sort.value)))
const visible=computed(()=>filtered.value.slice(0,limit.value))
const selectable=computed(()=>filtered.value.filter(row=>row.compatible&&!row.imported))
const selectedRows=computed(()=>candidates.value.filter(row=>selected.value.has(row.id)&&row.compatible&&!row.imported))
const allVisibleSelected=computed(()=>{const rows=visible.value.filter(row=>row.compatible&&!row.imported);return rows.length>0&&rows.every(row=>selected.value.has(row.id))})
watch([query,project,status,sort],()=>{limit.value=30})
function select(id:string){const next=new Set(selected.value);if(next.has(id))next.delete(id);else next.add(id);selected.value=next}
function selectVisible(){const next=new Set(selected.value),remove=allVisibleSelected.value;for(const row of visible.value.filter(row=>row.compatible&&!row.imported)){if(remove)next.delete(row.id);else next.add(row.id)}selected.value=next}
function details(id:string){const next=new Set(expanded.value);if(next.has(id))next.delete(id);else next.add(id);expanded.value=next}
async function check(row:SourceCandidate){
  if(!preview.value||checking.value.has(row.id))return
  const token=generation,previewID=preview.value.id
  checking.value=new Set([...checking.value,row.id]);error.value=''
  try{const result=await track(checkSourceCandidate(previewID,row.id,keyword.value));if(token===generation&&!disposed&&preview.value){preview.value={...preview.value,candidates:preview.value.candidates.map(current=>current.id===result.id?{...result,imported:current.imported||result.imported}:current)}}}
  catch(raw){if(token===generation&&!disposed)error.value=normalizeApiError(raw).message}
  finally{if(token===generation&&!disposed){const next=new Set(checking.value);next.delete(row.id);checking.value=next}}
}
async function checkSelected(){
  const rows=selectedRows.value.slice();if(!rows.length)return
  // Three workers keep large selections from flooding providers or the bridge.
  const token=generation;let index=0
  await Promise.all(Array.from({length:Math.min(3,rows.length)},async()=>{while(index<rows.length&&token===generation&&!disposed){await check(rows[index++]!)}}))
}
async function install(rows:SourceCandidate[]=selectedRows.value){
  if(!preview.value||!rows.length||importing.value)return
  const token=generation;importing.value=true;error.value='';message.value=''
  try{
    const results:SourceImportResult[]=[]
    for(let start=0;start<rows.length;start+=100){
      if(token!==generation||disposed)return
      const batch:SourceImportResult[]=await track(importSources(preview.value.id,rows.slice(start,start+100).map(row=>row.id)))
      if(token!==generation||disposed)return
      results.push(...batch)
      const done:Set<string>=new Set(batch.filter(row=>row.status!=='failed').map(row=>row.id))
      if(preview.value)preview.value={...preview.value,candidates:preview.value.candidates.map(row=>done.has(row.id)?{...row,imported:true}:row)}
      if(done.size)emit('imported')
    }
    if(token!==generation||disposed||!preview.value)return
    const installed=new Set(results.filter(row=>row.status!=='failed').map(row=>row.id))
    preview.value={...preview.value,candidates:preview.value.candidates.map(row=>installed.has(row.id)?{...row,imported:true}:row)}
    selected.value=new Set([...selected.value].filter(id=>!installed.has(id)))
    const failed=results.filter(row=>row.status==='failed')
    message.value=t('sourceCatalog.importedCount',{count:installed.size})
    if(failed.length)error.value=failed.map(row=>`${row.name}：${row.message}`).join('\n')
  }catch(raw){if(token===generation&&!disposed)error.value=normalizeApiError(raw).message}
  finally{if(token===generation&&!disposed)importing.value=false}
}
function checkedTime(row:SourceCandidate){return row.quality?new Date(row.quality.checkedAt*1000).toLocaleString():''}
onMounted(()=>{void load()})
onBeforeUnmount(()=>{disposed=true;cancel()})
</script>

<template>
  <section class="source-catalog" :aria-label="t('sourceCatalog.title')">
    <header class="source-catalog-heading"><div><h3>{{t('sourceCatalog.title')}}</h3><p>{{t('sourceCatalog.simpleIntro')}}</p></div></header>
    <nav class="source-catalog-tabs" :aria-label="t('sourceCatalog.library')"><Button size="sm" :aria-pressed="!community" :disabled="loading||importing" @click="community=false;load()">{{t('sourceCatalog.bundled')}}</Button><Button size="sm" :aria-pressed="community" :disabled="loading||importing" @click="loadCommunity">{{t(kind==='novel'?'sourceCatalog.moreNovels':'sourceCatalog.moreComics')}}</Button><Button size="sm" variant="text" :aria-expanded="advanced" @click="advanced=!advanced">{{t('sourceCatalog.otherImport')}}</Button></nav>
    <div v-if="advanced" class="source-catalog-advanced">
    <form class="source-catalog-address" @submit.prevent="load(address)"><label><span>{{t('sourceCatalog.url')}}</span><input v-model="address" type="url" placeholder="https://…/sources.json" :disabled="loading||importing" /></label><Button type="submit" size="sm" :disabled="!address||loading||importing">{{t('sourceCatalog.preview')}}</Button><Button type="button" size="sm" :disabled="loading||importing" @click="fileInput?.click()">{{t('sourceCatalog.file')}}</Button><input ref="fileInput" type="file" accept=".json,application/json" hidden @change="readFile" /></form>
    <p class="source-catalog-help">{{t('sourceCatalog.formats')}}</p>
    </div>
    <div v-if="error" class="source-catalog-error" role="alert">{{error}} <Button v-if="!loading" size="sm" :disabled="importing" @click="load()">{{t('sourceCatalog.bundled')}}</Button></div>
    <p v-if="message" role="status">{{message}}</p>
    <Spinner v-if="loading" :label="t('common.loading')" />
    <template v-else-if="preview">
      <label v-if="community||advanced||candidates.length>10" class="source-catalog-search"><span>{{t('sourceCatalog.find')}}</span><input v-model="query" type="search" /></label>
      <details v-if="community||advanced||candidates.length>10" class="source-catalog-options"><summary>{{t('sourceCatalog.filters')}}</summary><div class="source-catalog-filters">
        <label><span>{{t('sourceCatalog.project')}}</span><select v-model="project"><option value="">{{t('common.all')}}</option><option v-for="value in projects" :key="value">{{value}}</option></select></label>
        <label><span>{{t('sourceCatalog.state')}}</span><select v-model="status"><option value="">{{t('common.all')}}</option><option v-for="value in ['healthy','partial','untested','stale','failed','unsupported']" :key="value" :value="value">{{t(`sourceCatalog.status.${value}`)}}</option></select></label>
        <label><span>{{t('sourceCatalog.sort')}}</span><select v-model="sort"><option value="quality">{{t('sourceCatalog.qualityFirst')}}</option><option value="latency">{{t('sourceCatalog.latencyFirst')}}</option><option value="name">{{t('sourceCatalog.nameSort')}}</option></select></label>
      </div>
      <p class="source-catalog-help">{{t('sourceCatalog.ranking')}}</p>
      </details>
      <p class="source-catalog-help" role="status">{{t(community?'sourceCatalog.availableCount':'sourceCatalog.readyCount',{ready:candidates.filter(row=>row.compatible).length,total:candidates.length})}}</p>
      <Button v-if="community" size="sm" variant="ghost" :disabled="loading||importing" @click="load('community:refresh')">{{t('sourceCatalog.refreshLibrary')}}</Button>
      <div class="source-catalog-selection"><span role="status">{{t('sourceCatalog.count',{count:filtered.length,selected:selectedRows.length})}}</span><Button size="sm" variant="text" :disabled="!selectable.length||importing" @click="selectVisible">{{t(allVisibleSelected?'sourceCatalog.deselectVisible':'sourceCatalog.selectVisible')}}</Button><Button v-if="selectedRows.length" size="sm" variant="ghost" @click="selected=new Set()">{{t('sourceCatalog.clear')}}</Button></div>
      <ul class="source-catalog-list">
        <li v-for="row in visible" :key="row.id" class="source-candidate" :data-source-id="row.id" :data-status="sourceStatus(row)">
          <div class="source-candidate-top"><label class="source-candidate-select"><input type="checkbox" :checked="selected.has(row.id)" :disabled="!row.compatible||row.imported||importing" @change="select(row.id)" /><strong>{{row.name}}</strong></label><span class="source-candidate-status">{{t(`sourceCatalog.status.${sourceStatus(row)}`)}}</span><span v-if="row.imported" class="source-candidate-installed">{{t('sourceCatalog.installed')}}</span></div>
          <div class="source-candidate-quick"><span>{{row.summary||row.project}}</span><Button v-if="row.compatible&&!row.imported" size="sm" variant="primary" :disabled="importing||loading" @click="install([row])">{{t('sourceCatalog.addOne')}}</Button><Button size="sm" variant="text" :aria-expanded="expanded.has(row.id)" @click="details(row.id)">{{t('sourceCatalog.details')}}</Button></div>
          <template v-if="expanded.has(row.id)">
          <div class="source-candidate-body"><p class="source-candidate-meta">{{row.project}} · {{t(`sourceCatalog.format.${['adapted','builtin','legado','mihon'].includes(row.format)?row.format:'custom'}`)}}<template v-if="row.quality"> · {{row.quality.score}}/100 · {{row.quality.millis}} ms</template></p><p class="source-candidate-url">{{row.apiUrl}}</p><p v-if="row.reason" class="source-candidate-reason">{{row.reason}}</p><p v-if="row.quality" class="source-candidate-meta">{{t('sourceCatalog.samples',{success:row.quality.successes,total:row.quality.samples})}} · {{checkedTime(row)}}</p>
            <p v-if="row.quality" class="source-candidate-meta">{{t(row.quality.evidence==='bundled'?'sourceCatalog.shippedTest':'sourceCatalog.localTest')}}</p>
            <div class="source-candidate-actions"><Button v-if="row.compatible" size="sm" variant="ghost" :loading="checking.has(row.id)" :disabled="importing" @click="check(row)">{{t('sourceCatalog.check')}}</Button><a v-if="row.origin" :href="row.origin" target="_blank" rel="noopener noreferrer">{{t('sourceCatalog.origin')}}</a></div>
            <div v-if="expanded.has(row.id)" class="source-candidate-details"><p>{{row.summary||t('sourceCatalog.noDetails')}}</p><ol v-if="row.quality"><li v-for="stage in row.quality.stages" :key="stage.name"><span>{{t(`librarySources.stage.${stage.name}`)}}</span><span>{{t(`librarySources.stageStatus.${stage.status}`)}}</span><span>{{stage.millis}} ms</span><p v-if="stage.message">{{stage.message}}</p></li></ol></div>
          </div>
          </template>
        </li>
      </ul>
      <p v-if="!filtered.length" class="source-catalog-help">{{t('sourceCatalog.noMatches')}}</p>
      <Button v-if="filtered.length>limit" size="sm" @click="limit+=30">{{t('sourceCatalog.more')}}</Button>
      <footer v-if="selectedRows.length" class="source-catalog-footer"><Button size="sm" variant="primary" :loading="importing" :disabled="checking.size>0||loading" @click="install()">{{t('sourceCatalog.importSelected',{count:selectedRows.length})}}</Button><details><summary>{{t('sourceCatalog.checkSelected')}}</summary><label><span>{{t('sourceCatalog.keyword')}}</span><input v-model="keyword" type="text" :placeholder="t('sourceCatalog.keywordHint')" /></label><Button size="sm" :disabled="checking.size>0||importing" @click="checkSelected">{{t('sourceCatalog.checkSelected')}}</Button></details></footer>
    </template>
  </section>
</template>

<style scoped>
.source-catalog-tabs,.source-candidate-quick{@apply cczj-flex cczj-items-center cczj-flex-wrap cczj-gap-4}.source-candidate-quick>span{@apply cczj-flex-1;min-width:120px;@apply cczj-text-13;line-height:1.6;opacity:.75}.source-catalog-advanced{@apply cczj-flex cczj-flex-col cczj-gap-5}.source-catalog-options summary,.source-catalog-footer summary{@apply cczj-cursor-pointer;min-height:44px;@apply cczj-flex cczj-items-center cczj-text-13}.source-catalog-search{@apply cczj-max-w-full}.source-catalog-filters{@apply cczj-mt-4}
.source-catalog :deep(.ui-btn){min-height:44px}
.source-catalog{@apply cczj-flex cczj-flex-col cczj-gap-6}
.source-catalog-heading,.source-catalog-address,.source-catalog-selection,.source-candidate-top,.source-candidate-actions,.source-catalog-footer{@apply cczj-flex cczj-items-center cczj-flex-wrap cczj-gap-5}.source-catalog-heading{@apply cczj-items-start cczj-justify-between}.source-catalog h3{margin:0;@apply cczj-text-lg}.source-catalog p{margin:0;line-height:1.6}.source-catalog-heading p,.source-catalog-help,.source-candidate-meta,.source-candidate-url{@apply cczj-text-sm;opacity:.75}.source-catalog label{@apply cczj-flex cczj-flex-col;gap:5px;@apply cczj-text-sm cczj-min-w-0}.source-catalog input:not([type=checkbox]),.source-catalog select{border:1px solid color-mix(in srgb,currentColor 24%,transparent);@apply cczj-rounded-md;background:var(--bgInput,transparent);color:inherit;font:inherit;min-height:44px;padding:8px 10px;@apply cczj-min-w-0 cczj-max-w-full}.source-catalog input:focus-visible,.source-catalog select:focus-visible{outline:2px solid currentColor;outline-offset:2px}.source-catalog-address>label{@apply cczj-flex-1;min-width:180px}.source-catalog-filters{@apply cczj-grid;grid-template-columns:2fr repeat(3,1fr);@apply cczj-gap-5}.source-catalog-list{list-style:none;padding:0;margin:0;@apply cczj-flex cczj-flex-col cczj-gap-5}.source-candidate{@apply cczj-p-6;border:1px solid color-mix(in srgb,currentColor 14%,transparent);border-radius:10px;@apply cczj-min-w-0}.source-candidate-select{flex-direction:row!important;@apply cczj-items-center;gap:8px!important;min-height:44px;font-size:14px!important}.source-candidate input[type=checkbox]{width:18px;height:18px;@apply cczj-flex-shrink-0}.source-candidate-status,.source-candidate-installed{@apply cczj-text-sm;padding:3px 7px;border-radius:5px;background:color-mix(in srgb,currentColor 8%,transparent)}.source-candidate[data-status=healthy] .source-candidate-status{color:var(--textPrimary,inherit);@apply cczj-font-bold;border:1px solid currentColor}.source-candidate[data-status=unsupported]{@apply cczj-border-dashed}.source-candidate-body{padding-left:26px;@apply cczj-flex cczj-flex-col cczj-gap-3 cczj-min-w-0}.source-candidate-url,.source-candidate-reason{overflow-wrap:anywhere;@apply cczj-text-sm}.source-candidate-actions a{@apply cczj-flex cczj-items-center cczj-gap-2 cczj-text-sm;min-height:44px;color:inherit;@apply cczj-underline}.source-catalog-error{color:var(--colorDanger,#b63838);@apply cczj-text-13 cczj-whitespace-pre}.source-catalog-footer{@apply cczj-pt-6;border-top:1px solid color-mix(in srgb,currentColor 12%,transparent)}.source-catalog-footer label{@apply cczj-flex-1;min-width:150px}.source-candidate-details{@apply cczj-text-sm cczj-p-5;background:color-mix(in srgb,currentColor 4%,transparent);@apply cczj-rounded-md}.source-candidate-details ol{padding:0;list-style:none;@apply cczj-flex cczj-flex-col cczj-gap-3}.source-candidate-details li{@apply cczj-flex cczj-gap-6 cczj-flex-wrap}.source-candidate-details li p{flex-basis:100%}.source-catalog-selection>span{@apply cczj-flex-1 cczj-text-sm}@media(max-width:640px){.source-catalog{padding:0}.source-catalog-filters{grid-template-columns:repeat(2,minmax(0,1fr))}.source-candidate-body{padding-left:0}.source-catalog-address>label{flex-basis:100%}.source-catalog-footer>label{flex-basis:100%}}

.source-catalog-tabs :deep(.ui-btn){@apply cczj-rounded-md}.source-catalog-tabs :deep(.ui-btn[aria-pressed=true]){@apply cczj-text-accent;background:var(--accent-alpha-10);@apply cczj-border-primary}
.source-candidate-quick{@apply cczj-grid;grid-template-columns:1fr auto auto}.source-candidate-quick>span{grid-column:1/-1;@apply cczj-min-w-0}.source-candidate-quick :deep(.ui-btn--primary){grid-column:2}
.source-candidate-quick :deep(.ui-btn--text){grid-column:3}.source-candidate-status{border:0!important;@apply cczj-rounded-xl;padding:5px 8px}.source-candidate[data-status=healthy] .source-candidate-status{background:#eaf4ec;color:#2e693e}
.source-candidate[data-status=failed] .source-candidate-status{background:#faebeb;color:#9e3434}

/* WebView 110 needs plain-color fallbacks before color-mix declarations. */
.source-catalog input:not([type=checkbox]),.source-catalog select{border:1px solid #d8d2cb;background:var(--bg-input,#fff);@apply cczj-rounded-md}
.source-candidate{border:1px solid #e7e1da;background:var(--bg-card,#fff)}.source-candidate[data-status=unsupported]{@apply cczj-border-dashed}
.source-candidate-details{background:#f6f3ef}.source-catalog-footer{border-top:1px solid #e7e1da}
</style>

