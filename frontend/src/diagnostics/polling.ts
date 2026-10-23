/** Polling lifecycle used by retained settings panels. */
export function createVisiblePolling<T>(request: () => Promise<T> & {cancel?:()=>void|Promise<void>}, receive: (value: T) => void, fail: (error: unknown) => void, interval = 2000) {
  let timer: ReturnType<typeof setTimeout> | null = null
  let active = false
  let generation = 0
  let busy = false
  let pending:(Promise<T>&{cancel?:()=>void|Promise<void>})|undefined
  const run = async () => {
    if (!active || busy) return
    busy = true
    const captured = generation
    try {
      pending=request()
      const value = await pending
      if (active && captured === generation) receive(value)
    } catch (error) {
      if (active && captured === generation) fail(error)
    } finally {
      busy = false
      pending=undefined
      if (active) timer = setTimeout(() => { timer = null; void run() }, interval)
    }
  }
  return {
    setActive(next: boolean) {
      if (active === next) return
      active = next
      generation++
      if (timer !== null) clearTimeout(timer)
      timer = null
      if(!active){try{void Promise.resolve(pending?.cancel?.()).catch(()=>{})}catch{/* Request already settled. */}}
      if (active) void run()
    },
  }
}
