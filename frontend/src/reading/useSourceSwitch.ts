import { computed, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useAsyncResource, type AsyncResource } from './asyncResource'
import { findNovelSources,findComicSources,matchNovelChapter,matchComicEpisode } from '../api/readingSources'
import type { ChapterDTO, SearchOutcomeDTO } from '../api/library'
import type { ChapterMatch } from '../api/readingSources'

export function useSourceSwitch(kind:'novel'|'comic',item:()=>number,chapter:()=>number,leaving:()=>void) {
  const router=useRouter()
  const sources:AsyncResource<SearchOutcomeDTO,[string]>=useAsyncResource({load:(cursor:string)=>{
    const previous=cursor?sources.data.value:null
    return (kind==='novel'?findNovelSources:findComicSources)(item(),cursor).then(result=>{
      if(!previous)return result
      return {...result,results:[...(previous.results??[]),...(result.results??[])]}
    })
  }})
  const match:AsyncResource<ChapterMatch,[number]>=useAsyncResource({load:(target:number)=>(kind==='novel'?matchNovelChapter:matchComicEpisode)(item(),chapter(),target)})
  const candidates=computed(()=>[...new Map((sources.data.value?.results??[]).flatMap(row=>row?.items??[]).filter(row=>!!row).map(row=>[row.id,row])).values()])
  const targets=computed(()=>(match.data.value?.targets??[]).filter((row):row is ChapterDTO=>!!row))
  const failed=computed(()=>[...new Map((sources.data.value?.results??[]).filter(row=>!!row).map(row=>[row.sourceKey,row])).values()].filter(row=>!!row.errorCode).length)
  function more():void{if(sources.status.value!=='loading'&&sources.data.value?.nextCursor)void sources.run(sources.data.value.nextCursor)}
  async function choose(target:ChapterDTO):Promise<void>{
    const result=match.data.value
    if(!result||!targets.value.some(row=>row.id===target.id)||!target.available)return
    leaving()
    await router.push(kind==='novel'?{path:`/novel/read/${result.targetItemId}/${target.id}`,query:{sourceSwitch:'1'}}:{path:`/comic/read/${result.targetItemId}/${target.ord}`,query:{episodeRemoteId:target.remoteId,sourceSwitch:'1'}})
  }
  async function catalog():Promise<void>{const result=match.data.value;if(!result)return;leaving();await router.push(kind==='novel'?`/novel/book/${result.targetItemId}`:`/comic/series/${result.targetItemId}`)}
  watch(()=>[item(),chapter()],()=>{match.cancel();match.data.value=null;sources.data.value=null;void sources.run('')},{immediate:true})
  return {sources,match,candidates,targets,failed,more,choose,catalog}
}
