import { ref } from 'vue'

/**
 * 屏幕中央的操作提示（音量/快进快退）。
 *
 * 监听必须由调用方把 AbortController 的 signal 传进来：OSD 自己 addEventListener 的话，
 * 换集时 destroyPlayerInternal 那次 abort 管不到它们，同一个 video 上就会越挂越多份
 * volumechange，一次调音量能叠出好几个提示。
 */
export function usePlayerOsd() {
  const osdText = ref('')
  const osdIcon = ref('')
  let _osdTimer: ReturnType<typeof setTimeout> | null = null

  function showOsd(icon: string, text: string, duration = 800): void {
    osdIcon.value = icon
    osdText.value = text
    if (_osdTimer) clearTimeout(_osdTimer)
    _osdTimer = setTimeout(() => { osdText.value = ''; osdIcon.value = '' }, duration)
  }

  function bindOsdListeners(video: HTMLVideoElement, signal: AbortSignal): void {
    video.addEventListener('volumechange', () => {
      const pct = Math.round(video.volume * 100)
      showOsd(video.muted ? '🔇' : '🔊', `${pct}%`)
    }, { signal })

    // 自定义 seek OSD 事件（由 Player.vue 键盘快捷键派发）
    video.addEventListener('cczj-seek-osd', ((e: CustomEvent) => {
      const { delta } = e.detail
      const icon = delta > 0 ? '⏩' : '⏪'
      const absSec = Math.abs(delta)
      showOsd(icon, `${delta > 0 ? '+' : '-'}${absSec}s`)
    }) as EventListener, { signal })
  }

  function clearOsdTimer(): void {
    if (_osdTimer) { clearTimeout(_osdTimer); _osdTimer = null }
  }

  return { osdText, osdIcon, showOsd, bindOsdListeners, clearOsdTimer }
}
