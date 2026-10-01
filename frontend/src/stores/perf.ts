import { computed, reactive, ref } from 'vue'
import { defineStore } from 'pinia'

// 播放质量读数。全部来自真实媒体事件与解码器帧计数，本次会话累计、重启归零：
// 诊断页要回答的是「我刚播的这片子卡不卡」，历史平均值反而会把最近的问题盖掉。
//
// 装载耗时用 performance.now() 计时：它比 Date.now() 细，且不受系统对时影响。
export const usePerfStore = defineStore('perf', () => {
  const loads = ref(0)
  const firstFrame = reactive({ count: 0, totalMs: 0, maxMs: 0 })
  const stalls = reactive({ count: 0, totalMs: 0 })
  const frames = reactive({ total: 0, dropped: 0 })
  const errors = reactive({ count: 0, lastKind: '', lastHost: '' })
  const watchedSec = ref(0)
  // 当前正在放的这一路是什么规格，供面板说明上面那些数字是在什么条件下测出来的。
  const current = reactive({ host: '', resolution: '', bitrateBps: 0 })

  // 一次装载只结算一个首播耗时：_loadStart 用掉就清零，否则 canplay 和 playing
  // 都上报会把均值拉低。0 表示当前没有在计时。
  let _loadStart = 0

  function beginLoad(host: string): void {
    loads.value++
    _loadStart = performance.now()
    if (host) current.host = host
  }

  function markFirstFrame(): void {
    if (!_loadStart) return
    const ms = Math.round(performance.now() - _loadStart)
    _loadStart = 0
    // 上限 60 秒：跨集残留的标记或系统休眠都能算出荒谬值，宁可丢掉这一样本。
    if (ms < 0 || ms > 60000) return
    firstFrame.count++
    firstFrame.totalMs += ms
    if (ms > firstFrame.maxMs) firstFrame.maxMs = ms
  }

  // 卡顿时长由播放器实测（waiting 到恢复播放之间），这里只结算，不猜。
  function addStall(ms: number): void {
    if (ms <= 0) return
    stalls.count++
    stalls.totalMs += Math.round(ms)
  }

  function addFrames(totalDelta: number, droppedDelta: number): void {
    if (totalDelta > 0) frames.total += totalDelta
    if (droppedDelta > 0) frames.dropped += droppedDelta
  }

  function addWatched(sec: number): void {
    if (sec > 0) watchedSec.value += Math.round(sec)
  }

  function noteError(kind: string, host: string): void {
    errors.count++
    if (kind) errors.lastKind = kind
    if (host) errors.lastHost = host
  }

  function setMedia(resolution: string, bitrateBps: number): void {
    if (resolution) current.resolution = resolution
    if (bitrateBps > 0) current.bitrateBps = bitrateBps
  }

  const avgFirstFrameMs = computed(() =>
    firstFrame.count ? Math.round(firstFrame.totalMs / firstFrame.count) : 0)

  // 丢帧率按本会话累计解码帧算，样本不足 200 帧时不报：刚开播的几十帧里丢两帧是常态，
  // 报成一个刺眼的百分比毫无意义。
  const dropRate = computed(() =>
    frames.total >= 200 ? (frames.dropped / frames.total) * 100 : 0)

  return {
    loads, firstFrame, stalls, frames, errors, watchedSec, current,
    avgFirstFrameMs, dropRate,
    beginLoad, markFirstFrame, addStall, addFrames, addWatched, noteError, setMedia,
  }
})
