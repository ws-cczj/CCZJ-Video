import { ref } from 'vue'
import { clearPlaybackTime, readPlaybackTime, savePlaybackTime } from './usePlaybackProgress'

/** Persisted keys keep their existing spelling so upgrades retain local progress. */
export function playbackResumeKey(videoKey: string, url: string): string {
  if (videoKey) return 'vp_t_' + videoKey
  try {
    const parsed = new URL(url)
    return 'vp_t_url_' + parsed.origin + parsed.pathname
  } catch {
    return 'vp_t_url_' + url.split('?')[0].split('#')[0].slice(0, 120)
  }
}

export function usePlaybackResume(deps: {
  getVideo: () => HTMLVideoElement | null
  getKey: () => string
  resolveResume: () => Promise<number> | undefined
  shouldResume?: () => boolean
  settings: { read: (key: string, fallback: string) => string; write: (key: string, value: string) => void }
}) {
  const savedTime = ref<number | null>(null)
  const showResumePrompt = ref(false)
  const resumeRemainSec = ref(5)
  let generation = 0
  let key = ''
  let resolving = false
  let pendingSeek: number | null = null
  let promptTimer: ReturnType<typeof setInterval> | null = null
  let saveTimer: ReturnType<typeof setInterval> | null = null

  function dismissResumePrompt(): void {
    showResumePrompt.value = false
    if (promptTimer !== null) clearInterval(promptTimer)
    promptTimer = null
  }

  function reset(): void {
    generation++
    dismissResumePrompt()
    if (saveTimer !== null) clearInterval(saveTimer)
    saveTimer = null
    key = ''
    resolving = false
    pendingSeek = null
    savedTime.value = null
  }

  function seekWhenReady(seconds: number): void {
    const video = deps.getVideo()
    if (!video || !Number.isFinite(seconds) || seconds < 0) return
    pendingSeek = seconds
    if (video.readyState >= 1) flushPendingSeek()
  }

  function flushPendingSeek(): void {
    const video = deps.getVideo()
    if (pendingSeek === null || !video || !Number.isFinite(video.duration) || video.duration <= 0) return
    const target = Math.min(pendingSeek, Math.max(0, video.duration - 1))
    try {
      video.currentTime = target
      pendingSeek = null
    } catch { /* Retain the seek until the next metadata/canplay event. */ }
  }

  async function load(continuation?: number): Promise<void> {
    reset()
    key = deps.getKey()
    // Changing transport for the same episode is a continuation of this live
    // session. Opening saved history remains governed by auto_resume_jump.
    if (continuation !== undefined && Number.isFinite(continuation) && continuation >= 0) {
      seekWhenReady(continuation)
      return
    }
    const token = generation
    resolving = true
    let seconds = readPlaybackTime(key)
    if (seconds <= 5 || deps.shouldResume?.()) {
      try {
        const durable = (await deps.resolveResume()) || 0
        if (durable > 5 || seconds <= 5) seconds = durable
      } catch { /* Keep a usable local checkpoint when durable storage is unavailable. */ }
    }
    if (token !== generation) return
    resolving = false
    if (!Number.isFinite(seconds) || seconds <= 5) return
    savedTime.value = seconds
    if (deps.shouldResume?.() || deps.settings.read('auto_resume_jump', '0') === '1') {
      seekWhenReady(deps.shouldResume?.() ? seconds : Math.max(0, seconds - 1))
      return
    }
    showResumePrompt.value = true
    resumeRemainSec.value = 5
    promptTimer = setInterval(() => {
      if (--resumeRemainSec.value <= 0) dismissResumePrompt()
    }, 1000)
  }

  function jumpToSavedTime(remember: boolean): void {
    if (savedTime.value !== null) seekWhenReady(Math.max(0, savedTime.value - 1))
    if (remember) deps.settings.write('auto_resume_jump', '1')
    dismissResumePrompt()
  }

  function canPersist(): boolean {
    const video = deps.getVideo()
    return !!key && !!video && !resolving && pendingSeek === null && !showResumePrompt.value && !video.seeking && video.readyState >= 1
  }

  function save(): void {
    const video = deps.getVideo()
    if (!video || !key || video.ended || !canPersist()) return
    savePlaybackTime(key, video.currentTime, video.duration)
    deps.settings.write('volume', String(video.volume))
    deps.settings.write('muted', video.muted ? '1' : '0')
  }

  function track(): void {
    if (saveTimer === null) saveTimer = setInterval(save, 3000)
  }

  function ended(): void {
    if (key) clearPlaybackTime(key)
    reset()
  }

  return { savedTime, showResumePrompt, resumeRemainSec, load, track, save, reset,
    ended, flushPendingSeek, jumpToSavedTime, dismissResumePrompt, canPersist }
}
