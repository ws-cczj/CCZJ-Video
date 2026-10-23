/** Resume a stopped HLS loader only when the browser has no future frame.
 * Restarting on every play discards useful buffering after a short pause. */
export function resumeStreaming(video: HTMLVideoElement): void {
  if (video.readyState >= 3) return
  const hls = (video as HTMLVideoElement & { __hls?: { startLoad: (position: number) => void } }).__hls
  try { hls?.startLoad(video.currentTime) } catch (error) {
    console.warn('[Player] Failed to resume streaming:', error)
  }
}
