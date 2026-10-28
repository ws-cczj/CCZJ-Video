// Lazy loading must cover the proxy request, not only the final img download.
type Observation = { observer: IntersectionObserver; waiting: Map<Element, () => void> }
const observations = new Map<Element | null, Observation>()

export function whenImageNearViewport(element: Element, ready: () => void, root: Element | null = null): () => void {
  if (typeof IntersectionObserver === 'undefined') { ready(); return () => {} }
  let observation = observations.get(root)
  if (!observation) {
    const waiting = new Map<Element, () => void>()
    const observer = new IntersectionObserver(entries => {
      for (const entry of entries) {
        if (!entry.isIntersecting) continue
        const callback = waiting.get(entry.target)
        stop(entry.target)
        callback?.()
      }
    }, { root, rootMargin: '240px', threshold: 0 })
    observation = { observer, waiting }
    observations.set(root, observation)
  }
  const { observer, waiting } = observation
  function stop(target: Element): void {
    if (!waiting.has(target)) return
    waiting.delete(target)
    observer.unobserve(target)
    if (!waiting.size) {
      observer.disconnect()
      if (observations.get(root) === observation) observations.delete(root)
    }
  }
  waiting.set(element, ready)
  observer.observe(element)
  return () => stop(element)
}
