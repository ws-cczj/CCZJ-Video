import { onActivated, onDeactivated, onMounted, onScopeDispose, ref, type Directive, type Ref } from 'vue'

/** Hold reveals mobile removal actions; Back exits without changing saved data. */
export function useHoldToEdit(enabled: boolean, editing: Ref<boolean> = ref(false), onFinish = () => {}) {
  let active = true
  const disposers = new Map<HTMLElement, () => void>()
  const cancelPending = new Set<() => void>()
  function finish(): void { cancelPending.forEach(cancel => cancel()); editing.value = false; onFinish() }
  function back(event: Event): void {
    if (!active || !editing.value || document.querySelector('[role="dialog"]')) return
    event.preventDefault(); event.stopImmediatePropagation(); finish()
  }
  onMounted(() => { if (enabled) window.addEventListener('cczj:android-overlay-back', back) })
  onActivated(() => { active = true })
  onDeactivated(() => { active = false; finish() })
  onScopeDispose(() => {
    active = false; finish(); disposers.forEach(dispose => dispose())
    window.removeEventListener('cczj:android-overlay-back', back)
  })
  const vHoldToEdit: Directive<HTMLElement, boolean | undefined> = {
    mounted(element, binding) {
      if (!enabled) return
      element.dataset.cczjHoldEnabled = String(binding.value !== false)
      let timer: ReturnType<typeof setTimeout> | undefined
      let origin: { x: number; y: number; id: number } | undefined
      let suppressClick = false
      const cancel = () => { clearTimeout(timer); timer = undefined; origin = undefined }
      const allowed = () => active && element.dataset.cczjHoldEnabled !== 'false'
      const enter = () => { cancel(); editing.value = true; suppressClick = true }
      const down = (event: PointerEvent) => {
        cancel(); suppressClick = false
        if (!allowed() || editing.value || !event.isPrimary || event.button !== 0 || (event.target as Element).closest('input,textarea,select,[data-cczj-remove]')) return
        origin = { x: event.clientX, y: event.clientY, id: event.pointerId }
        timer = setTimeout(enter, 520)
      }
      const move = (event: PointerEvent) => {
        if (origin?.id === event.pointerId && Math.hypot(event.clientX-origin.x, event.clientY-origin.y) > 10) cancel()
      }
      const click = (event: MouseEvent) => {
        if (!suppressClick) return
        suppressClick = false; event.preventDefault(); event.stopImmediatePropagation()
      }
      const context = (event: MouseEvent) => {
        if (!allowed() || (event.target as Element).closest('[data-cczj-remove]')) return
        event.preventDefault(); if (!editing.value) enter()
      }
      element.addEventListener('pointerdown', down)
      element.addEventListener('pointermove', move)
      element.addEventListener('click', click, true)
      element.addEventListener('contextmenu', context)
      const endings = ['pointerup', 'pointercancel', 'pointerleave'] as const
      endings.forEach(name => element.addEventListener(name, cancel))
      cancelPending.add(cancel)
      disposers.set(element, () => {
        cancel(); cancelPending.delete(cancel)
        element.removeEventListener('pointerdown', down); element.removeEventListener('pointermove', move)
        element.removeEventListener('click', click, true); element.removeEventListener('contextmenu', context)
        endings.forEach(name => element.removeEventListener(name, cancel))
      })
    },
    updated(element, binding) { if (enabled) element.dataset.cczjHoldEnabled = String(binding.value !== false) },
    unmounted(element) { disposers.get(element)?.(); disposers.delete(element) },
  }
  return { editing, finish, vHoldToEdit }
}
