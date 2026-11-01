// Lazy loading must cover the proxy request, not only the final img download.
type Observation = { observer: IntersectionObserver; waiting: Map<Element, (visible: boolean) => void> }
const observations = new Map<Element | null, Map<string, Observation>>()

export function whenImageNearViewport(element: Element, ready: () => void, root: Element | null = null): () => void {
  let cancel = () => {}
  cancel = observeViewportVisibility(element, visible => {
    if (visible) { cancel(); ready() }
  }, root, '240px')
  return cancel
}

/** Continuous consumers (such as carousels) share the same observer ownership. */
export function observeViewportVisibility(element: Element, changed: (visible: boolean) => void, root: Element | null = null, margin = '0px'): () => void {
  if (typeof IntersectionObserver === 'undefined') { changed(true); return () => {} }
  let margins = observations.get(root)
  if (!margins) { margins = new Map(); observations.set(root, margins) }
  const group = margins
  let observation = group.get(margin)
  if (!observation) {
    const waiting = new Map<Element, (visible: boolean) => void>()
    const observer = new IntersectionObserver(entries => {
      for (const entry of entries) {
        const callback = waiting.get(entry.target)
        callback?.(entry.isIntersecting)
      }
    }, { root, rootMargin: margin, threshold: 0 })
    observation = { observer, waiting }
    group.set(margin, observation)
  }
  const { observer, waiting } = observation
  function stop(target: Element): void {
    if (!waiting.has(target)) return
    waiting.delete(target)
    observer.unobserve(target)
    if (!waiting.size) {
      observer.disconnect()
      if (group.get(margin) === observation) group.delete(margin)
      if (!group.size && observations.get(root) === group) observations.delete(root)
    }
  }
  waiting.set(element, changed)
  observer.observe(element)
  return () => stop(element)
}
