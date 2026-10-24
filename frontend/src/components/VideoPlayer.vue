<script setup lang="ts">
import { defineAsyncComponent, ref, nextTick, onMounted, onBeforeUnmount, watch, computed } from 'vue'
import PlayerSubtitleControls from '../player/views/PlayerSubtitleControls.vue'
import PlayerPreferencesControls from '../player/views/PlayerPreferencesControls.vue'
import PlayerAdReportControls from '../player/views/PlayerAdReportControls.vue'
import PlayerQualityControls from '../player/views/PlayerQualityControls.vue'
import Icon from './Icon.vue'
import { MotionTransition } from './ui'
import { TsCache } from '../utils/tsCache'
import { usePerfStore } from '../stores/perf'
import { usePluginStore } from '../stores/plugins'
import { preparePlayback } from '../player/preparation'
import { recordDiagnosticEvent } from '../diagnostics/recording'
import { registerPlayerResourceSnapshot } from '../diagnostics/browserResources'
import { createPlayerEvents } from '../diagnostics/playerEvents'
import { createPlayerTrace } from '../diagnostics/architecture'
import { releasePlayerAnimations } from '../player/releaseAnimations'
import { usePlaybackPreferences } from '../player/usePlaybackPreferences'
import { usePlayerOsd } from '../player/usePlayerOsd'
import { useSubtitles } from '../player/useSubtitles'
import { useThumbnailScrubber } from '../player/useThumbnailScrubber'
import { useVideoQuality } from '../player/useVideoQuality'
import { playbackResumeKey, usePlaybackResume } from '../player/usePlaybackResume'
import { usePlayerChrome } from '../player/usePlayerChrome'
import { useAdReport } from '../player/useAdReport'
import { usePlaybackTelemetry } from '../player/usePlaybackTelemetry'
import { createPlaybackControl } from '../player/playbackControl'
import { createPlaybackSession } from '../player/playbackSession'
import { usePlayerPresentationMode } from '../player/usePlayerPresentationMode'
import { useVideoInfo, type StreamVariantInfo } from '../player/useVideoInfo'
import { formatPlaybackTime as fmt } from '../player/videoInfoFormat'
import { createPlaybackSource } from '../player/hls/playbackSource'
import { resumeStreaming } from '../player/hls/resumeStreaming'
import mobileVideoPoster from '../mobile/assets/player-poster.svg'
import loadingGif from '../assets/videos/loading.gif'
import pauseImg from '../assets/images/pause.png'
import { useI18n } from 'vue-i18n'
import { tr } from '../locales'
import { useMobilePlayerGestures } from '../mobile/useMobilePlayerGestures'
import { createPlayerPlatform } from '../player/platform'
import { normalizeApiError } from '../api/errors'
import type { MobileChromeHandle } from '../player/views/controlState'
import { usePlayerVolume } from '../player/usePlayerVolume'
import { WriteLog } from '../api/app'
import PlayerDanmakuButtons from '../player/danmaku/PlayerDanmakuButtons.vue'
const PlayerDanmaku=defineAsyncComponent(()=>import('../player/danmaku/PlayerDanmaku.vue'))
const danmakuRef=ref<{open:(anchor?:HTMLElement,hover?:boolean)=>void;close:()=>void;scheduleClose:()=>void;toggle:()=>void;ready:boolean;engaged:boolean;enabled:boolean}|null>(null)
const PlayerDesktopControls = defineAsyncComponent(()=>import('../player/views/PlayerDesktopControls.vue'))
const desktopControls=ref<{engaged:boolean;close:()=>void}|null>(null)
const CczjMobilePlayerChrome=defineAsyncComponent(()=>import('../mobile/CczjMobilePlayerChrome.vue'))
const mobileChrome=ref<MobileChromeHandle|null>(null)
const CczjMobileGestureOverlay = defineAsyncComponent(()=>import('../mobile/CczjMobileGestureOverlay.vue'))

const PlayerEnhancementWarning=defineAsyncComponent(()=>import('../player/views/PlayerEnhancementWarning.vue'))
const PlayerVideoInfoDialog=defineAsyncComponent(()=>import('../player/views/PlayerVideoInfoDialog.vue'))
const PlayerShortcutDialog=defineAsyncComponent(()=>import('../player/views/PlayerShortcutDialog.vue'))
const infoDialogVisited=ref(false),shortcutDialogVisited=ref(false)
const platform = createPlayerPlatform()
const mobile = platform.profile.ui === 'mobile'
const mobileMoreCategory=computed(()=>mobileChrome.value?.category??'quality')
const mobileMoreTarget = ref<HTMLElement | null>(null)
const desktopToolsTarget=ref<HTMLElement|null>(null)

const props = withDefaults(defineProps<{
  url: string
  autoplay?: boolean
  hasPrev?: boolean
  hasNext?: boolean
  videoKey?: string
  danmakuKey?:string
  mediaTitle?:string
  mediaYear?:string
  mediaKind?:string
  episodeLabel?:string
  showTitleBar?: boolean
  title?: string
  isFav?: boolean
  favBusy?: boolean
  doubanId?: string
  /** 取当前集在后端 watch_history 里的位置（秒），本地缓存缺失时兜底 */
  resolveResume?: () => Promise<number>
}>(), {
  autoplay: true,
  hasPrev: false,
  hasNext: false,
  videoKey: '',
  danmakuKey:'',mediaTitle:'',mediaYear:'',mediaKind:'',episodeLabel:'',
  showTitleBar: true,
  title: '',
  isFav: false,
  favBusy: false,
  doubanId: '',
  resolveResume: undefined,
})

const emit = defineEmits(['back', 'prev', 'next', 'toggleFavorite', 'toggleAutoplay', 'showComments'])
const mediaSession = createPlaybackSession()
const { t } = useI18n()
const perf = usePerfStore()
const playerTrace = createPlayerTrace()
const playerEvents = createPlayerEvents(playerTrace.id)
let mediaLoadStarted=performance.now(),firstPlayingLogged=false
function logStartup(kind:string,data:Record<string,unknown>={}):void {
  const detail={player_session_id:playerTrace.id,elapsed_ms:Math.round(performance.now()-mediaLoadStarted),...data}
  playerEvents.event(kind,detail)
  void WriteLog({level:'INFO',source:'player',message:`[player.startup] ${kind}`,detail:JSON.stringify(detail)}).catch(()=>{})
}

function mediaHost(url:string):string {try{return new URL(url).hostname}catch{return ''}}

const wrapperRef = ref<HTMLDivElement>()
const errorMsg = ref('')
const showNetworkError = ref(false)
function clearNetworkErrTimer(): void { source.cancelNetworkTimeout() }
function retryPlayback(): void {
  const video = getVideoEl()
  if (!video || !props.url) return
  errorMsg.value = ''
  showNetworkError.value = false
  loadHls(video, props.url)
}

