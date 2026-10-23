import { beforeEach, expect, it, vi } from 'vitest'
import { CancellablePromise } from '@wailsio/runtime'
const mocks=vi.hoisted(()=>({stream:vi.fn(),handlers:[] as Array<(value:unknown)=>void>,stopped:vi.fn()}))
vi.mock('../src/api/app',()=>({LibrarySearchStream:mocks.stream}))
vi.mock('../src/api/events',()=>({onBackendEvent:(_name:string,handler:(value:unknown)=>void)=>{mocks.handlers.push(handler);return mocks.stopped}}))
import { searchLibrary } from '../src/api/library'
import { Item } from '../bindings/cczjVideo/app/library/models'
beforeEach(()=>{mocks.stream.mockReset();mocks.stopped.mockClear();mocks.handlers.length=0})

it('keeps expanding when the book title is only an author pen name on unrelated books',async()=>{
 const row=(id:number,title:string,author:string)=>({sourceKey:String(id),sourceName:'Source',hasNext:false,nextUrl:'',items:[new Item({id,kind:'novel',sourceKey:String(id),remoteId:String(id),title,author,available:true})]})
 mocks.stream.mockReturnValueOnce(CancellablePromise.resolve({keyword:'天龙八部',page:1,total:1,failedCount:0,results:[row(1,'超神医婿','天龙八部')],searchedSources:5,pendingSources:7,nextCursor:'next'}))
 mocks.stream.mockReturnValueOnce(CancellablePromise.resolve({keyword:'天龙八部',page:2,total:1,failedCount:0,results:[row(2,'天龙八部','金庸')],searchedSources:5,pendingSources:2,nextCursor:'remaining'}))
 const result=await searchLibrary('novel','天龙八部',1,'',vi.fn())
 expect(mocks.stream).toHaveBeenCalledTimes(2)
 expect(result.results.flatMap(row=>row?.items??[]).some(item=>item?.title==='天龙八部')).toBe(true)
})

it('delivers a fast source before the final result and isolates concurrent search requests',async()=>{
 const resolves:Array<(value:unknown)=>void>=[]
 mocks.stream.mockImplementation(()=>new CancellablePromise(resolve=>{resolves.push(resolve)}))
 const first=vi.fn(),second=vi.fn()
 const a=searchLibrary('novel','book',1,'',first),b=searchLibrary('novel','book',1,'',second)
 const result={sourceKey:'fast',sourceName:'Fast',items:[],nextUrl:'',hasNext:false}
 const envelope={requestId:mocks.stream.mock.calls[0]![3],kind:'novel',keyword:'book',result}
 for(const handler of mocks.handlers)handler(envelope)
 expect(first).toHaveBeenCalledTimes(1);expect(second).not.toHaveBeenCalled()
 const outcome={keyword:'book',page:1,total:0,failedCount:0,results:[result],nextCursor:''}
 resolves[0]!(outcome);resolves[1]!(outcome);await Promise.all([a,b])
 expect(mocks.stopped).toHaveBeenCalledTimes(2)
})

it('automatically expands missing exact matches, deduplicates failure history and keeps the final cursor',async()=>{
 const failed={sourceKey:'dead',sourceName:'Dead',items:[],errorCode:'TIMEOUT',errorMessage:'timeout',hasNext:false,nextUrl:''}
 mocks.stream.mockReturnValueOnce(CancellablePromise.resolve({keyword:'鬼吹灯',page:1,total:0,failedCount:1,results:[failed],searchedSources:5,pendingSources:7,nextCursor:'next'}))
 mocks.stream.mockReturnValueOnce(CancellablePromise.resolve({keyword:'鬼吹灯',page:2,total:1,failedCount:1,results:[failed,{sourceKey:'hit',sourceName:'Hit',hasNext:false,nextUrl:'',items:[new Item({id:1,kind:'novel',sourceKey:'hit',remoteId:'1',title:'鬼吹灯',author:'天下霸唱',available:true})]}],searchedSources:5,pendingSources:2,nextCursor:'remaining'}))
 const result=await searchLibrary('novel','鬼吹灯',1,'',vi.fn())
 expect(mocks.stream).toHaveBeenCalledTimes(2)
 expect(mocks.stream.mock.calls[1]![2]).toBe('next')
 expect(result.results).toHaveLength(2)
 expect(result.searchedSources).toBe(10)
 expect(result.nextCursor).toBe('remaining')
})

it('cancels the currently expanding request without launching another round',async()=>{
 mocks.stream.mockReturnValueOnce(CancellablePromise.resolve({keyword:'missing',page:1,total:0,failedCount:0,results:[],searchedSources:5,pendingSources:20,nextCursor:'next'}))
 const cancelled=vi.fn()
 mocks.stream.mockReturnValueOnce(new CancellablePromise(()=>{},cancelled))
 const request=searchLibrary('comic','missing',1,'',vi.fn())
 const rejection=expect(request).rejects.toMatchObject({name:'CancelError'})
 await vi.waitFor(()=>expect(mocks.stream).toHaveBeenCalledTimes(2))
 await request.cancel();await rejection
 expect(cancelled).toHaveBeenCalledTimes(1)
 expect(mocks.stopped).toHaveBeenCalledTimes(2)
})

it('bounds automatic expansion and leaves unsearched sources reachable',async()=>{
 mocks.stream.mockImplementation(()=>CancellablePromise.resolve({keyword:'missing',page:1,total:0,failedCount:0,results:[],searchedSources:5,pendingSources:100,nextCursor:'remaining'}))
 const result=await searchLibrary('novel','missing',1,'',vi.fn())
 expect(mocks.stream).toHaveBeenCalledTimes(3)
 expect(result.nextCursor).toBe('remaining')
})
