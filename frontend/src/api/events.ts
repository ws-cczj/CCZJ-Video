import { Events } from '@wailsio/runtime'
import { eventDelivery,recordEvent } from './eventPipeline'
import { diagnosticMode } from '../diagnostics/modeScope'

const progressFlushers=new Set<()=>void>()
function frame(work:()=>void):()=>void{
 if(typeof requestAnimationFrame==='function'){const id=requestAnimationFrame(work);return ()=>cancelAnimationFrame(id)}
 const id=setTimeout(work,16);return ()=>clearTimeout(id)
}
export interface EventOptions<T>{accept?:(value:T)=>boolean;decode?:(value:unknown)=>T}

// Native event names belong to the runtime adapter, alongside subscriptions.
export function nativeWindowEvents() { return Events.Types.Common }

export interface BackendEventPayloads {
  'update:download:progress': { downloaded?: number; total?: number; speed_bps?: number }
  // Go 侧 VideoDownloadStatus 只按 JSON tag 出字段，全是 snake_case，没有第二套写法。
  'download:progress': {
    task_id?: string
    downloaded?: number
    total?: number
  }
  'collect:progress': { operation_id?: string; source_key?: string; current?: number; total?: number }
  'collect:done': { operation_id?: string; source_key?: string; error?: string; mode?: string }
  'collect:log': { operation_id?: string; source_key?: string; message?: string }
  'collect:page': { operation_id?: string; source_key?: string; page?: number; names?: string[] }
  'search:progress': { requestId?: string; source_key?: string; keyword?: string; page?: number; stage?: string; message?: string; current?: number; total?: number }
  'app:ready': { data_dir?: string; schema_version?: string }
  'cache:invalidate': { scope?: string; source_key?: string; vod_ids?: string[]; reason?: string }
  /** 原生拖放：Go 只交被拖条目的绝对路径，读目录与校验都在 app/plugin。 */
  'plugin:filedrop': string[]
}

/**
 * Centralises Wails event subscriptions so feature stores receive a reliable
 * disposer regardless of the runtime version's return convention.
 */
export function onBackendEvent<K extends keyof BackendEventPayloads>(
  name: K,
  handler: (payload: BackendEventPayloads[K]) => void,
  options?:EventOptions<BackendEventPayloads[K]>,
): () => void
export function onBackendEvent<T>(name: string, handler: (payload: T) => void,options?:EventOptions<T>): () => void
export function onBackendEvent<T>(name: string, handler: (payload: T) => void,options?:EventOptions<T>): () => void {
  const owner=diagnosticMode()
  const delivery=eventDelivery(name,handler,frame,value=>diagnosticMode()===owner&&(options?.accept?.(value)??true),value=>recordEvent(owner,value),options?.decode)
  const progress=name.endsWith(':progress')
  if(progress)progressFlushers.add(delivery.flush)
  const unsubscribe = Events.On(name, (event: { data: unknown }) => {
    if(name.endsWith(':done')||name.endsWith(':error'))for(const flush of progressFlushers)flush()
    delivery.receive(event.data)
  }) as unknown
  return ()=>{progressFlushers.delete(delivery.flush);delivery.dispose();if(typeof unsubscribe==='function')unsubscribe();else Events.Off(name)}
}
