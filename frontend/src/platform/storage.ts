import { tr } from '../locales'

const STORAGE_VERSION = 1

interface VersionedValue<T> {
  version: number
  value: T
}

function readRaw<T>(key: string): T | undefined {
  try {
    const raw = localStorage.getItem(key)
    if (!raw) return undefined
    const parsed = JSON.parse(raw) as VersionedValue<T> | T
    if (typeof parsed === 'object' && parsed !== null && 'version' in parsed && 'value' in parsed) {
      return parsed.version === STORAGE_VERSION ? parsed.value : undefined
    }
    // One-time compatibility read for pre-contract values.
    return parsed as T
  } catch {
    return undefined
  }
}

export function readStorage<T>(key: string, fallback: T): T {
  return readRaw<T>(key) ?? fallback
}

export function writeStorage<T>(key: string, value: T): void {
  try {
    localStorage.setItem(key, JSON.stringify({ version: STORAGE_VERSION, value }))
  } catch {
    // Browser storage is an optional UI cache; callers retain in-memory state.
  }
}

export function removeStorage(key: string): void {
  try {
    localStorage.removeItem(key)
  } catch {
    // Ignore unavailable storage (private mode or quota errors).
  }
}

export function readStorageString(key: string, fallback = ''): string {
  try {
    return localStorage.getItem(key) ?? fallback
  } catch {
    return fallback
  }
}

export function readStorageBoolean(key: string, fallback = false): boolean {
  try {
    const raw = localStorage.getItem(key)
    if (raw === null) return fallback
    const parsed = JSON.parse(raw) as VersionedValue<boolean> | boolean | string
    if (typeof parsed === 'object' && parsed !== null && 'version' in parsed && 'value' in parsed) {
      return parsed.version === STORAGE_VERSION ? Boolean(parsed.value) : fallback
    }
    if (typeof parsed === 'boolean') return parsed
    return parsed === 'true' || parsed === '1'
  } catch {
    return fallback
  }
}

export function localStorageBytes(): number {
  try {
    let bytes = 0
    for (let i = 0; i < localStorage.length; i++) {
      const key = localStorage.key(i)
      if (key) bytes += (key.length + (localStorage.getItem(key)?.length ?? 0)) * 2
    }
    return bytes
  } catch {
    return 0
  }
}

const RESET_MARKER = 'cczj_database_reset_generation'

// Clears browser-owned caches exactly once per destructive database reset.
// This deliberately uses native storage only inside this adapter.
export async function applyDatabaseResetGeneration(generation: string): Promise<void> {
  if (!generation || readStorageString(RESET_MARKER) === generation) return
  try {
    const remove: string[] = []
    for (let i = 0; i < localStorage.length; i++) {
      const key = localStorage.key(i) || ''
      if (key === 'poster_cache_v1' || key === 'cczj_detail_cache_v1' || key === 'cczj_image_proxy_cache_v1' || key === 'cczj_douban_chart_cache_v1' || key === 'cczj_ep_prog_v1' || key === 'cczj_fav_folders' || key === 'cczj_fav_mapping' || key === 'cczj_update_download_state' || key.startsWith('cczj_video_refresh_')) remove.push(key)
    }
    remove.forEach(key => localStorage.removeItem(key))
    await Promise.all(['tscache'].map(name => new Promise<void>((resolve, reject) => {
      const req = indexedDB.deleteDatabase(name)
      req.onsuccess = () => resolve()
      req.onerror = () => reject(req.error || new Error(tr('errors.idbDeleteFailed', { name })))
      req.onblocked = () => reject(new Error(tr('errors.idbBlocked', { name })))
    })))
    localStorage.setItem(RESET_MARKER, generation)
  } catch {
    // Storage can be unavailable; the next load will retry the cleanup.
  }
}
