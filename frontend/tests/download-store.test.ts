import { beforeEach, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useDownloadStore } from '../src/stores/download'

const api = vi.hoisted(() => ({
  GetSetting: vi.fn(async () => ''), GetDownloadDir: vi.fn(async () => '/downloads'),
  SetDownloadDir: vi.fn(async () => '/downloads'), ListDownloads: vi.fn(async () => []),
  CancelDownload: vi.fn(async () => true), PauseDownload: vi.fn(async () => true),
  ResumeDownload: vi.fn(async () => true), RemoveDownload: vi.fn(async () => true),
  GetDownloadProgress: vi.fn(), StartVideoDownload: vi.fn(), OpenFileInExplorer: vi.fn(),
  downloadErrorCode: vi.fn(() => 'UNKNOWN'),
}))
const errors = vi.hoisted(() => ({ fromError: vi.fn() }))
const events = vi.hoisted(() => ({ listener: (_raw: unknown) => {} }))
vi.mock('../src/api/download', () => api)
vi.mock('../src/api/app', () => ({ normalizeApiError: (error: Error) => ({ message: error.message }) }))
vi.mock('../src/stores/error', () => ({ useErrorStore: () => errors }))
vi.mock('../src/api/events', () => ({ onBackendEvent: vi.fn((_name: string, listener: (raw: unknown) => void) => { events.listener = listener; return vi.fn() }) }))
vi.mock('../src/locales', () => ({ tr: (key: string) => key }))

beforeEach(() => { setActivePinia(createPinia()); vi.clearAllMocks() })

function pausedTask() {
  return { task_id: 'one', url: 'https://example.test/v', filename: 'v.bin', save_path: '/downloads/v.bin', total: 2, downloaded: 1, speed_bps: 0, eta_sec: 0, status: 'paused' as const, start_time: 1 }
}

it('retains the visible record when durable deletion fails or is rejected', async () => {
  const store = useDownloadStore()
  store.tasks = [pausedTask()]
  const failure = new Error('storage full')
  api.RemoveDownload.mockRejectedValueOnce(failure)
  expect(await store.remove('one')).toBe(false)
  expect(store.tasks).toHaveLength(1)
  expect(errors.fromError).toHaveBeenCalledWith('errors.removeTaskFailed', failure)
  api.RemoveDownload.mockResolvedValueOnce(false)
  expect(await store.remove('one')).toBe(false)
  expect(store.tasks).toHaveLength(1)
  expect(await store.remove('one')).toBe(true)
  expect(store.tasks).toHaveLength(0)
})

it('reports history restore failure instead of silently showing an empty queue', async () => {
  const store = useDownloadStore()
  const failure = new Error('corrupt journal')
  api.ListDownloads.mockRejectedValueOnce(failure)
  await store.init()
  expect(errors.fromError).toHaveBeenCalledWith('errors.loadDownloadsFailed', failure)
  store.cleanup()
})

it('preserves state and shows the storage error when cancellation was not committed', async () => {
  const store = useDownloadStore()
  store.tasks = [pausedTask()]
  const failure = new Error('cancel checkpoint failed')
  api.CancelDownload.mockRejectedValueOnce(failure)
  expect(await store.cancel('one')).toBe(false)
  expect(store.tasks[0].status).toBe('paused')
  expect(errors.fromError).toHaveBeenCalledWith('errors.cancelTaskFailed', failure)
})

it('reconciles resumed work against the backend queue snapshot', async () => {
  const store = useDownloadStore()
  store.tasks = [pausedTask()]
  api.GetDownloadProgress.mockResolvedValueOnce({ ...pausedTask(), status: 'queued' })
  expect(await store.resume('one')).toBe(true)
  expect(store.tasks[0].status).toBe('queued')
})

it('does not resurrect a removed task from a late progress event', async () => {
  const store = useDownloadStore()
  await store.init()
  store.tasks = [pausedTask()]
  expect(await store.remove('one')).toBe(true)
  events.listener({ ...pausedTask(), status: 'cancelled' })
  expect(store.tasks).toHaveLength(0)
  store.cleanup()
})
