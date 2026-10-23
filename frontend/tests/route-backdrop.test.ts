import {expect,it} from 'vitest'
import {routeBackdropFocus} from '../src/mobile/routeBackdrop'

it('uses stable bounded crops for every tab and returns to identical coordinates',()=>{
 const paths=['/','/search','/favorites','/history','/settings','/sources','/settings?unused','/novel','/novel/shelf','/comic/library']
 for(const path of paths){const focus=routeBackdropFocus(path);expect(focus.x).toBeGreaterThanOrEqual(0);expect(focus.x).toBeLessThanOrEqual(1);expect(focus.y).toBeGreaterThanOrEqual(0);expect(focus.y).toBeLessThanOrEqual(1);expect(routeBackdropFocus(path)).toEqual(focus)}
 expect(routeBackdropFocus('/search').x).toBeLessThan(routeBackdropFocus('/settings').x)
 expect(routeBackdropFocus('/settings','theme')).not.toEqual(routeBackdropFocus('/settings'))
})
it('does not move a reader backdrop on chapter or episode changes',()=>{
 expect(routeBackdropFocus('/novel/read/1/101')).toEqual(routeBackdropFocus('/novel/read/1/9000'))
 expect(routeBackdropFocus('/comic/read/2/101')).toEqual(routeBackdropFocus('/comic/read/2/102'))
})
