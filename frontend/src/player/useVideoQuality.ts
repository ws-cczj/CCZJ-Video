import { computed, ref, watch, type Ref } from 'vue'
import { FilmUpscaler, FILM_PRESET, checkFilmSupport } from '../utils/filmUpscaler'
import { Anime4kUpscaler, ANIME4K_PRESET, checkAnime4kSupport } from '../utils/anime4kUpscaler'
import type { Anime4kTier } from '../utils/anime4kUpscaler'
import { CustomShaderUpscaler } from '../utils/customShaderUpscaler'
import { usePluginStore } from '../stores/plugins'
import { tr } from '../locales'

/** 画质模式：原高清 / 动画增强 / 影视增强 / 扩展包着色器 */
export type QualityMode = 'original' | 'ai_anime' | 'ai_film' | 'ai_custom'

export interface VideoQualityDeps {
  getVideoEl: () => HTMLVideoElement | null
  /** 管线要把 WebGL canvas 覆盖在这个容器上，对比分界线也按它的宽度换算 */
  wrapperRef: Ref<HTMLDivElement | undefined>
  /** 与组件共用同一份 vp_settings，档位偏好才不会被两处互相覆盖 */
  settings: {
    read: (key: string, fallback: string) => string
    write: (key: string, value: string) => void
  }
  /** 切档位属于用户操作，控制条得跟着重新计时 */
  keepVisible: () => void
}

/**
 * 画质增强管线（WebGL2 实时增强：锐化/对比度/边缘/去色带）的整套生命周期。
 *
 * 管线的实例本身是普通变量而不是 ref：它只会在 loadedmetadata 这类时机被显式重建，
 * 做成响应式会让「对比」按钮的出现时机跟着变化，而模板要读的只是「此刻有没有管线」。
 * 用户想要的档位（_desiredAiMode）与「本机此刻能不能跑」是两件事：换集只拆实例，
 * 偏好留着等下一条媒体的元数据自动重试，只有永久不支持或用户主动切回才落盘清掉。
 */
