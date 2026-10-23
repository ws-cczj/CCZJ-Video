import { beforeEach, expect, it, vi } from 'vitest'
import { CancellablePromise } from '@wailsio/runtime'
const binding = vi.hoisted(() => ({ page: (): Promise<unknown> => Promise.resolve(null) }))
// A plain transport stub leaves ownership of the runtime's cancellable chain
// with the adapter; spy result tracking attaches its own thenable consumers.
vi.mock('../src/api/app', () => ({ ComicBrowsePage: () => binding.page(), ComicBrowseChannels: () => Promise.resolve([]) }))
import { comicBrowsePage } from '../src/api/comicBrowse'

const book = { id: 1, kind: 'comic', sourceKey: 'official', sourceName: '官方', remoteId: '/book/1', title: '漫画', author: '', cover: '', summary: '', genres: '', status: '', latest: '', words: '', updatedAt: '', available: true }
const page = (items: unknown[]) => ({ channelId: 'honkai', sourceName: '官方', fetchedAt: 100, fromCache: false, stale: false, warning: '', items })
beforeEach(() => { binding.page = () => Promise.resolve(null) })

it('rejects novel records in a comic catalog before they become clickable comic routes', async () => {
  binding.page = () => CancellablePromise.resolve(page([{ ...book, kind: 'novel' }]))
  await expect(comicBrowsePage('honkai')).rejects.toMatchObject({ code: 'CORRUPT' })
})

it('rejects unbounded catalogs and malformed natural identities', async () => {
  binding.page = () => CancellablePromise.resolve(page(Array.from({ length: 501 }, () => book)))
  await expect(comicBrowsePage('honkai')).rejects.toMatchObject({ code: 'CORRUPT' })
  binding.page = () => CancellablePromise.resolve(page([{ ...book, remoteId: null }]))
  await expect(comicBrowsePage('honkai')).rejects.toMatchObject({ code: 'CORRUPT' })
})

it('cancels the actual catalog request when the browsing page leaves', async () => {
  const cancelled = vi.fn(), started = vi.fn()
  binding.page = () => { started(); return new CancellablePromise(() => {}, cancelled) }
  const request = comicBrowsePage('honkai')
  const rejected = expect(request).rejects.toMatchObject({ name: 'CancelError' })
  await Promise.resolve(); await Promise.resolve()
  expect(started).toHaveBeenCalledOnce()
  await request.cancel()
  await rejected
  expect(cancelled).toHaveBeenCalledOnce()
})