const playing = ref(false)
const current = ref(0)
const duration = ref(0)
const speed = ref(1)
const {isFullscreen,isPiP,toggleFullscreen,togglePiP} = usePlayerPresentationMode({
  platform,wrapperRef,video:getVideoEl,visible:keepVisible,
  failure:(operation,error)=>{
    playerEvents.issue(operation,error instanceof Error?error.name:'PLATFORM_OPERATION_FAILED',false)
    if(operation==='pip'){
      const code=normalizeApiError(error).code
      qualityToastText.value=t(code==='PERMISSION_DENIED'?'mobile.pipPermissionDenied':code==='UNSUPPORTED'?'mobile.pipUnavailable':'mobile.pipFailed')
    }
  },
  unavailable:capability=>{qualityToastText.value = t(capability==='pip'?'mobile.pipUnavailable':'player.fullscreenUnavailable')},
})
const chrome=usePlayerChrome({mobile,playing,root:()=>wrapperRef.value,
  pinned:()=>mobileControlsPinned.value||!!desktopControls.value?.engaged||!!danmakuRef.value?.engaged,
  leaveBlocked:()=>!!desktopControls.value?.engaged||qualityOpen.value||!!danmakuRef.value?.engaged,
})
const {showControls,engaged:mobileControlsEngaged,mouseInside,controlsVisible,toggleShow,onMouseLeave}=chrome
function keepVisible():void {chrome.keepVisible()}
function revealMobileControls():void {chrome.reveal()}
const loading = ref(true)
const preparationStage = ref<'video'|'accessories'|'danmaku'|'ready'|'complete'>('video')
const accessoriesReady = ref(false), prepared = ref(false), preparationNote = ref('')
const preparationMessage = computed(() => preparationStage.value === 'ready' && preparationNote.value
  ? preparationNote.value : t(`playerPreparation.${preparationStage.value}`))
const autoplayStarting = ref(false)
let preparingAccessories = false, settleDanmaku: ((status:string) => void) | undefined
const videoReady = ref(false)

// 弹出面板状态（音量和倍速）
const showPlaybackSettings = ref(false)
const showVideoInfo = ref(false)
const showShortcutModal = ref(false)
watch(showVideoInfo,open=>{if(open)infoDialogVisited.value=true})
watch(showShortcutModal,open=>{if(open)shortcutDialogVisited.value=true})

const m3u8StreamInfo = ref<StreamVariantInfo[]>([])
const {showReportAd,reportAdDomains,reportAdToast,toggleReportAd,doReportAd}=useAdReport(()=>props.url)


// ========= 外挂字幕 =========
const subtitleFileRef = ref<HTMLInputElement | null>(null)
const {
  showSubtitlePanel,
  subtitleCues,
  subtitleName,
  subtitleVisible,
  subtitleError,
  activeSubtitle,
  openSubtitlePicker,
  onSubtitleFileChosen,
  clearSubtitle,
} = useSubtitles({ current, fileInput: subtitleFileRef })

// ⭐ 播放器设置统一存储在单个 JSON 对象中（key: 'vp_settings'），避免 localStorage 碎片化。
// 旧版散落的 vp_* 键会在首次读取时自动迁移并清理。
const {settings,autoPlayEnabled,autoNextEnabled,autoResume:mobileAutoResume,toggleAutoPlay,toggleAutoNext,
  toggleAutoResume:toggleMobileAutoResume}=usePlaybackPreferences(()=>props.autoplay,()=>emit('toggleAutoplay'))
const {read:readSetting,write:writeSetting}=settings
const {volume,muted,restorePlaybackVolume,setMobileVolume,onWheel,toggleMute,onVolumeInput,
 showVolumeToastRef,showVolumeToast,toastVolumePct}=usePlayerVolume({platform,video:getVideoEl,visible:keepVisible,settings})

// ========= 画质模式与增强管线 =========
// 模式：原高清 / 动画增强 M·L / 影视增强 / 扩展包着色器档位（useVideoQuality）。
const {
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
} = useVideoQuality({
  getVideoEl,
  wrapperRef,
  settings: { read: readSetting, write: writeSetting },
  keepVisible,
})
function selectMobileQuality(value: string | number): void {
  onQualityChange(value)
  if (showAiWarning.value) mobileChrome.value?.close('more')
  keepVisible()
}

const resume = usePlaybackResume({
  getVideo: getVideoEl,
  getKey: () => playbackResumeKey(props.videoKey, props.url),
  resolveResume: () => props.resolveResume?.(),
  settings: { read: readSetting, write: writeSetting },
})
const { savedTime, showResumePrompt, resumeRemainSec, dismissResumePrompt,
  flushPendingSeek, jumpToSavedTime } = resume

// 计算进度百分比（给 CSS 用）
const progressPct = computed(() => {
  const d = duration.value
  if (!d || d <= 0) return 0
  const pct = (current.value / d) * 100
  return Math.max(0, Math.min(100, pct))
})

// 缓冲进度百分比（通过 video.buffered 计算）
const bufferPct = ref(0)
function updateBuffer(): void {
  const v = getVideoEl()
  if (!v || !v.duration || v.duration <= 0) {
    TsCache.setPlaybackBufferAhead(0)
    return
  }
  const buf = v.buffered
  if (!buf || buf.length === 0) {
    bufferPct.value = 0
    TsCache.setPlaybackBufferAhead(0)
    return
  }
  // A distant range left after seeking cannot feed the current playback point.
  let end = v.currentTime
  for (let i = 0; i < buf.length; i++) {
    if (buf.start(i) <= end + .05 && buf.end(i) > end) end = buf.end(i)
  }
  bufferPct.value = Math.max(0, Math.min(100, (end / v.duration) * 100))
  TsCache.setPlaybackBufferAhead(Math.max(0, end - v.currentTime), v.playbackRate)
}

// 缓存监控
const telemetry=usePlaybackTelemetry({video:getVideoEl,url:()=>props.url,cachedBitrate:()=>extractBitrateFromCachedTs(),t,
  stall:(phase,detail)=>logStartup(`player_buffering_${phase}`,detail)})
const {cacheStats,loadingCached,loadingTotal,loadingSpeed,loadingElapsed,startLoadingStats,stopLoadingStats,
  resetPerfBaseline,alignPlaybackClock,endStall}=telemetry
const { videoInfo, extractBitrateFromCachedTs } = useVideoInfo({
  getVideoEl, getUrl: () => props.url, duration, current, muted, volume, speed,
  streams: m3u8StreamInfo, cacheStats, qualityLabel: () => qualityLabel(qualityMode.value), t,
})

// ------ 工具 ------

function isHls(u: string): boolean {
  return /\.m3u8(\?|$)/i.test(u || '')
}

// ------ 获取 video 元素 ------
function getVideoEl(): HTMLVideoElement | null {
  const el = wrapperRef.value?.querySelector('video') as HTMLVideoElement | null
  return el || null
}

const playback = createPlaybackControl({
  getVideo: getVideoEl,
  onMuted: () => { muted.value = true },
  onUnsupported: () => { errorMsg.value = t('player.errFormatUnsupported'); loading.value = false },
})
function safePlay(auto: boolean): Promise<void> { return playback.play(auto) }
function tryAutoplay(video:HTMLVideoElement):void {
  if (!prepared.value || !videoReady.value || !autoPlayEnabled.value || video.hasAttribute('data-autoplay-done')) return
  video.setAttribute('data-autoplay-done','1')
  if(!video.paused)return
  const session=mediaSession.capture()
  autoplayStarting.value=true
  logStartup('player_autoplay_requested')
  void safePlay(true).finally(()=>{
    if(!session.current())return
    autoplayStarting.value=false
    if(video.paused){loading.value=false;logStartup('player_autoplay_blocked')}
  })
}
function onDanmakuPrepared(key:string,status:string):void {
  if(key!==props.danmakuKey)return
  playerEvents.event('player_danmaku_availability',{status})
  if(status==='noMatch')preparationNote.value=t('playerPreparation.noMatch')
  else if(status==='error')preparationNote.value=t('playerPreparation.deferred')
  settleDanmaku?.(status)
}
async function prepareAccessories(video:HTMLVideoElement):Promise<void> {
  if(preparingAccessories)return
  preparingAccessories=true
  const session=mediaSession.capture()
  const complete=await preparePlayback({
    signal:session.signal,
    stage:stage=>{preparationStage.value=stage;logStartup('player_preparation',{stage})},
    accessories:async()=>{await initThumbSampler();await usePluginStore().ensureLoaded();if(session.current())onMediaReady()},
    danmaku:()=>{
      if(!props.danmakuKey)return Promise.resolve()
      const pending=new Promise<string>(resolve=>{settleDanmaku=resolve})
      accessoriesReady.value=true
      return pending
    },
    timeout:stage=>{
      logStartup('player_preparation_deferred',{stage})
      if(stage==='danmaku')preparationNote.value=t('playerPreparation.deferred')
    },
  })
  if(!session.current())return
  settleDanmaku=undefined
  if(complete){
    prepared.value=true;tryAutoplay(video)
    if(videoReady.value&&!autoplayStarting.value)loading.value=false
    if(!video.paused)mediaSession.later(()=>{preparationStage.value='complete'},1200)
  }
}

