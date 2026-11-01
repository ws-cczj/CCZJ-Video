import { shallowRef } from 'vue'
import { GetDiagnosticsRecordingStatus, GetSetting, SetSetting, SubmitDiagnosticsEvents } from '../api/app'
import { createSettingsQueue } from '../api/settingsQueue'
import { CCZJ } from '../constants/cczj'
import { diagnosticMode } from './modeScope'
import { recordingContext } from './architecture'
import { activityDiagnostics } from './activity'
import { captureBrowserResources } from './browserResources'
import { configureDiagnosticTransport, setDailyDiagnostics, setReproductionRecording, recordTimeline, numericDiagnosticData, flushDiagnostics } from './session'
export { flushDiagnostics, diagnosticHealth } from './session'

export interface RecordingStatus {
  active: boolean
  path: string
  samples: number
  markers: number
  started_at_ms: number
  ended_at_ms: number
  reason: string
  error: string
  profile?: {kind:string;active:boolean;path:string;error:string}
}

export const recordingStatus = shallowRef<RecordingStatus>({ active: false, path: '', samples: 0, markers: 0, started_at_ms: 0, ended_at_ms: 0, reason: '', error: '' })
export const recordingObserverFailed = shallowRef(false)
let timer: ReturnType<typeof setTimeout> | undefined
let playbackSnapshot: (() => unknown) | null = null
let busy = false, observing = true, revision = 0
let longtasks: PerformanceObserver | undefined
let preferenceError: unknown
const preferenceQueue = createSettingsQueue(async(key,value)=>{await SetSetting(key,value);setDailyDiagnostics(value==='1')},(_pending,error)=>{preferenceError=error})
export async function saveDailyDiagnostics(enabled: boolean): Promise<void> {
  if(!await preferenceQueue.set(CCZJ.Settings.diagnosticsCapture,enabled?'1':'0'))throw preferenceError || new Error('诊断设置保存失败')
}
export function recordingRevision(): number { return revision }

export async function initializeDiagnostics(): Promise<void> {
  // Install after preference decoding; disabling routine collection discards bootstrap metadata.
  try { setDailyDiagnostics(await GetSetting(CCZJ.Settings.diagnosticsCapture) !== '0') }
  catch { recordingObserverFailed.value = true; setDailyDiagnostics(false) }
  if (!observing) return
  configureDiagnosticTransport(payload => SubmitDiagnosticsEvents(payload))
  await refreshRecordingStatus()
}

export function setRecordingStatus(value: RecordingStatus | null | undefined, expectedRevision?: number): void {
  if (expectedRevision !== undefined && expectedRevision !== revision) return
  if (!value || typeof value.active !== 'boolean') return
  if (value.path !== recordingStatus.value.path) recordingObserverFailed.value = false
  if (value.path !== recordingStatus.value.path || value.active !== recordingStatus.value.active) revision++
  recordingStatus.value = value
  setReproductionRecording(value.active)
  if (!value.active) { clearTimeout(timer);timer=undefined;longtasks?.disconnect();longtasks=undefined }
  if (observing && value.active) {
    if (!timer && !busy) scheduleSnapshot()
    if (!longtasks && typeof PerformanceObserver !== 'undefined' && PerformanceObserver.supportedEntryTypes?.includes('longtask')) {
      try {
        longtasks = new PerformanceObserver(list => {
          const entries=list.getEntries().slice(0,64)
          if (entries.length) recordTimeline('perf.longtask',{name:'main_thread',duration_ms:Math.max(...entries.map(entry=>entry.duration)),data:{count:entries.length,total_ms:entries.reduce((sum,entry)=>sum+entry.duration,0)}})
        })
        longtasks.observe({type:'longtask',buffered:false})
      } catch { longtasks?.disconnect();longtasks=undefined }
    }
  }
}
function scheduleSnapshot(): void {
  timer=setTimeout(async()=>{
    timer=undefined;busy=true
    try {
      if (diagnosticMode()==='video' && playbackSnapshot) await recordDiagnosticEvent('playback_snapshot',playbackSnapshot())
      else await recordDiagnosticEvent('activity_snapshot',{...activityDiagnostics.snapshot(diagnosticMode()),browser:captureBrowserResources()})
      await refreshRecordingStatus()
    } catch { recordingObserverFailed.value=true }
    finally {busy=false;if(observing&&recordingStatus.value.active&&!timer)scheduleSnapshot()}
  },15000)
}
export async function refreshRecordingStatus(): Promise<void> {
  const captured=revision
  try { setRecordingStatus(await GetDiagnosticsRecordingStatus(),captured) }
  catch { if(recordingStatus.value.active)recordingObserverFailed.value=true }
}
export function registerPlaybackSnapshot(snapshot: () => unknown): () => void {
  playbackSnapshot=snapshot
  return ()=>{if(playbackSnapshot===snapshot)playbackSnapshot=null}
}
export async function recordDiagnosticEvent(kind: string,data: unknown): Promise<void> {
  const payload=data && typeof data==='object' ? data as Record<string,unknown> : {}
  const owner=recordingContext()
  recordTimeline(kind,{name:typeof payload.operation==='string'?payload.operation:undefined,code:typeof payload.code==='string'?payload.code:undefined,outcome:kind==='error'||kind==='player_issue'||kind.endsWith('_failed')?'failed':undefined,duration_ms:typeof payload.elapsed_ms==='number'?payload.elapsed_ms:typeof payload.ms==='number'?payload.ms:undefined,resource_id:typeof payload.player_session_id==='string'?payload.player_session_id:owner.player_session_id||undefined,data:numericDiagnosticData(data)})
}
export function disposeRecordingObserver(): void {
  observing=false;revision++;clearTimeout(timer);timer=undefined;longtasks?.disconnect();longtasks=undefined;playbackSnapshot=null
  void flushDiagnostics()
}
if(import.meta.hot)import.meta.hot.dispose(disposeRecordingObserver)
