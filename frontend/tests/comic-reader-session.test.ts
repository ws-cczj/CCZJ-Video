import {expect,it,vi} from 'vitest'
import {CancellablePromise} from '@wailsio/runtime'
import {comicReaderSession} from '../src/comic/reader/session'
import {Chapter,ChapterPages,Item,Page,ReadingContext} from '../bindings/cczjVideo/app/library/models'
const location=(id:number)=>({itemId:1,chapterId:id,remoteId:'',ord:0,first:false})
it('reuses page tables and geometry, separates source revisions, invalidates signed URLs on retry',async()=>{
 let revision='v1'
 const context=vi.fn(input=>CancellablePromise.resolve(new ReadingContext({item:new Item({id:1}),chapter:new Chapter({id:input.chapterId,available:true}),cacheIdentity:`${revision}:${input.chapterId}`})))
 const content=vi.fn(id=>CancellablePromise.resolve(new ChapterPages({chapterId:id,pages:[new Page({index:0,url:'https://example.org/1.jpg'})],warnings:[]})))
 const session=comicReaderSession(context,content,{entries:2,bytes:4096,ttl:1000})
 await session.read(location(1));session.rememberRatio('v1:1',0,.5)
 await session.read(location(2));const returned=await session.read(location(1))
 expect(content).toHaveBeenCalledTimes(2);expect(returned.ratios).toEqual([.5])
 returned.pages[0].url='changed';expect((await session.read(location(1))).pages[0].url).toBe('https://example.org/1.jpg')
 session.invalidate('v1:1');await session.read(location(1));expect(content).toHaveBeenCalledTimes(3)
 await session.read(location(3));await session.read(location(2));expect(content).toHaveBeenCalledTimes(5)
 revision='v2';await session.read(location(2));expect(content).toHaveBeenCalledTimes(6)
 session.clear();await session.read(location(2));expect(content).toHaveBeenCalledTimes(7)
})
