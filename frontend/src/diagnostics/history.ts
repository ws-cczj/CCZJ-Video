export interface ResourceSample {
  at: number
  heap: number
  committed: number | null
  cpu: number | null
  goroutines: number
  handles: number | null
}

export const RESOURCE_HISTORY_LIMIT = 150

/** Keep only compact numbers, never whole snapshots or process trees. */
export function appendResourceSample(history: ResourceSample[], metrics: Record<string, any>, nativeMemory?: number): ResourceSample[] {
  const data = metrics.resources
  if (!data?.sampled_at_ms || history[history.length - 1]?.at === data.sampled_at_ms) return history
  const sample: ResourceSample = {
    at: Number(data.sampled_at_ms), heap: Number(data.heap_alloc_bytes) || 0,
    committed: nativeMemory ?? (data.native_available ? (data.native_scope==='app_process' ? data.proportional_bytes ?? null : Number(data.tree_private_bytes) || 0) : null),
    cpu: data.cpu_percent ?? null, goroutines: Number(metrics.goroutines) || 0,
    handles: data.native_available ? (data.native_scope==='app_process' ? data.file_descriptors ?? null : Number(data.handles) || 0) : null,
  }
  return [...history.slice(-(RESOURCE_HISTORY_LIMIT - 1)), sample]
}
