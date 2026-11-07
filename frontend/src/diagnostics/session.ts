// Metadata collection has no runtime or store dependency; error decoding is pure.
import { normalizeApiError } from '../api/errors'
export interface DiagnosticContext { application_mode: string; route: string; action_id?: string }
export interface DiagnosticEvent extends DiagnosticContext {
  id: string; session_id: string; client_at_ms: number; kind: string
  name?: string; request_id?: string; flight_id?: string; backend_request_id?: string
  parent_id?: string; cast_session_id?: string; resource_id?: string; outcome?: string; code?: string; duration_ms?: number
  data?: Record<string, number | boolean | string>
}
export interface BatchReceipt { accepted: number; rejected: number; daily_enabled: boolean }
type Transport = (payload: string) => Promise<BatchReceipt> & { cancel?: () => unknown }
const MAX_EVENTS = 128, MAX_BYTES = 128 * 1024, MAX_EVENT_BYTES = 2048
const sessionID = `web-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}`
const label = /^[A-Za-z0-9_.:-]{0,120}$/
let sequence = 0, queuedBytes = 0, enabled = true, recording = false, disposed = false
let location: DiagnosticContext = { application_mode: 'video', route: '' }
let action: string | undefined, actionEvent: Event | undefined, transport: Transport | undefined
let actionTimer: ReturnType<typeof setTimeout> | undefined
let timer: ReturnType<typeof setTimeout> | undefined, inFlight: Promise<boolean> | undefined
let failures = 0
const queue: { wire: string; bytes: number; important: boolean }[] = []
const health = { evicted: 0, oversized: 0, transport_failures: 0, transport_dropped: 0, rejected: 0, accepted: 0 }
const encoder = new TextEncoder()

export function diagnosticHealth() { return { ...health, queued: queue.length, queued_bytes: queuedBytes, in_flight: !!inFlight, daily_enabled: enabled, recording, session_id: sessionID, longtask_supported: typeof PerformanceObserver !== 'undefined' && PerformanceObserver.supportedEntryTypes?.includes('longtask') === true } }
export function setDiagnosticLocation(mode: string, route: string): void {
  location = { application_mode: ['video','novel','comic'].includes(mode) ? mode : 'video', route: route.length <= 120 && /^\/[A-Za-z0-9_/:()*.-]*$/.test(route) ? route : '' }
}
export function captureDiagnosticContext(): DiagnosticContext { return { ...location, action_id: actionEvent && actionEvent.eventPhase !== Event.NONE ? action : undefined } }
export function newDiagnosticAction(): string { return `${sessionID}-a${++sequence}` }
export function setDailyDiagnostics(value: boolean): void {
  enabled = value
  if (!value && !recording) { queue.length = 0; queuedBytes = 0; clearTimeout(timer); timer = undefined }
  else schedule()
}
export function setReproductionRecording(value: boolean): void { recording = value; schedule() }
export function configureDiagnosticTransport(value: Transport): void { transport = value; schedule() }

// Bounded numeric summaries do not retain content, URLs, errors or DOM references.
export function numericDiagnosticData(value: unknown): Record<string, number | boolean> {
  const result: Record<string, number | boolean> = {}
  function visit(value: unknown, prefix: string, depth: number): void {
    if (!value || typeof value !== 'object' || Array.isArray(value) || depth > 2) return
    for (const key of Object.keys(value).slice(0,32)) {
      if (Object.keys(result).length >= 16) return
      const name = prefix ? `${prefix}_${key}` : key
      if (!/^[a-zA-Z0-9_]{1,48}$/.test(name) || /(?:id|url|path|title|text|message|token|cookie|header|keyword|content)/i.test(key)) continue
      const item = (value as Record<string,unknown>)[key]
      if (typeof item === 'boolean' || (typeof item === 'number' && Number.isFinite(item))) result[name] = item
      else visit(item,name,depth+1)
    }
  }
  visit(value,'',0)
  return result
}

export function recordTimeline(kind: string, fields: Partial<DiagnosticEvent> = {}, context = captureDiagnosticContext()): void {
  if (disposed || (!enabled && !recording)) return
  const event: DiagnosticEvent = { ...context, id: `${sessionID}-${++sequence}`, session_id: sessionID, client_at_ms: Date.now(), kind }
  for (const key of ['name','request_id','flight_id','backend_request_id','parent_id','resource_id','cast_session_id','outcome','code'] as const) {
    const value = fields[key]; if (typeof value === 'string' && label.test(value)) event[key] = value
  }
  if (typeof fields.duration_ms === 'number' && Number.isFinite(fields.duration_ms)) event.duration_ms = Math.max(0,fields.duration_ms)
  if (fields.data) event.data = numericDiagnosticData(fields.data)
  const wire = JSON.stringify(event), bytes = encoder.encode(wire).byteLength
  if (bytes > MAX_EVENT_BYTES) { health.oversized++; return }
  const important=event.outcome==='failed'||event.kind==='ui.error'||event.kind==='perf.longtask'
  while(queue.length&&(queue.length>=MAX_EVENTS||queuedBytes+bytes>MAX_BYTES)) {
    const normal=queue.findIndex(row=>!row.important)
    if(normal<0&&!important){health.evicted++;return}
    queuedBytes-=queue.splice(Math.max(0,normal),1)[0].bytes;health.evicted++
  }
  queue.push({wire,bytes,important}); queuedBytes += bytes
  schedule()
}

