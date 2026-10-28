<script setup lang="ts">
import { computed } from 'vue'
import { useApplicationMode } from '../modes/applicationMode'
import { useI18n } from 'vue-i18n'
import { activityDiagnostics, activityRevision } from '../diagnostics/activity'
import { startupRevision, startupSnapshot } from '../diagnostics/startup'
import { captureBrowserResources } from '../diagnostics/browserResources'
import Icon from './Icon.vue'

const props=defineProps<{section:'startup'|'activity'|'coverage';metrics:Record<string,any>|null}>()
const {t}=useI18n()
const mode=useApplicationMode()
const isVideo=computed(()=>mode.mode==='video')
const startup=computed(()=>{startupRevision.value;props.metrics?.uptime_seconds;return startupSnapshot(mode.mode)})
const activity=computed(()=>{activityRevision.value;props.metrics?.uptime_seconds;return activityDiagnostics.snapshot(mode.mode)})
const executor=computed(()=>props.metrics?.download_executor)
const danmaku=computed(()=>isVideo.value?props.metrics?.danmaku:null)
const browser=computed(()=>{props.metrics?.uptime_seconds;return captureBrowserResources()})
function navigationElapsed(end:number|undefined):number|null {
  const began=startup.value.native?.page_started_ms
  return began&&end&&end>=began?end-began:null
}
const ms=(value:number|undefined|null)=>value==null?'—':`${Math.round(value)} ms`
const coverage=computed(()=>[
  {name:t('diagnostics.startupTitle'),observed:startup.value.events.length>0,note:t('diagnostics.startupScope')},
  {name:t('diagnostics.nativeStartup'),observed:!!startup.value.native,note:t('diagnostics.nativeStartupScope')},
  {name:t('diagnostics.processMemory'),observed:!!props.metrics?.resources?.native_available,note:props.metrics?.resources?.native_scope==='app_process'?t('diagnostics.androidMemoryScope'):t('diagnostics.resourceNote')},
  {name:t('diagnostics.activityTitle'),observed:activity.value.operations.length>0,note:t('diagnostics.activityScope')},
  ...(isVideo.value?[{name:t('diagnostics.downloadExecutor'),observed:!!executor.value,note:t('diagnostics.executorScope')}]:[]),
])
function phaseName(phase:string):string {return t(`diagnostics.startupPhases.${phase}`)}
const operationNames:Record<string,string>={
  'novel.catalog':'diagnostics.novelCatalog','comic.catalog':'diagnostics.comicCatalog',
  'novel.reader':'diagnostics.novelReader','comic.reader':'diagnostics.comicReader',
  'novel.search':'diagnostics.novelSearch','comic.search':'diagnostics.comicSearch',
  'reading.request':'diagnostics.readingRequest',
  'reading.novel.hydrate':'diagnostics.novelHydrate','reading.comic.hydrate':'diagnostics.comicHydrate',
  'reading.novel.save':'diagnostics.novelSave','reading.comic.save':'diagnostics.comicSave',
  'video.transport':'diagnostics.transportCalls','novel.transport':'diagnostics.transportCalls','comic.transport':'diagnostics.transportCalls',
}
const networkNames:Record<string,string>={
  playback:'diagnostics.netPlayback',lineprobe:'diagnostics.netLineProbe',collect:'diagnostics.netCollect',
  douban:'diagnostics.netDouban',image:'diagnostics.netImage',update:'diagnostics.netUpdate',
  download:'diagnostics.netDownload',danmaku:'danmaku.title',cast:'mobile.cast',novel:'modes.novel',comic:'modes.comic',
}
</script>

