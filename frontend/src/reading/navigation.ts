import type { Router } from 'vue-router'

/** Chapter changes replace the reader URL, so history.back retains the entry
 * page (including its query). Never push a detail page as a back action: its
 * own back button would then reopen the reader. Deep links have no entry. */
export function leaveReader(router: Router, mode: 'novel' | 'comic', fallback: string): void {
  const previous = router.options.history.state.back
  const path = typeof previous === 'string' ? previous.split(/[?#]/)[0]! : ''
  const sameMode = path === `/${mode}` || path.startsWith(`/${mode}/`)
  if (sameMode && !path.startsWith(`/${mode}/read/`)) router.back()
  else void router.replace(fallback)
}
