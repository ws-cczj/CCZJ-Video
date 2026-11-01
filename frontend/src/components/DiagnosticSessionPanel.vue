<script setup lang="ts">
import { computed, onBeforeUnmount, ref, shallowRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { CaptureDiagnosticsProfile, GetDiagnosticsSession, normalizeApiError } from '../api/app'
import { useViewActivity } from '../composables/useViewActivity'
import { diagnosticHealth, flushDiagnostics, recordingStatus, refreshRecordingStatus, saveDailyDiagnostics } from '../diagnostics/recording'
import { Button, Select } from './ui'

const props=defineProps<{active:boolean}>()
const {t}=useI18n()
const visible=useViewActivity(()=>props.active,'committed')
interface Row {id:string;at_ms:number;kind:string;name:string;route:string;outcome:string;code:string;duration_ms:number;request_id:string;flight_id:string;backend_request_id:string;action_id:string}
interface Window {daily_enabled:boolean;buffered:number;bytes:number;evicted:number;rejected:number;writer_dropped:number;events:Row[];findings:{code:string;count:number}[]}
const session=shallowRef<Window|null>(null),health=shallowRef(diagnosticHealth())
const loading=ref(false),changing=ref(false),profileKind=ref<string|number>('cpu'),error=ref('')
const profileOptions=computed(()=>['cpu','heap','goroutine'].map(value=>({value,label:t(`diagnostics.profileKinds.${value}`)})))
const profile=computed(()=>recordingStatus.value.profile)
const rows=computed(()=>session.value?.events.slice(-40).reverse()||[])
let generation=0,disposed=false,pending:ReturnType<typeof GetDiagnosticsSession>|undefined
function decode(value:unknown):Window {
  if(!value||typeof value!=='object')throw new Error(t('diagnostics.sessionInvalid'))
  const record=value as Record<string,unknown>
  if(typeof record.daily_enabled!=='boolean'||!Array.isArray(record.events)||!Array.isArray(record.findings))throw new Error(t('diagnostics.sessionInvalid'))
  const number=(item:unknown)=>typeof item==='number'&&Number.isFinite(item)&&item>=0?item:0
  const events:Row[]=[]
  for(const item of record.events.slice(-200)) {
    if(!item||typeof item!=='object')continue
    const raw=item as Record<string,unknown>
    if(typeof raw.id!=='string'||typeof raw.kind!=='string')continue
    const text=(key:string)=>typeof raw[key]==='string'?(raw[key] as string).slice(0,120):''
    events.push({id:text('id'),at_ms:number(raw.at_ms),kind:text('kind'),name:text('name'),route:text('route'),outcome:text('outcome'),code:text('code'),duration_ms:number(raw.duration_ms),request_id:text('request_id'),flight_id:text('flight_id'),backend_request_id:text('backend_request_id'),action_id:text('action_id')})
  }
  const findings=record.findings.flatMap(item=>{
    if(!item||typeof item!=='object')return []
    const raw=item as Record<string,unknown>
    return ['operation_failed','slow_operation','main_thread_longtask'].includes(String(raw.code))?[{code:String(raw.code),count:number(raw.count)}]:[]
  })
  return {daily_enabled:record.daily_enabled,buffered:number(record.buffered),bytes:number(record.bytes),evicted:number(record.evicted),rejected:number(record.rejected),writer_dropped:number(record.writer_dropped),events,findings}
}
function cancel():void {generation++;try{void Promise.resolve(pending?.cancel()).catch(()=>{})}catch{}pending=undefined;loading.value=false}
async function refresh():Promise<void> {
  if(!visible.value||disposed)return
  cancel();const token=generation;loading.value=true;error.value=''
  try {
    await flushDiagnostics()
    if(token!==generation||!visible.value||disposed)return
    pending=GetDiagnosticsSession()
    const value=await pending
    if(token!==generation||!visible.value||disposed)return
    session.value=decode(value);health.value=diagnosticHealth()
  }catch(failure){if(token===generation&&visible.value)error.value=normalizeApiError(failure).message}
  finally{if(token===generation){loading.value=false;pending=undefined}}
}
async function toggleDaily():Promise<void> {
  if(!session.value||changing.value)return
  changing.value=true;error.value=''
  try{await saveDailyDiagnostics(!session.value.daily_enabled);await refresh()}catch(failure){error.value=normalizeApiError(failure).message}finally{changing.value=false}
}
async function captureProfile():Promise<void> {
  if(changing.value)return
  changing.value=true;error.value=''
  try{await CaptureDiagnosticsProfile(String(profileKind.value));await refreshRecordingStatus();await refresh()}catch(failure){error.value=normalizeApiError(failure).message}finally{changing.value=false}
}
watch(visible,value=>{cancel();if(value)void refresh()},{immediate:true})
watch(()=>[recordingStatus.value.active,profile.value?.active],()=>{if(visible.value)void refresh()})
onBeforeUnmount(()=>{disposed=true;cancel()})
</script>

<template>
  <section class="cczj-session cczj-min-w-0" data-diagnostics-controls :aria-label="t('diagnostics.sessionTitle')">
    <div class="cczj-flex cczj-items-center cczj-justify-between cczj-flex-wrap cczj-gap-6">
      <h2>{{t('diagnostics.sessionTitle')}}</h2>
      <div class="cczj-flex cczj-items-center cczj-flex-wrap cczj-gap-4">
        <Button size="sm" :aria-pressed="session?.daily_enabled" :disabled="!session||changing" @click="toggleDaily">{{t(session?.daily_enabled?'diagnostics.dailyOn':'diagnostics.dailyOff')}}</Button>
        <Button size="sm" variant="text" :loading="loading" @click="refresh">{{t('common.refresh')}}</Button>
      </div>
    </div>
    <p>{{t('diagnostics.sessionBudget')}}</p>
    <p v-if="error" role="alert" class="cczj-session-error">{{error}}</p>
    <p v-if="session" class="cczj-text-sm">{{t('diagnostics.sessionHealth',{events:session.buffered,kib:Math.round(session.bytes/1024),evicted:session.evicted,rejected:session.rejected+health.rejected,dropped:session.writer_dropped+health.evicted+health.transport_dropped,failures:health.transport_failures,pending:health.queued})}}</p>
    <ul v-if="session?.findings.length" class="cczj-session-findings">
      <li v-for="finding in session.findings" :key="finding.code">{{t(`diagnostics.sessionFindings.${finding.code}`,{count:finding.count})}}</li>
    </ul>
    <p v-else-if="session">{{t('diagnostics.sessionNoEvidence')}}</p>
    <details>
      <summary>{{t('diagnostics.sessionTimeline')}}</summary>
      <p>{{t('diagnostics.sessionCorrelation')}}</p>
      <ol class="cczj-session-events">
        <li v-for="row in rows" :key="row.id">
          <div class="cczj-flex cczj-flex-wrap cczj-gap-4"><time>{{new Date(row.at_ms).toLocaleTimeString()}}</time><strong>{{row.name||row.kind}}</strong><span>{{row.outcome ? t(`diagnostics.sessionOutcomes.${row.outcome}`):''}}</span><span v-if="row.duration_ms">{{Math.round(row.duration_ms)}} ms</span><code v-if="row.code">{{row.code}}</code></div>
          <small>{{row.route}} · {{row.kind}}</small>
          <small v-if="row.action_id||row.flight_id||row.backend_request_id">{{t('diagnostics.sessionIdentity')}}: {{row.action_id||row.request_id||'—'}} / {{row.flight_id||'—'}} / {{row.backend_request_id||'—'}}</small>
        </li>
      </ol>
      <p v-if="!rows.length">{{t('diagnostics.sessionEmpty')}}</p>
    </details>
    <details class="cczj-mt-8">
      <summary>{{t('diagnostics.deepProfile')}}</summary>
      <p>{{t('diagnostics.deepProfileNote')}}</p>
      <div class="cczj-flex cczj-items-center cczj-flex-wrap cczj-gap-4">
        <Select v-model="profileKind" :options="profileOptions" :aria-label="t('diagnostics.deepProfile')" :disabled="changing||profile?.active" />
        <Button size="sm" :disabled="profile?.active" :loading="changing" @click="captureProfile">{{t(profile?.active?'diagnostics.profileRunning':'diagnostics.profileStart')}}</Button>
      </div>
      <p v-if="profile?.error" role="alert">{{t('diagnostics.profileFailed')}}</p>
      <p v-else-if="profile?.path">{{t('diagnostics.profileReady')}}</p>
    </details>
  </section>
</template>

<style scoped>
.cczj-session{@apply cczj-p-8 cczj-mb-10 cczj-border cczj-rounded-xl;background:var(--bg-card)}
.cczj-session h2{margin:0;font-size:calc(17rem / 14)}
.cczj-session p{@apply cczj-text-sm cczj-text-secondary;line-height:1.7;margin:12px 0}
.cczj-session summary{@apply cczj-cursor-pointer cczj-text-sm;min-height:32px;line-height:32px}
.cczj-session summary:focus-visible{outline:2px solid var(--accent);outline-offset:2px}
.cczj-session-events{@apply cczj-grid cczj-gap-6 cczj-overflow-auto;list-style:none;padding:0;margin:12px 0;max-height:420px}
.cczj-session-events li{@apply cczj-p-6 cczj-rounded-lg cczj-min-w-0;background:var(--bg-secondary);overflow-wrap:anywhere;font-size:calc(12rem / 14)}
.cczj-session-events small{@apply cczj-block cczj-text-muted;line-height:1.7}
.cczj-session-findings{@apply cczj-text-sm;line-height:1.8;padding-left:20px}
.cczj-session .cczj-session-error{color:var(--danger)}
</style>
