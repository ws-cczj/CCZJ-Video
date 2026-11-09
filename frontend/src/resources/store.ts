import { computed, onScopeDispose, ref, shallowRef } from 'vue'
import { defineStore, acceptHMRUpdate } from 'pinia'
import { installedResourcesAPI, type InstalledResource } from '../api/installedResources'
import { normalizeApiError } from '../api/errors'
export const installedResourcesRevision=ref(0)
export const useInstalledResources=defineStore('installed-resources',()=>{
 const items=shallowRef<InstalledResource[]>([]),directory=ref(''),ready=ref(false),busy=ref(''),error=ref('')
 let pending:ReturnType<typeof installedResourcesAPI.list>|ReturnType<typeof installedResourcesAPI.install>|ReturnType<typeof installedResourcesAPI.remove>|undefined
 let loading:Promise<boolean>|undefined,disposed=false
 const font=computed(()=>items.value.find(item=>item.id==='danmaku-source-han'))
 function cancel():void {try{void Promise.resolve(pending?.cancel?.()).catch(()=>{})}catch{/* Already settled. */}}
 async function load():Promise<boolean>{
  if(loading)return loading
  if(busy.value||disposed)return false
  const request=installedResourcesAPI.list();pending=request
  loading=request.then(value=>{if(disposed)return false;const changed=ready.value&&JSON.stringify(items.value)!==JSON.stringify(value.items);items.value=value.items;directory.value=value.directory;ready.value=true;error.value='';if(changed)installedResourcesRevision.value++;return true})
   .catch(failure=>{if(!disposed&&normalizeApiError(failure).code!=='CANCELLED')error.value=normalizeApiError(failure).message;return false})
   .finally(()=>{if(pending===request)pending=undefined;loading=undefined})
  return loading
 }
 async function change(id:string,remove=false):Promise<boolean>{
  if(busy.value||disposed)return false
  if(loading)await loading
  if(disposed||busy.value)return false
  busy.value=id;error.value=''
  const request=remove?installedResourcesAPI.remove(id):installedResourcesAPI.install(id);pending=request
  try{
   const value=await request
   if(disposed)return false
   items.value=items.value.map(item=>item.id===id?(remove?{...item,installed:false,corrupt:false,url:''}:value as InstalledResource):item)
   installedResourcesRevision.value++
   return true
  }catch(failure){if(!disposed&&normalizeApiError(failure).code!=='CANCELLED')error.value=normalizeApiError(failure).message;return false}
  finally{if(pending===request)pending=undefined;if(!disposed){busy.value='';if(!error.value)void load()}}
 }
 function dispose(){disposed=true;cancel();window.removeEventListener('pagehide',cancel)}
 window.addEventListener('pagehide',cancel)
 onScopeDispose(dispose)
 if(import.meta.hot)import.meta.hot.dispose(dispose)
 return {items,directory,ready,busy,error,font,load,change,cancel}
})
if(import.meta.hot)import.meta.hot.accept(acceptHMRUpdate(useInstalledResources,import.meta.hot))
