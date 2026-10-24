import { computed, type Ref } from 'vue'
import { TsCache } from '../utils/tsCache'
import { formatPlaybackTime as fmt } from './videoInfoFormat'
export interface StreamVariantInfo { bandwidth: number; resolution: string; codecs: string; url: string }

// 从 TS URL 推断码率（优先匹配 "数字+k" 模式，如 2100k, 3000K）
export function extractBitrateFromUrl(url: string): number {
  try {
    const path = new URL(url).pathname
    // 优先: /2100k/ 或 _3000K. 等明确的码率标记
    const mk = path.match(/[\/_.-](\d{3,5})[kK][\/_.-]/)
    if (mk) {
      const val = parseInt(mk[1])
      if (val >= 200 && val <= 50000) return val * 1000
    }
    // 其次: 2100k 作为路径段
    const segments = path.split('/').filter(Boolean)
    for (const seg of segments) {
      const m = seg.match(/^(\d{3,5})[kK]$/)
      if (m) {
        const val = parseInt(m[1])
        if (val >= 200 && val <= 50000) return val * 1000
      }
    }
  } catch { }
  return 0
}


export function useVideoInfo(deps: {
  getVideoEl: () => HTMLVideoElement | null
  getUrl: () => string
  duration: Ref<number>; current: Ref<number>; muted: Ref<boolean>; volume: Ref<number>; speed: Ref<number>
  streams: Ref<StreamVariantInfo[]>
  cacheStats: Ref<{ entries: number; totalSegments: number; bytes: number; hitRate: number }>
  qualityLabel: () => string
  t: (key: string, values?: Record<string, unknown>) => string
}) {
  const { getVideoEl, duration, current, muted, volume, speed, cacheStats, t } = deps
  const m3u8StreamInfo = deps.streams
  const formatBytes = (n: number) => n < 1024 ? n + ' B' : n < 1024 * 1024 ? (n / 1024).toFixed(1) + ' KB' : (n / 1024 / 1024).toFixed(2) + ' MB'
  // 从 m3u8 缓存的 TS 片段 URL 中提取码率
  function extractBitrateFromCachedTs(): number {
    try {
      const cached = TsCache.getM3u8FromCache(deps.getUrl())
      if (cached) {
        const base = deps.getUrl().substring(0, deps.getUrl().lastIndexOf('/') + 1)
        const lines = cached.split('\n')
        for (const line of lines) {
          const t = line.trim()
          if (!t || t.startsWith('#')) continue
          try {
            const abs = new URL(t, base).href
            const br = extractBitrateFromUrl(abs)
            if (br > 0) return br
          } catch { }
        }
      }
    } catch { }
    return 0
  }

  // ========= 视频信息弹出面板 =========
  const videoInfo = computed(() => {
    const v = getVideoEl()
    if (!v) return null
    const dur = duration.value
    const cur = current.value
    const pct = dur > 0 ? ((cur / dur) * 100).toFixed(1) : '0'
    // 分辨率
    const videoWidth = v.videoWidth || 0
    const videoHeight = v.videoHeight || 0
    const resolution = videoWidth && videoHeight ? `${videoWidth} x ${videoHeight}` : ''
    // 帧率
    let fps = ''
    let droppedFrames = 0
    try {
      const q = (v as any).getVideoPlaybackQuality?.()
      if (q && q.totalVideoFrames > 10 && cur > 2) {
        droppedFrames = q.droppedVideoFrames || 0
        const total = q.totalVideoFrames
        const rate = total > 0 ? ((total - droppedFrames) / total * 100).toFixed(0) : '100'
        const fpsNum = Math.round(total / cur)
        if (fpsNum >= 15 && fpsNum <= 120) fps = fpsNum + ' fps (' + t('player.smoothness') + ' ' + rate + '%)'
      }
    } catch { }

    // 码率和编码 - 多层级获取
    let streamBitrate = ''    // 源声明的码率 (URL/m3u8)
    let currentBitrate = ''   // hls.js 当前播放级别的码率
    let codec = ''

    // 1) 从 URL 提取码率
    let urlBitrate = extractBitrateFromUrl(deps.getUrl())
    // 1b) m3u8 URL 本身无码率标记 → 从缓存的 TS 片段 URL 提取
    if (urlBitrate === 0) {
      urlBitrate = extractBitrateFromCachedTs()
    }
    if (urlBitrate > 0) {
      streamBitrate = urlBitrate >= 1000000 ? (urlBitrate / 1000000).toFixed(1) + ' Mbps' : Math.round(urlBitrate / 1000) + ' Kbps'
    }
    // 2) 从 m3u8 streamInfo 提取
    if (!streamBitrate && m3u8StreamInfo.value.length > 0) {
      const best = m3u8StreamInfo.value.reduce((a, b) => a.bandwidth > b.bandwidth ? a : b)
      if (best.bandwidth > 0) {
        streamBitrate = best.bandwidth >= 1000000 ? (best.bandwidth / 1000000).toFixed(1) + ' Mbps' : Math.round(best.bandwidth / 1000) + ' Kbps'
      }
      if (best.codecs) {
        const vc = best.codecs.split(',')[0].split('.')[0]
        if (vc.startsWith('avc')) codec = 'H.264 (AVC)'
        else if (vc.startsWith('hev') || vc.startsWith('hvc')) codec = 'H.265 (HEVC)'
        else if (vc.startsWith('av01') || vc.startsWith('av1')) codec = 'AV1'
        else codec = best.codecs.split(',')[0]
      }
    }

    // 3) hls.js 当前级别信息
    let hlsLevelInfo = ''
    let hlsLevelsCount = 0
    try {
      const hls = (v as any).__hls
      if (hls && hls.levels && hls.levels.length > 0) {
        hlsLevelsCount = hls.levels.length
        const lvlIdx = hls.currentLevel >= 0 ? hls.currentLevel : hls.nextAutoLevel
        const lvl = hls.levels[lvlIdx] || hls.levels[hls.levels.length - 1]
        if (lvl) {
          if (lvl.bitrate) {
            const bps = lvl.bitrate
            currentBitrate = bps >= 1000000 ? (bps / 1000000).toFixed(1) + ' Mbps' : Math.round(bps / 1000) + ' Kbps'
          }
          if (!codec && lvl.videoCodec) {
            const vc = lvl.videoCodec.split('.')[0]
            if (vc.startsWith('avc')) codec = 'H.264 (AVC)'
            else if (vc.startsWith('hev') || vc.startsWith('hvc')) codec = 'H.265 (HEVC)'
            else if (vc.startsWith('av01') || vc.startsWith('av1')) codec = 'AV1'
            else if (vc.startsWith('vp9') || vc.startsWith('vp09')) codec = 'VP9'
            else codec = lvl.videoCodec
          }
          hlsLevelInfo = `Level ${lvlIdx + 1}/${hlsLevelsCount}`
        }
      }
    } catch { }

    // 缓冲进度
    let buffered = ''
    let bufferedSec = 0
    try {
      if (v.buffered.length > 0) {
        const bufEnd = v.buffered.end(v.buffered.length - 1)
        bufferedSec = bufEnd - cur
        const bufPct = dur > 0 ? ((bufEnd / dur) * 100).toFixed(0) : '0'
        buffered = bufPct + '%'
        if (bufferedSec > 0) buffered += ` (${t('player.bufferedAvailable', { time: fmt(bufferedSec) })})`
      }
    } catch { }

    // 源域名 + 完整源 URL
    let sourceHost = ''
    let sourceUrl = ''
    try {
      const u = new URL(deps.getUrl())
      sourceHost = u.hostname
      sourceUrl = deps.getUrl().length > 80 ? deps.getUrl().slice(0, 77) + '...' : deps.getUrl()
    } catch { }

    // 缓存统计
    const cs = cacheStats.value
    const cacheInfo = t('player.cacheInfoLine', { cached: cs.entries, total: cs.totalSegments, bytes: formatBytes(cs.bytes), rate: (cs.hitRate * 100).toFixed(0) })
    const cacheMode = cs.totalSegments > 0 ? t('player.tscacheActive') : t('player.notActive')

    // 网络模式
    let networkMode = ''
    try {
      const mode = TsCache.getNetworkMode()
      networkMode = t(mode === 'normal' ? 'player.networkNormal' : mode === 'bandwidth_limited' ? 'player.networkLimited' : 'player.networkUnstable')
    } catch { }

    // 画质模式
    const qm = deps.qualityLabel()
    return {
      duration: fmt(dur),
      current: fmt(cur),
      progress: pct + '%',
      resolution,
      fps,
      droppedFrames,
      streamBitrate,
      currentBitrate,
      codec,
      qualityMode: qm,
      volume: Math.round((muted.value ? 0 : volume.value) * 100) + '%',
      speed: speed.value + 'x',
      buffered,
      bufferedSec,
      sourceHost,
      sourceUrl,
      hlsLevelInfo,
      hlsLevelsCount,
      cacheInfo,
      cacheMode,
      networkMode,
    }
  })
  return { videoInfo, extractBitrateFromCachedTs }
}
