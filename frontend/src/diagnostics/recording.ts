import { diagnosticMode } from './modeScope'
import { shallowRef } from 'vue'
import { AddDiagnosticsRecordingMarker, GetDiagnosticsRecordingStatus } from '../api/app'
import { recordingContext } from './architecture'
import { activityDiagnostics } from './activity'
import { startupSnapshot } from './startup'

export interface RecordingStatus {
  active: boolean
  path: string
  samples: number
  markers: number
  started_at_ms: number
  ended_at_ms: number
  reason: string
  error: string
}

export const recordingStatus = shallowRef<RecordingStatus>({ active: false, path: '', samples: 0, markers: 0, started_at_ms: 0, ended_at_ms: 0, reason: '', error: '' })
export const recordingObserverFailed = shallowRef(false)
let timer: ReturnType<typeof setTimeout> | null = null
let playbackSnapshot: (() => unknown) | null = null
let busy = false
let observing = true
let revision = 0
const lastAuxiliary = new Map<string,string>()
export function recordingRevision(): number { return revision }

// The frontend keeps one periodic observer only during an explicitly started session.
// Go independently records native resources while WebView timers are throttled.
export function setRecordingStatus(value: RecordingStatus | null | undefined, expectedRevision?: number): void {
  if (expectedRevision !== undefined && expectedRevision !== revision) return
  if (!value || typeof value.active !== 'boolean') return
  if (value.path !== recordingStatus.value.path) { recordingObserverFailed.value = false; lastAuxiliary.clear() }
  if (value.path !== recordingStatus.value.path || value.active !== recordingStatus.value.active) revision++
  recordingStatus.value = value
  if (!value.active && timer !== null) { clearTimeout(timer); timer = null }
  if (observing && value.active && timer === null && !busy) scheduleSnapshot()
}

function scheduleSnapshot(): void {
  timer = setTimeout(async () => {
    timer = null
    busy = true
    try {
      if (diagnosticMode()==='video' && playbackSnapshot) await recordDiagnosticEvent('playback_snapshot', playbackSnapshot())
      if(diagnosticMode()!=='video') await recordDiagnosticEvent('activity_snapshot',activityDiagnostics.snapshot(diagnosticMode()))
      // Check the backend's deadline/disk-error state even when no player is mounted.
      await refreshRecordingStatus()
    } finally {
      busy = false
      if (observing && recordingStatus.value.active && timer === null) scheduleSnapshot()
    }
  }, 5000)
}

export async function refreshRecordingStatus(): Promise<void> {
  const captured = revision
  try { setRecordingStatus(await GetDiagnosticsRecordingStatus(), captured) }
  catch { if(recordingStatus.value.active)recordingObserverFailed.value=true }
}

export function registerPlaybackSnapshot(snapshot: () => unknown): void {
  playbackSnapshot = snapshot
}

async function writeMarker(path:string,kind:string,data:unknown):Promise<boolean> {
  if(!recordingStatus.value.active||recordingStatus.value.path!==path)return false
  try {
    const payload = data && typeof data === 'object' && !Array.isArray(data)
      ? {...recordingContext(),...data} : {...recordingContext(),value:data}
    const accepted = await AddDiagnosticsRecordingMarker(kind, JSON.stringify(payload))
    if (!accepted && path === recordingStatus.value.path) await refreshRecordingStatus()
    return accepted
  } catch {
    // Marker validation/bridge failures do not necessarily change native status.
    // Keep an explicit incomplete flag for this recording without interrupting media.
    if (path === recordingStatus.value.path) recordingObserverFailed.value = true
    return false
  }
}

export async function recordDiagnosticEvent(kind: string, data: unknown): Promise<void> {
  if (!recordingStatus.value.active) return
  const path=recordingStatus.value.path
  if(!await writeMarker(path,kind,data)||!observing)return
  if(kind!=='playback_snapshot'&&kind!=='recording_start'&&kind!=='recording_stop'&&kind!=='activity_snapshot')return
  // Keep each marker within the native 8 KiB budget. Only changed auxiliary
  // observations are written; they do not inflate every media snapshot.
  for(const [auxiliary,snapshot] of [['startup_snapshot',startupSnapshot()],['activity_snapshot',activityDiagnostics.snapshot(diagnosticMode())]] as const) {
    const serialized=JSON.stringify(snapshot)
    if(lastAuxiliary.get(auxiliary)===serialized)continue
    if(await writeMarker(path,auxiliary,snapshot))lastAuxiliary.set(auxiliary,serialized)
  }
}

export function disposeRecordingObserver(): void {
  observing = false
  revision++
  if (timer !== null) clearTimeout(timer)
  timer = null
  playbackSnapshot = null
}
