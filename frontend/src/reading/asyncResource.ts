import { getCurrentScope, onScopeDispose, ref, type Ref } from 'vue'
import { normalizeApiError, type ApiError } from '../api/app'
import { activityDiagnostics } from '../diagnostics/activity'

/**
 * 阅读模式的资源加载五态。
 *
 * 这五个状态是刻意分开的，合成一个 `loading | error` 会丢掉两件界面必须区分的事：
 *
 * - **empty 不是 error**：空列表只能代表一次成功的"没有结果"（reports/00:135）。
 *   把它并进 error，用户看到的是"搜索失败了"，而真正发生的是"这个源确实没有这本书"，
 *   两种情况的处置完全相反——前者该重试，后者该换一个词或换一个源。
 * - **cancelled 不是 error**：用户翻页/切章会主动取消上一批请求。把它记成错误
 *   会在界面上闪一条红提示，而那是用户自己刚刚做的操作。
 */
export type ResourceStatus = 'idle' | 'loading' | 'ready' | 'empty' | 'error'

export interface AsyncResource<T, A extends unknown[] = []> {
  status: Ref<ResourceStatus>
  data: Ref<T | null>
  error: Ref<ApiError | null>
  /** 触发一次加载。旧的在途响应会因为代号不匹配被丢掉。 */
  run: (...args: A) => Promise<void>
  /** 用同样的参数重来一次。错误态下的"重试"按钮走这里。 */
  retry: () => Promise<void>
  /** 主动作废在途请求并把状态收回 idle。 */
  cancel: () => void
  /** 当前代号。调试用：迟到响应被丢弃时日志里能看到它属于哪一代。 */
  generation: () => number
}

export interface AsyncResourceOptions<T, A extends unknown[]> {
  diagnosticLabel?: string
  /** 真正干活的那一步。参数原样透传自 run。 */
  load: (...args: A) => Promise<T>
  /**
   * 判断一次**成功**的结果是不是"没有内容"。
   *
   * 只有它返回真才会进 empty；抛出来的错误一律进 error。
   * 不提供的话，成功的结果永远是 ready。
   */
  isEmpty?: (data: T) => boolean
  /** 加载成功后要做的副作用（例如把结果写进 store）。迟到响应不会触发它。 */
  onData?: (data: T) => void
}

/** Cancels superseded work when supported and rejects late results by generation.
 * Deferred promises make out-of-order completion testable without a real network.
 */
export function useAsyncResource<T, A extends unknown[] = []>(
  options: AsyncResourceOptions<T, A>,
): AsyncResource<T, A> {
  const status = ref<ResourceStatus>('idle') as Ref<ResourceStatus>
  const data = ref<T | null>(null) as Ref<T | null>
  const error = ref<ApiError | null>(null) as Ref<ApiError | null>
  let generation = 0
  let lastArgs: A | null = null
  let pending: (Promise<T> & { cancel?: () => unknown }) | null = null
  let finishObservation: ReturnType<typeof activityDiagnostics.begin> | null = null

  function cancelPending(): void {
    const previous = pending
    pending = null
    finishObservation?.('cancelled')
    finishObservation = null
    try { void Promise.resolve(previous?.cancel?.()).catch(() => {}) }
    catch { /* Generation still protects state if cancellation itself fails. */ }
  }

  async function run(...args: A): Promise<void> {
    generation += 1
    const current = generation
    cancelPending()
    const finish = activityDiagnostics.begin(options.diagnosticLabel ?? 'reading.request')
    finishObservation = finish
    lastArgs = args
    status.value = 'loading'
    // 错误先清掉：留着上一条错误会让"正在重试"的界面上同时显示旧错误与新加载态，
    // 用户分不清现在这一屏是新的还是旧的。
    error.value = null
    try {
      const request = options.load(...args)
      pending = request
      const result = await request
      finish('success')
      if (current !== generation) {
        return
      }
      data.value = result
      status.value = options.isEmpty && options.isEmpty(result) ? 'empty' : 'ready'
      options.onData?.(result)
    } catch (raw) {
      const normalized = normalizeApiError(raw)
      finish(normalized.code === 'CANCELLED' ? 'cancelled' : 'failed',normalized.code)
      if (current !== generation) {
        return
      }
      if (normalized.code === 'CANCELLED') {
        // 取消不是失败：状态收回 idle，不显示错误，也不触发重试按钮。
        status.value = 'idle'
        return
      }
      error.value = normalized
      status.value = 'error'
    } finally {
      if (current === generation) { pending = null; finishObservation = null }
    }
  }

  async function retry(): Promise<void> {
    if (lastArgs === null) {
      return
    }
    await run(...lastArgs)
  }

  function cancel(): void {
    generation += 1
    cancelPending()
    status.value = 'idle'
    error.value = null
  }

  if(getCurrentScope())onScopeDispose(cancel)
  return { status, data, error, run, retry, cancel, generation: () => generation }
}

/**
 * 一个列表是不是"成功但没有内容"。
 *
 * 单独抽出来是因为这条判断在小说与漫画两侧都要用，而写错的代价不对称：
 * 判成 ready 会让界面显示一个空壳（用户以为在加载），判成 error 会让
 * "这个源确实没有这本书"显示成"搜索失败"。
 */
export function isEmptyList(value: unknown): boolean {
  return Array.isArray(value) ? value.length === 0 : value == null
}
