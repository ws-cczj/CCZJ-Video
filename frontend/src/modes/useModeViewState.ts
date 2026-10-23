import { onScopeDispose, watch, type Ref } from 'vue'
import { modeSessions } from './sessionRuntime'
import type { ApplicationMode } from './definitions'
import type { ViewScalar, ViewState } from './modeSessions'

/** Pages opt in only their small form/filter fields. Synchronous capture keeps
 * the switch checkpoint current even when a cached page is not being rendered. */
export function useModeViewState(mode:ApplicationMode,key:string,fields:Record<string,Ref<ViewScalar>>){
  const saved=modeSessions.view(mode,key)
  for(const [name,field] of Object.entries(fields)){
    const value=saved[name]
    if(value!==undefined&&typeof value===typeof field.value)field.value=value
  }
  const stop=watch(()=>Object.fromEntries(Object.entries(fields).map(([name,field])=>[name,field.value])) as ViewState,value=>modeSessions.saveView(mode,key,value),{immediate:true,flush:'sync'})
  onScopeDispose(stop)
}
