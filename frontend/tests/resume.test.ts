import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { usePlaybackResume, playbackResumeKey } from '../src/player/usePlaybackResume'

const storage = vi.hoisted(() => ({ values: new Map<string, number>() }))
vi.mock('../src/platform/storage', () => ({
  readStorage: (key: string, fallback: unknown) => storage.values.get(key) ?? fallback,
  writeStorage: (key: string, value: number) => storage.values.set(key, value),
  removeStorage: (key: string) => storage.values.delete(key),
}))

beforeEach(() => { storage.values.clear(); vi.useFakeTimers() })
afterEach(() => vi.useRealTimers())

function setup(immediate=false) {
  let key = 'episode-1'
  const video = { readyState: 0, currentTime: 0, duration: 120, volume: 0.8, muted: false, ended: false }
  const settings = new Map<string, string>()
  const backend = vi.fn(async () => 30)
  const resume = usePlaybackResume({
    shouldResume:()=>immediate,
    getVideo: () => video as HTMLVideoElement, getKey: () => key, resolveResume: backend,
    settings: { read: (key, fallback) => settings.get(key) ?? fallback, write: (key, value) => settings.set(key, value) },
  })
  return { resume, video, settings, backend, switchEpisode: () => { key = 'episode-2'; video.currentTime = 0 } }
}

describe('resume across episodes and lines', () => {
  it('keeps the same episode key across URL signatures and line changes', () => {
    expect(playbackResumeKey('source:movie:5', 'https://a/1?token=old')).toBe(playbackResumeKey('source:movie:5', 'https://b/2?token=new'))
    expect(playbackResumeKey('', 'https://a/1?token=old#t')).toBe(playbackResumeKey('', 'https://a/1?token=new'))
    expect(playbackResumeKey('source:movie:5', '')).not.toBe(playbackResumeKey('source:movie:6', ''))
  })

  it('falls back to backend progress and applies seek only after metadata', async () => {
    const { resume, video, settings } = setup()
    settings.set('auto_resume_jump', '1')
    await resume.load()
    expect(video.currentTime).toBe(0)
    video.readyState = 1
    resume.flushPendingSeek()
    expect(video.currentTime).toBe(29)
    resume.reset()
  })

  it('ignores backend progress returned after switching episodes', async () => {
    const { resume, backend, switchEpisode } = setup()
    let resolve!: (value: number) => void
    backend.mockImplementationOnce(() => new Promise(yes => { resolve = yes }))
    const old = resume.load()
    switchEpisode()
    resume.reset()
    resolve(80)
    await old
    expect(resume.savedTime.value).toBeNull()
    expect(resume.showResumePrompt.value).toBe(false)
  })

  it('prefers local progress, expires the prompt and clears timers on exit', async () => {
    const { resume, backend } = setup()
    storage.values.set('vp_t_episode-1', 45)
    await resume.load()
    expect(backend).not.toHaveBeenCalled()
    expect(resume.showResumePrompt.value).toBe(true)
    vi.advanceTimersByTime(5000)
    expect(resume.showResumePrompt.value).toBe(false)
    resume.track()
    resume.reset()
    expect(vi.getTimerCount()).toBe(0)
  })

  it('saves under the loaded episode rather than newly changed props', async () => {
    const { resume, video, switchEpisode } = setup()
    await resume.load()
    switchEpisode()
    video.currentTime = 42
    video.readyState = 1
    resume.dismissResumePrompt()
    resume.save()
    expect(storage.values.get('vp_t_episode-1')).toBe(42)
    expect(storage.values.has('vp_t_episode-2')).toBe(false)
    resume.reset()
  })

  it('removes finished progress and never restores beyond the end', async () => {
    const { resume, video, settings, backend } = setup()
    settings.set('auto_resume_jump', '1')
    backend.mockResolvedValue(900)
    await resume.load()
    video.readyState = 1
    resume.flushPendingSeek()
    expect(video.currentTime).toBe(119)
    video.ended = true
    storage.values.set('vp_t_episode-1', 119)
    resume.ended()
    resume.save()
    expect(storage.values.has('vp_t_episode-1')).toBe(false)
    expect(vi.getTimerCount()).toBe(0)
  })

  it('continue watching restores the durable position without enabling global auto resume', async () => {
    const {resume,video,backend,settings}=setup(true)
    storage.values.set('vp_t_episode-1',7)
    backend.mockResolvedValue(60)
    await resume.load()
    expect(resume.showResumePrompt.value).toBe(false)
    expect(resume.canPersist()).toBe(false)
    video.currentTime=1;resume.save()
    expect(storage.values.get('vp_t_episode-1')).toBe(7)
    video.readyState=1;resume.flushPendingSeek()
    expect(video.currentTime).toBe(60)
    expect(resume.canPersist()).toBe(true)
    expect(settings.has('auto_resume_jump')).toBe(false)
    resume.reset()
  })

  it('does not overwrite a checkpoint while the resume prompt is pending', async () => {
    const {resume,video}=setup()
    storage.values.set('vp_t_episode-1',60)
    await resume.load()
    video.readyState=1;video.currentTime=7;resume.track()
    vi.advanceTimersByTime(3000)
    expect(storage.values.get('vp_t_episode-1')).toBe(60)
    resume.jumpToSavedTime(false)
    expect(video.currentTime).toBe(59)
    resume.reset()
  })

  it('a failed backend read does not fail playback initialization', async () => {
    const { resume, backend } = setup()
    backend.mockRejectedValue(new Error('offline'))
    await expect(resume.load()).resolves.toBeUndefined()
    expect(resume.showResumePrompt.value).toBe(false)
    resume.reset()
  })
})
