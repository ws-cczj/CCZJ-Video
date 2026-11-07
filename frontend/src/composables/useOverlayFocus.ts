import { onBeforeUnmount, onDeactivated, watch, type Ref } from 'vue'

interface OverlayOwner {
  element: HTMLElement
  previous: HTMLElement | null
  close: () => void
  closable: () => boolean
  open: () => boolean
}

const stack: OverlayOwner[] = []
const focusable = 'button:not(:disabled),input:not(:disabled),select:not(:disabled),textarea:not(:disabled),a[href],[tabindex]:not([tabindex="-1"])'

function keydown(event: KeyboardEvent): void {
  const owner = stack.at(-1)
  if (!owner || (event.key !== 'Escape' && event.key !== 'Tab')) return
  if (event.key === 'Escape' && event.target instanceof Element && event.target.closest('[role="combobox"][aria-expanded="true"]')) return
  event.stopImmediatePropagation()
  if (event.key === 'Escape') {
    event.preventDefault()
    if (owner.open() && owner.closable()) owner.close()
    return
  }
  const targets = Array.from(owner.element.querySelectorAll<HTMLElement>(focusable))
    .filter(element => element.getClientRects().length && !element.closest('[inert],[aria-hidden="true"]'))
  const first = targets[0], last = targets.at(-1), current = document.activeElement
  if (!first || !owner.element.contains(current) || (event.shiftKey && (current === first || current === owner.element))) {
    event.preventDefault()
    ;(event.shiftKey ? last : first)?.focus({ preventScroll: true })
    if (!first) owner.element.focus({ preventScroll: true })
  } else if (!event.shiftKey && current === last) {
    event.preventDefault()
    first.focus({ preventScroll: true })
  }
}

function back(event: Event): void {
  const owner = stack.at(-1)
  if (!owner) return
  event.preventDefault()
  event.stopImmediatePropagation()
  if (owner.open() && owner.closable()) owner.close()
}

function listen(enabled: boolean): void {
  if (enabled) {
    window.addEventListener('keydown', keydown, true)
    window.addEventListener('cczj:android-overlay-back', back, true)
  } else {
    window.removeEventListener('keydown', keydown, true)
    window.removeEventListener('cczj:android-overlay-back', back, true)
  }
}

/** Modal and reader overlays share topmost-only dismissal and focus ownership.
 * A closing modal retains its place until its stable transition finishes. */
export function useOverlayFocus(element: Ref<HTMLElement | null>, open: () => boolean, close: () => void, closable = () => true) {
  let owner: OverlayOwner | undefined
  function release(): void {
    if (!owner) return
    const top = stack.at(-1) === owner
    const previous = owner.previous
    const index = stack.indexOf(owner)
    if (index >= 0) stack.splice(index, 1)
    owner = undefined
    if (!stack.length) listen(false)
    if (top) {
      const target = previous?.isConnected && !previous.matches(':disabled') && !previous.closest('[inert],[aria-hidden="true"]') ? previous : stack.at(-1)?.element
      target?.focus({ preventScroll: true })
    }
  }
  watch([element, open], ([node, shown]) => {
    if (!shown || !node) return
    if (owner?.element === node) return
    if (owner) release()
    owner = { element: node, previous: document.activeElement instanceof HTMLElement ? document.activeElement : null, open, close, closable }
    if (!stack.length) listen(true)
    stack.push(owner)
    node.focus({ preventScroll: true })
  }, { immediate: true, flush: 'post' })
  onDeactivated(release)
  onBeforeUnmount(release)
  return { release }
}

if (import.meta.hot) import.meta.hot.dispose(() => { listen(false); stack.length = 0 })
