import { expect, it, vi } from 'vitest'
import { installAndroidTransport } from '../src/api/androidTransport'
import { objectNames, type RuntimeTransport } from '@wailsio/runtime'

it('preserves structured Android causes and supports a legacy host error string',async()=>{
 const previous=vi.fn();let callback!:string
 const host={wails:{invokeAsync:vi.fn((id:string)=>{callback=id})},_wailsAndroidCallback:previous}
 let transport!:RuntimeTransport;installAndroidTransport(host,value=>{transport=value})
 const request=transport.call(0,0,'',null)
 const rejected=expect(request).rejects.toMatchObject({code:'RATE_LIMITED',message:'wait',requestId:'r1',retryAfterSeconds:20,logged:true})
 host._wailsAndroidCallback(callback,JSON.stringify({ok:false,error:{kind:'RuntimeError',message:'diagnostic',cause:{code:'RATE_LIMITED',message:'wait',requestId:'r1',retryAfterSeconds:20,logged:true}}}))
 await rejected
 const legacy=transport.call(0,0,'',null);const rejectedLegacy=expect(legacy).rejects.toMatchObject({code:'STORAGE',message:'save failed'})
 host._wailsAndroidCallback(callback,JSON.stringify({ok:false,error:'Binding call failed: Bound method returned an error: STORAGE: save failed'}));await rejectedLegacy
 host._wailsAndroidCallback('older-runtime-call','{}');expect(previous).toHaveBeenCalledOnce()
})
it('acknowledges only expected native cancellations and preserves real late failures',async()=>{
 const ids:string[]=[];let transport!:RuntimeTransport
 const host={wails:{invokeAsync:(id:string)=>{ids.push(id)}},_wailsAndroidCallback:(_id:string,_response:string)=>{}}
 installAndroidTransport(host,value=>{transport=value})
 const request=transport.call(objectNames.Call,0,'',{'call-id':'b1'})
 const cancellation=transport.call(objectNames.CancelCall,0,'',{'call-id':'b1'})
 host._wailsAndroidCallback(ids[1]!,'{"ok":true,"data":null}');await cancellation
 host._wailsAndroidCallback(ids[0]!,'{"ok":false,"error":{"cause":{"code":"CANCELLED","message":"cancelled"}}}')
 expect(await request).toBeUndefined()
 const fault=transport.call(objectNames.Call,0,'',{'call-id':'b2'});const rejected=expect(fault).rejects.toMatchObject({code:'STORAGE'})
 const cancelFault=transport.call(objectNames.CancelCall,0,'',{'call-id':'b2'})
 host._wailsAndroidCallback(ids[3]!,'{"ok":true,"data":null}');await cancelFault
 host._wailsAndroidCallback(ids[2]!,'{"ok":false,"error":{"cause":{"code":"STORAGE","message":"write failed"}}}');await rejected
})
it('retains success values including false and releases synchronous dispatch failures',async()=>{
 let id='';let transport!:RuntimeTransport
 const host={wails:{invokeAsync:(value:string)=>{id=value}},_wailsAndroidCallback:(_id:string,_response:string)=>{}}
 installAndroidTransport(host,value=>{transport=value})
 const request=transport.call(0,0,'',{});host._wailsAndroidCallback(id,'{"ok":true,"data":false}');expect(await request).toBe(false)
 host.wails.invokeAsync=()=>{throw new Error('dispatch')};await expect(transport.call(0,0,'',{})).rejects.toMatchObject({code:'INTERNAL'})
})
