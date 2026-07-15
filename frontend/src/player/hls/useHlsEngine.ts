type HlsConstructor = typeof import('hls.js').default

/** Owns the primary playback HLS instance and its dynamic runtime import. */
export function useHlsEngine() {
  let runtime: HlsConstructor | null = null
  let active: any = null

  async function loadRuntime(): Promise<HlsConstructor> {
    if (runtime) return runtime
    const module = await import('hls.js')
    runtime = module.default
    return runtime
  }

  function create(video: HTMLVideoElement, config: Record<string, unknown>, Hls: HlsConstructor): any {
    dispose(video)
    active = new Hls(config as any)
    ;(video as any).__hls = active
    return active
  }

  function dispose(video?: HTMLVideoElement): void {
    const instance = video ? (video as any).__hls : active
    if (instance) {
      try { instance.destroy() } catch { /* HLS cleanup must be idempotent. */ }
    }
    if (video) {
      try { delete (video as any).__hls } catch { /* ignore */ }
    }
    if (!video || instance === active) active = null
  }

  return { loadRuntime, create, dispose }
}
