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
}
function bridge(): PlayerBridge | undefined {
  return (window as unknown as { wails?: PlayerBridge }).wails
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
