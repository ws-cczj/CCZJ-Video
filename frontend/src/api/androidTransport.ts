import { clientId, objectNames, setTransport } from '@wailsio/runtime'
import { normalizeApiError, toApiFailure } from './errors'

interface BridgeHost {
  wails?: { invokeAsync?: (id:string,payload:string)=>void }
  _wailsAndroidCallback?: (id:string,response:string,error?:string)=>void
}

/** Own only calls dispatched by this adapter. The previous runtime callback
 * still receives calls started before installation, including runtime startup. */
export function installAndroidTransport(host:BridgeHost, install= setTransport):void {
  const bridge=host.wails
  if(typeof bridge?.invokeAsync!=='function')return
  const previous=host._wailsAndroidCallback
  type Job={resolve:(value:unknown)=>void;reject:(error:unknown)=>void;bindingID?:string;cancelled?:boolean}
  const pending=new Map<string,Job>()
  const bindings=new Map<string,Job>()
  let sequence=0
  host._wailsAndroidCallback=(id,response,error)=>{
    const job=pending.get(id)
    if(!job){previous?.(id,response,error);return}
    pending.delete(id)
    if(job.bindingID)bindings.delete(job.bindingID)
    if(error){job.reject(toApiFailure(new Error(error)));return}
    try{
      const envelope:unknown=JSON.parse(response)
      if(!envelope||typeof envelope!=='object')throw new Error('Invalid Android envelope')
      const value=envelope as Record<string,unknown>
      if(value.ok!==true){
        const cause=value.error
        const normalized=normalizeApiError(typeof cause==='string'?new Error(cause):cause)
        // A successful local cancellation already settled Wails' promise. Its
        // expected native cancellation is an acknowledgement, not a late fault.
        // Genuine errors after cancellation still reject and remain diagnosable.
        if(job.cancelled&&normalized.code==='CANCELLED'){job.resolve(undefined);return}
        job.reject(toApiFailure(normalized));return
      }
      job.resolve('text' in value?value.text:value.data)
    }catch(error){job.reject(toApiFailure(error))}
  }
  install({call(objectID,method,windowName,args){
    return new Promise((resolve,reject)=>{
      const id=`cczj-${Date.now().toString(36)}-${++sequence}`
      const job:Job={resolve,reject}
      const bindingID=args&&typeof args==='object'?args['call-id']:undefined
      if(typeof bindingID==='string'){
        if(objectID===objectNames.Call){job.bindingID=bindingID;bindings.set(bindingID,job)}
        else if(objectID===objectNames.CancelCall){const active=bindings.get(bindingID);if(active)active.cancelled=true}
      }
      pending.set(id,job)
      try{bridge.invokeAsync!(id,JSON.stringify({object:objectID,method,windowName,args:args??null,clientId}))}
      catch(error){pending.delete(id);if(job.bindingID)bindings.delete(job.bindingID);reject(toApiFailure(error))}
    })
  }})
}

if(typeof window!=='undefined')installAndroidTransport(window as unknown as BridgeHost)
