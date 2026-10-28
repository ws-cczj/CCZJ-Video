import { architectureSnapshot } from './architecture'
import { readAndroidMemoryState } from '../api/runtime'
const playerSnapshots = new Map<symbol,{id:string;snapshot:()=>unknown}>()

/** Registers the current player without retaining a closed component. */
export function registerPlayerResourceSnapshot(snapshot: () => unknown, id = 'legacy'): () => void {
  const key = Symbol(id)
  playerSnapshots.set(key,{id,snapshot})
  return () => { playerSnapshots.delete(key) }
}

export function captureBrowserResources() {
  const memory = (performance as Performance & { memory?: { usedJSHeapSize: number; totalJSHeapSize: number; jsHeapSizeLimit: number } }).memory
  let player: unknown = null
  const players = Array.from(playerSnapshots.values()).slice(-4).map(entry => {
    let snapshot: unknown = null
    try { snapshot = entry.snapshot() } catch { /* Observation must not interrupt media. */ }
    return {session_id:entry.id,snapshot}
  })
  player = players[players.length-1]?.snapshot ?? null
  if (typeof document === 'undefined') return { player, js_heap: null, attached_dom: null }
  const videos = Array.from(document.querySelectorAll('video'))
  const canvases = Array.from(document.querySelectorAll('canvas'))
  const images = Array.from(document.querySelectorAll('img'))
  return {
    android_memory: readAndroidMemoryState(),
    js_heap: memory ? { source: 'performance.memory', used_bytes: memory.usedJSHeapSize, total_bytes: memory.totalJSHeapSize, limit_bytes: memory.jsHeapSizeLimit } : null,
    player, players, registered_player_count:playerSnapshots.size, architecture:architectureSnapshot(),
    attached_dom: {
      video_count: videos.length, canvas_count: canvases.length, image_count: images.length,
      videos: videos.slice(0, 8).map(mediaResourceSnapshot),
      // Size estimates only: these do not measure GPU allocations or retained
      // off-document KeepAlive trees. Shared images can be counted more than once.
      canvas_rgba_estimate_bytes: canvases.slice(0, 32).reduce((total, canvas) => total + canvas.width * canvas.height * 4, 0),
      image_rgba_estimate_bytes: images.slice(0, 256).reduce((total, img) => total + img.naturalWidth * img.naturalHeight * 4, 0),
    },
  }
}

export function mediaResourceSnapshot(video: HTMLVideoElement) {
  const buffered: number[][] = []
  try { for (let index = 0; index < video.buffered.length && index < 8; index++) buffered.push([video.buffered.start(index), video.buffered.end(index)]) }
  catch { /* Detached media may be releasing its MediaSource. */ }
  const at = video.currentTime
  const containing = buffered.find(([start, end]) => start <= at && at <= end)
  return { current_time: at, ready_state: video.readyState, network_state: video.networkState,
    paused: video.paused, ended: video.ended, width: video.videoWidth, height: video.videoHeight,
    has_source: !!video.getAttribute('src'), buffered_ranges: buffered,
    buffer_ahead_seconds: containing ? Math.max(0, containing[1] - at) : 0,
    buffer_behind_seconds: containing ? Math.max(0, at - containing[0]) : 0,
  }
}
