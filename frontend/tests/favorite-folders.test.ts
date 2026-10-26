import { beforeEach, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
const env=vi.hoisted(()=>({database:new Map<string,string>(),legacy:new Map<string,unknown>(),fail:false,error:vi.fn()}))
vi.mock('../src/api/app',()=>({GetSetting:vi.fn(async(key:string)=>env.database.get(key)||''),SetSetting:vi.fn(async(key:string,value:string)=>{if(env.fail)throw new Error('disk full');env.database.set(key,value)})}))
vi.mock('../src/platform/storage',()=>({readStorage:(key:string,fallback:unknown)=>env.legacy.get(key)??fallback,writeStorage:(key:string,value:unknown)=>{env.legacy.set(key,value);return true}}))
vi.mock('../src/stores/error',()=>({useErrorStore:()=>({error:env.error})}))
vi.mock('../src/locales',()=>({tr:(key:string)=>key}))
import { useFavoriteFoldersStore } from '../src/stores/favoriteFolders'
beforeEach(()=>{setActivePinia(createPinia());env.database.clear();env.legacy.clear();env.fail=false;env.error.mockClear()})
it('imports old folders and restores them after WebView storage is cleared',async()=>{
 env.legacy.set('cczj_fav_folders',[{id:'default',name:'',default:true},{id:'old',name:'Saved shows',default:false}])
 env.legacy.set('cczj_fav_mapping',{'source-1':'old'})
 const store=useFavoriteFoldersStore();await store.load()
 store.folders.push({id:'new',name:'New folder',default:false});expect(await store.persistFolders()).toBe(true)
 env.legacy.clear();setActivePinia(createPinia())
 const reopened=useFavoriteFoldersStore();await reopened.load()
 expect(reopened.folders.map(row=>row.id)).toEqual(['default','old','new'])
 expect(reopened.mapping['source-1']).toBe('old')
})
it('reports failed persistence and retries without losing the pending edit',async()=>{
 const store=useFavoriteFoldersStore();await store.load()
 store.folders.push({id:'new',name:'Saved shows',default:false});env.fail=true
 expect(await store.persistFolders()).toBe(false);expect(env.error).toHaveBeenCalledOnce()
 expect(JSON.parse(env.database.get('cczj_fav_folders')!).length).toBe(1)
 env.fail=false;expect(await store.persistFolders()).toBe(true)
 expect(JSON.parse(env.database.get('cczj_fav_folders')!).map((row:{id:string})=>row.id)).toEqual(['default','new'])
})
