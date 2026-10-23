import { ref, onBeforeUnmount } from 'vue'
import type { PlayerPlatform } from './platform'

export function usePlayerVolume(deps:{platform:PlayerPlatform;video:()=>HTMLVideoElement|null;visible:()=>void;
 settings:{read:(key:string,fallback:string)=>string;write:(key:string,value:string)=>unknown}}) {
const volume=ref(1),muted=ref(false)
function restorePlaybackVolume(video: HTMLVideoElement): void {
  const native = deps.platform.device.read()
  if (native) {
    video.volume = 1; video.muted = false
    volume.value = native.volume; muted.value = native.volume === 0
    return
  }
  const savedVol = parseFloat(deps.settings.read('volume', '1'))
  if (isFinite(savedVol) && savedVol >= 0 && savedVol <= 1) {
    try { video.volume = savedVol; volume.value = savedVol } catch { /* ignore */ }
  }
  const savedMuted = deps.settings.read('muted', '0') === '1'
  try { video.muted = savedMuted; muted.value = savedMuted } catch { /* ignore */ }
}
function setMobileVolume(value: number): void {
  deps.platform.device.setVolume(value)
  volume.value = value; muted.value = value === 0
  const video = deps.video()
  if (video && value > 0) { video.volume = 1; video.muted = false }
}

// 滚轮调整音量：向上=+2%，向下=-2%
function onWheel(e: WheelEvent): void {
  const v = deps.video()
  if (!v) return
  const delta = e.deltaY < 0 ? 0.02 : -0.02
  v.volume = Math.max(0, Math.min(1, v.volume + delta))
  if (v.volume > 0 && v.muted) { v.muted = false }
  if (v.volume === 0 && !v.muted) { v.muted = true }
  volume.value = v.volume
  muted.value = v.muted
  deps.visible()
  // ⭐ volumechange 事件会统一触发 showVolumeToastRef()，此处不再重复调用
}

let nativeVolumeBeforeMute = .5
function toggleMute(): void {
  const native = deps.platform.device.read()
  if (native) {
    if (native.volume > 0) nativeVolumeBeforeMute = native.volume
    volume.value = native.volume > 0 ? 0 : nativeVolumeBeforeMute
    muted.value = volume.value === 0
    setMobileVolume(volume.value)
    return
  }
  const v = deps.video()
  if (!v) return
  v.muted = !v.muted
  muted.value = v.muted
  if (!v.muted && v.volume === 0) {
    v.volume = 0.5
    volume.value = 0.5
  }
}

function changeVolume(e: Event): void {
  const v = deps.video()
  if (!v) return
  const target = e.target as HTMLInputElement
  const val = parseFloat(target.value)
  v.volume = val
  volume.value = val
  if (val > 0 && v.muted) { v.muted = false; muted.value = false }
  deps.visible()
}

// ⭐ 新：新的音量处理 + 可视化临时 toast
const showVolumeToast = ref(false)
const toastVolumePct = ref(100)
let _toastTimer: number | null = null
function onVolumeInput(e: Event): void {
  if (deps.platform.device.read()) {
    volume.value = Number((e.target as HTMLInputElement).value)
    muted.value = volume.value === 0
    setMobileVolume(volume.value)
    deps.visible()
    return
  }
  changeVolume(e)
  // ⭐ volumechange 事件已统一触发 showVolumeToastRef()
}
function showVolumeToastRef(): void {
  const v = deps.video()
  if (!v) return
  toastVolumePct.value = Math.round(v.volume * 100)
  showVolumeToast.value = true
  if (_toastTimer != null) window.clearTimeout(_toastTimer)
  _toastTimer = window.setTimeout(() => { showVolumeToast.value = false }, 1000)
}


onBeforeUnmount(()=>{if(_toastTimer!==null)clearTimeout(_toastTimer)})
return {volume,muted,restorePlaybackVolume,setMobileVolume,onWheel,toggleMute,onVolumeInput,showVolumeToastRef,showVolumeToast,toastVolumePct}
}