function schedule(): void {
  if (!disposed && transport && queue.length && !timer && !inFlight) timer = setTimeout(() => { timer = undefined; void flushDiagnosticBatch() }, Math.min(30000,2000 * 2 ** failures))
}
async function flushDiagnosticBatch(): Promise<boolean> {
  if (inFlight) return inFlight
  if (!transport || !queue.length || disposed) return queue.length === 0
  clearTimeout(timer); timer = undefined
  const batch: typeof queue = []
  let bytes = 2
  while (queue.length && batch.length < 32 && bytes + queue[0].bytes + 1 <= 60 * 1024) {
    const entry = queue.shift()!; queuedBytes -= entry.bytes; bytes += entry.bytes + 1; batch.push(entry)
  }
  const sender = transport
  inFlight = (async () => {
    let timeout: ReturnType<typeof setTimeout> | undefined
    let pending: ReturnType<Transport> | undefined
    try {
      pending = sender(`[${batch.map(row => row.wire).join(',')}]`)
      const receipt = await Promise.race([pending, new Promise<never>((_resolve,reject) => { timeout = setTimeout(() => { try { void Promise.resolve(pending?.cancel?.()).catch(()=>{}) } catch {} reject(new Error('diagnostics_timeout')) },5000) })])
      if (!Number.isInteger(receipt.accepted) || receipt.accepted < 0 || !Number.isInteger(receipt.rejected) || receipt.rejected < 0 || receipt.accepted + receipt.rejected !== batch.length) throw new Error('diagnostics_receipt')
      health.accepted += receipt.accepted; health.rejected += receipt.rejected; failures = 0
      return true
    } catch {
      health.transport_failures++; failures++
      // A lost acknowledgement may have reached the host; IDs permit deduplication.
      if (!disposed && (enabled || recording) && failures < 3) {
        queue.unshift(...batch); queuedBytes += batch.reduce((sum,row)=>sum+row.bytes,0)
        while (queue.length > MAX_EVENTS || queuedBytes > MAX_BYTES) { const normal=queue.findIndex(row=>!row.important);queuedBytes-=queue.splice(Math.max(0,normal),1)[0].bytes;health.evicted++ }
      } else health.transport_dropped += batch.length
      return false
    } finally { clearTimeout(timeout) }
  })()
  try { return await inFlight } finally { inFlight = undefined; schedule() }
}

export async function flushDiagnostics(): Promise<boolean> {
  for (let index=0;index<5;index++) { if (!queue.length && !inFlight) return true; if (!await flushDiagnosticBatch()) return false }
  return queue.length === 0 && !inFlight
}

/** Document-owned listeners record semantic controls without reading their text. */
export function installDiagnosticListeners(): () => void {
  const click = (event: Event) => {
    if (!event.isTrusted || !(event.target instanceof Element)) return
    const element = event.type === 'submit' ? event.target.closest('form') : event.target.closest('button,a,[role="button"],input[type="checkbox"],select')
    if (!element || element.closest('[data-diagnostics-controls]')) return
    const id = newDiagnosticAction(); action = id; actionEvent = event
    clearTimeout(actionTimer)
    const name = element.getAttribute('data-diagnostic-action') || element.getAttribute('data-player-action') || element.tagName.toLowerCase()
    recordTimeline('ui.action',{name})
    // eventPhase prevents later asynchronous work inheriting a completed dispatch.
    actionTimer=setTimeout(()=>{if(action===id){action=undefined;actionEvent=undefined}actionTimer=undefined},0)
  }
  const errorCode = (cause: unknown): string => {
    const code=normalizeApiError(cause).code
    if(['CANCELLED','TIMEOUT','UNAVAILABLE','NOT_FOUND','VALIDATION','CONFLICT','PERMISSION_DENIED','UNSUPPORTED','STORAGE','CORRUPT','AUTHENTICATION_REQUIRED','RATE_LIMITED'].includes(code))return code
    const name = cause instanceof Error || cause instanceof DOMException ? cause.name : ''
    return ['Error','TypeError','RangeError','ReferenceError','SyntaxError','URIError','EvalError','AbortError','CancelError','CancellationError','InvalidStateError','NotSupportedError','NetworkError','TimeoutError','QuotaExceededError','OperationError','NotAllowedError'].includes(name) ? name : 'UNHANDLED'
  }
  const error = (event: ErrorEvent) => recordTimeline('ui.error',{name:'window.error',code:errorCode(event.error),outcome:'failed',data:{line:event.lineno,column:event.colno}})
  const rejection = (event: PromiseRejectionEvent) => {
    const code=errorCode(event.reason)
    recordTimeline(code==='CANCELLED'?'ui.lifecycle':'ui.error',{name:'unhandledrejection',code,outcome:code==='CANCELLED'?'cancelled':'failed'})
  }
  const visibility = () => { recordTimeline('ui.lifecycle',{name:document.visibilityState});if(document.hidden)void flushDiagnostics() }
  const pagehide = () => { recordTimeline('ui.lifecycle',{name:'pagehide'});void flushDiagnostics() }
  document.addEventListener('click',click,true)
  document.addEventListener('change',click,true)
  document.addEventListener('submit',click,true)
  document.addEventListener('visibilitychange',visibility)
  window.addEventListener('error',error)
  window.addEventListener('unhandledrejection',rejection)
  window.addEventListener('pagehide',pagehide)
  return () => {
    disposed = true; clearTimeout(timer); clearTimeout(actionTimer);action=undefined;actionEvent=undefined;timer = undefined; queue.length=0;queuedBytes=0;transport=undefined
    document.removeEventListener('click',click,true);document.removeEventListener('visibilitychange',visibility)
    document.removeEventListener('change',click,true);document.removeEventListener('submit',click,true)
    window.removeEventListener('error',error);window.removeEventListener('unhandledrejection',rejection);window.removeEventListener('pagehide',pagehide)
  }
}
