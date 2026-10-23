import {expect,it} from 'vitest'
import {primaryTabs,routeDirection,routeScreenKey} from '../src/mobile/routeMotion'
const route=(path:string,group?:string)=>({path,query:group?{group}:{},name:undefined,params:{}})
it('retains one novel reader scene across chapters but separates books',()=>{
 expect(routeScreenKey(route('/novel/read/1/21'))).toBe(routeScreenKey(route('/novel/read/1/22')))
 expect(routeScreenKey(route('/novel/read/1/21'))).not.toBe(routeScreenKey(route('/novel/read/2/21')))
})
it('distinguishes settings screens while retaining inline tabs and player episodes',()=>{
 expect(routeScreenKey(route('/settings'))).not.toBe(routeScreenKey(route('/settings','basic')))
 expect(routeScreenKey({...route('/settings','basic'),query:{group:'basic',section:'data'}})).toBe(routeScreenKey(route('/settings','basic')))
 expect(routeScreenKey({...route('/player/source/12/0'),name:'player',params:{sourceKey:'source',globalId:'12',epIndex:'0'}})).toBe(routeScreenKey({...route('/player/source/12/1'),name:'player',params:{sourceKey:'source',globalId:'12',epIndex:'1'}}))
})
it('keeps novel discovery and shelf plus comic exploration and following as primary tabs',()=>{
 expect(routeScreenKey(route('/novel/settings','reading'))).not.toBe(routeScreenKey(route('/comic/settings','reading')))
 expect(primaryTabs.find(tabs=>tabs.includes('/novel'))).toEqual(['/novel','/novel/shelf','/novel/settings'])
 expect(primaryTabs.find(tabs=>tabs.includes('/comic'))).toEqual(['/comic','/comic/library','/comic/settings'])
 expect(routeDirection(route('/novel'),route('/novel/shelf'),4,5)).toBe('forward')
 expect(routeDirection(route('/novel/shelf'),route('/novel'),4,5)).toBe('back')
 expect(routeDirection(route('/novel'),route('/novel/search'),4,5)).toBe('forward')
 expect(routeDirection(route('/comic/settings'),route('/comic/library'),4,5)).toBe('back')
 expect(primaryTabs.find(tabs=>tabs.includes('/comic'))).not.toContain('/comic/search')
 for(const mode of ['novel','comic']){
  expect(routeScreenKey(route(`/${mode}/settings`))).not.toBe(routeScreenKey(route(`/${mode}/settings`,'reading')))
  expect(routeDirection(route(`/${mode}/settings`,'reading'),route(`/${mode}/settings`),4,4)).toBe('back')
 }
})
it('uses back motion for history pops and parent replacements',()=>{
 expect(routeDirection(route('/settings'),route('/settings','basic'),3,4)).toBe('forward')
 expect(routeDirection(route('/settings','basic'),route('/settings'),4,3)).toBe('back')
 expect(routeDirection(route('/settings','basic'),route('/settings'),4,4)).toBe('back')
 expect(routeDirection(route('/sources'),route('/settings'),4,4)).toBe('back')
})
it('orders peer tabs and adjacent reading chapters without inventing a history push',()=>{
 expect(routeDirection(route('/history'),route('/search'),4,5)).toBe('back')
 expect(routeDirection(route('/comic'),route('/comic/library'),4,5)).toBe('forward')
 expect(routeDirection(route('/novel/read/book/3'),route('/novel/read/book/2'),4,4)).toBe('back')
 expect(routeDirection(route('/comic/read/book/2'),route('/comic/read/book/3'),4,4)).toBe('forward')
})

it('retains one comic reader across episodes, separates series and modes',()=>{
 expect(routeScreenKey(route('/comic/read/1/0'))).toBe(routeScreenKey(route('/comic/read/1/1')))
 expect(routeScreenKey(route('/comic/read/1/0'))).not.toBe(routeScreenKey(route('/comic/read/2/0')))
 expect(routeScreenKey(route('/comic/read/1/0'))).not.toBe(routeScreenKey(route('/novel/read/1/0')))
})
