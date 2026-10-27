import { afterEach, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
vi.mock('../src/api/app',async()=>({...await vi.importActual('../src/api/errors'),WriteLog:vi.fn(()=>Promise.resolve())}))
vi.mock('../src/locales',()=>({tr:(key:string)=>key}))
import { WriteLog } from '../src/api/app'
import { useErrorStore } from '../src/stores/error'
afterEach(()=>vi.useRealTimers())
it('action tips expire after five seconds and manual dismissal releases their timers',()=>{
 vi.useFakeTimers();setActivePinia(createPinia());const store=useErrorStore();const action=vi.fn()
 const tip=store.push({level:'info',title:'Download ready',message:'View update',autoDismiss:5000,actionLabel:'Details',action})
 expect(store.toasts[0]?.action).toBe(action)
 vi.advanceTimersByTime(4999);expect(store.toasts).toHaveLength(1)
 vi.advanceTimersByTime(1);expect(store.toasts).toHaveLength(0);expect(action).not.toHaveBeenCalled()
 const closed=store.push({level:'info',title:'Collecting',message:'View source',autoDismiss:5000})
 store.dismiss(closed.id);expect(vi.getTimerCount()).toBe(0);expect(store.toasts).toHaveLength(0)
})
it('keeps cancellations quiet and coalesces failures already logged by the backend',()=>{
 vi.useFakeTimers();const pinia=createPinia();setActivePinia(pinia);const store=useErrorStore()
 store.fromError('cancel',{code:'CANCELLED',message:'cancel'})
 expect(store.toasts).toHaveLength(0);expect(WriteLog).not.toHaveBeenCalled()
 const error={code:'RATE_LIMITED',message:'wait',requestId:'r1',logged:true}
 store.fromError('source',error,'danmaku');store.fromError('source',error,'danmaku')
 expect(store.toasts).toHaveLength(1);expect(store.toasts[0]).toMatchObject({level:'warn',count:2,requestId:'r1'})
 expect(WriteLog).not.toHaveBeenCalled();store.clearToasts();expect(vi.getTimerCount()).toBe(0)
})
