import { useRoute } from 'vue-router'
import { onScopeDispose } from 'vue'
import { primaryTabs } from './routeMotion'

export function useTabSwipe(enabled: () => boolean, presentation: { preview: (path: string, edge: number) => void; move: (distance: number) => void; settle: (path?: string) => Promise<void> }) {
  const route = useRoute()
  let lastDistance = 0
  let start: { x: number; y: number; width: number; path: string; target: string; locked: boolean } | undefined
  let gestureSurface: EventTarget | null = null
  function releaseMove(): void {
    gestureSurface?.removeEventListener('touchmove', touchMove as EventListener)
    gestureSurface = null
  }
  let suppressUntil = 0
  let sampleX = 0, sampleAt = 0, velocity = 0
  let settling = false
  function cancel(): void {
    const wasDragging = start?.locked
    start = undefined
    releaseMove()
    if (wasDragging && !settling) { settling = true; void presentation.settle().finally(() => { settling = false }) }
  }
  function touchStart(event: TouchEvent): void {
    cancel(); suppressUntil = 0; lastDistance = 0
    if (settling || !enabled() || event.touches.length !== 1 || route.query.group || Array.from(document.querySelectorAll('[role="dialog"]')).some(el => el.getBoundingClientRect().width > 0)) return
    const group = primaryTabs.find(tabs => tabs.includes(route.path))
    if (!group) return
    const element = event.target as HTMLElement
    const nav = element.closest('nav')?.querySelector('a[href]') && element.closest('nav')
    if (!nav) {
      if (element.closest('input,textarea,select,[role="slider"],.select-dropdown,.carousel,[data-cczj-no-tab-swipe]')) return
      const scene = element.closest('[data-route-active="true"]')
      if (!scene || scene.querySelector('[data-cczj-editing="true"]')) return
      // Horizontal lists own their drag, including filter and folder strips.
      for (let node: HTMLElement | null = element; node && node !== scene; node = node.parentElement) {
        if (node.scrollWidth > node.clientWidth + 2 && /auto|scroll/.test(getComputedStyle(node).overflowX) && node.scrollHeight <= node.clientHeight + 2) return
      }
    }
    const touch = event.touches[0]
    const width = innerWidth
    // Preserve Android's edge Back gesture.
    if (touch.clientX < 20 || touch.clientX > width - 20) return
    start = { x: touch.clientX, y: touch.clientY, width, path: route.fullPath, target: '', locked: false }
    sampleX = touch.clientX; sampleAt = performance.now(); velocity = 0
    // Only a possible tab gesture needs a cancellable listener. Release it as
    // soon as vertical intent wins, so continued scrolling stays on Chromium's
    // compositor instead of waiting for a main-thread touchmove every frame.
    gestureSurface = event.currentTarget
    gestureSurface?.addEventListener('touchmove', touchMove as EventListener, { passive: false })
  }
  function touchMove(event: TouchEvent): void {
    if (!start) return
    if (event.touches.length !== 1 || start.path !== route.fullPath) { cancel(); return }
    const dx = event.touches[0].clientX-start.x, dy = event.touches[0].clientY-start.y
    if (!start.locked && Math.abs(dy) > 10 && Math.abs(dy) >= Math.abs(dx)) { cancel(); return }
    if (!start.locked && Math.abs(dx) > 12 && Math.abs(dx) > Math.abs(dy)*1.5) start.locked = true
    if (!start.locked) return
    lastDistance = dx
    const now = performance.now(), elapsed = now - sampleAt
    if (elapsed > 0) velocity = (event.touches[0].clientX - sampleX) / elapsed
    sampleX = event.touches[0].clientX; sampleAt = now
    if (event.cancelable) event.preventDefault()
    const group = primaryTabs.find(tabs => tabs.includes(route.path))!
    start.target = group[group.indexOf(route.path)+(dx < 0 ? 1 : -1)] || ''
    presentation.preview(start.target, dx < 0 ? 1 : -1)
    presentation.move(start.target ? Math.max(-start.width, Math.min(start.width, dx)) : Math.max(-28, Math.min(28, dx*.18)))
  }
  function touchEnd(): void {
    const gesture = start; start = undefined; releaseMove()
    if (!gesture?.locked) return
    suppressUntil = performance.now()+500
    settling = true
    const flick = performance.now()-sampleAt < 100 && Math.abs(velocity) > .45 && Math.abs(lastDistance) > 32 && Math.sign(velocity) === Math.sign(lastDistance)
    void presentation.settle(gesture.target && gesture.path === route.fullPath && (Math.abs(lastDistance) >= gesture.width*.22 || flick) ? gesture.target : undefined).finally(() => { settling = false })
  }
  function click(event: MouseEvent): void {
    if (performance.now() < suppressUntil) { event.preventDefault(); event.stopImmediatePropagation() }
  }
  onScopeDispose(() => { start = undefined; releaseMove() })
  return { touchStart, touchEnd, cancel, click }
}
