import {expect,it} from 'vitest'
import {orderChapterEntries,type ChapterEntry} from '../src/novel/reader/catalog'
import type {NovelChapter} from '../src/novel/domain'

it('keeps volume headings before their chapters in descending catalogs without mutating ordinals',()=>{
 const entry=(id:number,number:number,isVolume=false):ChapterEntry=>({number,chapter:{id,isVolume} as NovelChapter})
 const original=[entry(1,1),entry(2,0,true),entry(3,2),entry(4,3),entry(5,0,true),entry(6,4),entry(7,5)]
 const ordered=orderChapterEntries(original,true)
 expect(ordered.map(row=>row.chapter.id)).toEqual([5,7,6,2,4,3,1])
 expect(ordered.map(row=>row.number)).toEqual([0,5,4,0,3,2,1])
 expect(original.map(row=>row.chapter.id)).toEqual([1,2,3,4,5,6,7])
 expect(orderChapterEntries(original,false)).toBe(original)
})
