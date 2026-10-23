import { readingRequest } from '../../api/requestChain'
import { readingContentCache } from '../../reading/contentCache'
import { fetchComicContext,fetchChapterPages } from '../../api/library'
import type { ChapterPagesDTO,ReadingContextDTO,ReadingLocationDTO } from '../../api/library'
import type { ComicChapter,ComicPage,ComicSeries } from '../domain'

export interface ComicEpisode {
 item:ComicSeries;chapter:ComicChapter;previous:ComicChapter|null;next:ComicChapter|null
 index:number;count:number;cacheIdentity:string;pages:ComicPage[];warnings:string[];fromCache:boolean;ratios:number[]
}
/** Cache page tables, never decoded bitmaps. At most five images remain mounted
 * in the reader; the existing image proxy owns persistent image bytes. */
export function comicReaderSession(context=fetchComicContext,content=fetchChapterPages,limits={entries:12,bytes:2*1024*1024,ttl:5*60*1000}) {
 const cache=readingContentCache<{content:ChapterPagesDTO;ratios:number[]}>(limits)
 function read(location:ReadingLocationDTO) {
  const started=cache.generation()
  return readingRequest<ComicEpisode>(async request=>{
   const view:ReadingContextDTO=await request(context(location)),chapter=view.chapter
   if(!view.item||!chapter)throw new Error('NOT_FOUND: 这一话不在目录里')
   const saved=cache.get(view.cacheIdentity)
   const found=saved?.content??await request(content(chapter.id))
   const pages=(found.pages??[]).filter((page):page is ComicPage=>!!page?.url)
   if(!saved&&pages.length)cache.put(view.cacheIdentity,{content:found,ratios:[]},256+2*pages.reduce((sum,p)=>sum+p.url.length+24,0),started)
   return {...view,item:view.item,chapter,ratios:[...(saved?.ratios??[])],pages:pages.map(page=>({...page})),warnings:[...(found.warnings??[])],fromCache:!!saved||found.fromCache}
  })
 }
 return {read,clear:cache.clear,invalidate:cache.remove,rememberRatio:(key:string,index:number,ratio:number)=>{const saved=cache.get(key);if(saved&&index>=0&&index<saved.content.pages.length)saved.ratios[index]=ratio}}
}