const source = createPlaybackSource({
  streams: streams => { m3u8StreamInfo.value = streams },
  error: (code, detail) => {
    playerEvents.issue('source',code,code==='timeout')
    if (code === 'timeout') showNetworkError.value = true
    errorMsg.value = code === 'unsupported' ? tr('player.errHlsUnsupported')
      : code === 'load_failed' ? tr('player.errLoadFailed')
      : code === 'timeout' ? tr('player.errNetworkTimeout')
      : tr('player.errStreamLoadFailed', { detail: detail || tr('player.unknownError') })
    loading.value = false
  },
  fatal: detail => perf.noteError(detail, mediaHost(props.url)),
  recovering:detail=>playerEvents.issue('source',detail,true),
  unrecoverable: destroyPlayer,
  bufferChanged: updateBuffer,
  networkPolicy: detail => logStartup('player_network_policy', detail),
  networkIssue: detail => logStartup('player_network_issue', detail),
})

async function loadHls(video: HTMLVideoElement, url: string, continuation?:number): Promise<void> {
  mediaLoadStarted=performance.now();firstPlayingLogged=false
  playerTrace.mediaChanged()
  logStartup('player_media_load',{host:mediaHost(url)})
  perf.beginLoad(mediaHost(url))
  resetPerfBaseline(video)
  destroyPlayerInternal(video)
  const session = mediaSession.capture()
  bindCommonVideoEvents(video)
  errorMsg.value = ''
  loading.value = true
  try {
    await resume.load(continuation)
    if (!session.current()) return
    logStartup('player_resume_loaded')
    const attached = await source.load(video, url, session)
    if (!session.current() || !attached) return
    logStartup('player_source_attached')
    telemetry.start()
  } catch (error: unknown) {
    if (!session.current()) return
    destroyPlayerInternal(video)
    errorMsg.value = t('player.errInitFailed', { detail: error instanceof Error ? error.message : String(error) })
    loading.value = false
  }
}

let _retryCount = 0
function setupPlayer(continuation?:number): void {
  const video = getVideoEl()
  if (!video) {
    if (_retryCount < 8) {
      _retryCount++
      mediaSession.later(()=>setupPlayer(continuation), 50)
    } else {
      errorMsg.value = t('player.errPlayerCreateFailed')
    }
    return
  }
  _retryCount = 0
  // Android uses one native media volume; desktop retains its saved element gain.
  restorePlaybackVolume(video)
  const savedSpeed = parseFloat(readSetting('speed', '1'))
  if (isFinite(savedSpeed) && savedSpeed >= 0.5 && savedSpeed <= 3) {
    try { video.playbackRate = savedSpeed; speed.value = savedSpeed } catch { /* ignore */ }
  }

  const url = props.url
  if (!url) {
    errorMsg.value = t('player.errNoVideoUrl')
    return
  }
  // 非 .m3u8 后缀也走这条路：短链常 302 到 HLS，loadHls 先拆旧实例、按 m3u8 解析，
  // 拿不到清单再回退 video.src，两条分支本来就汇到同一个调用。
  loadHls(video, url, continuation)
}

function bindCommonVideoEvents(video: HTMLVideoElement): void {
  if ((video as any).__eventsBound) return
  ;(video as any).__eventsBound = true
  const signal = mediaSession.capture().signal
  const on = (name: string, listener: EventListenerOrEventListenerObject) =>
    video.addEventListener(name, listener, { signal: signal })
  if (!mobile) bindOsdListeners(video, signal)

  on('play', () => {
    playing.value = true
    const ready = video.readyState >= 2 && video.videoWidth > 0
    loading.value = !ready
    videoReady.value = ready
    if (ready) stopLoadingStats()
    else startLoadingStats()
    clearNetworkErrTimer()

    // 暂停较久后 HLS 可能已经停止拉流。仅在没有可播放未来帧时，
    // 从当前位置恢复加载，避免无条件重启造成已有缓冲失效。
    resumeStreaming(video)
  })
  on('pause', () => {
    endStall('paused')
    if(!firstPlayingLogged)logStartup('player_startup_pause',{prepared:prepared.value,autoplay_pending:autoplayStarting.value})
    playback.cancel()
    resume.save()
    playing.value = false
    loading.value = !prepared.value || autoplayStarting.value
    stopLoadingStats()
    clearNetworkErrTimer()
  })
  on('seeked', () => alignPlaybackClock(video))
  on('timeupdate', () => {
    current.value = video.currentTime
    if (video.duration) duration.value = video.duration
    updateBuffer()
    resume.track()
  })
  on('loadedmetadata', () => {
    logStartup('player_metadata_loaded',{duration_ms:Math.round(video.duration*1000)})
    duration.value = video.duration
    restorePlaybackVolume(video)
    const savedSpeed = parseFloat(readSetting('speed', '1'))
    if (isFinite(savedSpeed) && savedSpeed >= 0.5 && savedSpeed <= 3) {
      try { video.playbackRate = savedSpeed; speed.value = savedSpeed } catch { /* ignore */ }
    }
    updateBuffer()
    flushPendingSeek()
    console.log(`[Player] loadedmetadata: duration=${video.duration.toFixed(1)}s, volume=${video.volume.toFixed(2)}`)
    // ⭐ 换集/换源后自动重建 AI 增强：destroyPlayerInternal 会 resetPipeline() 把 WebGL 上下文
    // 还掉，而元数据就绪是唯一安全的挂点（要有 videoWidth/Height 才能建管线）。要不要重试由
    // useVideoQuality 自己判断（它记得用户想要的档位）。
    void prepareAccessories(video)
  })
  on('progress', updateBuffer)
  on('seeking', updateBuffer)
  on('seeked', () => {
    updateBuffer()
    notifySeeked()
  })
  on('waiting', () => {
    if (video.paused || video.ended) return
    updateBuffer()
    telemetry.beginStall()
    loading.value = true
    startLoadingStats()
  })
  on('playing', () => {
    playing.value=true
    loading.value=false
    if(!firstPlayingLogged){firstPlayingLogged=true;logStartup('player_media_playing',{prepared:prepared.value})}
    perf.markFirstFrame()
    endStall('playing')
    mediaSession.later(()=>{if(prepared.value)preparationStage.value='complete'},1200)
  })
  on('canplay', () => {
    flushPendingSeek()
    videoReady.value = true
    endStall('ready')
    loading.value = !prepared.value || autoplayStarting.value || (autoPlayEnabled.value&&!video.hasAttribute('data-autoplay-done'))
    // ⭐ 修复：视频解码出帧后才触发自动播放，避免 AbortError
    tryAutoplay(video)
  })
  on('volumechange', () => {
    const native = platform.device.read()
    if (native) { volume.value = native.volume; muted.value = video.muted || native.volume === 0; return }
    volume.value = video.volume
    muted.value = video.muted
    writeSetting('volume', String(video.volume))
    writeSetting('muted', video.muted ? '1' : '0')
    // ⭐ 统一由 volumechange 触发 toast，覆盖所有场景（滚轮/键盘/按钮）
    if (!mobile) showVolumeToastRef()
  })
  on('ratechange', () => {
    speed.value = video.playbackRate
    if (gestures.boosting.value) return
    // load() resets playbackRate to defaultPlaybackRate when switching media.
    // Keep the selected default, while temporary holds never change it.
    if (video.defaultPlaybackRate !== video.playbackRate) video.defaultPlaybackRate = video.playbackRate
    writeSetting('speed', String(video.playbackRate))
  })
  on('enterpictureinpicture', () => { isPiP.value = true })
  on('leavepictureinpicture', () => { isPiP.value = false })
  on('ended', () => {
    // 播放结束：移除当前进度（下次不跳回结尾）
    resume.ended()
    // 「自动连播」原先是个没有接线的开关：勾上了也没有任何代码消费它。
    if (autoNextEnabled.value && props.hasNext) emit('next')
  })
  on('error', () => {
    loading.value = false
    // 媒体元素自己的错误只在换源回退直连时出现（hls.js 路径的错误走 Hls.Events.ERROR）。
    // 首播耗时不结算：这次装载根本没有出画，记它会把它算成一次成功的快。
    endStall()
    perf.noteError(String(video.error?.code ?? ''), mediaHost(props.url))
  })
}

