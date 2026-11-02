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
export function prepareDeviceFullscreen(orientation: FullscreenOrientation, failure: (error: ApiFailure) => void, transition?: (state:'exiting'|'exited')=>void) {
  const host = bridge()
  if (!host?.playerPrepareFullscreen || !host.playerFinishFullscreen || !host.playerFullscreenPolicy) return undefined
  const id = `${Date.now()}-${++fullscreenSequence}`
  let phase:'entering'|'fullscreen'|'exiting'|'closed'='entering'
  let readySettled=false,entered=false
  let close!:()=>void
  const exited=new Promise<void>(resolve=>{close=resolve})
  let resolve!:()=>void,reject!:(error:ApiFailure)=>void
  const ready=new Promise<void>((yes,no)=>{resolve=yes;reject=no})
  void ready.catch(()=>{})
  let timeout:ReturnType<typeof setTimeout>|undefined
  function rejectReady(code:string):void {
    if(readySettled)return
    readySettled=true
    reject(new ApiFailure({code,message:'Native fullscreen operation failed'},undefined))
  }
  function stop():void {
    phase='closed';clearTimeout(timeout)
    window.removeEventListener('cczj:player-fullscreen',changed)
    close()
  }
  function fail(code:string):void {
    rejectReady(code)
    const report=entered
    stop()
    host!.playerFinishFullscreen!(id)
    if(report)failure(new ApiFailure({code,message:'Native fullscreen operation failed'},undefined))
  }
  function beginExit():void {
    if(phase==='closed'||phase==='exiting')return
    phase='exiting';rejectReady('CANCELLED');clearTimeout(timeout)
    timeout=setTimeout(()=>fail('TIMEOUT'),4000)
    transition?.('exiting')
  }
  function changed(event:Event):void {
    const row:unknown=(event as CustomEvent).detail
    if(!row||typeof row!=='object'||Array.isArray(row))return
    const state=row as Record<string,unknown>
    if(state.id!==id||phase==='closed')return
    if(state.state==='entered'){
      if(phase!=='entering')return
      phase='fullscreen';entered=true;clearTimeout(timeout)
      readySettled=true;resolve()
    }else if(state.state==='exiting')beginExit()
    else if(state.state==='exited'){
      rejectReady('CANCELLED')
      transition?.('exited');stop()
    }else if(state.state==='error')fail(typeof state.code==='string'?state.code:'UNAVAILABLE')
  }
  timeout=setTimeout(()=>fail('TIMEOUT'),7000)
  window.addEventListener('cczj:player-fullscreen',changed)
  try {
    const error=host.playerPrepareFullscreen(id,orientation)
    if(error)throw new ApiFailure({code:error,message:'Native fullscreen preparation failed'},undefined)
  }catch(error){
    rejectReady('UNAVAILABLE');stop();host.playerFinishFullscreen(id)
    throw error
  }
  return {
    ready,
    lock:(orientation:FullscreenOrientation,locked:boolean)=>{if(phase==='fullscreen')host.playerFullscreenPolicy!(id,orientation,locked)},
    release:()=>{
      if(phase!=='closed'&&phase!=='exiting'){
        beginExit()
        host.playerFinishFullscreen!(id)
      }
      return exited
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
  // AppOps probes vary across OEMs. Let the native entry report the actual
  // rejection instead of treating a default/unknown probe as a denied request.
  bridge()?.playerEnterPip(width, height)
  return true
}
export function setCastMulticast(enabled: boolean): void { bridge()?.playerMulticast?.(enabled) }
export function openSystemCastSettings(): boolean {
  const host=bridge()
  return !!host?.playerOpenCastSettings && host.playerOpenCastSettings()===''
}
