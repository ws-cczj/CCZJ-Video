import { beforeEach, expect, it, vi } from 'vitest'
import { CancellablePromise } from '@wailsio/runtime'

vi.mock('../bindings/cczjVideo/app/service/app', async original => ({ ...await original<typeof import('../bindings/cczjVideo/app/service/app')>(),
  LibrarySearch: vi.fn(),
  LibrarySearchPage: vi.fn(),
}))

import { LibrarySearch, LibrarySearchPage } from '../bindings/cczjVideo/app/service/app'
import { searchLibrary, type SearchOutcomeDTO } from '../src/api/library'
import { searchNovels } from '../src/novel/data/novelLibrary'
import { searchComics } from '../src/comic/data/repository'
import { Item } from '../bindings/cczjVideo/app/library/models'

beforeEach(() => { vi.mocked(LibrarySearch).mockReset(); vi.mocked(LibrarySearchPage).mockReset() })

function outcome(cursor = ''): SearchOutcomeDTO {
  return { keyword: 'query', page: 2, total: 0, failedCount: 0, results: [], nextCursor: cursor }
}

it.each(['searchedSources','pendingSources'] as const)('rejects corrupt %s instead of displaying invalid progress', async field => {
  const result=outcome()
  result[field]=-1
  vi.mocked(LibrarySearchPage).mockReturnValue(CancellablePromise.resolve(result))
  await expect(searchNovels('query')).rejects.toMatchObject({code:'CORRUPT'})
})

it.each(['novel', 'comic'] as const)('puts %s title/author matches before recommendations mentioning the query', async kind => {
  const row = outcome()
  row.keyword = '鬼吹灯'
  row.results = [{ sourceKey:'one',sourceName:'One',nextUrl:'',hasNext:false,items:[
    new Item({id:1,kind,sourceKey:'one',remoteId:'1',title:'王锋锋的第9本书',author:'王锋锋',summary:'《鬼吹灯》爱好者的故事',available:true}),
    new Item({id:2,kind,sourceKey:'one',remoteId:'2',title:'鬼吹灯',author:'天下霸唱',available:true}),
  ] }]
  vi.mocked(LibrarySearchPage).mockReturnValue(CancellablePromise.resolve(row))
  const found = kind === 'novel' ? await searchNovels('鬼吹灯') : await searchComics('鬼吹灯',1)
  expect(found.items.map(item => item.id)).toEqual([2,1])
})

it('ranks a resolved pinyin title using the effective upstream keyword', async () => {
  const row = outcome()
  row.keyword='西游记'
  row.results=[{sourceKey:'one',sourceName:'One',nextUrl:'',hasNext:false,items:[
    new Item({id:1,kind:'novel',sourceKey:'one',remoteId:'1',title:'其他书籍',author:'',available:true}),
    new Item({id:2,kind:'novel',sourceKey:'one',remoteId:'2',title:'西游记',author:'吴承恩',available:true}),
  ]}]
  vi.mocked(LibrarySearchPage).mockReturnValue(CancellablePromise.resolve(row))
  const found=await searchNovels('xiyouji')
  expect(found.keyword).toBe('西游记')
  expect(found.items.map(item=>item.id)).toEqual([2,1])
})

it('sends opaque continuations to the page binding and retains the legacy numbered entry', async () => {
  vi.mocked(LibrarySearchPage).mockReturnValue(CancellablePromise.resolve(outcome('next')))
  vi.mocked(LibrarySearch).mockReturnValue(CancellablePromise.resolve(outcome()))
  expect((await searchLibrary('novel', 'query', 2, 'opaque')).nextCursor).toBe('next')
  expect(LibrarySearchPage).toHaveBeenCalledWith('novel', 'query', 'opaque')
  expect(LibrarySearch).not.toHaveBeenCalled()
  await searchLibrary('novel', 'query', 2)
  expect(LibrarySearch).toHaveBeenCalledWith('novel', 'query', 2)
})

it.each(['novel', 'comic'] as const)('preserves %s cancellation from the domain adapter to the transport', async kind => {
  const cancelled = vi.fn()
  const transport = new CancellablePromise<SearchOutcomeDTO>(() => {}, cancelled)
  vi.mocked(LibrarySearchPage).mockReturnValue(transport)
  const result = kind === 'novel' ? searchNovels('query', 2, 'opaque') : searchComics('query', 2, 'opaque')
  const rejection = expect(result).rejects.toMatchObject({ name: 'CancelError' })
  await result.cancel()
  await rejection
  expect(cancelled).toHaveBeenCalledTimes(1)
  expect(LibrarySearchPage).toHaveBeenCalledWith(kind, 'query', 'opaque')
})

it.each(['novel', 'comic'] as const)('uses the aggregate continuation as %s hasNext, even for an empty intermediate page', async kind => {
  vi.mocked(LibrarySearchPage).mockReturnValue(CancellablePromise.resolve(outcome('next')))
  const result = kind === 'novel' ? await searchNovels('query') : await searchComics('query', 1)
  expect(result.hasNext).toBe(true)
  expect(result.nextCursor).toBe('next')
  expect(result.items).toHaveLength(0)
})
