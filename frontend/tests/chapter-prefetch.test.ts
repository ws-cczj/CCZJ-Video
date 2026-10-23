import { expect, it } from 'vitest'
import { chapterPrefetch } from '../src/reading/chapterPrefetch'

it('prefetches adjacent available chapters and cancels old work when navigation changes',()=>{
 const started:number[]=[],cancelled:number[]=[]
 const prefetch=chapterPrefetch(id=>{started.push(id);return Object.assign(new Promise(()=>{}),{cancel:()=>{cancelled.push(id)}})})
 const chapters=[1,2,3,4,5].map(id=>({id,available:id!==2}))
 prefetch.update(chapters,2);expect(started).toEqual([1,4])
 prefetch.update(chapters,3);expect(cancelled).toEqual([1,4]);expect(started).toEqual([1,4,3,5])
 prefetch.clear();expect(cancelled).toEqual([1,4,3,5])
})
