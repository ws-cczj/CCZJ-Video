import { expect,it,vi } from 'vitest'
import { createSettingsQueue } from '../src/api/settingsQueue'
import { eventDelivery } from '../src/api/eventPipeline'
import { chapterText,detail,outcome } from '../src/api/libraryDecode'

it('serialises writes and coalesces only edits not yet acknowledged',async()=>{
 let release!:()=>void
 const first=new Promise<void>(resolve=>{release=resolve})
 const write=vi.fn(async(_key:string,value:string)=>{if(value==='first')await first})
 const queue=createSettingsQueue(write)
 const saving=queue.set('theme_customs','first')
 void queue.set('theme_customs','second');void queue.set('theme_customs','latest')
 expect(write).toHaveBeenCalledTimes(1)
 release();expect(await saving).toBe(true)
 expect(write.mock.calls.map(call=>call[1])).toEqual(['first','latest'])
 expect(queue.pending()).toBe(0)
})

it('retains a failed write for explicit retry without pretending it was saved',async()=>{
 const failure=new Error('disk full'),changed=vi.fn()
 const write=vi.fn().mockRejectedValueOnce(failure).mockResolvedValue(undefined)
 const queue=createSettingsQueue(write,changed)
 expect(await queue.set('theme_id','dark')).toBe(false)
 expect(queue.pending()).toBe(1);expect(changed).toHaveBeenLastCalledWith(1,failure)
 expect(await queue.retry()).toBe(true);expect(queue.pending()).toBe(0)
})

it('coalesces progress per identity, rejects malformed payload and flushes terminal status',()=>{
 const deliver=vi.fn(),cancel=vi.fn();let next:(()=>void)|undefined
 const delivery=eventDelivery('download:progress',deliver,work=>{next=work;return cancel})
 delivery.receive({task_id:'a',downloaded:1,status:'downloading'});delivery.receive({task_id:'a',downloaded:2,status:'downloading'})
 delivery.receive({task_id:'b',downloaded:-1})
 expect(deliver).not.toHaveBeenCalled();next!()
 expect(deliver).toHaveBeenCalledTimes(1);expect(deliver).toHaveBeenLastCalledWith(expect.objectContaining({downloaded:2}))
 delivery.receive({task_id:'a',downloaded:3,status:'downloading'});delivery.receive({task_id:'a',downloaded:4,status:'completed'})
 expect(deliver).toHaveBeenLastCalledWith(expect.objectContaining({status:'completed'}))
 delivery.receive({task_id:'a',downloaded:5});delivery.dispose();next!()
 expect(deliver).toHaveBeenCalledTimes(3)
})

it('filters obsolete operation identities before and after scheduled delivery',()=>{
 const deliver=vi.fn();let next:(()=>void)|undefined,current='one'
 const delivery=eventDelivery<{source_key:string;operation_id:string}>('collect:progress',deliver,work=>{next=work;return()=>{}},value=>value.operation_id===current)
 delivery.receive({source_key:'source',operation_id:'one',current:1,total:2});current='two';next!()
 expect(deliver).not.toHaveBeenCalled()
})

it('validates real reading shapes without changing null collections or warning semantics',()=>{
 const value={chapterId:1,paragraphs:null,warnings:['truncated'],fromCache:true}
 expect(chapterText(value)).toBe(value)
 expect(()=>chapterText({...value,paragraphs:[1]})).toThrow()
 expect(()=>detail({item:null,chapters:[],chapterCount:0})).toThrow()
 expect(outcome({sourceKey:'a',ok:false,stages:[{name:'catalog',status:'skipped'}]})).toMatchObject({ok:false})
})
