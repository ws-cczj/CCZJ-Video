import { ref } from 'vue'
import { defineStore } from 'pinia'
import { GetSetting, SetSetting } from '../api/app'
import { createSettingsQueue } from '../api/settingsQueue'
import { readStorage, writeStorage } from '../platform/storage'
import { useErrorStore } from './error'
import { tr } from '../locales'

export interface FavFolder { id: string; name: string; default: boolean }
const FOLDERS_KEY='cczj_fav_folders', MAPPING_KEY='cczj_fav_mapping'
export function decodeFavoriteFolders(raw:unknown):FavFolder[] {
  const result:FavFolder[]=[{id:'default',name:'',default:true}]
  if(!Array.isArray(raw))return result
  const ids=new Set(['default'])
  for(const row of raw)if(row&&typeof row==='object'&&typeof row.id==='string'&&row.id&&typeof row.name==='string'&&!ids.has(row.id)){
    ids.add(row.id);result.push({id:row.id,name:row.name,default:false})
  }
  return result
}
export function decodeFavoriteMapping(raw:unknown):Record<string,string> {
  if(!raw||typeof raw!=='object'||Array.isArray(raw))return {}
  return Object.fromEntries(Object.entries(raw).filter((row):row is [string,string]=>typeof row[1]==='string'))
}
export const useFavoriteFoldersStore=defineStore('favoriteFolders',()=>{
  const folders=ref<FavFolder[]>(decodeFavoriteFolders(null)),mapping=ref<Record<string,string>>({}),ready=ref(false)
  const errors=useErrorStore(),queue=createSettingsQueue(SetSetting)
  let flight:Promise<void>|undefined
  function parse(raw:string):unknown {try{return JSON.parse(raw)}catch{throw new Error(tr('favorites.saveFailedDetail'))}}
  async function load():Promise<void>{
    if(ready.value)return
    if(flight)return flight
    flight=(async()=>{
      const [savedFolders,savedMapping]=await Promise.all([GetSetting(FOLDERS_KEY),GetSetting(MAPPING_KEY)])
      folders.value=decodeFavoriteFolders(savedFolders?parse(savedFolders):readStorage(FOLDERS_KEY,null))
      mapping.value=decodeFavoriteMapping(savedMapping?parse(savedMapping):readStorage(MAPPING_KEY,{}))
      // Retain browser values so a failed first import can be retried.
      if(!savedFolders&&!await queue.set(FOLDERS_KEY,JSON.stringify(folders.value)))throw new Error(tr('favorites.saveFailedDetail'))
      if(!savedMapping&&!await queue.set(MAPPING_KEY,JSON.stringify(mapping.value)))throw new Error(tr('favorites.saveFailedDetail'))
      ready.value=true
    })().finally(()=>{flight=undefined})
    return flight
  }
  async function persist(key:string,value:unknown):Promise<boolean>{
    const ok=await queue.set(key,JSON.stringify(value))
    if(ok)writeStorage(key,value)
    else errors.error(tr('favorites.saveFailed'),tr('favorites.saveFailedDetail'),'','Favorites')
    return ok
  }
  return {folders,mapping,ready,load,persistFolders:()=>persist(FOLDERS_KEY,folders.value),persistMapping:()=>persist(MAPPING_KEY,mapping.value)}
})
