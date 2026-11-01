import { CCZJ } from '../constants/cczj'
import { readStorage, removeStorage, writeStorage } from '../platform/storage'

const RESUME_THRESHOLD_SEC = 5
const storageKey = CCZJ.Keys.playbackTime

export function readPlaybackTime(videoKey: string): number {
  const value = readStorage<number | null>(storageKey(videoKey), null) ?? readStorage<number | null>(CCZJ.Keys.legacyPlaybackTime(videoKey), null)
  const seconds = value ? Number(value) : 0
  return Number.isFinite(seconds) ? seconds : 0
}

export function writePlaybackTime(videoKey: string, seconds: number): void {
  if (!videoKey) return
  writeStorage(storageKey(videoKey), seconds)
}

export function savePlaybackTime(videoKey: string, seconds: number, duration: number): void {
  if (!videoKey || seconds <= RESUME_THRESHOLD_SEC) return
  if (duration > 0 && seconds >= duration - 1) return
  writePlaybackTime(videoKey, seconds)
}

export function clearPlaybackTime(videoKey: string): void {
  removeStorage(storageKey(videoKey))
  if(storageKey(videoKey)!==CCZJ.Keys.legacyPlaybackTime(videoKey))removeStorage(CCZJ.Keys.legacyPlaybackTime(videoKey))
}