function destroyPlayerInternal(video: HTMLVideoElement): void {
  playback.cancel()
  mediaSession.reset()
  preparationStage.value='video';prepared.value=false;autoplayStarting.value=false;accessoriesReady.value=false;preparingAccessories=false;preparationNote.value='';settleDanmaku=undefined
  resume.reset()
  clearNetworkErrTimer()
  m3u8StreamInfo.value = []
  current.value = 0
  duration.value = 0
  bufferPct.value = 0
  TsCache.setPlaybackBufferAhead(0)
  source.dispose(video)
  try {
    video.onerror = null
    try { video.pause() } catch { /* ignore */ }
    try { video.removeAttribute('src') } catch { /* ignore */ }
    try { video.load() } catch { /* ignore */ }
    try { video.removeAttribute('data-autoplay-done') } catch { /* ignore */ }
    try { delete (video as any).__eventsBound } catch { /* ignore */ }
  } catch { /* ignore */ }
  telemetry.reset()
  resetPipeline()
  loading.value = true
  videoReady.value = false
  showResumePrompt.value = false
}

function destroyPlayer(): void {
  telemetry.stop()
  const v = getVideoEl()
  if (v) { resume.save(); destroyPlayerInternal(v) }
  else { source.dispose(); mediaSession.reset(); playback.cancel(); resume.reset(); resetPipeline() }
}

// ------ 用户交互控制 ------
function togglePlay(): void {
  const v = getVideoEl()
  if (!v || !prepared.value) return
  v.setAttribute('data-autoplay-done','1')
  if (v.paused) {
    safePlay(false) // 用户点击，不允许静音 fallback
  } else {
    v.pause()
  }
  keepVisible()
}

const progressContainerRef = ref<HTMLDivElement>()
const thumbCanvasRef = ref<HTMLCanvasElement | null>(null)
const {
  progressHoverPct,
  progressHoverTime,
  thumbPreviewImg,
  thumbPreviewVisible,
  thumbPreviewState,
  thumbPreviewTime,
  initThumbSampler,
  destroyThumbSampler,
  onProgressHover,
  onProgressLeave,
  requestThumbnail,
  snapshot: thumbnailSnapshot,
} = useThumbnailScrubber({
  wrapperRef,
  progressContainerRef,
  thumbCanvasRef,
  getVideoEl,
  getUrl: () => props.url,
  isHlsUrl: isHls,
  idleReleaseMs: mobile ? 2000 : 10000,
  keepPreviousFrame: mobile,
})

const mobileScrubTime = ref(-1)
const displayedProgress = computed(() => mobileScrubTime.value >= 0 && duration.value > 0 ? mobileScrubTime.value / duration.value * 100 : progressPct.value)
function onProgressPointerDown(event: PointerEvent): void {
  if (!mobile || mobileLocked.value || !event.isPrimary || event.button !== 0 || duration.value <= 0) return
  cancelMobileTap()
  const input = event.currentTarget as HTMLInputElement
  const rect = input.getBoundingClientRect()
  mobileScrubTime.value = Math.max(0, Math.min(1, (event.clientX - rect.left) / rect.width)) * duration.value
  input.setPointerCapture(event.pointerId)
  keepVisible()
}
function onProgressInput(event: Event): void {
  if (mobile && mobileScrubTime.value >= 0) {
    mobileScrubTime.value = Number((event.target as HTMLInputElement).value)
    return
  }
  seek(event)
}
function onProgressPointerEnd(event: PointerEvent): void {
  if (!mobile || mobileScrubTime.value < 0) return
  if (event.type !== 'pointercancel') {
    const video = getVideoEl()
    if (video) { video.currentTime = mobileScrubTime.value; current.value = mobileScrubTime.value }
  }
  mobileScrubTime.value = -1
  keepVisible()
}

watch(thumbPreviewState,(value,previous)=>{
  if(previous==='idle'&&value!=='idle')playerEvents.event('preview_started',{})
  if(value==='idle'&&previous!=='idle')playerEvents.event('preview_stopped',{})
  if(value==='unavailable')playerEvents.issue('preview','FRAME_UNAVAILABLE',true)
})
const detachResourceSnapshot = registerPlayerResourceSnapshot(() => ({
  quality_mode: qualityMode.value, enhancement_active: hasPipeline(),
  presentation: {profile:platform.profile,controls:mobile?'mobile':'desktop',
    fullscreen_adapter:platform.fullscreen.kind,pip_adapter:platform.pip.kind,
    native_volume:!!platform.device.read()},
  hls: source.snapshot(), thumbnail: thumbnailSnapshot(), issues:playerEvents.snapshot(),
  segment_cache: Object.fromEntries(Object.entries(TsCache.stats()).filter(([, value]) => typeof value === 'number' || typeof value === 'boolean')),
}), playerTrace.id)

function seek(e: Event): void {
  const v = getVideoEl()
  if (!v) return
  const target = e.target as HTMLInputElement
  const val = parseFloat(target.value)
  if (!isFinite(val)) return
  v.currentTime = val
  current.value = val
    // ⭐ 修复：seek 后让输入框失焦，避免键盘事件被拦截
    ; (target as HTMLElement).blur()
  wrapperRef.value?.focus?.()
}

function seekRelative(delta: number): void {
  const v = getVideoEl()
  if (!v) return
  const newTime = Math.max(0, Math.min(v.duration || 0, v.currentTime + delta))
  v.currentTime = newTime
  current.value = newTime
}

// 通过鼠标位置直接计算跳转时间（比 range input 更精确）
function onProgressMouseDown(e: MouseEvent): void {
  if (mobile) return
  const v = getVideoEl()
  if (!v || !progressContainerRef.value || !v.duration) return
  const rect = progressContainerRef.value.getBoundingClientRect()
  const pct = Math.max(0, Math.min(1, (e.clientX - rect.left) / rect.width))
  const newTime = pct * v.duration
  v.currentTime = newTime
  current.value = newTime
  // ⭐ 修复：seek 后让输入框失焦，避免键盘事件被拦截
  const slider = progressContainerRef.value.querySelector('.progress-slider') as HTMLElement | null
  if (slider) slider.blur()
  wrapperRef.value?.focus?.()
}

// ========= 操作 OSD（屏幕中央提示：快进/快退/音量/倍速） =========
const { osdText, osdIcon, bindOsdListeners, clearOsdTimer } = usePlayerOsd()

function changeSpeed(s: number): void {
  gestures.cancel()
  const v = getVideoEl()
  if (v) {
    v.playbackRate = s
    speed.value = s
    writeSetting('speed', String(s))
  }
  mobileChrome.value?.close('speed')
}

