import { diagnosticMode } from './modeScope'
import { presentationProfile } from '../platform/presentation'

export const UI_ARCHITECTURE_REVISION = 'player-seams-20261019-4'
let sequence = 0
const activePlayers = new Map<string, {started_at_ms:number; media_loads:number}>()
let lastClosed: {id:string;closed_at_ms:number;lifetime_ms:number;media_loads:number} | null = null

/** Holds numeric descriptors only: diagnostics must not retain a media element,
 * component, request or resource disposer after playback has closed. */
export function createPlayerTrace() {
  const started = Date.now()
  const id = `${started.toString(36)}-${(++sequence).toString(36)}`
  const descriptor = {started_at_ms:started, media_loads:0}
  activePlayers.set(id,descriptor)
  let closed = false
  return {
    id,
    mediaChanged() { if (!closed) descriptor.media_loads++ },
    close() {
      if (closed) return
      closed = true
      activePlayers.delete(id)
      lastClosed = {id,closed_at_ms:Date.now(),lifetime_ms:Date.now()-started,media_loads:descriptor.media_loads}
    },
  }
}

export function architectureSnapshot() {
  return { revision:UI_ARCHITECTURE_REVISION, presentation:presentationProfile(),
    active_player_count:activePlayers.size,
    active_players:Array.from(activePlayers,([id,value])=>({id,...value})).slice(-4),
    last_closed_player:lastClosed }
}

export function recordingContext() {
  const ids = Array.from(activePlayers.keys())
  return {application_mode:diagnosticMode(),ui_architecture_revision:UI_ARCHITECTURE_REVISION,
    presentation:presentationProfile().ui, player_session_id:ids[ids.length-1] ?? null}
}
