import { computed, ref, shallowRef } from 'vue'
import { defineStore } from 'pinia'
import { GetSetting, SetSetting } from '../../api/app'
import { createSettingsQueue } from '../../api/settingsQueue'
import { useErrorStore } from '../../stores/error'
import { tr } from '../../locales'
import type { Video } from '../../types'

type Identity = {source:string; vod:string}
const key = 'short_drama_recommendations_v1'
const identityKey = (value:Identity) => JSON.stringify([value.source,value.vod])

/** Durable exclusions and a bounded session history share source identities.
 * Recent recommendations may relax; explicit exclusions never do. */
export const useShortDramaPreferences = defineStore('short-drama-preferences',()=>{
  const blocked=shallowRef<Identity[]>([]), recent=shallowRef<string[]>([])
  const ready=ref(false), busy=ref(false), writable=ref(false)
  const undoItem=shallowRef<Identity>()
  let loading:Promise<void>|undefined
  const errors=useErrorStore()
  const writes=createSettingsQueue((name,value)=>SetSetting(name,value),(_pending,error)=>{
    if(error)errors.fromError(tr('shortDrama.preferenceFailed'),error,'short-drama.preferences')
  })
  const excluded=computed(()=>new Set(blocked.value.map(identityKey)))
  function load():Promise<void>{
    if(writable.value)return Promise.resolve()
    if(loading)return loading
    loading=GetSetting(key).then(raw=>{
      if(raw){
        const row:unknown=JSON.parse(raw)
        if(!row||typeof row!=='object'||Array.isArray(row))throw new Error(tr('shortDrama.preferenceInvalid'))
        const data=row as Record<string,unknown>
        if(data.version!==1||!Array.isArray(data.blocked)||data.blocked.length>300)throw new Error(tr('shortDrama.preferenceInvalid'))
        const values:Identity[]=[]
        for(const item of data.blocked){
          if(!item||typeof item!=='object'||typeof item.source!=='string'||!item.source||item.source.length>2048||typeof item.vod!=='string'||!item.vod||item.vod.length>2048)throw new Error(tr('shortDrama.preferenceInvalid'))
          values.push({source:item.source,vod:item.vod})
        }
        blocked.value=values
      }
      writable.value=true
    }).catch(error=>errors.fromError(tr('shortDrama.preferenceFailed'),error,'short-drama.preferences')).then(()=>{ready.value=true}).finally(()=>{loading=undefined})
    return loading
  }
  function choose(batch:Video[],fallback:string,current?:Identity):Video[]{
    const seen=new Set<string>(), currentKey=current?identityKey(current):''
    const result=batch.filter(item=>{
      const id=identityKey({source:item.source_key||fallback,vod:String(item.vod_id||'')})
      if(!item.vod_id||id===currentKey||seen.has(id)||excluded.value.has(id))return false
      seen.add(id);return true
    })
    const fresh=result.filter(item=>!recent.value.includes(identityKey({source:item.source_key||fallback,vod:String(item.vod_id)})))
    // Retain older candidates after fresh ones so a small library remains usable.
    return [...fresh,...result.filter(item=>!fresh.includes(item))]
  }
  function remember(source:string,vod:string):void{
    if(!source||!vod)return
    const id=identityKey({source,vod})
    if(recent.value.at(-1)===id)return
    recent.value=[...recent.value.filter(value=>value!==id),id].slice(-40)
  }
  async function persist(values:Identity[]):Promise<boolean>{
    if(busy.value)return false
    busy.value=true
    try{
      await load()
      if(!writable.value)return false
      if(!await writes.set(key,JSON.stringify({version:1,blocked:values})))return false
      blocked.value=values
      return true
    }finally{busy.value=false}
  }
  async function dismiss(source:string,vod:string):Promise<boolean>{
    if(!source||!vod||source.length>2048||vod.length>2048||busy.value)return false
    await load()
    const item={source,vod}
    if(excluded.value.has(identityKey(item)))return true
    if(blocked.value.length>=300){errors.warn(tr('shortDrama.preferenceFailed'),tr('shortDrama.exclusionLimit'));return false}
    if(!await persist([...blocked.value,item]))return false
    undoItem.value=item
    return true
  }
  async function undo():Promise<void>{
    const item=undoItem.value
    if(!item)return
    if(await persist(blocked.value.filter(value=>identityKey(value)!==identityKey(item))))undoItem.value=undefined
  }
  async function restoreAll():Promise<void>{
    if(await persist([]))undoItem.value=undefined
  }
  return {ready,busy,blocked,undoItem,load,choose,remember,dismiss,undo,restoreAll}
})
