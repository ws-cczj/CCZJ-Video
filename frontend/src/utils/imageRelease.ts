// Brief route transitions should reuse decoded images instead of repeatedly
// allocating GPU resources. All inactive images share one retirement timer.
const pending = new Set<() => void>()
let timer: number | null = null

export function deferImageRelease(release: () => void): () => void {
  pending.add(release)
  if (timer === null) timer = window.setTimeout(() => {
    timer = null
    const callbacks = [...pending]
    pending.clear()
    callbacks.forEach(callback => callback())
  }, 750)
  return () => {
    pending.delete(release)
    if (!pending.size && timer !== null) { window.clearTimeout(timer); timer = null }
  }
}
