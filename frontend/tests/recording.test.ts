import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import * as api from '../src/api/app'

vi.mock('../src/api/app', () => ({ AddDiagnosticsRecordingMarker: vi.fn(), GetDiagnosticsRecordingStatus: vi.fn() }))
vi.mock('../src/api/runtime', () => ({ isMobileRuntime: () => false, isDesktopRuntime: () => true }))
let recording: typeof import('../src/diagnostics/recording')
const active = { active: true, path: 'recording.jsonl', samples: 1, markers: 0, started_at_ms: 1, ended_at_ms: 0, error: '', reason: '' }

beforeEach(async () => {
  vi.resetModules()
  vi.useFakeTimers()
  vi.mocked(api.AddDiagnosticsRecordingMarker).mockReset().mockResolvedValue(true)
  vi.mocked(api.GetDiagnosticsRecordingStatus).mockReset().mockResolvedValue(active as any)
  recording = await import('../src/diagnostics/recording')
})
afterEach(() => { recording.disposeRecordingObserver(); vi.clearAllTimers(); vi.useRealTimers() })

describe('background playback recording observer', () => {
  it('does no work before opt-in and releases its timer when stopped', async () => {
    await recording.recordDiagnosticEvent('stall', { ms: 1200 })
    expect(api.AddDiagnosticsRecordingMarker).not.toHaveBeenCalled()
    expect(vi.getTimerCount()).toBe(0)
    recording.registerPlaybackSnapshot(() => ({ frames: { total: 1000, dropped: 10 } }))
    recording.setRecordingStatus(active)
    await vi.advanceTimersByTimeAsync(5000)
    expect(api.AddDiagnosticsRecordingMarker).toHaveBeenCalledTimes(3)
    const [kind, payload] = vi.mocked(api.AddDiagnosticsRecordingMarker).mock.calls[0]
    expect(kind).toBe('playback_snapshot')
    expect(JSON.parse(payload)).toEqual({
      application_mode: 'video', ui_architecture_revision: expect.any(String), presentation: 'desktop', player_session_id: null,
      frames: { total: 1000, dropped: 10 },
    })
    expect(vi.mocked(api.AddDiagnosticsRecordingMarker).mock.calls.map(([kind])=>kind)).toEqual(['playback_snapshot','startup_snapshot','activity_snapshot'])
    expect(JSON.parse(vi.mocked(api.AddDiagnosticsRecordingMarker).mock.calls[1][1])).toMatchObject({scope:'current_document',time_origin_ms:expect.any(Number),native:null,events:[],paint_entries:[]})
    expect(JSON.parse(vi.mocked(api.AddDiagnosticsRecordingMarker).mock.calls[2][1])).toMatchObject({scope:'current_document',operations:[],reading:[]})
    recording.setRecordingStatus({ ...active, active: false })
    await vi.advanceTimersByTimeAsync(10000)
    expect(api.AddDiagnosticsRecordingMarker).toHaveBeenCalledTimes(3)
    expect(vi.getTimerCount()).toBe(0)
  })

  it('serializes slow snapshots even when the visible panel refreshes status', async () => {
    let complete!: (value: boolean) => void
    vi.mocked(api.AddDiagnosticsRecordingMarker).mockImplementation(() => new Promise(resolve => { complete = resolve }) as any)
    recording.registerPlaybackSnapshot(() => ({}))
    recording.setRecordingStatus(active)
    await vi.advanceTimersByTimeAsync(5000)
    recording.setRecordingStatus(active)
    await vi.advanceTimersByTimeAsync(20000)
    expect(api.AddDiagnosticsRecordingMarker).toHaveBeenCalledOnce()
    recording.disposeRecordingObserver()
    complete(true)
    await vi.advanceTimersByTimeAsync(10000)
    expect(vi.getTimerCount()).toBe(0)
  })

  it('keeps auxiliary observations separate from large playback markers and writes only changes',async()=>{
    recording.setRecordingStatus(active)
    const value={frames:{total:1000,dropped:1},padding:'a'.repeat(7000)}
    await recording.recordDiagnosticEvent('playback_snapshot',value)
    await recording.recordDiagnosticEvent('playback_snapshot',value)
    const calls=vi.mocked(api.AddDiagnosticsRecordingMarker).mock.calls
    expect(calls.map(([kind])=>kind)).toEqual(['playback_snapshot','startup_snapshot','activity_snapshot','playback_snapshot'])
    expect(calls.every(([,payload])=>new TextEncoder().encode(payload).byteLength<=8192)).toBe(true)
    expect(JSON.parse(calls[0][1]).padding).toHaveLength(7000)
    expect(JSON.parse(calls[0][1])).not.toHaveProperty('startup')
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
    await vi.advanceTimersByTimeAsync(5000)
    expect(recording.recordingObserverFailed.value).toBe(true)
    expect(vi.getTimerCount()).toBe(1)
    recording.setRecordingStatus({ ...active, active: false })
    expect(vi.getTimerCount()).toBe(0)
  })
})
