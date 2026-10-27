import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { effectScope, nextTick, ref } from 'vue'
import { CancellablePromise } from '@wailsio/runtime'
vi.mock('vue', async original => ({ ...await original<typeof import('vue')>(), onBeforeUnmount: vi.fn() }))
vi.mock('../src/api/video', () => ({ GetVideoDetail: vi.fn(), GetShortDramaRecommendations: vi.fn() }))
vi.mock('../src/locales', () => ({ tr: (key: string) => key }))
import { GetVideoDetail, GetShortDramaRecommendations } from '../src/api/video'
import { useShortDramaFeed, type ShortDramaEntry } from '../src/player/shortDrama/useShortDramaFeed'
import type { VideoDetailResponse } from '../src/types'
import { getPlayerPath } from '../src/utils'

let scope: ReturnType<typeof effectScope>
const detail = (id: string): VideoDetailResponse => ({ video: { vod_id: id, vod_name: id }, episodes: [1, 2].map(ep_num => ({ ep_num, ep_url: `https://test.invalid/${id}/${ep_num}.m3u8` })) })
async function settle() { for (let i = 0; i < 18; i++) await nextTick() }
function fixture(initialEntry: ShortDramaEntry | null = null) {
  scope = effectScope()
  const source = ref('a'), active = ref(true), beforeChange = vi.fn()
  const entry = ref(initialEntry)
  const feed = scope.run(() => useShortDramaFeed(source, active, beforeChange, entry))!
  return { feed, source, active, beforeChange, entry }
}
beforeEach(() => {
  vi.mocked(GetShortDramaRecommendations).mockReset()
  vi.mocked(GetVideoDetail).mockReset()
  vi.mocked(GetShortDramaRecommendations).mockImplementation(() => CancellablePromise.resolve([{ vod_id: 'one' }, { vod_id: 'two' }]))
  vi.mocked(GetVideoDetail).mockImplementation(request => CancellablePromise.resolve(detail(request.vod_id)))
})
afterEach(() => scope?.stop())

it('clears an old failure when the selected source is removed', async () => {
  vi.mocked(GetVideoDetail).mockReturnValueOnce(CancellablePromise.reject({ code:'NOT_FOUND', message:'catalog lookup' }))
  vi.mocked(GetShortDramaRecommendations).mockReturnValueOnce(CancellablePromise.resolve([{vod_id:'deleted'}])).mockReturnValueOnce(CancellablePromise.resolve([]))
  const { feed, source } = fixture(); await settle()
  source.value = ''; await settle()
  expect(feed.error.value).toBe('')
  expect(feed.items.value).toEqual([])
  expect(feed.url.value).toBe('')
  expect(feed.playbackSource.value).toBe('')
})

it('reloads a stale cached recommendation batch when its catalog entry was deleted', async () => {
  const { feed, active } = fixture(); await settle()
  active.value = false; await settle()
  vi.mocked(GetVideoDetail).mockReturnValueOnce(CancellablePromise.reject({code:'NOT_FOUND',message:'catalog lookup'}))
  vi.mocked(GetShortDramaRecommendations).mockReturnValueOnce(CancellablePromise.resolve([])).mockReturnValueOnce(CancellablePromise.resolve([]))
  active.value = true; await settle()
  expect(feed.items.value).toEqual([])
  expect(feed.error.value).toBe('')
  expect(feed.url.value).toBe('')
})

it('bounds recovery if refreshed catalog entries also disappear and lets retry reload the batch', async () => {
  vi.mocked(GetVideoDetail).mockImplementation(() => CancellablePromise.reject({code:'NOT_FOUND',message:'catalog lookup'}))
  const { feed } = fixture(); await settle()
  await vi.waitFor(() => expect(feed.error.value).toBe('catalog lookup'))
  expect(GetVideoDetail).toHaveBeenCalledTimes(2)
  vi.mocked(GetShortDramaRecommendations).mockReturnValueOnce(CancellablePromise.resolve([])).mockReturnValueOnce(CancellablePromise.resolve([]))
  await feed.retry()
  expect(feed.items.value).toEqual([])
  expect(feed.error.value).toBe('')
})

it('uses different titles for both manual navigation and automatic playback in recommendations', async () => {
  const { feed } = fixture(); await settle()
  expect(GetShortDramaRecommendations).toHaveBeenCalledWith('a', [])
  expect(feed.video.value?.vod_id).toBe('one')
  expect(feed.url.value).toContain('/one/1')
  feed.ended(); await settle(); expect(feed.video.value?.vod_id).toBe('two')
  expect(feed.episodeIndex.value).toBe(0)
  feed.step('previous'); await settle(); expect(feed.url.value).toContain('/one/1')
})

it('selecting even the current episode enters a series and stays within its episode boundaries', async () => {
  const { feed } = fixture(); await settle()
  feed.selectEpisode(0)
  expect(feed.series.value).toBe(true)
  feed.step('previous'); expect(feed.url.value).toContain('/one/1')
  feed.step('next'); expect(feed.url.value).toContain('/one/2')
  feed.ended(); await settle()
  expect(feed.video.value?.vod_id).toBe('one')
  expect(feed.completed.value).toBe(true)
  expect(feed.canNext.value).toBe(false)
  feed.step('next'); await settle(); expect(feed.video.value?.vod_id).toBe('one')
  feed.step('previous'); expect(feed.completed.value).toBe(false)
  feed.discover(); expect(feed.series.value).toBe(false)
  feed.step('next'); await settle(); expect(feed.video.value?.vod_id).toBe('two')
})

