import { beforeEach, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useNovelReading } from '../src/novel/readingState'
import { readNovelBookTab, rememberNovelBookTab } from '../src/novel/bookViewState'
import { ReadingStateApply } from '../bindings/cczjVideo/app/service/app'
import { readStorage } from '../src/platform/storage'
import { normalizeReadingPreferences } from '../src/novel/domain'
vi.mock('../src/platform/storage',()=>({readStorage:vi.fn()}))
vi.mock('../bindings/cczjVideo/app/service/app', async original => ({ ...await original<typeof import('../bindings/cczjVideo/app/service/app')>(), ReadingStateLoad: vi.fn(async (_kind: string, raw: string) => JSON.parse(raw)), ReadingStateApply: vi.fn(async () => {}) }))
vi.mock('../src/api/library',()=>({
  parseRefKey: (key: string) => { const [kind,sourceKey,remoteId] = key.split('\u0000'); return sourceKey && remoteId ? {kind,sourceKey,remoteId} : null },
  refKey:(ref:{kind:string;sourceKey:string;remoteId:string})=>`${ref.kind}\u0000${ref.sourceKey}\u0000${ref.remoteId}`,
  refOf:(item:{kind:string;sourceKey:string;remoteId:string})=>({kind:item.kind,sourceKey:item.sourceKey,remoteId:item.remoteId}),
}))
const KEY='novel\u0000src-a\u0000r1'
it('validates desktop bindings, reserves fixed shortcuts and separates both directions',()=>{
  expect(normalizeReadingPreferences({wheelPaging:false,keyboardPaging:false,nextKey:'KeyD',previousKey:'KeyA'})).toMatchObject({wheelPaging:false,keyboardPaging:false,nextKey:'KeyD',previousKey:'KeyA'})
  expect(normalizeReadingPreferences({nextKey:'KeyF',previousKey:'Escape'})).toMatchObject({nextKey:'ArrowRight',previousKey:'ArrowLeft'})
  const duplicate=normalizeReadingPreferences({nextKey:'KeyD',previousKey:'KeyD'})
  expect(duplicate.nextKey).not.toBe(duplicate.previousKey)
})
function makeItem(overrides:Record<string,unknown>={}) {
  return {id:1,kind:'novel',sourceKey:'src-a',sourceName:'Source A',remoteId:'r1',title:'远山',author:'作者',cover:'',summary:'',genres:'玄幻,热血',status:'连载中',latest:'',words:'',updatedAt:'',available:true,...overrides} as never
}
beforeEach(()=>{setActivePinia(createPinia());vi.mocked(readStorage).mockImplementation((_key,fallback)=>fallback);vi.mocked(ReadingStateApply).mockResolvedValue(undefined)})
it('bounds malformed preferences and keeps structurally valid progress without a repository check',async ()=>{
  vi.mocked(readStorage).mockImplementation((key,fallback)=>key.endsWith('reader.v2')?{size:90,theme:'invalid',serif:'yes'}:key.endsWith('progress.v2')?{
    [KEY]:{itemId:1,title:'远山',chapterOrd:2,chapterId:9,ratio:7,updatedAt:100},
    'novel\u0000gone\u0000x':{itemId:2,title:'下架书',chapterOrd:0,chapterId:1,ratio:.5,updatedAt:50},
    bad:{itemId:'x'},
  }:fallback)
  const reading=useNovelReading();await reading.init()
  expect(reading.preferences).toMatchObject({size:28,theme:'paper',serif:true})
  // 源没了/书查不到也保留条目：进度不再被静默丢弃。
  expect(Object.keys(reading.progress).sort()).toEqual([KEY,'novel\u0000gone\u0000x'].sort())
  expect(reading.progress[KEY].ratio).toBe(1)
})
it('keeps unavailable shelf entries instead of dropping them, and rejects malformed rows',async ()=>{
  vi.mocked(readStorage).mockImplementation((key,fallback)=>key.endsWith('shelf.v2')?[
    {key:KEY,itemId:1,addedAt:5,available:false,item:makeItem({available:false})},
    {key:KEY,itemId:1,item:makeItem()},
    {key:'bad'},null,'x',
  ]:fallback)
  const reading=useNovelReading();await reading.init()
  expect(reading.shelf).toHaveLength(1)
  expect(reading.shelf[0].available).toBe(false)
  expect(reading.shelf[0].item.title).toBe('远山')
})
it('persists shelf and progress per identity and reports storage failure',async ()=>{
  const reading=useNovelReading();await reading.init()
  await reading.toggleShelf(makeItem());expect(reading.inShelf(KEY)).toBe(true)
  await reading.toggleShelf(makeItem({id:2,remoteId:'r2'}) as never);expect(reading.shelf).toHaveLength(2)
  await reading.removeShelf(KEY);expect(reading.inShelf(KEY)).toBe(false)
  await reading.remember(KEY,{itemId:1,title:'远山',chapterOrd:0,chapterId:5,ratio:.4})
  expect(reading.progress[KEY]).toMatchObject({chapterId:5,ratio:.4})
  expect(reading.lastRead?.key).toBe(KEY)
  expect(reading.readingPath(KEY)).toBe('/novel/read/1/5')
  await reading.removeShelf(KEY)
  expect(reading.readingPath(KEY)).toBe('/novel/read/1/5')
  expect(reading.readingPath('novel\u0000nope\u0000nope')).toBeNull()
  const before={...reading.progress}
  await reading.remember(KEY,{itemId:1,title:'远山',chapterOrd:0,chapterId:5,ratio:Number.NaN})
  expect(reading.progress).toEqual(before)
  expect(vi.mocked(ReadingStateApply).mock.calls.every(([kind]) => kind === 'novel')).toBe(true)
  vi.mocked(ReadingStateApply).mockRejectedValueOnce(new Error('disk full'));await reading.setPreferences({size:23});expect(reading.storageError).toBe(true)
})
it('derives reading paths for shelf entries without progress',async ()=>{
  const reading=useNovelReading();await reading.init()
  await reading.toggleShelf(makeItem())
  expect(reading.readingPath(KEY)).toBe('/novel/read/1/first')
})
it('retains the unavailable shelf flag when the cached item snapshot is still active',async ()=>{
  vi.mocked(readStorage).mockImplementation((key,fallback)=>key.endsWith('shelf.v2')?[
    {key:KEY,itemId:1,addedAt:5,available:false,item:makeItem()},
  ]:fallback)
  const reading=useNovelReading();await reading.init()
  expect(reading.shelf[0].available).toBe(false)
})

it('retains the book detail tab for a reader round trip without persisting content', () => {
  expect(readNovelBookTab(101)).toBe('intro')
  rememberNovelBookTab(101, 'chapters')
  expect(readNovelBookTab(101)).toBe('chapters')
})