<template>
  <component :is="section==='coverage'?'details':'section'" class="cczj-diag-observability" :class="{'cczj-diag-coverage':section==='coverage'}">
    <summary v-if="section==='coverage'" class="cczj-diag-coverage-summary"><Icon name="info" :size="18"/><span>{{t('diagnostics.measurementNotes')}}</span><Icon name="chevron-right" :size="16"/></summary>
    <div v-if="section==='coverage'" class="cczj-diag-coverage-content">
      <h3>{{t('diagnostics.coverageTitle')}}</h3>
      <p>{{t('diagnostics.coverageNote')}}</p>
      <div v-for="row in coverage" :key="row.name" class="cczj-diag-observation-row">
        <strong>{{row.name}}</strong><span>{{t(row.observed?'diagnostics.observed':'diagnostics.notObserved')}}</span><small>{{row.note}}</small>
      </div>
      <h3>{{t('diagnostics.browserOwnership')}}</h3><p>{{t('diagnostics.browserOwnershipNote')}}</p>
      <div class="cczj-diag-observation-grid">
        <div v-if="isVideo"><span>{{t('diagnostics.activePlayers')}}</span><strong>{{browser.architecture?.active_player_count ?? '—'}}</strong></div>
        <div v-if="isVideo"><span>{{t('diagnostics.resourceObservers')}}</span><strong>{{browser.registered_player_count ?? '—'}}</strong></div>
        <div v-if="isVideo"><span>{{t('diagnostics.attachedVideos')}}</span><strong>{{browser.attached_dom?.video_count ?? '—'}}</strong></div>
        <div><span>{{t('diagnostics.attachedCanvases')}}</span><strong>{{browser.attached_dom?.canvas_count ?? '—'}}</strong></div>
        <div><span>{{t('diagnostics.browserHeapEstimate')}}</span><strong>{{browser.js_heap?`${(browser.js_heap.used_bytes/1048576).toFixed(1)} MiB`:'—'}}</strong></div>
      </div>
      <template v-if="browser.android_memory">
        <p>{{t('diagnostics.androidSystemMemoryNote')}}</p>
        <div class="cczj-diag-observation-grid">
          <div><span>{{t('diagnostics.androidSystemPss')}}</span><strong>{{browser.android_memory.available && browser.android_memory.pss_bytes != null ? `${(browser.android_memory.pss_bytes/1048576).toFixed(1)} MiB` : '—'}}</strong></div>
          <div><span>{{t('diagnostics.androidGraphics')}}</span><strong>{{browser.android_memory.available && browser.android_memory.graphics_bytes != null ? `${(browser.android_memory.graphics_bytes/1048576).toFixed(1)} MiB` : '—'}}</strong></div>
          <div><span>{{t('diagnostics.androidWholeMemory')}}</span><strong>{{browser.android_memory.attributed?.available && browser.android_memory.attributed.pss_bytes != null ? `${(browser.android_memory.attributed.pss_bytes/1048576).toFixed(1)} MiB` : '—'}}</strong></div>
          <div><span>WebView</span><strong>{{browser.android_memory.webview_version}}</strong></div>
        </div>
      </template>
    </div>
    <template v-else-if="section==='startup'">
      <p>{{t('diagnostics.startupScope')}}</p>
      <div class="cczj-diag-observation-grid">
        <div><span>{{t('diagnostics.modeActivation')}}</span><strong>{{t(startup.mode_activation.activation==='resume'?'diagnostics.modeResume':'diagnostics.modeCold')}}</strong></div>
        <div><span>{{t('diagnostics.modeCheckpoint')}}</span><strong>{{(startup.mode_activation.checkpoint_bytes/1024).toFixed(1)}} KiB</strong></div>
      </div>
      <p>{{t('diagnostics.modeRetentionScope')}}</p>
      <div class="diag-table-wrap"><table class="diag-table"><thead><tr><th>{{t('diagnostics.startupPhase')}}</th><th>{{t('diagnostics.sinceDocument')}}</th><th>{{t('diagnostics.operationDuration')}}</th><th>{{t('diagnostics.observationState')}}</th></tr></thead><tbody>
        <tr v-for="event in startup.events" :key="event.phase"><td>{{phaseName(event.phase)}}</td><td>{{ms(event.at_ms)}}</td><td>{{ms(event.duration_ms)}}</td><td>{{t(event.failed?'diagnostics.operationFailed':'diagnostics.observed')}}</td></tr>
      </tbody></table></div>
      <p>{{t('diagnostics.renderTimingNote')}}</p>
      <h3>{{t('diagnostics.nativeStartup')}}</h3>
      <p>{{t('diagnostics.nativeStartupScope')}}</p>
      <div v-if="startup.native" class="cczj-diag-observation-grid">
        <div><span>{{t('diagnostics.nativeGoReady')}}</span><strong>{{ms(startup.native.go_ready_ms||null)}}</strong></div>
        <div><span>{{t('diagnostics.nativePageCommit')}}</span><strong>{{ms(navigationElapsed(startup.native.page_committed_ms))}}</strong></div>
        <div><span>{{t('diagnostics.nativePageFinished')}}</span><strong>{{ms(navigationElapsed(startup.native.page_finished_ms))}}</strong></div>
      </div>
      <p v-else>{{t('diagnostics.hostObservationUnavailable')}}</p>
    </template>
    <template v-else>
      <p>{{t('diagnostics.activityScope')}}</p>
      <div v-if="activity.operations.length" class="diag-table-wrap"><table class="diag-table"><thead><tr>
        <th>{{t('diagnostics.operation')}}</th><th>{{t('diagnostics.operationActive')}}</th><th>{{t('diagnostics.operationSucceeded')}}</th><th>{{t('diagnostics.operationFailed')}}</th><th>{{t('diagnostics.operationCancelled')}}</th><th>{{t('diagnostics.operationAvg')}}</th><th>{{t('diagnostics.operationMax')}}</th>
      </tr></thead><tbody><tr v-for="row in activity.operations" :key="row.name"><td>{{t(operationNames[row.name]||'diagnostics.readingRequest')}}</td><td>{{row.active}}</td><td>{{row.success}}</td><td>{{row.failed}}</td><td>{{row.cancelled}}</td><td>{{ms(row.avg_ms)}}</td><td>{{ms(row.max_ms)}}</td></tr></tbody></table></div>
      <p v-else>{{t('diagnostics.activityEmpty')}}</p>
      <h3>{{t('diagnostics.eventDelivery')}}</h3><div class="cczj-diag-observation-grid"><div v-for="key in ['received','malformed','coalesced','filtered'] as const" :key="key"><span>{{t(`diagnostics.events.${key}`)}}</span><strong>{{activity.events[key]}}</strong></div></div>
      <template v-if="metrics?.operations?.length"><h3>{{t('diagnostics.backendOperations')}}</h3><p>{{t('diagnostics.backendOperationsScope')}}</p><div class="diag-table-wrap"><table class="diag-table"><thead><tr><th>{{t('diagnostics.operation')}}</th><th>{{t('diagnostics.operationSucceeded')}}</th><th>{{t('diagnostics.operationFailed')}}</th><th>{{t('diagnostics.operationCancelled')}}</th><th>{{t('diagnostics.operationAvg')}}</th><th>{{t('diagnostics.operationMax')}}</th></tr></thead><tbody><tr v-for="row in metrics.operations" :key="row.name"><td>{{row.name}}</td><td>{{row.succeeded}}</td><td>{{row.failed}}</td><td>{{row.cancelled}}</td><td>{{ms(row.total_ms/Math.max(1,row.succeeded+row.failed+row.cancelled))}}</td><td>{{ms(row.max_ms)}}</td></tr></tbody></table></div></template>
      <div v-for="row in activity.reading" :key="row.kind" class="cczj-diag-observation-row"><strong>{{t(row.kind==='novel'?'diagnostics.novelSave':'diagnostics.comicSave')}}</strong><span>{{t(row.failed?'diagnostics.operationFailed':row.ready?'diagnostics.operationSucceeded':'diagnostics.notObserved')}}</span><small>{{t('diagnostics.pendingReadingWrites',{count:row.pending})}}</small></div>
      <template v-if="isVideo"><h3>{{t('diagnostics.downloadExecutor')}}</h3>
      <p>{{t('diagnostics.executorScope')}}</p>
      <div v-if="executor" class="cczj-diag-observation-grid"><div><span>{{t('diagnostics.operationActive')}}</span><strong>{{executor.active}} / {{executor.concurrency_limit}}</strong></div><div><span>{{t('diagnostics.executorAccepted')}}</span><strong>{{executor.accepted}} / {{executor.admission_limit}}</strong></div></div>
      <p v-else>{{t('diagnostics.hostObservationUnavailable')}}</p></template>
      <template v-if="metrics?.network_policy?.length"><h3>{{t('networkPolicy.title')}}</h3><p>{{t('networkPolicy.diagnosticScope')}}</p><div v-for="row in metrics.network_policy" :key="row.category" class="cczj-diag-observation-grid"><strong>{{t(networkNames[row.category]||'diagnostics.netUnknown')}}</strong><div><span>{{t('networkPolicy.requests')}}</span><strong>{{row.requests}}</strong></div><div><span>{{t('networkPolicy.reused')}}</span><strong>{{row.reused}}</strong></div><div><span>{{t('networkPolicy.blocked')}}</span><strong>{{row.blocked}}</strong></div></div></template>
      <template v-if="metrics?.outbound_proxy"><h3>{{t('networkPolicy.proxyTitle')}}</h3><p>{{t('networkPolicy.proxyHint')}}</p><div class="cczj-diag-observation-grid"><div><span>{{t('networkPolicy.proxyHealthy')}}</span><strong>{{metrics.outbound_proxy.healthy}}</strong></div><div><span>{{t('networkPolicy.proxyAttempts')}}</span><strong>{{metrics.outbound_proxy.publicAttempts}}</strong></div><div><span>{{t('networkPolicy.proxyFallbacks')}}</span><strong>{{metrics.outbound_proxy.localFallbacks}}</strong></div></div><p v-if="metrics.outbound_proxy.refreshing">{{t('common.loading')}}</p><p v-if="metrics.outbound_proxy.lastError" role="status">{{t('networkPolicy.proxyRefreshFailed')}}</p>
      <div v-if="metrics.outbound_proxy.sources?.length" class="diag-table-wrap"><table class="diag-table"><thead><tr><th>{{t('networkPolicy.proxySource')}}</th><th>{{t('diagnostics.observationState')}}</th><th>{{t('networkPolicy.proxyCandidates')}}</th><th>{{t('networkPolicy.proxyNextAttempt')}}</th></tr></thead><tbody><tr v-for="source in metrics.outbound_proxy.sources" :key="source.id"><td>{{source.id}}</td><td>{{t(`networkPolicy.proxyStates.${source.state}`)}}<span v-if="source.lastError"> · {{source.lastError}}</span></td><td>{{source.candidates}}</td><td>{{source.nextAttempt?new Date(source.nextAttempt*1000).toLocaleString():'—'}}</td></tr></tbody></table></div></template>
      <template v-if="danmaku"><h3>{{t('danmaku.title')}}</h3><p>{{t('danmaku.metrics.scope')}}</p>
       <div class="cczj-diag-observation-grid"><div v-for="key in ['cache_hits','fetches','stale_fallbacks','cache_failures','match_runs','fallbacks','cooldown_hits']" :key="key"><span>{{t(`danmaku.metrics.${key}`)}}</span><strong>{{danmaku[key]??'—'}}</strong></div></div>
      </template>
    </template>
  </component>
