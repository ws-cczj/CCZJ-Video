import type { RouteLocationNormalizedLoaded } from 'vue-router'

/** The catalogue follows the application theme; only playback owns dark chrome. */
export function shortDramaScene(route: Pick<RouteLocationNormalizedLoaded, 'path' | 'query'>, mobile: boolean): 'theatre' | 'playback' | null {
  if (route.path !== '/short-drama') return null
  const { source, vod, view } = route.query
  const series = typeof source === 'string' && !!source && typeof vod === 'string' && !!vod
  return series || (mobile ? view !== 'theatre' : view === 'feed') ? 'playback' : 'theatre'
}
