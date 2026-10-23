type HlsConstructor = typeof import('hls.js').default

/** Owns the primary playback HLS instance and its dynamic runtime import. */
export function useHlsEngine() {
  let runtime: HlsConstructor | null = null
  let active: any = null
  let owner: HTMLVideoElement | null = null
  let loading: Promise<HlsConstructor> | null = null

  async function loadRuntime(): Promise<HlsConstructor> {
    if (runtime) return runtime
    if (!loading) {
      loading = import('hls.js').then(module => {
        runtime = module.default
        return runtime
      }).catch(error => { loading = null; throw error })
    }
    return loading
  }

  function create(video: HTMLVideoElement, config: Record<string, unknown>, Hls: HlsConstructor): any {
    dispose()
    active = new Hls(config as any)
    owner = video
    ;(video as any).__hls = active
    return active
  }

  function dispose(video?: HTMLVideoElement): void {
    // A detached/old element must never destroy the replacement instance.
    if (video && video !== owner) return
    const instance = active
    const element = owner
    active = null
    owner = null
    if (instance) {
      try { instance.destroy() } catch { /* HLS cleanup must be idempotent. */ }
    }
    if (element) {
      try { delete (element as any).__hls } catch { /* ignore */ }
    }
  }

  function snapshot() {
    const config = active?.config
    return { active: !!active, bandwidth_estimate_bps: active?.bandwidthEstimate ?? null,
      config: config ? { forward_seconds: config.maxBufferLength, max_forward_seconds: config.maxMaxBufferLength, back_seconds: config.backBufferLength, max_bytes: config.maxBufferSize } : null,
    }
  }
  return { loadRuntime, create, dispose, snapshot }
}
