import { onBeforeUnmount, ref, shallowRef } from 'vue'
import { usePerfStore } from '../stores/perf'
import { TsCache } from '../utils/tsCache'
import { extractBitrateFromUrl } from './useVideoInfo'

export function usePlaybackTelemetry(deps:{video:()=>HTMLVideoElement|null;url:()=>string;cachedBitrate:()=>number;t:(key:string)=>string}) {
const perf=usePerfStore()
const t=deps.t
// ------ 播放质量取样 ------
// Decoder counters belong to the media element; align the baseline on every load,
// including source replacements that reuse that element.
let _perfFramesRaw = 0
let _perfDroppedRaw = 0
let _perfClockRaw = 0
let _perfSampleAt = 0
let _perfRateRaw = 1
let _stallSince = 0

function resetPerfBaseline(video: HTMLVideoElement): void {
  const q = video.getVideoPlaybackQuality?.()
  _perfFramesRaw = q ? q.totalVideoFrames : 0
  _perfDroppedRaw = q ? q.droppedVideoFrames : 0
  alignPlaybackClock(video)
  _stallSince = 0
}

// Align after an explicit seek so even short skips are not counted as watching.
function alignPlaybackClock(video: HTMLVideoElement): void {
  _perfClockRaw = video.currentTime
  _perfSampleAt = performance.now()
  _perfRateRaw = video.playbackRate
}

// Content seconds follow playback rate. A fixed two-second cutoff discarded
// valid 3x playback and rounded each 0.5x sample into a full second.
function samplePerf(video: HTMLVideoElement): void {
  const q = video.getVideoPlaybackQuality?.()
  if (q) {
    perf.addFrames(q.totalVideoFrames - _perfFramesRaw, q.droppedVideoFrames - _perfDroppedRaw)
    _perfFramesRaw = q.totalVideoFrames
    _perfDroppedRaw = q.droppedVideoFrames
  }
  const elapsed = Math.max(0, (performance.now() - _perfSampleAt) / 1000)
  if (!video.paused && !video.ended && !video.seeking) {
    const delta = video.currentTime - _perfClockRaw
    const expected = elapsed * Math.max(_perfRateRaw, video.playbackRate)
    if (delta > 0 && delta <= expected + .35) perf.addWatched(delta)
  }
  alignPlaybackClock(video)

  let bitrate = 0
  try {
    const hls = (video as any).__hls
    const idx = hls && hls.currentLevel >= 0 ? hls.currentLevel : hls?.nextAutoLevel
    const lvl = hls?.levels?.[idx] || hls?.levels?.[hls.levels.length - 1]
    bitrate = lvl?.bitrate || 0
  } catch { bitrate = 0 }
  if (!bitrate) bitrate = extractBitrateFromUrl(deps.url()) || deps.cachedBitrate()
  const w = video.videoWidth || 0
  const h = video.videoHeight || 0
  perf.setMedia(w && h ? `${w}x${h}` : '', bitrate)
}

// 卡顿结算：waiting 到恢复出声之间就是用户看到的「转圈」。seek 也算，
// 因为对播放器来说那同样是一段没有画面的等待。
function endStall(): void {
  if (!_stallSince) return
  perf.addStall(performance.now() - _stallSince)
  _stallSince = 0
}

// Loading indicator: HLS owns buffering; this poll only presents cache statistics.
const loadingCached = ref(0)          // 当前已缓存片段数
const loadingTotal = ref(0)           // 总片段数
const loadingSpeed = ref('')          // 下载速度文本
const loadingElapsed = ref(0)         // 已等待秒数
let loadingStatsTimer: number | null = null
let loadingStatsStartTime = 0
let loadingStatsLastBytes = 0
let loadingStatsSampleAt = 0

/** 启动加载统计轮询（用于显示缓冲进度和速度） */
function startLoadingStats(): void {
  if(loadingStatsTimer!==null)return
  loadingStatsStartTime = Date.now()
  loadingStatsSampleAt=loadingStatsStartTime
  loadingStatsLastBytes = TsCache.stats().bytes
  loadingElapsed.value = 0
  loadingSpeed.value = ''

  if (loadingStatsTimer) clearInterval(loadingStatsTimer)
  loadingStatsTimer = window.setInterval(() => {
    const s = TsCache.stats()
    loadingCached.value = s.entries
    loadingTotal.value = s.totalSegments
    loadingElapsed.value = Math.round((Date.now() - loadingStatsStartTime) / 1000)

    // 计算下载速度
    const bytesDelta = s.bytes - loadingStatsLastBytes
    loadingStatsLastBytes = s.bytes
    const now=Date.now()
    const intervalSec=Math.max(.001,(now-loadingStatsSampleAt)/1000)
    loadingStatsSampleAt=now
    if (bytesDelta > 0) {
      const speedBps = bytesDelta / intervalSec
      if (speedBps > 1024 * 1024) {
        loadingSpeed.value = (speedBps / 1024 / 1024).toFixed(1) + ' MB/s'
      } else {
        loadingSpeed.value = Math.round(speedBps / 1024) + ' KB/s'
      }
    } else if (loadingSpeed.value === '') {
      loadingSpeed.value = t('player.connecting')
    }
  }, 300)
}

function stopLoadingStats(): void {
  if (loadingStatsTimer) {
    clearInterval(loadingStatsTimer)
    loadingStatsTimer = null
  }
  loadingSpeed.value = ''
}

const cacheStats = shallowRef<{
  hits: number; misses: number; entries: number; bytes: number; hitRate: number;
  totalSegments: number; prefetched: number; prefetchTarget: number; queued: number; inflight: number;
}>({
  hits: 0, misses: 0, entries: 0, bytes: 0, hitRate: 0, totalSegments: 0, prefetched: 0,
  prefetchTarget: 0, queued: 0, inflight: 0,
})
let cacheStatsTimer: number | null = null
function formatBytes(n: number): string {
  if (n < 1024) return n + ' B'
  if (n < 1024 * 1024) return (n / 1024).toFixed(1) + ' KB'
  return (n / 1024 / 1024).toFixed(2) + ' MB'
}

function updateCacheStats(): void {
  const s = TsCache.stats()
  const changed = cacheStats.value.hits !== s.hits || cacheStats.value.misses !== s.misses
  cacheStats.value = {
    hits: s.hits, misses: s.misses, entries: s.entries, bytes: s.bytes,
    hitRate: s.hitRate, totalSegments: s.totalSegments, prefetched: s.entries,
    prefetchTarget: s.prefetchTarget, queued: s.queued, inflight: s.inflight,
  }
  if (changed && (s.hits + s.misses) > 0) {
    console.log(
      `[TsCache] 命中=${s.hits} 未命中=${s.misses} 命中率=${(s.hitRate * 100).toFixed(0)}% ` +
      `已缓存 ${s.entries} 片 / ${formatBytes(s.bytes)}`
    )
  }
  // 播放质量跟着这条已有的 1s 定时器取样，不再另开一个计时器。
  const v = deps.video()
  if (v) samplePerf(v)
}


function start():void {
  // Cache sampling must not stop an already running buffering indicator.
  if(cacheStatsTimer!==null)window.clearInterval(cacheStatsTimer)
  updateCacheStats()
  cacheStatsTimer=window.setInterval(updateCacheStats,1000)
}
function stop():void {
  if(cacheStatsTimer!==null)window.clearInterval(cacheStatsTimer)
  cacheStatsTimer=null
  stopLoadingStats()
}
function reset():void {
  stop()
  cacheStats.value={hits:0,misses:0,entries:0,bytes:0,hitRate:0,totalSegments:0,prefetched:0,prefetchTarget:0,queued:0,inflight:0}
}
onBeforeUnmount(stop)
return {cacheStats,loadingCached,loadingTotal,loadingSpeed,loadingElapsed,startLoadingStats,stopLoadingStats,
  resetPerfBaseline,alignPlaybackClock,endStall,beginStall:()=>{if(!_stallSince)_stallSince=performance.now()},start,stop,reset}
}