</template>

<style scoped>
.cczj-diag-observability{padding:18px;@apply cczj-border cczj-rounded-lg;background:var(--bg-card);@apply cczj-text-primary;margin:14px 0}
.cczj-diag-coverage{padding:0;margin:0}
.cczj-diag-coverage-summary{@apply cczj-flex cczj-items-center cczj-gap-5 cczj-text-secondary cczj-cursor-pointer;min-height:60px;padding:16px 20px;font-size:calc(14rem / 14);list-style:none}
.cczj-diag-coverage-summary::-webkit-details-marker{@apply cczj-hidden}
.cczj-diag-coverage-summary span{@apply cczj-flex-1}
.cczj-diag-coverage[open] .cczj-diag-coverage-summary>svg:last-child{transform:rotate(90deg)}
.cczj-diag-coverage-summary:focus-visible{outline:2px solid var(--accent);outline-offset:3px}
.cczj-diag-coverage-content{padding:0 20px 20px}
.cczj-diag-observability h3{font-size:calc(15rem / 14);margin:10px 0}.cczj-diag-observability p,.cczj-diag-observability small{@apply cczj-text-sm;line-height:1.7;@apply cczj-text-muted}
.cczj-diag-observation-row{@apply cczj-flex cczj-flex-wrap cczj-justify-between cczj-gap-4;padding:12px 0;@apply cczj-border-bottom cczj-text-13}.cczj-diag-observation-row small{flex-basis:100%}
.cczj-diag-observation-grid{@apply cczj-grid;grid-template-columns:repeat(auto-fit,minmax(140px,1fr));@apply cczj-gap-5}.cczj-diag-observation-grid>div{@apply cczj-flex cczj-flex-col cczj-gap-5 cczj-p-7;background:var(--bg-secondary);@apply cczj-rounded-md}.cczj-diag-observation-grid span{@apply cczj-text-sm cczj-text-muted}
.diag-table-wrap{@apply cczj-max-w-full;@apply cczj-overflow-x-auto;overscroll-behavior:contain}.diag-table{@apply cczj-w-full;border-collapse:collapse;@apply cczj-text-sm cczj-text-left}.diag-table th,.diag-table td{@apply cczj-whitespace-nowrap;padding:10px 8px;@apply cczj-border-bottom}.diag-table th{@apply cczj-text-secondary cczj-font-semibold}
</style>
