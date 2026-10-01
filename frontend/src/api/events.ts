import { Events } from '@wailsio/runtime'

export interface BackendEventPayloads {
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
): () => void
export function onBackendEvent<T>(name: string, handler: (payload: T) => void): () => void
export function onBackendEvent<T>(name: string, handler: (payload: T) => void): () => void {
  const unsubscribe = Events.On(name, (event: { data: T }) => handler(event.data)) as unknown
  if (typeof unsubscribe === 'function') {
    return unsubscribe as () => void
  }
  return () => Events.Off(name)
}
