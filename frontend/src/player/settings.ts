import { CCZJ } from '../constants/cczj'
import { readStorage, readStorageString, removeStorage, writeStorage } from '../platform/storage'

const STORAGE_KEY = CCZJ.Storage.playerSettings
const STORAGE_VERSION = '1'
const LEGACY_KEYS = [
  CCZJ.Legacy.playerQualityMode,
  CCZJ.Legacy.playerAnime4KTier,
  CCZJ.Legacy.playerAiWarningAccepted,
  CCZJ.Legacy.playerAutoResumeJump,
  CCZJ.Legacy.playerVolume,
  CCZJ.Legacy.playerMuted,
  CCZJ.Legacy.playerSpeed,
  CCZJ.Legacy.playerAutoNext,
]

/**
 * Provides versioned player-preference storage and performs the one-time
 * migration from the historical scattered vp_* keys.
 */
export function createPlayerSettings() {
  let cache: Record<string, string> | null = null

  function load(): Record<string, string> {
    if (cache) return cache
    const parsed = readStorage<unknown>(STORAGE_KEY, null)
    if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
      cache = Object.fromEntries(Object.entries(parsed).filter((entry):entry is [string,string]=>typeof entry[1]==='string'))
      return cache
    }

    const migrated: Record<string, string> = { _version: STORAGE_VERSION }
    for (const legacyKey of LEGACY_KEYS) {
      const value = readStorageString(legacyKey)
      if (value !== '') {
        migrated[legacyKey.replace(/^vp_/, '')] = value
      }
    }
    cache = migrated
    if (Object.keys(migrated).length > 1 && save())for(const key of LEGACY_KEYS)removeStorage(key)
    return cache
  }

  function save(): boolean {
    if (!cache) return false
    cache._version = STORAGE_VERSION
    return writeStorage(STORAGE_KEY, cache)
  }

  function read(key: string, fallback: string): string {
    return load()[key] ?? fallback
  }

  function write(key: string, value: string): boolean {
    const settings = load()
    if (settings[key] === value) return true
    const previous=settings[key]
    settings[key] = value
    if(save())return true
    if(previous===undefined)delete settings[key];else settings[key]=previous
    return false
  }

  return { read, write }
}