export function useVideoQuality(deps: VideoQualityDeps) {
  const getVideoEl = deps.getVideoEl
  const wrapperRef = deps.wrapperRef
  const keepVisible = deps.keepVisible
  const { read: readSetting, write: writeSetting } = deps.settings

  // 画质下拉框是否展开 —— 展开期间锁定控制条可见，避免全屏下 2.5s 自动隐藏导致面板错位
  const qualityOpen = ref(false)

  // ========= 画质模式 =========
  // 模式：原高清 / 动画增强 M·L / 影视增强（M/L 直接在画质下拉框中选择）
  // 扩展包着色器再追加 ai_custom_<pack>/<shader> 档位。
  // 兼容旧版 localStorage 中存的 'ai_frame_interp' 和 'ai_enhance' 值。
  const qualityMode = ref<QualityMode>(normalizeQualityMode(readSetting('quality_mode', 'original')))

  const pluginStore = usePluginStore()
  void pluginStore.ensureLoaded()

  const anime4kTier = ref<Anime4kTier>(
    (readSetting('anime4k_tier', 'M') as Anime4kTier) || 'M'
  )

  /** 当前扩展包着色器档位，形如 `<packId>/<shaderId>`。 */
  const customShaderKey = ref<string>(readSetting('custom_shader_key', ''))

  /** 根据视频分辨率推荐最佳档位 */
  const recommendedTier = computed<Anime4kTier>(() => {
    const h = (getVideoEl()?.videoHeight) || 0
    if (h <= 0) return 'M'        // 元数据未就绪，默认 M
    if (h >= 1080) return 'S'
    if (h >= 720) return 'M'
    return 'L'
  })

  // 画质下拉框（原高清 + 动画增强三档(含推荐) + 影视增强 + 扩展包着色器）
  const qualityOptions = computed(() => {
    const rec = recommendedTier.value
    const tiers: Anime4kTier[] = ['S', 'M', 'L']
    const animeOptions = tiers.map(t => ({
      value: `ai_anime_${t}`,
      label: `${tr('player.animeEnhance')} ${t}${t === rec ? tr('player.recommendSuffix') : ''}`,
    }))
    const customOptions = pluginStore.shaderOptions.map(o => ({
      value: `ai_custom_${o.key}`,
      label: pluginStore.shaderLabel(o),
    }))
    return [
      { value: 'original', label: tr('player.originalQuality') },
      ...animeOptions,
      { value: 'ai_film', label: tr('player.filmEnhance') },
      ...customOptions,
    ]
  })
  // 当前下拉框选中值（根据 qualityMode + anime4kTier 计算）
  const qualityDropdownValue = computed(() => {
    if (qualityMode.value === 'ai_anime') return `ai_anime_${anime4kTier.value}`
    if (qualityMode.value === 'ai_custom') return `ai_custom_${customShaderKey.value}`
    return qualityMode.value
  })
  function normalizeQualityMode(v: string): QualityMode {
    if (v === 'ai_frame_interp' || v === 'ai_enhance') return 'ai_anime' // 旧版统一迁移
    if (v === 'ai_anime' || v === 'ai_film' || v === 'ai_custom') return v
    return 'original'
  }
  function isAiMode(mode: string): mode is 'ai_anime' | 'ai_film' | 'ai_custom' {
    return mode === 'ai_anime' || mode === 'ai_film' || mode === 'ai_custom'
  }
  const showAiWarning = ref(false)
  const aiWarningAccepted = ref(readSetting('ai_warning_accepted', '0') === '1')

  // 切换画质时的短暂提示（左下角）
  const qualityToastText = ref('')
  let qualityToastTimer: ReturnType<typeof setTimeout> | null = null
  function showQualityToast(text: string): void {
    qualityToastText.value = text
    if (qualityToastTimer) clearTimeout(qualityToastTimer)
    qualityToastTimer = setTimeout(() => { qualityToastText.value = '' }, 1500)
  }

  let upscaler: Anime4kUpscaler | FilmUpscaler | CustomShaderUpscaler | null = null
  let upscalerStatsTimer: ReturnType<typeof setInterval> | null = null
  const upscalerSupported = ref(false)
  const upscalerStats = ref<{ fps: number; gpuEnabled: boolean }>({ fps: 0, gpuEnabled: false })
  const compareEnabled = ref(false)
  const compareSplit = ref(50)
  let _aiReady = false // 视频是否已就绪（loadedmetadata 之后），AI 才会启动
  // 用户希望启用的增强模式，跨换集/换源保持（换源时 destroyPlayerInternal 只销毁管线，不动这个）。
  // 初始化类瞬时失败（例如 WebGL 上下文名额被占满）不清它，下一条媒体的 loadedmetadata 会自动重试；
  // 只有能力检查判定本机永久不支持、或用户主动切回原高清才清掉。
  let _desiredAiMode: 'ai_anime' | 'ai_film' | 'ai_custom' | null =
    (isAiMode(qualityMode.value) && aiWarningAccepted.value) ? qualityMode.value : null

  function toggleEnhancementCompare(): void {
    if (!upscaler) return
    compareEnabled.value = !compareEnabled.value
    upscaler.setCompareSplit(compareEnabled.value ? compareSplit.value : null)
    keepVisible()
  }
  function updateEnhancementCompare(e: MouseEvent): void {
    if (!compareEnabled.value || !wrapperRef.value || !upscaler) return
    const rect = wrapperRef.value.getBoundingClientRect()
    compareSplit.value = Math.max(0, Math.min(100, ((e.clientX - rect.left) / rect.width) * 100))
    upscaler.setCompareSplit(compareSplit.value)
  }

  let _pendingQualityMode: QualityMode = 'original'
  function onQualityChange(value: string | number): void {
    const raw = String(value)
    // 解析合并选项值：ai_anime_M / ai_anime_L / ai_film / ai_custom_<pack>/<id> / original
    let mode: QualityMode
    if (raw.startsWith('ai_anime_')) {
      mode = 'ai_anime'
      const tier = raw.slice('ai_anime_'.length) as Anime4kTier
      anime4kTier.value = tier
      writeSetting('anime4k_tier', tier)
    } else if (raw.startsWith('ai_custom_')) {
      mode = 'ai_custom'
      customShaderKey.value = raw.slice('ai_custom_'.length)
      writeSetting('custom_shader_key', customShaderKey.value)
    } else {
      mode = raw as QualityMode
    }
    if (isAiMode(mode) && !aiWarningAccepted.value) {
      _pendingQualityMode = mode
      showAiWarning.value = true
      return
    }
    applyQualityMode(mode)
  }

  /** 当前档位的人话名字：切换提示、对比按钮和 OSD 都用它。 */
  function qualityLabel(mode: QualityMode): string {
    if (mode === 'ai_anime') return `${tr('player.animeEnhance')} ${anime4kTier.value}`
    if (mode === 'ai_film') return tr('player.filmEnhance')
    if (mode === 'ai_custom') {
      const opt = pluginStore.shaderOptions.find(o => o.key === customShaderKey.value)
      return opt ? pluginStore.shaderLabel(opt) : tr('player.originalQuality')
    }
    return tr('player.originalQuality')
  }

  function applyQualityMode(mode: QualityMode): void {
    qualityMode.value = mode
    writeSetting('quality_mode', mode)
    if (isAiMode(mode)) {
      _desiredAiMode = mode
      if (_aiReady) {
        startAiPipeline(mode)
      }
      showQualityToast(tr('player.qualitySwitchedGpu', { label: qualityLabel(mode) }))
    } else {
      _desiredAiMode = null
      stopAiPipeline()
      showQualityToast(tr('player.qualitySwitchedOriginal'))
    }
  }

  function confirmAiMode(): void {
    aiWarningAccepted.value = true
    writeSetting('ai_warning_accepted', '1')
    showAiWarning.value = false
    applyQualityMode(_pendingQualityMode)
  }

  function cancelAiMode(): void {
    showAiWarning.value = false
    _desiredAiMode = null
    qualityMode.value = 'original'
    writeSetting('quality_mode', 'original')
  }

  // AI 增强管线：动画模式用 Anime4K CNN 超分，影视模式用 FSRCNNX + CAS

  /**
   * 回退到原高清。
   *
   * permanent 表示本机能力不支持（没有 WebGL2、GPU 不支持浮点渲染目标）—— 这种条件重启也不会变，
   * 落盘并丢掉重试意图，免得每次换集重复试探。初始化过程中的瞬时失败（例如 Chromium 每页约 16 个
   * WebGL 上下文的名额被临时占满）只回退本次画面：偏好留在盘上、_desiredAiMode 也留着，
   * 下一条媒体的 loadedmetadata 会再试一次。
   */
  function fallBackToOriginal(permanent: boolean): void {
    qualityMode.value = 'original'
    if (!permanent) return
    _desiredAiMode = null
    writeSetting('quality_mode', 'original')
  }

  async function startAiPipeline(mode: 'ai_anime' | 'ai_film' | 'ai_custom'): Promise<void> {
    // 先清除旧的统计定时器（避免切换模式时泄漏）
    if (upscalerStatsTimer) {
      clearInterval(upscalerStatsTimer)
      upscalerStatsTimer = null
    }
    // 先销毁旧实例（切换模式时）
    if (upscaler) {
      upscaler.stop()
      upscaler.destroy()
      upscaler = null
    }
    compareEnabled.value = false

    const v = getVideoEl()
    if (!v) return

    // 动画模式：Anime4K CNN 超分
    if (mode === 'ai_anime') {
      const a4kSupport = checkAnime4kSupport()
      if (!a4kSupport.recommended) {
        console.warn('[Player] Anime4K 不可用:', a4kSupport.message)
        fallBackToOriginal(true)
        return
      }
      upscaler = new Anime4kUpscaler({ ...ANIME4K_PRESET, tier: anime4kTier.value })
      const ok = await upscaler.init(v, wrapperRef.value ?? undefined)
      if (ok) {
        // 上次瞬时失败时界面显示的是原高清，这次重建成功要把画质标签恢复成用户实际享有的模式。
        qualityMode.value = mode
        upscaler.start()
        console.log(`[Player] Anime4K CNN 2x 超分管线已启动 (${anime4kTier.value} 档, WebGL2)`)
        upscalerStatsTimer = setInterval(() => {
          if (!upscaler) { if (upscalerStatsTimer) { clearInterval(upscalerStatsTimer); upscalerStatsTimer = null }; return }
          const s = upscaler.getStats()
          upscalerStats.value = { fps: s.fps, gpuEnabled: s.gpuEnabled }
        }, 2000)
        return
      }
      // init() 失败时已自行销毁并归还 WebGL 上下文，这里只需丢掉引用。
      console.warn('[Player] Anime4K 初始化失败:', upscaler.error)
      upscaler = null
      fallBackToOriginal(false)
      return
    }

    // 扩展包模式：用户声明式着色器
    if (mode === 'ai_custom') {
      await startCustomPipeline(v)
      return
    }

    // 影视模式：FSRCNNX + CAS
    const filmSupport = checkFilmSupport()
    upscalerSupported.value = filmSupport.supported

    if (!filmSupport.supported) {
      console.warn('[Player] 影视增强不可用:', filmSupport.message)
      fallBackToOriginal(true)
      return
    }

    upscaler = new FilmUpscaler({ ...FILM_PRESET })

    const ok = await upscaler.init(v, wrapperRef.value ?? undefined)
    if (!ok) {
      // 同上：init() 的 catch 分支已经走完 destroy()，上下文不会泄漏。
      console.error('[Player] FSRCNNX 影视增强初始化失败:', upscaler.error)
      upscaler = null
      fallBackToOriginal(false)
      return
    }

    qualityMode.value = mode
    upscaler.start()
    console.log('[Player] FSRCNNX + CAS 影视增强管线已启动 (WebGL2 多 Pass GPU 加速)')

    // 定期更新性能统计
    upscalerStatsTimer = setInterval(() => {
      if (!upscaler) {
        if (upscalerStatsTimer) clearInterval(upscalerStatsTimer)
        upscalerStatsTimer = null
        return
      }
      const s = upscaler.getStats()
      upscalerStats.value = { fps: s.fps, gpuEnabled: s.gpuEnabled }
    }, 2000)
  }

  /**
   * 扩展包着色器管线。
   *
   * 隔离粒度是「这一个档位」：着色器编译不过属于包本身的问题，换一条媒体也不会变好，
   * 所以记进本机隔离表并从下拉框摘掉，要重试得由用户在扩展面板明确触发。反过来，
   * WebGL2 名额被临时占满这类本机条件只回退本次画面，留给下一条媒体重试。
   */
  async function startCustomPipeline(v: HTMLVideoElement): Promise<void> {
    const key = customShaderKey.value
    const opt = pluginStore.shaderOptions.find(o => o.key === key)
    if (!opt) {
      // 包被删除、停用或校验失败：偏好已经没有可执行的东西，落回原高清并丢掉重试意图。
      fallBackToOriginal(true)
      return
    }
    const source = await pluginStore.shaderSource(opt)
    if (!source) {
      const message = tr('player.customShaderReadFailed')
      console.error('[Player] 扩展包着色器读取失败:', opt.key, message)
      pluginStore.markShaderBroken(key, message)
      fallBackToOriginal(true)
      return
    }
    upscaler = new CustomShaderUpscaler({ id: opt.shader.id, source, scale: opt.shader.scale === 1 ? 1 : 2 })
    const ok = await upscaler.init(v, wrapperRef.value ?? undefined)
    if (!ok) {
      const message = upscaler.error ?? 'init failed'
      const missingCapability = upscaler.capabilityMissing
      console.error('[Player] 扩展包着色器初始化失败:', opt.key, message)
      if (!missingCapability) pluginStore.markShaderBroken(key, message)
      upscaler = null
      fallBackToOriginal(!missingCapability)
      return
    }
    qualityMode.value = 'ai_custom'
    upscaler.start()
    console.log(`[Player] 扩展包着色器管线已启动 (${opt.key}, WebGL2 ${opt.shader.scale === 1 ? '1' : '2'}x)`)
    upscalerStatsTimer = setInterval(() => {
      if (!upscaler) {
        if (upscalerStatsTimer) clearInterval(upscalerStatsTimer)
        upscalerStatsTimer = null
        return
      }
      const s = upscaler.getStats()
      upscalerStats.value = { fps: s.fps, gpuEnabled: s.gpuEnabled }
    }, 2000)
  }

  // 扩展包被停用、删除或本机隔离后，正在使用的自定义档位不再可执行：落回原高清。
  watch(
    () => `${pluginStore.loaded}|${pluginStore.shaderOptions.map(o => o.key).join(',')}`,
    () => {
      if (!pluginStore.loaded || qualityMode.value !== 'ai_custom') return
      if (pluginStore.shaderOptions.some(o => o.key === customShaderKey.value)) return
      _desiredAiMode = null
      stopAiPipeline()
      qualityMode.value = 'original'
      writeSetting('quality_mode', 'original')
    },
  )

  function stopAiPipeline(): void {
    if (upscalerStatsTimer) {
      clearInterval(upscalerStatsTimer)
      upscalerStatsTimer = null
    }
    if (upscaler) {
      upscaler.stop()
      upscaler.destroy()
      upscaler = null
    }
    compareEnabled.value = false
    upscalerStats.value = { fps: 0, gpuEnabled: false }
    console.log('[Player] AI 增强管线已停止')
  }

  /**
   * 元数据就绪后重建管线。
   *
   * 换集/换源后自动重建 AI 增强：destroyPlayerInternal 会 stopAiPipeline() 把 WebGL 上下文
   * 还掉，而元数据就绪是唯一安全的挂点（要有 videoWidth/Height 才能建管线）。冷启动时
   * quality_mode 只是从设置里读回来的偏好，以前没人据此启动管线，现在也走这里。
   */
  function onMediaReady(): void {
    _aiReady = true
    if (_desiredAiMode && !upscaler) startAiPipeline(_desiredAiMode)
  }

  /** seek 之后让管线重取当前帧；没有管线时这一步是空操作。 */
  function notifySeeked(): void {
    if (upscaler) upscaler.onSeeked()
  }

  /** 换集/换源：拆掉实例并忘掉「已就绪」，但保留用户想要的档位等下一条媒体重试。 */
  function resetPipeline(): void {
    stopAiPipeline()
    _aiReady = false
  }

  /**
   * 此刻是否真的有管线在跑。
   *
   * 用函数而不是把实例做成 ref：模板要的只是这个真假值，而管线实例的创建/销毁时机是
   * 元数据驱动的，做成响应式会让「对比」按钮的出现时机跟着变。
   */
  function hasPipeline(): boolean {
    return upscaler !== null
  }

  return {
    qualityOpen,
    qualityMode,
    qualityOptions,
    qualityDropdownValue,
    onQualityChange,
    showAiWarning,
    confirmAiMode,
    cancelAiMode,
    qualityToastText,
    compareEnabled,
    compareSplit,
    toggleEnhancementCompare,
    updateEnhancementCompare,
    qualityLabel,
    onMediaReady,
    notifySeeked,
    resetPipeline,
    hasPipeline,
  }
}
