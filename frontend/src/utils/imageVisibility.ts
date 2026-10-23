// Lazy loading must cover the proxy request, not only the final img download.
const waiting = new Map<Element, () => void>()
let observer: IntersectionObserver | null = null

function stop(element: Element): void {
  waiting.delete(element)
  observer?.unobserve(element)
  if (!waiting.size) { observer?.disconnect(); observer = null }
}

export function whenImageNearViewport(element: Element, ready: () => void): () => void {
  if (typeof IntersectionObserver === 'undefined') { ready(); return () => {} }
  if (!observer) {
    observer = new IntersectionObserver(entries => {
      for (const entry of entries) {
        if (!entry.isIntersecting) continue
        const callback = waiting.get(entry.target)
        stop(entry.target)
        callback?.()
      }
    }, { rootMargin: '240px', threshold: 0 })
  }
  waiting.set(element, ready)
  observer.observe(element)
  return () => stop(element)
}
