import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { effectScope, nextTick, ref } from 'vue'
import { CancellablePromise } from '@wailsio/runtime'
vi.mock('vue',async original=>({...await original<typeof import('vue')>(),onActivated:vi.fn(),onDeactivated:vi.fn()}))
vi.mock('../src/api/app',()=>({SearchVideos:vi.fn()}))
vi.mock('../src/api/video',()=>({GetVideoPage:vi.fn()}))
vi.mock('../src/stores/video',()=>({useVideoStore:()=>({lastDeletion:null})}))
vi.mock('../src/stores/error',()=>({useErrorStore:()=>({fromError:vi.fn()})}))
vi.mock('../src/locales',()=>({tr:(key:string)=>key}))
import {GetVideoPage} from '../src/api/video'
import {useVideoList} from '../src/composables/useVideoList'

let scope:ReturnType<typeof effectScope>
const filter={short_drama:true,type_id:'',year:'',area:'',keyword:''}
const page=(id:string,cursor='')=>({videos:[{vod_id:id,vod_name:id}],total:2,next_cursor:cursor})
beforeEach(()=>{scope=effectScope();vi.clearAllMocks()})
afterEach(()=>scope.stop())

it('sends the short drama boundary through cursor pages and publishes a retryable failure',async()=>{
  const list=scope.run(()=>useVideoList())!
  vi.mocked(GetVideoPage).mockReturnValueOnce(CancellablePromise.resolve(page('one','next')))
  await list.load('source',filter,1,24)
  expect(GetVideoPage).toHaveBeenCalledWith(expect.objectContaining({short_drama:true,cursor:'',page_size:24}))
  vi.mocked(GetVideoPage).mockReturnValueOnce(CancellablePromise.reject({code:'UNAVAILABLE',message:'catalog unavailable'}))
  await list.load('source',filter,2,24)
  expect(list.error).toBe('catalog unavailable')
  expect(list.videos.map(row=>row.vod_id)).toEqual(['one'])
  vi.mocked(GetVideoPage).mockReturnValueOnce(CancellablePromise.resolve(page('two')))
  await list.load('source',filter,2,24)
  expect(GetVideoPage).toHaveBeenLastCalledWith(expect.objectContaining({short_drama:true,cursor:'next'}))
  expect(list.error).toBe('')
  expect(list.videos.map(row=>row.vod_id)).toEqual(['one','two'])
})

it('a retained hidden theatre cancels its request and discards late results until reactivated',async()=>{
  const active=ref(true)
  const list=scope.run(()=>useVideoList({enabled:()=>active.value}))!
  const late=CancellablePromise.withResolvers<ReturnType<typeof page>>()
  vi.mocked(GetVideoPage).mockReturnValueOnce(late.promise)
  const loading=list.load('source',filter)
  await nextTick();active.value=false
  expect(list.interrupted).toBe(true)
  expect(list.loading).toBe(false)
  late.resolve(page('late'));await loading
  expect(list.videos).toEqual([])
  await list.load('source',filter)
  expect(GetVideoPage).toHaveBeenCalledTimes(1)
  active.value=true
  vi.mocked(GetVideoPage).mockReturnValueOnce(CancellablePromise.resolve(page('current')))
  await list.load('source',filter)
  expect(list.videos[0]?.vod_id).toBe('current')
})
