import { onScopeDispose, ref, shallowRef } from 'vue'
import { defineStore, acceptHMRUpdate } from 'pinia'
import { installedResourcesAPI, type InstalledResource } from '../api/installedResources'
import { normalizeApiError } from '../api/errors'
export const useInstalledResources=defineStore('installed-resources',()=>{
 const items=shallowRef<InstalledResource[]>([]),directory=ref(''),busy=ref(''),error=ref('')
 let pending:ReturnType<typeof installedResourcesAPI.list>|ReturnType<typeof installedResourcesAPI.remove>|undefined
 let loading:Promise<boolean>|undefined,disposed=false
 function cancel():void {try{void Promise.resolve(pending?.cancel?.()).catch(()=>{})}catch{/* Already settled. */}}
 async function load():Promise<boolean>{
  if(loading)return loading
  if(busy.value||disposed)return false
  const request=installedResourcesAPI.list();pending=request
  loading=request.then(value=>{if(disposed)return false;items.value=value.items.filter(item=>item.installed||item.corrupt);directory.value=value.directory;error.value='';return true})
   .catch(failure=>{if(!disposed&&normalizeApiError(failure).code!=='CANCELLED')error.value=normalizeApiError(failure).message;return false})
   .finally(()=>{if(pending===request)pending=undefined;loading=undefined})
  return loading
 }
 async function remove(id:string):Promise<boolean>{
  if(busy.value||disposed)return false
  if(loading)await loading
  if(disposed||busy.value)return false
  busy.value=id;error.value=''
  const request=installedResourcesAPI.remove(id);pending=request
  try{
   await request
   if(disposed)return false
   items.value=items.value.filter(item=>item.id!==id)
   return true
  }catch(failure){if(!disposed&&normalizeApiError(failure).code!=='CANCELLED')error.value=normalizeApiError(failure).message;return false}
  finally{if(pending===request)pending=undefined;if(!disposed){busy.value='';if(!error.value)void load()}}
 }
 function dispose(){disposed=true;cancel();window.removeEventListener('pagehide',cancel)}
 window.addEventListener('pagehide',cancel)
 onScopeDispose(dispose)
 if(import.meta.hot)import.meta.hot.dispose(dispose)
 return {items,directory,busy,error,load,remove,cancel}
})
if(import.meta.hot)import.meta.hot.accept(acceptHMRUpdate(useInstalledResources,import.meta.hot))
