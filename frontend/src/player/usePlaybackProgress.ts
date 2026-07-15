import { readStorage, removeStorage, writeStorage } from '../platform/storage'

const RESUME_THRESHOLD_SEC = 5

export function readPlaybackTime(videoKey: string): number {
  const value = readStorage<number | null>(`vp_t_${videoKey}`, null)
  const seconds = value ? Number(value) : 0
  return Number.isFinite(seconds) ? seconds : 0
}

export function writePlaybackTime(videoKey: string, seconds: number): void {
  if (!videoKey) return
  writeStorage(`vp_t_${videoKey}`, seconds)
}

export function savePlaybackTime(videoKey: string, seconds: number, duration: number): void {
  if (!videoKey || seconds <= RESUME_THRESHOLD_SEC) return
  if (duration > 0 && seconds >= duration - 1) return
  writePlaybackTime(videoKey, seconds)
}

export function clearPlaybackTime(videoKey: string): void {
  removeStorage(`vp_t_${videoKey}`)
}
