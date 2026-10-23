import { afterEach, expect, it, vi } from 'vitest'
import { preparePlayback } from '../src/player/preparation'
afterEach(()=>vi.useRealTimers())
it('prepares real stages in order before playback can proceed',async()=>{
 const stages:string[]=[],controller=new AbortController()
 let complete!:()=>void
 const pending=new Promise<void>(resolve=>complete=resolve)
 const work=preparePlayback({signal:controller.signal,stage:value=>stages.push(value),accessories:()=>pending,danmaku:async()=>{stages.push('comments-read')}})
 await Promise.resolve();expect(stages).toEqual(['accessories'])
 complete();expect(await work).toBe(true)
 expect(stages).toEqual(['accessories','danmaku','comments-read','ready'])
})
it('does not hold video playback behind missing or slow optional comments',async()=>{
 vi.useFakeTimers();const stages:string[]=[],timeouts:string[]=[]
 const work=preparePlayback({signal:new AbortController().signal,stage:value=>stages.push(value),accessories:async()=>{},danmaku:()=>new Promise(()=>{}),timeout:value=>timeouts.push(value)})
 await vi.advanceTimersByTimeAsync(2501)
 expect(await work).toBe(true);expect(timeouts).toEqual(['danmaku']);expect(stages.at(-1)).toBe('ready');expect(vi.getTimerCount()).toBe(0)
})
it('a replaced media load cancels preparation and never starts the next stage',async()=>{
 vi.useFakeTimers();const controller=new AbortController(),comments=vi.fn(),stages:string[]=[]
 const work=preparePlayback({signal:controller.signal,stage:value=>stages.push(value),accessories:()=>new Promise(()=>{}),danmaku:comments})
 controller.abort();expect(await work).toBe(false);expect(comments).not.toHaveBeenCalled();expect(stages).toEqual(['accessories']);expect(vi.getTimerCount()).toBe(0)
})
