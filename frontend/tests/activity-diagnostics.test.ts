import { expect, it, vi } from 'vitest'
import { effectScope } from 'vue'
import { createActivityDiagnostics } from '../src/diagnostics/activity'
import { useAsyncResource } from '../src/reading/asyncResource'

vi.mock('../src/api/app',()=>({normalizeApiError:(value:{code?:string})=>({code:value.code??'INTERNAL',message:''})}))

it('counts cancellation once even if the underlying operation completes late',()=>{
  let time=100
  const diag=createActivityDiagnostics(()=>time)
  const finish=diag.begin('novel.reader')
  expect(diag.snapshot().operations[0]).toMatchObject({active:1,avg_ms:null})
  time=150;finish('cancelled','CANCELLED');finish('success')
  expect(diag.snapshot().operations[0]).toMatchObject({started:1,active:0,cancelled:1,success:0,failed:0,avg_ms:50,max_ms:50})
})

it('keeps unsaved reading edits and bounded observations visible without content identifiers',()=>{
  const diag=createActivityDiagnostics()
  diag.readingState('comic',{ready:true,failed:true,pending:3})
  expect(diag.snapshot().reading).toEqual([{kind:'comic',ready:true,failed:true,pending:3}])
  diag.begin('https://private.example/book/secret')('failed','private-path')
  expect(diag.snapshot().operations).toEqual([])
})

it('cancels owned reading work when its component scope is destroyed and rejects late data',async()=>{
  const scope=effectScope(),cancel=vi.fn(),onData=vi.fn()
  let resolve!:(value:string)=>void
  const pending=Object.assign(new Promise<string>(done=>{resolve=done}),{cancel})
  const resource=scope.run(()=>useAsyncResource({load:()=>pending,onData}))!
  const running=resource.run()
  scope.stop()
  expect(cancel).toHaveBeenCalledOnce()
  resolve('late chapter');await running
  expect(onData).not.toHaveBeenCalled()
  expect(resource.status.value).toBe('idle')
})

it('returns only the active reading domain without deleting the other observations',()=>{
 const diagnostics=createActivityDiagnostics(()=>0)
 diagnostics.begin('novel.reader')('success');diagnostics.begin('comic.reader')('failed')
 diagnostics.readingState('novel',{ready:true,failed:false,pending:0});diagnostics.readingState('comic',{ready:true,failed:true,pending:1})
 expect(diagnostics.snapshot('novel').operations.map(row=>row.name)).toEqual(['novel.reader'])
 expect(diagnostics.snapshot('comic').reading.map(row=>row.kind)).toEqual(['comic'])
 expect(diagnostics.snapshot('video').operations).toEqual([])
 expect(diagnostics.snapshot().operations).toHaveLength(2)
})