const gestures = useMobilePlayerGestures({ enabled: mobile, device:platform.device, video: getVideoEl, visible: revealMobileControls, onVolume: setMobileVolume,
  onStart: () => { cancelMobileTap(); toggleShow(false) } })
const { locked: mobileLocked, hud: gestureHud, kind: gestureKind, seekTarget: gestureSeekTarget, level: gestureLevel, compact: gestureCompact, brightness: mobileBrightness } = gestures
watch([gestureKind, gestureSeekTarget, mobileScrubTime], ([kind, target, scrub]) => {
  if (!mobile) return
  if (scrub >= 0) requestThumbnail(scrub)
  else if (kind === 'seek' && target >= 0) requestThumbnail(target)
  else onProgressLeave()
})
const mobileControlPointer = ref(false)
const mobileControlsPinned = computed(() => mobile && (mobileControlPointer.value || mobileChrome.value?.pinned || showPlaybackSettings.value || showSubtitlePanel.value || showReportAd.value || qualityOpen.value || showVideoInfo.value))
function onControlPointerDown(event: PointerEvent): void {
  if (!mobile || !(event.target instanceof Element) || !event.target.closest('button,input,.progress-bar-wrapper')) return
  cancelMobileTap()
  mobileControlsEngaged.value = true
  mobileControlPointer.value = true
  keepVisible()
}
function onControlPointerUp(): void {
  if (!mobileControlPointer.value) return
  mobileControlPointer.value = false
  keepVisible()
}
onMounted(() => { if (mobile) { window.addEventListener('pointerup', onControlPointerUp); window.addEventListener('pointercancel', onControlPointerUp) } })
onBeforeUnmount(() => { window.removeEventListener('pointerup', onControlPointerUp); window.removeEventListener('pointercancel', onControlPointerUp) })
function closeSharedTools():void {qualityOpen.value=false;showPlaybackSettings.value=showSubtitlePanel.value=showReportAd.value=false}
if(mobile)watch([showPlaybackSettings,showSubtitlePanel,showReportAd,qualityOpen,showVideoInfo],(values,previous)=>{
  const opened=values.findIndex((value,i)=>value&&!previous[i]);if(opened<0)return
  const panels=[showPlaybackSettings,showSubtitlePanel,showReportAd,qualityOpen,showVideoInfo]
  panels.forEach((panel,i)=>{if(i!==opened)panel.value=false})
  mobileChrome.value?.close('quick')
})
watch(isFullscreen, () => {cancelMobileTap();gestures.cancel();mobileScrubTime.value=-1;mobileLocked.value=false;mobileChrome.value?.close();keepVisible()})
// ------ 键盘控制（仅在鼠标位于播放器内 或 全屏时生效） ------
function onKeyDown(e: KeyboardEvent): void {
  // ⭐ V2：键盘快捷键现已由 Player.vue 页面级统一处理
  //   VideoPlayer 仅在全屏模式下保留 ESC 退出逻辑，避免重复响应
  if (!isFullscreen.value) return
  if (e.key === 'Escape') {
    e.preventDefault()
    toggleFullscreen()
  }
}

// 点击空白处关闭弹出面板
let mobileTapTimer: ReturnType<typeof setTimeout> | undefined
let mobileTapPoint: {x:number;y:number} | undefined
function cancelMobileTap(): void {
  if (mobileTapTimer !== undefined) clearTimeout(mobileTapTimer)
  mobileTapTimer = undefined
  mobileTapPoint = undefined
}
function onWrapperClick(event?: MouseEvent): void {
  if (mobile) {
    if (gestures.consumeClick()) return
    mobileControlsEngaged.value = true
    const point = { x:event?.clientX ?? 0, y:event?.clientY ?? 0 }
    if (mobileTapTimer !== undefined && mobileTapPoint && Math.hypot(point.x - mobileTapPoint.x, point.y - mobileTapPoint.y) < 36) {
      cancelMobileTap()
      if (!loading.value || videoReady.value) { togglePlay(); keepVisible() }
      return
    }
    if (mobileTapTimer !== undefined) { cancelMobileTap(); toggleShow(!showControls.value) }
    mobileTapPoint = point
    mobileTapTimer = setTimeout(() => { cancelMobileTap(); if (!mobileLocked.value) toggleShow(!showControls.value) }, 260)
    return
  }
  // 只有视频已就绪（或正在播放/暂停中）时才 toggle play
  if (!loading.value || videoReady.value) {
    togglePlay()
  }
  // 关闭所有弹出面板
  desktopControls.value?.close()
  mobileChrome.value?.close('speed')
}

function onMobileOverlayBack(event: Event): void {
  if (!mobile) return
  if (!(mobileChrome.value?.pinned || showVideoInfo.value || mobileLocked.value || showPlaybackSettings.value || showSubtitlePanel.value || showReportAd.value)) return
  event.preventDefault()
  event.stopImmediatePropagation()
  mobileChrome.value?.close()
  showVideoInfo.value = mobileLocked.value = showPlaybackSettings.value = showSubtitlePanel.value = showReportAd.value = false
  keepVisible()
}

// 挂载后那两件事（抢焦点、建缩略图采样器）都排在 100ms 之后，快速进出播放页时这个
// 定时器会跑到卸载之后的组件上，所以和 setupPlayer 的重试定时器一起记账、离开工件时清掉。
let _bootTimer: number | null = null
let playerDisposed = false
const releaseCache = TsCache.retainPlayback()

onMounted(async () => {
  await nextTick()
  if (playerDisposed) return
  setupPlayer()
  document.addEventListener('keydown', onKeyDown)
  if (mobile) window.addEventListener('cczj:android-overlay-back', onMobileOverlayBack)
  // 让播放器区域自动获得键盘焦点（上下键调节音量等）
  _bootTimer = window.setTimeout(() => {
    _bootTimer = null
    wrapperRef.value?.focus?.()
  }, 100)
})

onBeforeUnmount(() => {
  cancelMobileTap()
  playerDisposed = true
  // Disable motion before resetting progress, which can start a transition.
  releasePlayerAnimations(wrapperRef.value)
  if (_bootTimer !== null) { window.clearTimeout(_bootTimer); _bootTimer = null }
  _retryCount = 0
  void recordDiagnosticEvent('player_closing', perf.snapshot())
  destroyPlayer()
  destroyThumbSampler()
  releaseCache()
  detachResourceSnapshot()
  const issues=playerEvents.snapshot()
  playerEvents.close()
  playerTrace.close()
  void recordDiagnosticEvent('player_closed', {player_session_id:playerTrace.id,issues,...perf.snapshot()})
  clearNetworkErrTimer()
  document.removeEventListener('keydown', onKeyDown)
  window.removeEventListener('cczj:android-overlay-back', onMobileOverlayBack)
  clearOsdTimer()
})

watch(() => [props.url, props.videoKey] as const, ([newUrl, newKey], [oldUrl, oldKey]) => {
  const video = getVideoEl()
  const continuation = oldUrl && newUrl !== oldUrl && newKey && newKey === oldKey && video && Number.isFinite(video.currentTime)
    ? video.currentTime : undefined
  cancelMobileTap()
  gestures.cancel()
  mobileScrubTime.value = -1
  chrome.reset()
  desktopControls.value?.close()
  mobileChrome.value?.close()
  resume.save()
  if (!newUrl) {
    destroyPlayer()
    return
  }
  const switched = Boolean(oldUrl && (oldUrl !== newUrl || oldKey !== newKey))
  if (switched) {
    const v = getVideoEl()
    if (v) {
      try { v.pause() } catch { /* ignore */ }
      try { v.removeAttribute('src') } catch { /* ignore */ }
      try { v.load() } catch { /* ignore */ }
    }
    loading.value = true
    videoReady.value = false
    playing.value = false
    // 字幕文件是对着某一集配的，换集后时间轴基本必然错位，直接清掉比留着误导人好
    clearSubtitle()
  }
  mediaSession.reset()
  playback.cancel()
  resume.reset()
  clearNetworkErrTimer()
  mediaSession.later(() => {
    setupPlayer(continuation)
  }, 0)
})

