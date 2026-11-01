import type { RouteLocationNormalizedLoaded } from 'vue-router'
type RouteShape=Pick<RouteLocationNormalizedLoaded,'path'|'query'|'name'|'params'>
export const primaryTabs=[['/','/short-drama','/search','/favorites','/settings'],['/novel','/novel/shelf','/novel/settings'],['/comic','/comic/library','/comic/settings']]
export function routeScreenKey(route:RouteShape):string {
  if(/^\/(novel|comic)\/read\//.test(route.path))return route.path.split('/').slice(0,4).join('/')
  if(route.name==='player')return `player:${String(route.params.sourceKey||'')}:${String(route.params.globalId||'')}`
  if(['/settings','/novel/settings','/comic/settings'].includes(route.path))return route.path+':'+String(route.query.group||'index')
  return route.path
}
function depth(route:RouteShape):number {
  if(['/settings','/novel/settings','/comic/settings'].includes(route.path))return route.query.group?1:0
  if(primaryTabs.some(group=>group.includes(route.path)))return 0
  return /\/(?:read|player)\//.test(route.path)?2:1
}
export function routeDirection(from:RouteShape,to:RouteShape,previousPosition:number,nextPosition:number):'forward'|'back' {
  if(nextPosition<previousPosition)return 'back'
  if(depth(to)!==depth(from))return depth(to)>depth(from)?'forward':'back'
  for(const group of primaryTabs){const a=group.indexOf(from.path),b=group.indexOf(to.path);if(a>=0&&b>=0&&a!==b)return b>a?'forward':'back'}
  if(/\/(?:read)\//.test(to.path)&&to.path.split('/').slice(0,-1).join('/')===from.path.split('/').slice(0,-1).join('/'))return Number(to.path.split('/').at(-1))<Number(from.path.split('/').at(-1))?'back':'forward'
  return 'forward'
}

/** Peer sections move together; a deeper destination covers its parent.
 * Chapter/episode identity is already collapsed by routeScreenKey. */
export function routeTransition(from:RouteShape,to:RouteShape,previousPosition:number,nextPosition:number,mobile=true,sections:readonly string[]=[]):string {
  if(!mobile && from.name!==to.name && (from.name==='player'||to.name==='player'))return to.name==='player'?'cczj-desktop-player-open':'cczj-desktop-player-close'
  const fromIndex=sections.indexOf(from.path),toIndex=sections.indexOf(to.path)
  const desktopSections = !mobile && fromIndex>=0 && toIndex>=0 && fromIndex!==toIndex
  if(desktopSections)return toIndex>fromIndex?'cczj-desktop-tab-forward':'cczj-desktop-tab-back'
  const direction = routeDirection(from,to,previousPosition,nextPosition)
  const peer = primaryTabs.some(group => group.includes(from.path) && group.includes(to.path))
    && !from.query.group && !to.query.group
  if(peer)return mobile ? `cczj-route-tab-${direction}` : `cczj-desktop-tab-${direction}`
  return `cczj-mobile-route-${direction}`
}
