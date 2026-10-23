import { expect,it,vi } from 'vitest'
import { CancellablePromise } from '@wailsio/runtime'
import { response } from '../src/api/response'
import { integer } from '../src/api/decode'
import { activityDiagnostics } from '../src/diagnostics/activity'

it('preserves binding cancellation and settles transport observation once',async()=>{
 const cancelled=vi.fn()
 const pending=new CancellablePromise<number>(()=>{},cancelled)
 const result=response(()=>pending,value=>value)
 const rejected=expect(result).rejects.toMatchObject({name:'CancelError'})
 await Promise.resolve();await Promise.resolve()
 await result.cancel();await rejected
 expect(cancelled).toHaveBeenCalledOnce()
 await Promise.resolve()
 expect(activityDiagnostics.snapshot('video').operations.find(row=>row.name==='video.transport')).toMatchObject({active:0,cancelled:1})
})

it('classifies malformed successful responses as data errors',async()=>{
 const result=response(()=>CancellablePromise.resolve(-1),value=>integer(value))
 await expect(result).rejects.toMatchObject({code:'CORRUPT'})
})
