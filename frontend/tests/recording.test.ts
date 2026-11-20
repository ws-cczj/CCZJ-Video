import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import * as api from '../src/api/app'

vi.mock('../src/api/app', () => ({
  GetDiagnosticsRecordingStatus: vi.fn(),
  GetSetting: vi.fn(),
  SetSetting: vi.fn(),
  SubmitDiagnosticsEvents: vi.fn(),
}))
vi.mock('../src/api/runtime', () => ({ isMobileRuntime: () => false, isDesktopRuntime: () => true }))
let recording: typeof import('../src/diagnostics/recording')
const active = { active: true, path: 'recording.jsonl', samples: 1, markers: 0, started_at_ms: 1, ended_at_ms: 0, error: '', reason: '' }
const receipt = (payload: string) => ({ accepted: JSON.parse(payload).length, rejected: 0, daily_enabled: false })

beforeEach(async () => {
  vi.resetModules()
  vi.useFakeTimers()
  vi.mocked(api.GetSetting).mockReset().mockResolvedValue('0')
  vi.mocked(api.SubmitDiagnosticsEvents).mockReset().mockImplementation(async payload => receipt(payload))
  vi.mocked(api.GetDiagnosticsRecordingStatus).mockReset().mockResolvedValueOnce({ ...active, active: false }).mockResolvedValue(active)
  recording = await import('../src/diagnostics/recording')
  await recording.initializeDiagnostics()
})
afterEach(() => { recording.disposeRecordingObserver(); vi.clearAllTimers(); vi.useRealTimers() })

describe('background playback recording observer', () => {
  it('does no work before opt-in and releases its timer when stopped', async () => {
    await recording.recordDiagnosticEvent('stall', { ms: 1200 })
    expect(api.SubmitDiagnosticsEvents).not.toHaveBeenCalled()
    expect(vi.getTimerCount()).toBe(0)
    recording.registerPlaybackSnapshot(() => ({ frames: { total: 1000, dropped: 10 } }))
    recording.setRecordingStatus(active)
    await vi.advanceTimersByTimeAsync(17000)
    expect(api.SubmitDiagnosticsEvents).toHaveBeenCalledOnce()
    const [payload] = vi.mocked(api.SubmitDiagnosticsEvents).mock.calls[0]
    expect(JSON.parse(payload)).toMatchObject([{
      kind: 'playback_snapshot', application_mode: 'video',
      data: { frames_total: 1000, frames_dropped: 10 },
    }])
    recording.setRecordingStatus({ ...active, active: false })
    await vi.advanceTimersByTimeAsync(10000)
    expect(api.SubmitDiagnosticsEvents).toHaveBeenCalledOnce()
    expect(vi.getTimerCount()).toBe(0)
  })

  it('shares the current submission when the visible panel flushes a slow snapshot', async () => {
    let complete!: () => void
    vi.mocked(api.SubmitDiagnosticsEvents).mockImplementation(payload => new Promise(resolve => {
      complete = () => resolve(receipt(payload))
    }))
    recording.registerPlaybackSnapshot(() => ({}))
    recording.setRecordingStatus(active)
    await vi.advanceTimersByTimeAsync(17000)
    const flush = recording.flushDiagnostics()
    expect(api.SubmitDiagnosticsEvents).toHaveBeenCalledOnce()
    recording.disposeRecordingObserver()
    complete()
    expect(await flush).toBe(true)
    expect(vi.getTimerCount()).toBe(0)
  })

  it('keeps only bounded numeric summaries in playback observations', async () => {
    recording.setRecordingStatus(active)
    const value={frames:{total:1000,dropped:1},padding:'a'.repeat(7000)}
    await recording.recordDiagnosticEvent('playback_snapshot',value)
    await recording.recordDiagnosticEvent('playback_snapshot',value)
    expect(await recording.flushDiagnostics()).toBe(true)
    const [payload] = vi.mocked(api.SubmitDiagnosticsEvents).mock.calls[0]
    const events = JSON.parse(payload)
    expect(events).toHaveLength(2)
    expect(events.map((event: { data: unknown }) => event.data)).toEqual([
      { frames_total: 1000, frames_dropped: 1 },
      { frames_total: 1000, frames_dropped: 1 },
    ])
  })

  it('ignores stale status responses and continues observing after a transient read failure', async () => {
    let complete!: (value: unknown) => void
    vi.mocked(api.GetDiagnosticsRecordingStatus).mockImplementationOnce(() => new Promise(resolve => { complete = resolve }) as any)
    const pending = recording.refreshRecordingStatus()
    recording.setRecordingStatus(active)
    complete({ ...active, active: false })
    await pending
    expect(recording.recordingStatus.value.active).toBe(true)
    vi.mocked(api.GetDiagnosticsRecordingStatus).mockRejectedValueOnce(new Error('temporary IPC failure'))
    await vi.advanceTimersByTimeAsync(17000)
    expect(recording.recordingObserverFailed.value).toBe(true)
    expect(vi.getTimerCount()).toBe(1)
    recording.setRecordingStatus({ ...active, active: false })
    expect(vi.getTimerCount()).toBe(0)
  })
})
