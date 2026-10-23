/** Mouse/pen dragging for overflowing tab strips. Touch and wheel retain their
 * native axes; a drag suppresses only its following click, not normal selection. */
export function useHorizontalDrag() {
  let element: HTMLElement | null = null, pointer = -1, startX = 0, startY = 0, left = 0, dragged = false
  function end() {
    if (element) {
      element.classList.remove('is-dragging')
      if (element.hasPointerCapture(pointer)) element.releasePointerCapture(pointer)
    }
    element = null; pointer = -1
  }
  return {
    down(event: PointerEvent) {
      dragged = false
      if (event.pointerType === 'touch' || event.button !== 0) return
      const target = event.currentTarget as HTMLElement
      if (target.scrollWidth <= target.clientWidth + 1) return
      element = target; pointer = event.pointerId; startX = event.clientX; startY = event.clientY; left = target.scrollLeft
    },
    move(event: PointerEvent) {
      if (!element || event.pointerId !== pointer) return
      const distance = event.clientX - startX
      if (!dragged && (Math.abs(distance) < 6 || Math.abs(distance) <= Math.abs(event.clientY - startY))) return
      if (!dragged) { dragged = true; element.setPointerCapture(pointer); element.classList.add('is-dragging') }
      event.preventDefault(); element.scrollLeft = left - distance
    },
    end,
    click(event: MouseEvent) { if (dragged) { event.preventDefault(); event.stopImmediatePropagation(); dragged = false } },
  }
}
