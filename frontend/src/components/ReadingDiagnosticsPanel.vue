<script setup lang="ts">
import { computed, onBeforeUnmount, ref, shallowRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { GetDiagnostics, GetRuntimeMetrics, ExportDiagnosticsReport, StartDiagnosticsRecording, StopDiagnosticsRecording } from '../api/app'
import { useApplicationMode } from '../modes/applicationMode'
import { useViewActivity } from '../composables/useViewActivity'
import { useErrorStore } from '../stores/error'
import { activityDiagnostics } from '../diagnostics/activity'
import { startupSnapshot } from '../diagnostics/startup'
import { captureBrowserResources } from '../diagnostics/browserResources'
import { recordingStatus, recordingObserverFailed, recordDiagnosticEvent, setRecordingStatus, recordingRevision } from '../diagnostics/recording'
import { revealDiagnosticExport } from '../platform/diagnosticFiles'
import DiagnosticObservability from './DiagnosticObservability.vue'
import { Button, Segment } from './ui'
import Icon from './Icon.vue'

const props=withDefaults(defineProps<{active?:boolean;exportActionLabel?:string}>(),{active:true,exportActionLabel:'diagnostics.openReportFolder'})
const {t}=useI18n()
const mode=useApplicationMode()
const errors=useErrorStore()
const visible=useViewActivity(()=>props.active)
const section=ref<string|number>('overview')
const metrics=shallowRef<Record<string,any>|null>(null)
const diagnostics=shallowRef<Record<string,any>|null>(null)
const busy=ref(false)
const path=ref('')
const failed=ref(false)
const resource=computed(()=>metrics.value?.resources||{})
const tabs=['overview','startup','activity','network','storage'] as const
const tabKeys={overview:'diagnostics.overview',startup:'diagnostics.startupTitle',activity:'diagnostics.activityTitle',network:'diagnostics.networkAndCache',storage:'diagnostics.runtimeStorage'}
const tabOptions=computed(()=>tabs.map(tab=>({value:tab,label:t(tabKeys[tab])})))
const refreshing=ref(false)
let timer:ReturnType<typeof setTimeout>|undefined
let generation=0
let disposed=false
const pending=new Set<{cancel?:()=>void|Promise<void>}>()
async function owned<T>(request:Promise<T>&{cancel?:()=>void|Promise<void>}):Promise<T>{pending.add(request);try{return await request}finally{pending.delete(request)}}
function cancel(){generation++;clearTimeout(timer);for(const request of pending){try{void Promise.resolve(request.cancel?.()).catch(()=>{})}catch{}}}
async function poll(token:number){
  try {
    const revision=recordingRevision()
    const value=await owned(GetRuntimeMetrics())
    if(disposed||token!==generation||!visible.value)return
    metrics.value=value;failed.value=false;setRecordingStatus(value?.recording,revision)
  }catch{if(token===generation)failed.value=true}
  finally{if(!disposed&&token===generation&&visible.value)timer=setTimeout(()=>void poll(token),2000)}
}
async function refresh(){
  if(refreshing.value)return
  refreshing.value=true
  const token=generation
  try{const value=await owned(GetDiagnostics());if(!disposed&&token===generation&&visible.value)diagnostics.value=value}
  catch(error){if(token===generation)errors.fromError(t('diagnostics.loadFailed'),error,'ReadingDiagnostics.refresh')}
  finally{refreshing.value=false}
}
watch(visible,value=>{cancel();if(value){void poll(generation);void refresh()}},{immediate:true})
onBeforeUnmount(()=>{disposed=true;cancel()})
function snapshot(){return {application_mode:mode.mode,startup:startupSnapshot(),activity:activityDiagnostics.snapshot(mode.mode),browser:captureBrowserResources(),recording_observer_failed:recordingObserverFailed.value}}
async function exportReport(){
  if(busy.value)return
  busy.value=true
  try{const report=await ExportDiagnosticsReport(JSON.stringify(snapshot()));if(report)path.value=report.bundle_path||report.path}
  catch(error){errors.fromError(t('diagnostics.exportFailed'),error,'ReadingDiagnostics.export')}
  finally{busy.value=false}
}
async function toggleRecording(){
  if(busy.value)return
  busy.value=true
  try{
    if(recordingStatus.value.active){await recordDiagnosticEvent('recording_stop',snapshot());setRecordingStatus(await StopDiagnosticsRecording())}
    else{setRecordingStatus(await StartDiagnosticsRecording());await recordDiagnosticEvent('recording_start',snapshot())}
  }catch(error){errors.fromError(t('diagnostics.recordingFailed'),error,'ReadingDiagnostics.recording')}
  finally{busy.value=false}
}
async function openReport(){try{await revealDiagnosticExport(path.value)}catch(error){errors.fromError(t('diagnostics.exportFailed'),error,'ReadingDiagnostics.open')}}
const bytes=(value:number|undefined|null)=>value==null?'—':`${(value/1048576).toFixed(1)} MiB`
</script>

<template>
  <section class="cczj-reading-diagnostics">
    <header class="reading-diag-heading">
      <div><span class="reading-diag-eyebrow">{{t(`modes.${mode.mode}`)}}</span><h1>{{t('diagnostics.pageTitle')}}</h1><p>{{t('diagnostics.overviewDescription')}}</p></div>
      <div class="reading-diag-actions">
        <Button class="reading-diag-refresh" variant="text" :aria-label="t('common.refresh')" :loading="refreshing" @click="refresh"><Icon name="refresh" :size="16"/><span class="reading-diag-refresh-label">{{t('common.refresh')}}</span></Button>
        <Button variant="secondary" :disabled="busy" :aria-pressed="recordingStatus.active" @click="toggleRecording"><Icon :name="recordingStatus.active?'stop':'play'" :size="16"/>{{t(recordingStatus.active?'diagnostics.stopRecording':'diagnostics.startRecording')}}</Button>
        <Button variant="primary" :disabled="busy" @click="exportReport"><Icon name="download" :size="16"/>{{t('diagnostics.exportReport')}}</Button>
      </div>
    </header>
    <div v-if="path||recordingStatus.active" class="reading-diag-status" role="status"><span v-if="recordingStatus.active">{{t('diagnostics.recordingActive',{samples:recordingStatus.samples})}}</span><Button v-if="path" variant="text" @click="openReport">{{t(exportActionLabel)}}<Icon name="chevron-right" :size="16"/></Button></div>
    <p v-if="failed||recordingObserverFailed||recordingStatus.error" class="reading-diag-error" role="alert">{{recordingStatus.error||t('diagnostics.recordingObservationIncomplete')}}</p>
    <nav class="reading-diag-tabs" :aria-label="t('diagnostics.sectionNavigation')"><Segment v-model="section" :options="tabOptions" wrap /></nav>
    <div class="reading-diag-content">
    <template v-if="section==='overview'">
      <div class="reading-diag-section-heading"><h2>{{t('diagnostics.overview')}}</h2><small>{{t('diagnostics.sharedHost')}}</small></div>
      <div class="reading-diag-cards">
        <div><span class="reading-diag-metric-label"><Icon name="monitor" :size="18"/>{{t('diagnostics.processMemory')}}</span><strong>{{bytes(resource.native_available?(resource.native_scope==='app_process'?resource.proportional_bytes:resource.tree_private_bytes):null)}}</strong></div>
        <div><span class="reading-diag-metric-label"><Icon name="layers" :size="18"/>{{t('diagnostics.goHeap')}}</span><strong>{{bytes(resource.heap_alloc_bytes)}}</strong></div>
        <div><span class="reading-diag-metric-label"><Icon name="code" :size="18"/>{{t('diagnostics.goroutines')}}</span><strong>{{metrics?.goroutines??'—'}}</strong></div>
      </div>
      <details class="reading-diag-scope"><summary><Icon name="info" :size="16"/>{{t('diagnostics.modeScopeLabel')}}<Icon name="chevron-right" :size="16"/></summary><p>{{t('diagnostics.modeScopeNote')}}</p></details>
      <div class="reading-diag-shortcuts"><Button v-for="tab in ['startup','activity','network'] as const" :key="tab" variant="secondary" block @click="section=tab"><span><strong>{{t(tabKeys[tab])}}</strong><small>{{t(tab==='startup'?'diagnostics.startupDescription':tab==='activity'?'diagnostics.activityDescription':'diagnostics.networkDescription')}}</small></span><Icon name="chevron-right" :size="18"/></Button></div>
      <DiagnosticObservability section="coverage" :metrics="metrics" />
    </template>
    <DiagnosticObservability v-else-if="section==='startup'||section==='activity'" :section="section" :metrics="metrics" />
    <template v-else-if="section==='network'">
      <p>{{t('diagnostics.modeNetworkScope')}}</p>
      <div v-for="row in metrics?.network?.categories||[]" :key="row.category" class="reading-diag-cards"><div>{{t(`modes.${mode.mode}`)}}<strong>{{bytes(row.bytes)}}</strong></div><div>{{t('diagnostics.operationSucceeded')}}<strong>{{row.requests-row.fails-row.cancelled}}</strong></div><div>{{t('diagnostics.operationFailed')}}<strong>{{row.fails}}</strong></div><div>{{t('diagnostics.operationCancelled')}}<strong>{{row.cancelled}}</strong></div></div>
    </template>
    <template v-else-if="section==='storage'">
      <p>{{t('diagnostics.sharedStorageNote')}}</p><p>SQLite: {{bytes(diagnostics?.storage?.database_bytes)}}</p>
      <div v-for="row in diagnostics?.tables||[]" :key="row.name" class="table-row"><span>{{row.name}}</span><strong>{{row.rows}}</strong></div>
      <p v-for="note in diagnostics?.notes||[]" :key="note">{{note}}</p>
    </template>
    </div>
  </section>
</template>

<style scoped>
.cczj-reading-diagnostics{--bg-hover:var(--reading-soft,var(--bg-secondary));--accent-contrast:var(--reading-bg,var(--bg-primary));--btn-solid:var(--reading-accent,var(--accent));--btn-solid-text:var(--reading-bg,var(--accent-contrast));--btn-solid-hover:color-mix(in srgb,var(--btn-solid),var(--text-primary) 12%);--btn-solid-alpha-20:color-mix(in srgb,var(--btn-solid) 12%,transparent);--btn-solid-alpha-35:color-mix(in srgb,var(--btn-solid) 20%,transparent);@apply cczj-min-w-0 cczj-text-primary}
.reading-diag-heading{@apply cczj-flex cczj-items-end cczj-justify-between cczj-gap-12; margin-bottom:28px}
.reading-diag-eyebrow{@apply cczj-text-sm cczj-text-accent cczj-font-medium}
.reading-diag-heading h1{font-family:var(--room-serif,inherit);font-size:calc(30rem / 14);font-weight:500;line-height:1.4;margin:8px 0}
.reading-diag-heading p{margin:0}
.reading-diag-actions{@apply cczj-flex cczj-items-center cczj-gap-4 cczj-flex-shrink-0}
.reading-diag-actions :deep(.ui-btn){min-height:40px}
.reading-diag-tabs{@apply cczj-max-w-full; padding-bottom:4px}
.reading-diag-tabs :deep(.ui-seg){@apply cczj-w-full}
.reading-diag-tabs :deep(.ui-seg__item){min-height:40px}
.reading-diag-content{padding-top:28px}
.reading-diag-section-heading{@apply cczj-flex cczj-items-center cczj-justify-between cczj-gap-6; margin-bottom:16px}
.reading-diag-section-heading h2{font-size:calc(17rem / 14);font-weight:600;margin:0}
.reading-diag-section-heading small{@apply cczj-text-sm cczj-text-muted}
.reading-diag-cards{@apply cczj-grid cczj-gap-8;grid-template-columns:repeat(auto-fit,minmax(160px,1fr))}
.reading-diag-cards>div{padding:24px;@apply cczj-border cczj-rounded-xl cczj-min-w-0; background:var(--bg-card);font-size:calc(13rem / 14)}
.reading-diag-metric-label{@apply cczj-flex cczj-items-center cczj-gap-5 cczj-text-secondary}
.reading-diag-cards strong{@apply cczj-block cczj-mt-8; font-size:calc(30rem / 14);font-weight:500;letter-spacing:-.04em;font-variant-numeric:tabular-nums}
.reading-diag-cards strong:empty{min-height:42px}
.reading-diag-scope{margin:16px 0 24px}
.reading-diag-scope summary{@apply cczj-flex cczj-items-center cczj-gap-4 cczj-text-sm cczj-text-muted cczj-cursor-pointer;min-height:32px;list-style:none;width:fit-content}
.reading-diag-scope summary::-webkit-details-marker{@apply cczj-hidden}
.reading-diag-scope[open] summary svg:last-child{transform:rotate(90deg)}
.reading-diag-scope summary:focus-visible{outline:2px solid var(--accent);outline-offset:3px}
.reading-diag-scope p{max-width:76ch;margin:10px 0 0}
.reading-diag-shortcuts{@apply cczj-grid cczj-gap-8;grid-template-columns:repeat(3,minmax(0,1fr));margin-bottom:24px}
.reading-diag-shortcuts :deep(.ui-btn){@apply cczj-text-left;white-space:normal; padding:18px;background:var(--bg-card);border-color:var(--border);border-radius:12px}
.reading-diag-shortcuts :deep(.ui-btn__content){@apply cczj-w-full cczj-justify-between cczj-items-center cczj-gap-6}
.reading-diag-shortcuts strong{font-size:calc(14rem / 14);font-weight:500}
.reading-diag-shortcuts small{@apply cczj-block cczj-text-sm cczj-text-muted cczj-font-normal;line-height:1.7;margin-top:8px}
.reading-diag-shortcuts svg{@apply cczj-flex-shrink-0 cczj-text-muted}
.reading-diag-status{@apply cczj-flex cczj-items-center cczj-flex-wrap cczj-justify-between cczj-gap-6; margin:-8px 0 20px;font-size:calc(13rem / 14);color:var(--accent)}
.reading-diag-error{@apply cczj-p-8 cczj-rounded-lg; background:var(--bg-secondary);color:var(--danger)}
p{@apply cczj-text-secondary cczj-text-13;line-height:1.7}
.table-row{@apply cczj-flex cczj-justify-between cczj-gap-8 cczj-border-bottom cczj-text-13; padding:14px 0;overflow-wrap:anywhere}
@media(max-width:850px){.reading-diag-heading{@apply cczj-flex-col cczj-items-start cczj-gap-8}.reading-diag-actions{@apply cczj-w-full}.reading-diag-shortcuts{grid-template-columns:1fr}.reading-diag-shortcuts small{margin-top:4px}}
@media(max-width:480px){.reading-diag-heading h1{font-size:calc(26rem / 14)}.reading-diag-actions{@apply cczj-flex-wrap}.reading-diag-actions :deep(.ui-btn){min-height:44px;@apply cczj-flex-1}.reading-diag-actions :deep(.ui-btn:first-child){@apply cczj-flex-none}.reading-diag-cards{grid-template-columns:repeat(2,minmax(0,1fr));@apply cczj-gap-5}.reading-diag-cards>div{padding:16px}.reading-diag-cards>div:first-child{grid-column:1 / -1}.reading-diag-cards strong{font-size:calc(27rem / 14)}.reading-diag-cards>div:first-child strong{font-size:calc(34rem / 14)}.reading-diag-content{padding-top:24px}}
@media(max-width:480px){.reading-diag-actions :deep(.reading-diag-refresh){width:44px;padding:0}.reading-diag-refresh-label{@apply cczj-hidden}}
</style>