// 监听 loading 状态：播放中卡顿时自动显示加载统计
watch(loading, (val) => {
  if (val) {
    // 播放中卡顿，显示加载速度
    startLoadingStats()
  } else {
    stopLoadingStats()
  }
})

// 暴露 PiP 方法给父组件（Player.vue 快捷键调用）

const controlState = {
  get canPip(){void videoReady.value;const video=getVideoEl();return !!video&&platform.pip.supported(video)},
  get canFullscreen(){return platform.fullscreen.supported(wrapperRef.value)}, get canCast(){return platform.cast.supported},
  get pip(){return isPiP.value}, get playing(){return playing.value}, get current(){return current.value}, get duration(){return duration.value},
  get fullscreen(){return isFullscreen.value}, get speed(){return speed.value}, get volume(){return volume.value},
  get muted(){return muted.value}, get hasPrev(){return props.hasPrev}, get hasNext(){return props.hasNext},
}
const controlSource={get url(){return props.url},get title(){return props.title}}
const mobileCommands={video:getVideoEl,play:togglePlay,previous:()=>{if(props.hasPrev)emit('prev')},next:()=>{if(props.hasNext)emit('next')},
  fullscreen:toggleFullscreen,pip:togglePiP,speed:changeSpeed,interaction:keepVisible}
const desktopCommands = {video:getVideoEl,play:togglePlay,previous:()=>emit('prev'),next:()=>emit('next'),
  fullscreen:toggleFullscreen,mute:toggleMute,volume:onVolumeInput,speed:changeSpeed,interaction:keepVisible}

defineExpose({ togglePiP, toggleFullscreen, video:getVideoEl, overlayHost:()=>wrapperRef.value,openLines:()=>mobileChrome.value?.toggle('lines') })
</script>

