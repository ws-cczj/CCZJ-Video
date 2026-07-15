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
