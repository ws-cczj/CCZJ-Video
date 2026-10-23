import { afterEach, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
vi.mock('../src/api/app',async()=>({...await vi.importActual('../src/api/errors'),WriteLog:vi.fn(()=>Promise.resolve())}))
vi.mock('../src/locales',()=>({tr:(key:string)=>key}))
import { WriteLog } from '../src/api/app'
import { useErrorStore } from '../src/stores/error'
afterEach(()=>vi.useRealTimers())
it('keeps cancellations quiet and coalesces failures already logged by the backend',()=>{
 vi.useFakeTimers();const pinia=createPinia();setActivePinia(pinia);const store=useErrorStore()
 store.fromError('cancel',{code:'CANCELLED',message:'cancel'})
 expect(store.toasts).toHaveLength(0);expect(WriteLog).not.toHaveBeenCalled()
 const error={code:'RATE_LIMITED',message:'wait',requestId:'r1',logged:true}
 store.fromError('source',error,'danmaku');store.fromError('source',error,'danmaku')
 expect(store.toasts).toHaveLength(1);expect(store.toasts[0]).toMatchObject({level:'warn',count:2,requestId:'r1'})
 expect(WriteLog).not.toHaveBeenCalled();store.clearToasts();expect(vi.getTimerCount()).toBe(0)
})