<template>
  <div class="player-wrapper" :class="{ fullscreen: isFullscreen, 'cczj-mobile-video': mobile, 'cczj-mobile-player-locked': mobileLocked, 'cursor-hidden': !mobile && playing && !showControls, 'show-controls': showControls }"
    ref="wrapperRef" :style="mobile ? {'--cczj-mobile-image-brightness': .25 + mobileBrightness * 1.5} : undefined" tabindex="0" @mousemove="if (!mobile) { toggleShow(true); mouseInside = true; updateEnhancementCompare($event) }" @mouseenter="mouseInside = true"
    @mouseleave="onMouseLeave" @click="onWrapperClick" @dblclick.stop="!mobile && toggleFullscreen()" @wheel.prevent="onWheel"
    @pointerdown.capture="onControlPointerDown" @pointerdown="gestures.down" @pointermove="gestures.move" @pointerup="gestures.end" @pointercancel="gestures.end" @lostpointercapture.self="gestures.end" @contextmenu="mobile && $event.preventDefault()">
    <!-- 顶部栏：标题 + 缓存统计 + 收藏按钮 -->
    <Transition name="cczj-mobile-player-chrome" :css="mobile">
    <div v-show="showTitleBar && !mobileLocked && controlsVisible" class="player-title-bar" @click.stop @dblclick.stop>
      <button v-if="mobile" class="cczj-mobile-player-back" :aria-label="t('common.back')" @click="isFullscreen ? toggleFullscreen() : emit('back')"><Icon name="chevron-left" :size="22" /></button>
      <span v-show="!mobile || isFullscreen" class="player-title">{{ title || url }}</span>
      <span v-if="!mobile && isHls(url) && cacheStats.totalSegments > 0" class="cache-info"
        :class="{ working: cacheStats.queued + cacheStats.inflight > 0 }"
        :title="t('player.cacheStatsTip', { hits: cacheStats.hits, misses: cacheStats.misses, entries: cacheStats.entries, target: cacheStats.prefetchTarget })">
        {{ t('player.cachedCountShort', { count: cacheStats.entries }) }}<span v-if="cacheStats.queued + cacheStats.inflight > 0"> · {{ t('player.prefetching') }}</span><span v-else> · {{ t('player.ready') }}</span> ·
        {{ t('player.hitRateShort') }} {{ (cacheStats.hitRate * 100).toFixed(0) }}%
      </span>
      <PlayerDanmakuButtons v-if="mobile&&!isFullscreen&&danmakuKey" mobile :enabled="!!danmakuRef?.enabled" :ready="!!danmakuRef?.ready" :expanded="!!danmakuRef?.engaged" @toggle="danmakuRef?.toggle();keepVisible()" @settings="(anchor,hover)=>{danmakuRef?.open(anchor,hover);keepVisible()}" @leave="danmakuRef?.scheduleClose()" @dismiss="danmakuRef?.close()" />
      <button v-if="mobile" class="cczj-mobile-player-favorite" :aria-label="t(isFav?'detail.removeFav':'detail.addFav')" :aria-pressed="isFav" :disabled="favBusy" @click.stop="emit('toggleFavorite')"><Icon name="star" :size="21" /></button>
      <button v-if="mobile && isFullscreen && controlState.canCast" class="cczj-mobile-player-cast" :aria-label="t('mobile.cast')" @click.stop="mobileChrome?.toggle('cast')"><Icon name="cast" :size="22" /></button>
      <button v-if="mobile && isFullscreen && $slots.lines" class="cczj-mobile-player-lines" :aria-label="t('playLine.switch')" :aria-expanded="!!mobileChrome?.linesOpen" @click.stop="mobileChrome?.toggle('lines');keepVisible()"><Icon name="list" :size="20" /><span>{{t('playLine.title')}}</span></button>
      <button v-if="mobile" class="cczj-mobile-player-more-button" :aria-label="t('mobile.playerMore')" :aria-expanded="!!mobileChrome?.menuOpen" @click.stop="mobileChrome?.toggle('more'); keepVisible()"><Icon name="more" :size="22" /></button>
      <button v-show="!mobile" class="fav-btn-in-player" :class="{ 'is-fav': isFav }" :disabled="favBusy"
        :aria-label="isFav ? t('detail.removeFav') : t('player.addFavorite')"
        :title="isFav ? t('detail.removeFav') : t('player.addFavorite')" @click.stop="emit('toggleFavorite')">
        {{ isFav ? '★' : '☆' }}
      </button>
    </div>

    </Transition>

    <!-- 拖拽遮罩条：鼠标经过视频顶部时出现，可拖拽移动窗口 -->
    <!-- 注意：
         1) 用 v-show 而非 v-if —— 保持元素始终在 DOM 中，避免 Wails 的
            --wails-draggable 命中测试与动态挂载产生竞态（参考 TitleBar.vue 始终渲染）。
         2) 不加 @click.stop / @dblclick.stop —— 这些会干扰 Wails 在 mousedown 阶段
            的拖拽识别（参考 TitleBar.vue 的稳定写法：只设 --wails-draggable: drag）。
         3) z-index 高于 player-title-bar，保证拖拽区不被标题栏遮住；标题栏容器
            pointer-events:none，仅按钮区单独 pointer-events:auto。 -->
    <div v-show="mouseInside" class="player-drag-handle" :title="t('player.dragMoveWindow')" />

    <video class="native-video" :poster="mobile ? mobileVideoPoster : undefined" playsinline preload="auto" @click.stop="mobile ? onWrapperClick($event) : togglePlay()"></video>
    <PlayerDanmaku v-if="danmakuKey&&accessoriesReady" ref="danmakuRef" :media-key="danmakuKey" :media-title="mediaTitle" :media-year="mediaYear" :media-kind="mediaKind" :episode-label="episodeLabel" :current="current" :duration="duration" :playing="playing&&!loading" :fullscreen="isFullscreen" :mobile="mobile" :host="wrapperRef" :video="getVideoEl" @prepared="onDanmakuPrepared" />
    <div v-show="activeSubtitle" class="vp-subtitle" :class="{ 'with-controls': showControls }">
      <span>{{ activeSubtitle }}</span>
    </div>
    <div v-if="compareEnabled" class="enhance-compare-line" :style="{ left: compareSplit + '%' }" aria-hidden="true">
      <span>{{ t('player.compareOriginal') }}</span><i></i><span>{{ t('player.compareEnhanced') }}</span>
    </div>

    <!-- 操作 OSD：快进/快退/倍速 屏幕中央提示 -->
    <transition name="osd-fade">
      <div v-show="!mobile && osdText" class="action-osd">
        <span class="action-osd-icon">{{ osdIcon }}</span>
        <span class="action-osd-text">{{ osdText }}</span>
      </div>
    </transition>

    <div v-if="mobile && loading" class="cczj-mobile-video-loading" :class="{ 'cczj-mobile-video-loading-initial': !videoReady }" role="status" aria-live="polite">
      <img :src="loadingGif" class="loading-spinner" alt="" aria-hidden="true" />
      <span>{{ preparationStage !== 'complete' ? preparationMessage : t('mobile.videoLoading') }}<span v-if="loadingElapsed > 0"> · {{ loadingElapsed }}s</span></span>
    </div>
    <div v-if="!mobile && loading" class="loading">
      <div class="loading-overlay">
        <!-- 加载动画 -->
        <img :src="loadingGif" class="loading-spinner" :alt="t('common.loading')" />
        <!-- 缓冲信息 -->
        <div class="loading-info">
          <div class="loading-text" role="status" aria-live="polite">
            <template v-if="preparationStage !== 'complete'">{{ preparationMessage }}</template>
            <template v-else-if="loadingTotal > 0">{{ t('player.cachedSegments', { cached: loadingCached, total: loadingTotal }) }}</template>
            <template v-else>{{ t('player.connecting') }} {{ loadingElapsed }}s</template>
          </div>
          <div class="loading-speed" v-if="loadingSpeed">{{ loadingSpeed }}</div>
          <div class="loading-bar-wrap" v-if="loadingTotal > 0">
            <div class="loading-bar-fill"
              :style="{ width: (loadingTotal > 0 ? loadingCached / loadingTotal * 100 : 0) + '%' }">
            </div>
          </div>
        </div>
      </div>
    </div>

    <div v-if="errorMsg" class="player-error">
      <span>⚠</span>
      <span>{{ errorMsg }}</span>
      <div v-if="showNetworkError" class="error-actions">
        <button class="error-btn error-btn-primary" @click.stop="retryPlayback()">{{ t('player.retryPlayback') }}</button>
      </div>
    </div>

    <!-- B 站风格：底部左侧继续播放提示（小胶囊，仅在有记忆时出现） -->
    <div v-if="showResumePrompt && !mobile" class="resume-bili" @click.stop>
      <span class="resume-bili-text">{{ t('player.resumeLocatedAt') }} <b>{{ savedTime != null ? fmt(savedTime) : '00:00' }}</b></span>
      <button class="resume-bili-link" @click.stop="jumpToSavedTime(false)">{{ t('player.jumpBack') }}</button>
      <button class="resume-bili-link" @click.stop="jumpToSavedTime(true)">{{ t('player.jumpBackRemember') }}</button>
      <button class="resume-bili-link resume-bili-dismiss" @click.stop="dismissResumePrompt">
        {{ t('player.playFromStart') }} ({{ resumeRemainSec }}s)
      </button>
    </div>

    <div v-if="mobile && showResumePrompt && !mobileLocked" class="cczj-mobile-resume" :class="{'cczj-mobile-resume-lowered':!controlsVisible}" @click.stop @pointerdown.stop>
      <button @click="jumpToSavedTime(false)"><Icon name="play" :size="14" />{{ t('mobile.resume') }} <b>{{ savedTime != null ? fmt(savedTime) : '00:00' }}</b></button>
      <button :aria-label="t('common.close')" @click="dismissResumePrompt"><Icon name="close" :size="16" /></button>
    </div>
    <CczjMobileGestureOverlay v-if="mobile" :kind="mobileScrubTime >= 0 ? 'seek' : gestureKind" :hud="mobileScrubTime >= 0 ? t('mobile.gestureSeek') : gestureHud"
      :level="gestureLevel" :compact="gestureCompact" :target="mobileScrubTime >= 0 ? mobileScrubTime : gestureSeekTarget" :duration="duration"
      :image="thumbPreviewImg" :image-visible="thumbPreviewVisible" :preview-state="thumbPreviewState" :image-time="thumbPreviewTime" />
    <button v-if="mobile && isFullscreen && (controlsVisible || mobileLocked)" class="cczj-mobile-player-lock" :aria-label="t(mobileLocked ? 'mobile.unlock' : 'mobile.lock')" :aria-pressed="mobileLocked" @click.stop="gestures.toggleLock()" @pointerdown.stop><Icon :name="mobileLocked ? 'lock' : 'unlock'" :size="21" /></button>
    <!-- 画质切换 toast（左下角，1.5 秒自动消失） -->
    <div v-show="qualityToastText" class="quality-toast">
      <span>{{ qualityToastText }}</span>
    </div>

    <PlayerEnhancementWarning v-if="showAiWarning" :open="showAiWarning" @cancel="cancelAiMode" @confirm="confirmAiMode" />

    <!-- 暂停图标：右下角大字，仅暂停时显示 -->
    <button v-show="prepared && !autoplayStarting && !playing && !loading && (!mobile || !mobileLocked)" type="button" class="pause-overlay" :aria-label="t('player.play')" @pointerdown.stop @click.stop="togglePlay">
      <img :src="pauseImg" class="pause-icon" alt="" />
    </button>

    <!-- 进度条（独立行，在控制条上方） -->
    <Transition name="cczj-mobile-player-chrome"><div v-if="mobile" v-show="!mobileLocked && controlsVisible" class="cczj-mobile-control-shade" /></Transition>
    <!-- ⭐ @mousemove.stop 阻止冒泡到 player-wrapper，避免悬停进度条时触发控制栏和音量弹出 -->
    <Transition name="cczj-mobile-player-chrome" :css="mobile">
    <div :class="mobile ? 'cczj-mobile-control-stack' : 'cczj-player-desktop-control-stack'" v-show="!mobile || (!mobileLocked && controlsVisible)">
    <div class="progress-bar-wrapper" v-show="mobile || (!mobileLocked && (controlsVisible || qualityOpen || showVideoInfo))" @click.stop
      @mousedown.stop @dblclick.stop @mousemove.stop>
      <div class="progress-container" ref="progressContainerRef" @mousedown.stop="onProgressMouseDown"
        @mousemove="!mobile && onProgressHover($event)" @mouseleave="!mobile && onProgressLeave()">
        <div class="progress-track-bg"></div>
        <div class="progress-buffer" :style="{ width: bufferPct + '%' }"></div>
        <div class="progress-played" :style="{ width: displayedProgress + '%' }"></div>
        <!-- 悬停预览线 + 时间提示 + 缩略图 -->
        <div v-show="progressHoverPct >= 0" class="progress-hover-line" :style="{ left: progressHoverPct + '%' }"></div>
        <div v-show="!mobile && progressHoverPct >= 0 && progressHoverTime >= 0" class="progress-time-preview"
          :style="{ left: progressHoverPct + '%' }">
          <canvas v-show="false" ref="thumbCanvasRef"></canvas>
          <img v-if="thumbPreviewVisible && thumbPreviewImg" class="preview-thumb-img" :src="thumbPreviewImg" alt="" />
          <span class="preview-time">{{ fmt(progressHoverTime) }}</span>
        </div>
        <!-- 当前播放位置的“独特”指示点（白色内圆 + 蓝色光晕 + 外圈） -->
        <div class="progress-thumb" :style="{ left: displayedProgress + '%' }" :class="{ playing: playing }">
          <span class="thumb-halo"></span>
          <span class="thumb-core"></span>
          <span class="thumb-ring"></span>
        </div>
        <input class="progress-slider" type="range" :aria-label="t('player.currentTimeLabel')" min="0" :max="duration || 0" step="0.1" :value="mobileScrubTime >= 0 ? mobileScrubTime : current"
          @input="onProgressInput" @pointerdown="onProgressPointerDown" @pointerup="onProgressPointerEnd" @pointercancel="onProgressPointerEnd" />
      </div>
    </div>

    <!-- 底部控制条 -->
    <!-- ⭐ @wheel.stop 阻止 wheel 从控制栏/弹出面板冒泡到 player-wrapper，
         避免音量滑块和弹出面板内的滚动被 onWheel 劫持 -->
    <CczjMobilePlayerChrome v-if="mobile" ref="mobileChrome" :playback="controlState" :source="controlSource" :commands="mobileCommands" :host="wrapperRef"
      @tools-host="mobileMoreTarget=$event" @tools-closed="closeSharedTools" @category-changed="showSubtitlePanel=showReportAd=false" @quick-opened="closeSharedTools">
      <template v-if="danmakuKey" #danmaku><PlayerDanmakuButtons mobile :enabled="!!danmakuRef?.enabled" :ready="!!danmakuRef?.ready" :expanded="!!danmakuRef?.engaged" @toggle="danmakuRef?.toggle();keepVisible()" @settings="(anchor,hover)=>{danmakuRef?.open(anchor,hover);keepVisible()}" @leave="danmakuRef?.scheduleClose()" @dismiss="danmakuRef?.close()" /></template>
      <template v-if="$slots.episodes" #episodes="slotProps"><slot name="episodes" :close="slotProps.close" /></template>
      <template v-if="$slots.lines" #lines="slotProps"><slot name="lines" :close="slotProps.close" /></template>
    </CczjMobilePlayerChrome>
    </div>
    </Transition>
    <PlayerDesktopControls v-if="!mobile" ref="desktopControls" :playback="controlState" :commands="desktopCommands"
      :visible="showControls || !playing || qualityOpen || showVideoInfo || showPlaybackSettings || showSubtitlePanel || showReportAd || !!danmakuRef?.engaged"
      @tools-host="desktopToolsTarget=$event">
      <template v-if="danmakuKey" #danmaku><PlayerDanmakuButtons :enabled="!!danmakuRef?.enabled" :ready="!!danmakuRef?.ready" :expanded="!!danmakuRef?.engaged" @toggle="danmakuRef?.toggle();keepVisible()" @settings="(anchor,hover)=>{danmakuRef?.open(anchor,hover);keepVisible()}" @leave="danmakuRef?.scheduleClose()" @dismiss="danmakuRef?.close()" /></template>
      <template #quality><PlayerQualityControls :mobile="false" :value="qualityDropdownValue" :options="qualityOptions" @change="onQualityChange" @open="qualityOpen=$event" /></template>
    </PlayerDesktopControls>

    <!-- 报告广告成功 toast -->
      <Teleport v-if="mobile ? !!mobileMoreTarget : !!desktopToolsTarget" :to="(mobile ? mobileMoreTarget : desktopToolsTarget)!">
        <div class="player-extra-controls" style="display: contents" :class="{ 'cczj-mobile-more-actions': mobile }">
          <template v-if="mobile">
            <PlayerQualityControls v-show="mobileMoreCategory==='quality'" :mobile="true" :value="qualityDropdownValue" :options="qualityOptions" @change="selectMobileQuality" />
            <template v-if="mobileMoreCategory==='tools'">
            <button v-if="!isFullscreen && controlState.canCast" class="ctrl-btn" :aria-label="t('mobile.cast')" @click="mobileChrome?.toggle('cast')"><Icon name="cast" :size="18" /></button>
            </template>
          </template>

      <button v-if="qualityMode !== 'original' && hasPipeline() && (!mobile || mobileMoreCategory==='quality')" class="ctrl-btn" :class="{ active: compareEnabled }"
        @click.stop="toggleEnhancementCompare" :aria-label="t('player.compareToggleTip')" :title="t('player.compareToggleTip')">
        <Icon name="layers" :size="16" />
      </button>

      <PlayerPreferencesControls v-show="!mobile || mobileMoreCategory==='playback'" v-model:open="showPlaybackSettings" :mobile="mobile" :autoplay="autoPlayEnabled" :auto-next="autoNextEnabled" :auto-resume="mobileAutoResume" @autoplay="toggleAutoPlay" @auto-next="toggleAutoNext" @auto-resume="toggleMobileAutoResume" @interaction="keepVisible" @shortcuts="showShortcutModal=true" />
      <PlayerSubtitleControls v-show="!mobile || mobileMoreCategory==='tools'" v-model:open="showSubtitlePanel" v-model:visible="subtitleVisible" :name="subtitleName" :count="subtitleCues.length" :error="subtitleError" @pick="openSubtitlePicker" @clear="clearSubtitle" @interaction="keepVisible" />
      <input ref="subtitleFileRef" type="file" accept=".srt,.vtt,text/plain" class="subtitle-file-input"
        @change="onSubtitleFileChosen" />
      <PlayerAdReportControls v-show="!mobile || mobileMoreCategory==='tools'" :open="showReportAd" :domains="reportAdDomains" @toggle="toggleReportAd" @report="doReportAd" @interaction="keepVisible" />
      <!-- 视频信息 -->
      <div v-show="!mobile || mobileMoreCategory==='tools'" class="video-info-group" @click.stop>
        <button class="ctrl-btn" @click.stop="showVideoInfo = !showVideoInfo; mobileChrome?.close('more'); keepVisible()" :aria-label="t('player.videoInfo')" :title="t('player.videoInfo')">
          <Icon name="info" :size="16" />
        </button>
      </div>
      <!-- 豆瓣评论（仅有豆瓣ID时显示） -->
      <button v-if="doubanId && !mobile" class="ctrl-btn" @click.stop="emit('showComments'); keepVisible()" :aria-label="t('player.doubanComments')" :title="t('player.doubanComments')">
        <Icon name="comment" :size="16" />
      </button>
      <!-- 画中画 -->
      <button v-if="!mobile" class="ctrl-btn" data-player-action="pip" :disabled="!controlState.canPip" @click="togglePiP()" :aria-label="isPiP ? t('player.exitPip') : t('player.pipWithHotkey')" :title="isPiP ? t('player.exitPip') : t('player.pipWithHotkey')" :class="{ active: isPiP }">
        <Icon :name="isPiP ? 'pip-exit' : 'pip'" :size="16" />
      </button>
        </div>
      </Teleport>

    <MotionTransition preset="fade">
      <div v-show="reportAdToast" class="report-ad-toast">
        <Icon name="check" :size="14" />
        <span>{{ reportAdToast }}</span>
      </div>
    </MotionTransition>

    <PlayerVideoInfoDialog v-if="infoDialogVisited" :open="showVideoInfo" :video-info="showVideoInfo ? videoInfo : null" @close="showVideoInfo=false" />
    <PlayerShortcutDialog v-if="!mobile && shortcutDialogVisited" :open="showShortcutModal" @close="showShortcutModal=false" />
  </div>
</template>

<style scoped src="../styles/components/player.css"></style>
