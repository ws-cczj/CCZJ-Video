import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { CancellablePromise } from '@wailsio/runtime'
import * as updates from '../src/stores/updateState'

const api = vi.hoisted(() => ({ download: vi.fn(), write: vi.fn(), remove: vi.fn() }))
vi.mock('../src/api/app', () => ({ DownloadUpdate: (...args: unknown[]) => api.download(...args) }))
vi.mock('../src/platform/storage', () => ({ readStorage: (_key: string, fallback: unknown) => fallback, writeStorage: (...args: unknown[]) => api.write(...args), removeStorage: (...args: unknown[]) => api.remove(...args) }))
vi.mock('../src/locales', () => ({ tr: (key: string) => key }))

beforeEach(() => {
  updates.stopUpdateDownload()
  vi.clearAllMocks()
  updates.updateDownloaded.value = false
  updates.updateDownloadError.value = false
  updates.updateDownloadPath.value = ''
})
afterEach(() => updates.stopUpdateDownload())

it('owns one binding and persists only the completed package', async () => {
  const result = CancellablePromise.withResolvers<string>()
  api.download.mockReturnValue(result.promise)
  const done = updates.downloadUpdateArtifact('https://example.test/update', '1.2.3')
  await updates.downloadUpdateArtifact('https://example.test/other', '2.0.0')
  expect(api.download).toHaveBeenCalledOnce()
  expect(updates.updateDownloading.value).toBe(true)
  result.resolve('verified.exe')
  await done
  expect(updates.updateDownloadPath.value).toBe('verified.exe')
  expect(updates.updateDownloaded.value).toBe(true)
  expect(updates.updateDownloading.value).toBe(false)
  expect(api.write).toHaveBeenCalledWith('cczj_update_download_state', { path: 'verified.exe', version: '1.2.3', url: 'https://example.test/update' })
})

it('cancels released work without letting its late completion reset the next download', async () => {
  const old = CancellablePromise.withResolvers<string>(), current = CancellablePromise.withResolvers<string>()
  api.download.mockReturnValueOnce(old.promise).mockReturnValueOnce(current.promise)
  const oldDone = updates.downloadUpdateArtifact('old', '1').catch(error => error)
  updates.stopUpdateDownload()
  const newDone = updates.downloadUpdateArtifact('new', '2')
  old.resolve('stale.exe')
  expect((await oldDone).name).toBe('CancelError')
  expect(updates.updateDownloading.value).toBe(true)
  expect(updates.updateDownloaded.value).toBe(false)
  expect(updates.updateDownloadError.value).toBe(false)
  current.resolve('current.exe')
  await newDone
  expect(updates.updateDownloadPath.value).toBe('current.exe')
  expect(api.write).toHaveBeenCalledOnce()
})

it('reports download failure and releases admission for retry', async () => {
  const failed = CancellablePromise.withResolvers<string>()
  api.download.mockReturnValue(failed.promise)
  const error = { code: 'CORRUPT', message: 'checksum mismatch' }
  const rejected = expect(updates.downloadUpdateArtifact('first', '1')).rejects.toBeDefined()
  failed.reject(error)
  await rejected
  expect(updates.updateDownloadError.value).toBe(true)
  expect(updates.updateDownloading.value).toBe(false)
  api.download.mockReturnValue(CancellablePromise.resolve('retry.exe'))
  await updates.downloadUpdateArtifact('retry', '1')
  expect(updates.updateDownloadError.value).toBe(false)
  expect(updates.updateDownloadPath.value).toBe('retry.exe')
})
