export type PreparationStage = 'accessories' | 'danmaku' | 'ready'
/** Media metadata is the entry event. Optional work has bounded waits, while a
 * media replacement aborts the sequence and releases its timers/listeners. */
export async function preparePlayback(options: {
  signal: AbortSignal
  stage: (stage: PreparationStage) => void
  accessories: () => Promise<unknown>
  danmaku: () => Promise<unknown>
  timeout?: (stage: PreparationStage) => void
  accessoryBudget?: number
  danmakuBudget?: number
}): Promise<boolean> {
  async function wait(stage: PreparationStage, work: () => Promise<unknown>, budget: number) {
    if (options.signal.aborted) return
    options.stage(stage)
    let timer: ReturnType<typeof setTimeout> | undefined
    let abort: (() => void) | undefined
    let waiting = true
    try {
      await Promise.race([
        Promise.resolve().then(() => options.signal.aborted ? undefined : work()).catch(() => { if (waiting && !options.signal.aborted) options.timeout?.(stage) }),
        new Promise<void>(resolve => {
          abort = resolve
          options.signal.addEventListener('abort', abort, { once: true })
          timer = setTimeout(() => { if (!options.signal.aborted) options.timeout?.(stage); resolve() }, budget)
        }),
      ])
    } finally {
      waiting = false
      clearTimeout(timer)
      if (abort) options.signal.removeEventListener('abort', abort)
    }
  }
  await wait('accessories', options.accessories, options.accessoryBudget ?? 1500)
  if (options.signal.aborted) return false
  await wait('danmaku', options.danmaku, options.danmakuBudget ?? 2500)
  if (options.signal.aborted) return false
  options.stage('ready')
  return true
}
