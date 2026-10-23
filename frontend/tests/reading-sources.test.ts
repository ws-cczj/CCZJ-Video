import { expect,it,vi } from 'vitest'
import { CancellablePromise } from '@wailsio/runtime'
const rpc=vi.hoisted(()=>({novel:vi.fn(),comic:vi.fn(),comicMatch:vi.fn()}))
vi.mock('../src/api/app',()=>({NovelSourceDirectory:rpc.novel,ComicSourceDirectory:rpc.comic,ComicEpisodeMatch:rpc.comicMatch,NovelChapterMatch:vi.fn(),NovelSourceAlternatives:vi.fn(),ComicSourceAlternatives:vi.fn()}))
import { listNovelBuiltinSources,listComicBuiltinSources,matchComicEpisode } from '../src/api/readingSources'

it('rejects source directory data from another mode',async()=>{
  rpc.novel.mockReturnValue(CancellablePromise.resolve({kind:'comic',registered:0,awaitingAdapter:0,auditRunning:false,rows:[]}))
  await expect(listNovelBuiltinSources()).rejects.toMatchObject({code:'CORRUPT'})
})
it('keeps unknown quality distinct from zero and rejects invalid persisted scores',async()=>{
  const row={id:'comic',sourceKey:'source',name:'Comic',enabled:true,pool:'reserve',score:-1,confidence:'insufficient',checkedAt:0,deepAt:0,state:'unmeasured'}
  rpc.comic.mockImplementation(()=>CancellablePromise.resolve({kind:'comic',registered:1,awaitingAdapter:0,auditRunning:false,rows:[row]}))
  expect((await listComicBuiltinSources()).rows?.[0]?.score).toBe(-1)
  row.score=-2
  await expect(listComicBuiltinSources()).rejects.toMatchObject({code:'CORRUPT'})
})
it('does not accept a novel chapter mapping as a comic episode mapping',async()=>{
  rpc.comicMatch.mockReturnValue(CancellablePromise.resolve({kind:'novel',identity:'verified',status:'matched',chapterId:1,targetItemId:2,targets:[],evidence:'wrong mode'}))
  await expect(matchComicEpisode(1,1,2)).rejects.toMatchObject({code:'CORRUPT'})
})