it('opens a specific series using its source and episode without requesting a recommendation batch', async () => {
  const { feed, active } = fixture({ sourceKey:'other', vodId:'direct', globalId:23, episodeIndex:1 })
  await settle()
  expect(GetShortDramaRecommendations).not.toHaveBeenCalled()
  expect(GetVideoDetail).toHaveBeenCalledWith(expect.objectContaining({source_key:'other',vod_id:'direct',global_id:23}))
  expect(feed.series.value).toBe(true)
  expect(feed.url.value).toContain('/direct/2')
  feed.step('previous'); expect(feed.url.value).toContain('/direct/1')
  active.value = false; await settle(); active.value = true; await settle()
  expect(feed.url.value).toContain('/direct/1')
  feed.ended(); expect(feed.url.value).toContain('/direct/2')
  feed.ended(); expect(feed.completed.value).toBe(true)
  expect(GetShortDramaRecommendations).not.toHaveBeenCalled()
})

it('starts another recommendation when returning from a completed series', async () => {
  const {feed} = fixture(); await settle()
  feed.selectEpisode(1); feed.ended()
  expect(feed.completed.value).toBe(true)
  feed.discover(); await settle()
  expect(feed.series.value).toBe(false)
  expect(feed.completed.value).toBe(false)
  expect(feed.url.value).toContain('/two/1')
})

it('cancels a late direct entry and switches back to recommendations under the selected source', async () => {
  const { feed, entry } = fixture({sourceKey:'other',vodId:'direct',globalId:23,episodeIndex:0}); await settle()
  const late = CancellablePromise.withResolvers<VideoDetailResponse>()
  vi.mocked(GetVideoDetail).mockReturnValueOnce(late.promise)
  entry.value = {sourceKey:'third',vodId:'late',globalId:24,episodeIndex:0}; await settle()
  entry.value = null; await settle()
  late.resolve(detail('late')); await settle()
  expect(GetShortDramaRecommendations).toHaveBeenLastCalledWith('a',[])
  expect(feed.series.value).toBe(false)
  expect(feed.playbackSource.value).toBe('a')
  expect(feed.url.value).toContain('/one/1')
})

it('preserves the chosen playback line when a detail page opens the portrait player', async () => {
  vi.mocked(GetVideoDetail).mockReturnValueOnce(CancellablePromise.resolve({
    ...detail('direct'), lines:[{index:3,episodes:detail('line-three').episodes},{index:7,episodes:detail('line-seven').episodes}],
  }))
  const {feed} = fixture({sourceKey:'other',vodId:'direct',globalId:23,episodeIndex:1,lineIndex:7}); await settle()
  expect(feed.url.value).toContain('/line-seven/2')
  expect(getPlayerPath('other', {vod_id:'direct',global_id:23,type_name:'短剧'},1,7)).toBe('/short-drama?source=other&vod=direct&global_id=23&episode=1&line=7')
  expect(getPlayerPath('other', {vod_id:'电影名包含短剧',global_id:23,type_name:'电影'},1)).toBe('/player/other/23/1?vod=%E7%94%B5%E5%BD%B1%E5%90%8D%E5%8C%85%E5%90%AB%E7%9F%AD%E5%89%A7')
})

it('cancels and discards a late detail when another title is selected', async () => {
  const { feed } = fixture(); await settle()
  const old = CancellablePromise.withResolvers<VideoDetailResponse>()
  vi.mocked(GetVideoDetail).mockReturnValueOnce(old.promise)
  const opening = feed.open(1); await settle()
  await feed.open(0); old.resolve(detail('two')); await opening
  expect(feed.video.value?.vod_id).toBe('one')
  expect(feed.url.value).toContain('/one/1')
})

it('releases hidden playback and requests, preserves the selected episode, and resumes only when active', async () => {
  const { feed, active } = fixture(); await settle()
  feed.selectEpisode(1)
  active.value = false; await settle()
  expect(feed.url.value).toBe('')
  const count = vi.mocked(GetVideoDetail).mock.calls.length
  await feed.open(1); expect(GetVideoDetail).toHaveBeenCalledTimes(count)
  active.value = true; await settle()
  expect(feed.url.value).toContain('/one/2')
})

it('flushes progress under its original source before a source change', async () => {
  const { feed, source, beforeChange } = fixture(); await settle()
  const recorded: string[] = []
  beforeChange.mockImplementation(() => { if (feed.url.value) recorded.push(feed.playbackSource.value) })
  source.value = 'b'; await settle()
  expect(recorded).toEqual(['a'])
  expect(feed.playbackSource.value).toBe('b')
})

it('recycles a small library only when exclusions exhaust it and exposes an actual request failure', async () => {
  const { feed } = fixture(); await settle()
  vi.mocked(GetShortDramaRecommendations).mockReturnValueOnce(CancellablePromise.resolve([]))
  await feed.refresh()
  expect(GetShortDramaRecommendations).toHaveBeenLastCalledWith('a', [])
  vi.mocked(GetShortDramaRecommendations).mockReturnValueOnce(CancellablePromise.reject({ code: 'UNAVAILABLE', message: 'offline' }))
  await feed.refresh()
  expect(feed.error.value).toContain('offline')
  expect(feed.url.value).toBe('')
  expect(feed.loading.value).toBe(false)
})
