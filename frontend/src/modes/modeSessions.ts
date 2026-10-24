import { APPLICATION_MODE_KEYS, modeHome, routeMode, type ApplicationMode } from './definitions'

export type ViewScalar = string | number | boolean
export type ViewState = Record<string, ViewScalar>
interface SessionStorage {
  read(key: string): string | null
  write(key: string, value: string): boolean
  remove(key: string): void
}
interface Snapshot { at: number; route: string; scroll: Record<string, number>; views: Record<string, ViewState> }
const STATE_KEY = 'cczj.mode.sessions.v1', TICKET_KEY = 'cczj.mode.handoff.v1'
const MAX_BYTES = 65536, LIFETIME = 12 * 60 * 60 * 1000
const pages: Record<ApplicationMode, string[]> = {
  video: ['/', '/search', '/recent', '/favorites', '/history', '/recommendations', '/downloads'],
  novel: ['/novel', '/novel/categories', '/novel/search', '/novel/shelf', '/novel/settings'],
  comic: ['/comic', '/comic/categories', '/comic/search', '/comic/library', '/comic/settings'],
}
const record = (value: unknown): Record<string, unknown> | null => value && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : null
function route(mode: ApplicationMode, value: unknown): string | null {
  if (typeof value !== 'string' || value.length > 2048 || /[\\\x00-\x20#]/.test(value)) return null
  const previousPath = value.split('?')[0]!
  const path = previousPath === '/comic/me' ? '/comic/settings' : previousPath
  return pages[mode].includes(path) && routeMode(path) === mode ? path + value.slice(previousPath.length) : null
}
function scrollKey(mode: ApplicationMode, value: string): string | null {
  const ordinary = route(mode, value)
  if (ordinary) return ordinary
  const settings = /^(\/(?:novel|comic)\/settings):([a-zA-Z0-9_-]{1,64})$/.exec(value)
  return settings && route(mode, settings[1]) ? value : null
}
function scalars(value: unknown): ViewState {
  const source = record(value), result: ViewState = {}
  let bytes = 0
  for (const [key, item] of Object.entries(source || {}).slice(0, 24)) {
    if (!/^[a-zA-Z][a-zA-Z0-9_]{0,31}$/.test(key)) continue
    if (typeof item === 'boolean' || typeof item === 'number' && Number.isFinite(item) || typeof item === 'string' && item.length <= 512) {
      const size=JSON.stringify([key,item]).length*2
      if(bytes+size<=8192){result[key] = item;bytes+=size}
    }
  }
  return result
}
const viewKey = (key: string) => /^[a-z][a-z0-9_-]{0,31}$/.test(key)
function trim(state:Snapshot){
  while(JSON.stringify(state).length*2>16384){
    const scroll=Object.keys(state.scroll)[0],view=Object.keys(state.views)[0]
    if(scroll)delete state.scroll[scroll]
    else if(view)delete state.views[view]
    else break
  }
}

/** Dormant modes own plain, bounded checkpoints only. No Vue instances, content
 * arrays, media, closures, requests or timers enter the retained representation. */
export function createModeSessions(storage: SessionStorage, clock = Date.now) {
  const modes: Partial<Record<ApplicationMode, Snapshot>> = {}
  let activation = { mode: 'video' as ApplicationMode, warm: false }
  let storageAvailable: boolean|null = null
  let sequence = 0
  function fresh(value: unknown, lifetime = LIFETIME): value is number { return typeof value === 'number' && Number.isFinite(value) && value <= clock() && clock() - value <= lifetime }
  function get(mode: ApplicationMode): Snapshot {
    const old = modes[mode]
    if (old && fresh(old.at)) return old
    return modes[mode] = { at: clock(), route: modeHome(mode), scroll: {}, views: {} }
  }
  try {
    const raw = storage.read(STATE_KEY)
    if (raw && raw.length * 2 <= MAX_BYTES) {
      const value = record(JSON.parse(raw)), source = record(value?.modes)
      if (value?.version === 1) for (const mode of APPLICATION_MODE_KEYS) {
        const item = record(source?.[mode])
        if (!item || !fresh(item.at) || !route(mode, item.route)) continue
        const state: Snapshot = { at: item.at, route: route(mode, item.route)!, scroll: {}, views: {} }
        for (const [path, top] of Object.entries(record(item.scroll) || {}).slice(0, 12)) { const key=scrollKey(mode,path); if (key && typeof top === 'number' && Number.isFinite(top) && top >= 0 && top <= 1e7) state.scroll[key] = top }
        for (const [key, fields] of Object.entries(record(item.views) || {}).slice(0, 8)) if (viewKey(key)) state.views[key] = scalars(fields)
        trim(state);modes[mode] = state
      }
    }
  } catch { storageAvailable = false }
  function flush(): boolean {
    const raw = JSON.stringify({ version: 1, modes })
    try { storageAvailable = raw.length * 2 <= MAX_BYTES && storage.write(STATE_KEY, raw) } catch { storageAvailable = false }
    return storageAvailable
  }
  return {
    rememberRoute(mode: ApplicationMode, path: string) { const valid = route(mode, path); if (valid) { const state = get(mode); state.route = valid; state.at = clock();trim(state) } },
    rememberScroll(mode: ApplicationMode, path: string, top: number) {
      const key=scrollKey(mode,path)
      if (!key || !Number.isFinite(top) || top < 0) return
      const state = get(mode); state.at = clock(); delete state.scroll[key]; state.scroll[key] = Math.min(top, 1e7)
      for (const key of Object.keys(state.scroll).slice(0, -12)) delete state.scroll[key]
      trim(state)
    },
    scroll(mode: ApplicationMode, path: string) { const key=scrollKey(mode,path); return key ? get(mode).scroll[key] ?? 0 : 0 },
    saveView(mode: ApplicationMode, key: string, value: ViewState) {
      if (!viewKey(key)) return
      const state = get(mode); state.at = clock(); delete state.views[key]; state.views[key] = scalars(value)
      for (const name of Object.keys(state.views).slice(0, -8)) delete state.views[name]
      trim(state)
    },
    view(mode: ApplicationMode, key: string): ViewState { return { ...get(mode).views[key] } },
    resumeRoute(mode: ApplicationMode) { return get(mode).route },
    prepare(mode: ApplicationMode): string | undefined {
      if (!flush()) return undefined
      const token = `${clock().toString(36)}-${++sequence}-${Math.random().toString(36).slice(2, 12)}`
      try { if (storage.write(TICKET_KEY, JSON.stringify({ mode, token, at: clock() }))) return token } catch { /* cold startup remains available */ }
      storageAvailable = false
      return undefined
    },
    activate(mode: ApplicationMode, href: string) {
      let warm = false
      try {
        const value = record(JSON.parse(storage.read(TICKET_KEY) || 'null')), url = new URL(href)
        storage.remove(TICKET_KEY)
        warm = !!value && value.mode === mode && fresh(value.at, 120000) && typeof value.token === 'string' && value.token === url.searchParams.get('mode-resume') && url.searchParams.get('application-mode') === mode && route(mode, url.hash.slice(1)) !== null
      } catch { /* A bad or unavailable ephemeral checkpoint is a cold start. */ }
      activation = { mode, warm }
      return { ...activation }
    },
    diagnostics(mode: ApplicationMode=activation.mode) {
      const state = modes[mode]
      return { mode, strategy: 'state_checkpoint', activation: activation.mode === mode && activation.warm ? 'resume' : 'cold', runtime_retained: false, checkpoint_bytes: state ? JSON.stringify(state).length * 2 : 0, storage_available: storageAvailable }
    },
  }
}
