import { computed, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useAsyncResource, type AsyncResource } from './asyncResource'
import { findNovelSources,findComicSources,matchNovelChapter,matchComicEpisode,prepareNovelSwitch,novelBookSources } from '../api/readingSources'
import { useNovelSourceCache } from './useNovelSourceCache'
import { normalizeApiError } from '../api/app'
import type { ChapterDTO, SearchOutcomeDTO, ItemDTO } from '../api/library'
import type { ChapterMatch } from '../api/readingSources'

export function useSourceSwitch(kind:'novel'|'comic',item:()=>number,chapter:()=>number,leaving:()=>void) {
  const router=useRouter()
  const sourceCache=kind==='novel'?useNovelSourceCache(item):null
  const sources:AsyncResource<SearchOutcomeDTO,[string]>=useAsyncResource({load:(cursor:string)=>{
    if(kind==='novel'&&!cursor)return novelBookSources(item()).then(cache=>({keyword:'',page:1,results:cache.candidates.filter(row=>row?.item&&row.item.id!==item()).map(row=>({sourceKey:row!.item!.sourceKey,sourceName:row!.item!.sourceName,items:[row!.item!],nextUrl:'',hasNext:false,errorCode:'',errorMessage:''})),total:cache.candidates.length,failedCount:0,searchedSources:0,pendingSources:0,nextCursor:''}))
    const previous=cursor?sources.data.value:null
    return (kind==='novel'?findNovelSources:findComicSources)(item(),cursor===':expand'?'':cursor).then(result=>{
      if(!previous)return result
      return {...result,results:[...(previous.results??[]),...(result.results??[])]}
    })
  }})
  const match:AsyncResource<ChapterMatch,[number]>=useAsyncResource({load:(target:number)=>(kind==='novel'?matchNovelChapter:matchComicEpisode)(item(),chapter(),target)})
  const preview=useAsyncResource({load:(target:number)=>prepareNovelSwitch(item(),chapter(),match.data.value!.targetItemId,target)})
  const committing=ref(false),commitError=ref('')
  const candidates=computed(()=>[...new Map([...(sourceCache?.cached.data.value?.candidates??[]).map(row=>row?.item),...(sources.data.value?.results??[]).flatMap(row=>row?.items??[])].filter((row):row is ItemDTO=>!!row&&row.id!==item()).map(row=>[row.id,row])).values()])
  const targets=computed(()=>(match.data.value?.targets??[]).filter((row):row is ChapterDTO=>!!row))
  const failed=computed(()=>[...new Map((sources.data.value?.results??[]).filter(row=>!!row).map(row=>[row.sourceKey,row])).values()].filter(row=>!!row.errorCode).length)
  function more():void{if(sources.status.value!=='loading'&&sources.data.value?.nextCursor)void sources.run(sources.data.value.nextCursor)}
  async function choose(target:ChapterDTO):Promise<void>{
    const result=match.data.value
    if(committing.value||match.status.value==='loading'||!result||!targets.value.some(row=>row.id===target.id)||!target.available)return
    if(kind==='novel'){
      const prepared=preview.data.value
      if(preview.status.value!=='ready'||prepared?.item?.id!==result.targetItemId||prepared.chapter?.id!==target.id||Date.now()-prepared.checkedAt*1000>120000){await preview.run(target.id);return}
    }
    committing.value=true;commitError.value=''
    try {
      leaving()
      await router.push(kind==='novel'?{path:`/novel/read/${result.targetItemId}/${target.id}`,query:{sourceSwitch:'1'}}:{path:`/comic/read/${result.targetItemId}/${target.ord}`,query:{episodeRemoteId:target.remoteId,sourceSwitch:'1'}})
    }catch(error){commitError.value=normalizeApiError(error).message}finally{committing.value=false}
  }
  async function catalog():Promise<void>{const result=match.data.value;if(!result)return;leaving();await router.push(kind==='novel'?`/novel/book/${result.targetItemId}`:`/comic/series/${result.targetItemId}`)}
  watch(()=>match.status.value,status=>{
    if(kind!=='novel')return
    if(status==='loading'){preview.cancel();preview.data.value=null;commitError.value=''}
    if(status==='ready'&&match.data.value?.status==='matched'&&targets.value.length===1)void preview.run(targets.value[0]!.id)
  },{flush:'sync'})
  watch(()=>preview.status.value,status=>{if(sourceCache&&(status==='ready'||status==='error'))void sourceCache.cached.run(item())})
  watch(()=>[item(),chapter()],()=>{preview.cancel();preview.data.value=null;match.cancel();match.data.value=null;sources.cancel();sources.data.value=null;void sources.run('')},{immediate:true})
  const sourceState=(id:number)=>sourceCache?.cached.data.value?.candidates.find(row=>row?.item?.id===id)?.state??'candidate'
  const expand=()=>sources.run(':expand')
  return {sources,match,candidates,targets,failed,more,choose,catalog,preview,committing,commitError,sourceCache,sourceState,expand}
}
