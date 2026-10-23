/** Stop existing and newly-triggered CSS motion before a player is detached.
 * Resetting progress during teardown would otherwise start a new transition
 * after the old animations were cancelled, retaining the closed DOM in a
 * hidden WebView's suspended DocumentTimeline.
 */
export function releasePlayerAnimations(root: HTMLElement | null | undefined): void {
  if (!root) return
  // Capture the objects before changing CSS: a hidden WebView may stop
  // enumerating them as soon as the animation/transition properties change.
  const animations = new Set<Animation>()
  try { root.getAnimations({ subtree: true }).forEach(animation => animations.add(animation)) } catch { /* Older engines. */ }
  for (const element of [root, ...Array.from(root.querySelectorAll<HTMLElement>('*'))]) {
    element.style?.setProperty('animation', 'none', 'important')
    element.style?.setProperty('transition', 'none', 'important')
  }
  try {
    root.getAnimations({ subtree: true }).forEach(animation => animations.add(animation))
  }
  catch { /* Inline styles still stop CSS motion in older engines. */ }
  for (const animation of animations) {
    try { animation.cancel() } catch { /* Release the remaining animations too. */ }
  }
}
