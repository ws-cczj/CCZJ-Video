import { ApiFailure } from './errors'

/** Android player capabilities stay behind the runtime adapter boundary. */
interface PlayerBridge {
  playerState(): string
  playerVolume(value: number): void
  playerBrightness(value: number): void
  playerRestoreBrightness(): void
  playerEnterPip(width: number, height: number): void
  playerMulticast(enabled: boolean): void
  playerOpenCastSettings(): string
  playerPrepareFullscreen?(id: string, orientation: string): string
  playerFinishFullscreen?(id: string): void
  playerFullscreenPolicy?(id: string, orientation: string, locked: boolean): void
}
function bridge(): PlayerBridge | undefined {
  return (window as unknown as { wails?: PlayerBridge }).wails
}

export type FullscreenOrientation = 'portrait' | 'landscape' | 'preserve'
let fullscreenSequence = 0

/** The request identity also owns native rollback. A late outgoing session
 * cannot reset the orientation or bars of a newer fullscreen session. */
export function prepareDeviceFullscreen(orientation: FullscreenOrientation, failure: (error: ApiFailure) => void) {
  const host = bridge()
  if (!host?.playerPrepareFullscreen || !host.playerFinishFullscreen || !host.playerFullscreenPolicy) return undefined
  const id = `${Date.now()}-${++fullscreenSequence}`
  let settled = false, closed = false
  let resolve!: () => void, reject!: (error: ApiFailure) => void
  const ready = new Promise<void>((yes, no) => { resolve = yes; reject = no })
  // A browser rejection can precede awaiting the native acknowledgement.
  void ready.catch(() => {})
  function stop(): void {
    closed = true
    clearTimeout(timeout)
    window.removeEventListener('cczj:player-fullscreen', changed)
  }
  function fail(code: string): void {
    const error = new ApiFailure({code, message:'Native fullscreen operation failed'}, undefined)
    const wasReady = settled
    stop()
    settled = true
    if (wasReady) failure(error)
    else reject(error)
  }
  function changed(event: Event): void {
    const row: unknown = (event as CustomEvent).detail
    if (!row || typeof row !== 'object' || Array.isArray(row)) return
    const state = row as Record<string, unknown>
    if (state.id !== id || closed) return
    if (state.state === 'entered') {
      clearTimeout(timeout)
      settled = true
      resolve()
    } else if (state.state === 'exited') {
      if (!settled) fail('CANCELLED')
      else stop()
    } else if (state.state === 'error') fail(typeof state.code === 'string' ? state.code : 'UNAVAILABLE')
  }
  const timeout = setTimeout(() => {
    fail('TIMEOUT')
    host!.playerFinishFullscreen!(id)
  }, 7000)
  window.addEventListener('cczj:player-fullscreen', changed)
  try {
    const error = host.playerPrepareFullscreen(id, orientation)
    if (error) throw new ApiFailure({code:error,message:'Native fullscreen preparation failed'},undefined)
  } catch (error) {
    stop()
    reject(error instanceof ApiFailure ? error : new ApiFailure({code:'UNAVAILABLE',message:'Native fullscreen preparation failed'},undefined))
    throw error
  }
  return {
    ready,
    lock: (orientation: FullscreenOrientation, locked: boolean) => { if (!closed) host.playerFullscreenPolicy!(id, orientation, locked) },
    release: () => {
      if (!settled) fail('CANCELLED')
      else stop()
      host.playerFinishFullscreen!(id)
    },
  }
}
export function readPlayerDevice(): { volume: number; brightness: number; pip: boolean; pipAllowed?: boolean; castSettings?:boolean } | undefined {
  try {
    const b = bridge()
    const value: unknown = b?.playerState ? JSON.parse(b.playerState()) : undefined
    if (!value || typeof value !== 'object' || Array.isArray(value)) return undefined
    const state = value as Record<string, unknown>
    if (typeof state.volume !== 'number' || !Number.isFinite(state.volume) || typeof state.brightness !== 'number' || !Number.isFinite(state.brightness) || typeof state.pip !== 'boolean') return undefined
    return {volume:Math.max(0,Math.min(1,state.volume)),brightness:Math.max(0,Math.min(1,state.brightness)),pip:state.pip,...(typeof state.pipAllowed === 'boolean' ? {pipAllowed:state.pipAllowed} : {}),...(typeof state.castSettings==='boolean'?{castSettings:state.castSettings}:{})}
  } catch { return undefined }
}
export function setDeviceVolume(value: number): void { bridge()?.playerVolume?.(value) }
let brightnessOwner: symbol | undefined
export function setDeviceBrightness(value: number, owner?: symbol): boolean {
  const host = bridge()
  if (!host?.playerBrightness || !Number.isFinite(value)) return false
  host.playerBrightness(Math.max(.02, Math.min(1, value)))
  brightnessOwner = owner
  return true
}
/** An outgoing player/reader must not restore over an incoming session. */
export function restoreDeviceBrightness(owner?: symbol): void {
  if (owner && brightnessOwner !== owner) return
  brightnessOwner = undefined
  bridge()?.playerRestoreBrightness?.()
}
export function enterDevicePip(width: number, height: number): boolean {
  const state = readPlayerDevice()
  if (!state?.pip) return false
  if (state.pipAllowed === false) throw new ApiFailure({code:'PERMISSION_DENIED',message:'Picture-in-picture permission is disabled'},undefined)
  bridge()?.playerEnterPip(width, height)
  return true
}
export function setCastMulticast(enabled: boolean): void { bridge()?.playerMulticast?.(enabled) }
export function openSystemCastSettings(): boolean {
  const host=bridge()
  return !!host?.playerOpenCastSettings && host.playerOpenCastSettings()===''
}
