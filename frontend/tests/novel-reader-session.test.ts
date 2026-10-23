import { afterEach, expect, it, vi } from 'vitest'
import { CancellablePromise } from '@wailsio/runtime'
import { novelReaderSession } from '../src/novel/reader/session'
import { Chapter, ChapterText, Item, ReadingContext } from '../bindings/cczjVideo/app/library/models'

afterEach(()=>vi.useRealTimers())
const location=(id:number)=>({itemId:1,chapterId:id,remoteId:'',ord:0,first:false})
function fixture(limits={entries:2,bytes:4096,ttl:1000}) {
 let revision='v1'
 const context=vi.fn(input=>CancellablePromise.resolve(new ReadingContext({item:new Item({id:1}),chapter:new Chapter({id:input.chapterId,available:true}),cacheIdentity:`${revision}:${input.chapterId}`})))
 const content=vi.fn(id=>CancellablePromise.resolve(new ChapterText({chapterId:id,paragraphs:['正文'],warnings:[]})))
 return {session:novelReaderSession(context,content,limits),context,content,revise:()=>revision='v2'}
}
it('reuses read chapters, keeps source revision boundaries and evicts least recently read content',async()=>{
 const {session,context,content,revise}=fixture()
 await session.read(location(1));await session.read(location(2));await session.read(location(1))
 expect(content).toHaveBeenCalledTimes(2);expect(context).toHaveBeenCalledTimes(3)
 await session.read(location(3));await session.read(location(2))
 expect(content).toHaveBeenCalledTimes(4)
 revise();await session.read(location(2));expect(content).toHaveBeenCalledTimes(5)
 session.clear();await session.read(location(2));expect(content).toHaveBeenCalledTimes(6)
})
it('bounds bytes and TTL rather than retaining an entire book',async()=>{
 vi.useFakeTimers()
 const {session,content}=fixture({entries:24,bytes:360,ttl:1000})
 await session.read(location(1));await session.read(location(2));await session.read(location(1))
 expect(content).toHaveBeenCalledTimes(3)
 await session.read(location(1));expect(content).toHaveBeenCalledTimes(3)
 vi.advanceTimersByTime(1001);await session.read(location(1));expect(content).toHaveBeenCalledTimes(4)
})
it('never caches failures or a late result after reader disposal',async()=>{
 const {session,content}=fixture()
 content.mockReturnValueOnce(CancellablePromise.reject(new Error('offline')))
 await expect(session.read(location(1))).rejects.toBeTruthy()
 await session.read(location(1));expect(content).toHaveBeenCalledTimes(2)
 let resolve!:(value:ChapterText)=>void
 content.mockReturnValueOnce(new CancellablePromise(done=>{resolve=done}))
 const pending=session.read(location(2));await vi.waitFor(()=>expect(content).toHaveBeenCalledTimes(3))
 session.clear();resolve(new ChapterText({chapterId:2,paragraphs:['迟到正文'],warnings:[]}));await pending
 await session.read(location(2));expect(content).toHaveBeenCalledTimes(4)
})

it('cancels the actual content request without warming the cache',async()=>{
 const {session,content}=fixture(),cancelled=vi.fn()
 content.mockReturnValueOnce(new CancellablePromise(()=>{},cancelled))
 const pending=session.read(location(1))
 const rejection=expect(pending).rejects.toMatchObject({name:'CancelError'})
 await vi.waitFor(()=>expect(content).toHaveBeenCalledTimes(1))
 await pending.cancel();await rejection
 expect(cancelled).toHaveBeenCalledOnce()
 await session.read(location(1));expect(content).toHaveBeenCalledTimes(2)
})
