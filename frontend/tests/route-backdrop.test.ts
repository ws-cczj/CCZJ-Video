import {expect,it} from 'vitest'
import {routeBackdropFocus,routeBackdropFrame} from '../src/mobile/routeBackdrop'

it('uses stable bounded crops for every tab and returns to identical coordinates',()=>{
 const paths=['/','/search','/favorites','/history','/settings','/sources','/settings?unused','/novel','/novel/shelf','/comic/library']
 for(const path of paths){const focus=routeBackdropFocus(path);expect(focus.x).toBeGreaterThanOrEqual(0);expect(focus.x).toBeLessThanOrEqual(1);expect(focus.y).toBeGreaterThanOrEqual(0);expect(focus.y).toBeLessThanOrEqual(1);expect(routeBackdropFocus(path)).toEqual(focus)}
 expect(routeBackdropFocus('/search').x).toBeLessThan(routeBackdropFocus('/settings').x)
 expect(routeBackdropFocus('/settings','theme')).not.toEqual(routeBackdropFocus('/settings'))
})
it('allocates a full equal viewport cell to each primary tab without gaps',()=>{
 const paths=['/','/short-drama','/search','/favorites','/settings']
 const positions=paths.map(path=>{
   const frame=routeBackdropFrame(path,routeBackdropFocus(path))
   expect(frame.width).toBe('500%')
   const translation=Number(frame.transform.match(/translate3d\(([-\d.]+)%/)?.[1])
   // Translation is a percentage of the entire 5-screen canvas.
   return -translation/100*5*360
 })
 positions.forEach((position,index)=>expect(position).toBeCloseTo(index*360,8))
 const midway=routeBackdropFrame('/',{x:.125,y:0})
 expect(midway.transform).toBe('translate3d(-10%,0%,0)')
})
it('does not move a reader backdrop on chapter or episode changes',()=>{
 expect(routeBackdropFocus('/novel/read/1/101')).toEqual(routeBackdropFocus('/novel/read/1/9000'))
 expect(routeBackdropFocus('/comic/read/2/101')).toEqual(routeBackdropFocus('/comic/read/2/102'))
})
