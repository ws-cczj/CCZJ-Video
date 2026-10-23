import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { listNovelBuiltinSources, listComicBuiltinSources, type BuiltinSourceRow } from '../api/readingSources'
import { setLibrarySourceEnabled } from '../api/library'
import { useAsyncResource } from './asyncResource'
import { normalizeApiError } from '../api/app'

export function useBuiltinSources(kind:'novel'|'comic') {
  const query=ref(''),changing=ref(''),actionError=ref('')
  let alive=true,pending:ReturnType<typeof setLibrarySourceEnabled>|null=null
  onBeforeUnmount(()=>{alive=false;pending?.cancel()})
  const resource=useAsyncResource({load:kind==='novel'?listNovelBuiltinSources:listComicBuiltinSources})
  const rows=computed(()=>(resource.data.value?.rows??[]).filter((row):row is BuiltinSourceRow=>!!row).filter(row=>row.name.toLowerCase().includes(query.value.trim().toLowerCase())))
  const inventory=computed(()=>{
    const all=resource.data.value?.rows??[]
    return { ready:all.filter(row=>row?.enabled&&row.state==='search_ready').length, cooling:all.filter(row=>row?.enabled&&row.state==='search_cooldown').length, unmeasured:all.filter(row=>row?.enabled&&row.state==='unmeasured').length }
  })
  function stateLabel(row:BuiltinSourceRow):string {
    if(row.state==='search_ready')return 'sourceRouting.health.ready'
    if(row.state==='search_cooldown')return 'sourceRouting.health.cooling'
    if(['search_failed','search_unavailable','network_unknown','invalid_address','deep_unfinished','discovery_unfinished'].includes(row.state))return 'sourceRouting.health.failed'
    if(row.score>=0)return 'sourceRouting.health.sampled'
    return 'sourceRouting.health.waiting'
  }
  async function toggle(row:BuiltinSourceRow):Promise<void> {
    if(changing.value)return
    changing.value=row.sourceKey;actionError.value=''
    try{pending=setLibrarySourceEnabled(row.sourceKey,!row.enabled);await pending;if(alive)await resource.run()}catch(error){if(alive)actionError.value=normalizeApiError(error).message}finally{pending=null;if(alive)changing.value=''}
  }
  onMounted(()=>void resource.run())
  return { ...resource,query,rows,changing,actionError,toggle,inventory,stateLabel }
}
