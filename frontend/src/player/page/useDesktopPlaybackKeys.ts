import { onMounted, onBeforeUnmount } from 'vue'
import { readStorage } from '../../platform/storage'

export function useDesktopPlaybackKeys(deps: {
  enabled:boolean; video:()=>HTMLVideoElement|null; active:()=>boolean
  hasPrev:()=>boolean; hasNext:()=>boolean; previous:()=>void; next:()=>void
  fullscreen:()=>void; pip:()=>void
}) {
if (!deps.enabled) return
interface ShortcutMap {
  togglePlay: string[]
  seekBack: string[]
  seekForward: string[]
  seekBackBig: string[]
  seekForwardBig: string[]
  volumeUp: string[]
  volumeDown: string[]
  mute: string[]
  speedUp: string[]
  speedDown: string[]
  fullscreen: string[]
  prevEp: string[]
  nextEp: string[]
  pip: string[]
}

const DEFAULT_SHORTCUTS: ShortcutMap = {
  togglePlay: ['Space', 'K', 'k'],
  seekBack: ['ArrowLeft'],
  seekForward: ['ArrowRight'],
  seekBackBig: ['J', 'j'],
  seekForwardBig: ['L', 'l'],
  volumeUp: ['ArrowUp'],
  volumeDown: ['ArrowDown'],
  mute: ['M', 'm'],
  speedUp: [']'],
  speedDown: ['['],
  fullscreen: ['F', 'f'],
  prevEp: ['P', 'p'],
  nextEp: ['N', 'n'],
  pip: ['I', 'i'],
}

function loadShortcuts(): ShortcutMap {
  try {
    const parsed = readStorage<Record<string, string[]>>('cczj_shortcuts', {})
    if (parsed) {
      if (parsed && typeof parsed === 'object') {
        // 合并默认值，确保所有键都存在
        const merged = { ...DEFAULT_SHORTCUTS }
        for (const key of Object.keys(merged) as (keyof ShortcutMap)[]) {
          if (Array.isArray(parsed[key]) && parsed[key].length > 0) {
            // 将 Space 规范化，并且同时加入小写版本（对于字母键）
            const keys: string[] = []
            for (const k of parsed[key]) {
              if (typeof k !== 'string' || !k) continue
              keys.push(k)
              if (k.length === 1) keys.push(k.toLowerCase())
            }
            if (keys.length) merged[key] = keys
          }
        }
        return merged
      }
    }
  } catch { }
  return DEFAULT_SHORTCUTS
}

function normalizeKey(key: string): string {
  if (key === ' ') return 'Space'
  return key
}

function matchShortcut(action: keyof ShortcutMap, key: string, shortcuts: ShortcutMap): boolean {
  const normalizedKey = normalizeKey(key)
  return shortcuts[action].some(k => k === normalizedKey || k.toLowerCase() === key.toLowerCase())
}

/* ==================== 鼠标位置跟踪（用于键盘快捷键作用域） ==================== */
function onPageKeyDown(e: KeyboardEvent): void {
  if (e.defaultPrevented || (e.target instanceof Element && e.target.closest('[role="dialog"], [contenteditable="true"]'))) return
  // 键盘焦点在输入框时不响应
  const activeTag = (document.activeElement?.tagName || '').toLowerCase()
  if (activeTag === 'input' || activeTag === 'textarea') return

  // ESC 总是允许退出全屏（由 VideoPlayer 组件内部处理）
  // 其他键需要鼠标位于播放器区域内才响应
  const v = deps.video()
  if (!v) return

  const sc = loadShortcuts()
  const key = e.key

  if (matchShortcut('togglePlay', key, sc)) {
    if (!deps.active()) return
    e.preventDefault()
    if (v.paused) void v.play().catch(error => {
      // Interrupted play requests are expected during source replacement. Real
      // media failures also reach the player's existing error handler.
      if (error?.name !== 'AbortError') console.warn('[Player] Keyboard play failed:', error)
    })
    else v.pause()
  } else if (matchShortcut('seekBack', key, sc)) {
    if (!deps.active()) return
    e.preventDefault()
    v.currentTime = Math.max(0, v.currentTime - 5)
    v.dispatchEvent(new CustomEvent('cczj-seek-osd', { detail: { delta: -5 } }))
  } else if (matchShortcut('seekForward', key, sc)) {
    if (!deps.active()) return
    e.preventDefault()
    v.currentTime = Math.min(v.duration || 0, v.currentTime + 5)
    v.dispatchEvent(new CustomEvent('cczj-seek-osd', { detail: { delta: 5 } }))
  } else if (matchShortcut('seekBackBig', key, sc)) {
    if (!deps.active()) return
    e.preventDefault()
    v.currentTime = Math.max(0, v.currentTime - 30)
    v.dispatchEvent(new CustomEvent('cczj-seek-osd', { detail: { delta: -30 } }))
  } else if (matchShortcut('seekForwardBig', key, sc)) {
    if (!deps.active()) return
    e.preventDefault()
    v.currentTime = Math.min(v.duration || 0, v.currentTime + 30)
    v.dispatchEvent(new CustomEvent('cczj-seek-osd', { detail: { delta: 30 } }))
  } else if (matchShortcut('volumeUp', key, sc)) {
    if (!deps.active()) return
    e.preventDefault()
    v.volume = Math.min(1, v.volume + 0.05)
  } else if (matchShortcut('volumeDown', key, sc)) {
    if (!deps.active()) return
    e.preventDefault()
    v.volume = Math.max(0, v.volume - 0.05)
  } else if (matchShortcut('mute', key, sc)) {
    if (!deps.active()) return
    e.preventDefault()
    v.muted = !v.muted
  } else if (matchShortcut('speedUp', key, sc)) {
    if (!deps.active()) return
    e.preventDefault()
    const newRate = Math.min(4, v.playbackRate + 0.25)
    v.playbackRate = Math.round(newRate * 100) / 100
  } else if (matchShortcut('speedDown', key, sc)) {
    if (!deps.active()) return
    e.preventDefault()
    const newRate = Math.max(0.25, v.playbackRate - 0.25)
    v.playbackRate = Math.round(newRate * 100) / 100
  } else if (matchShortcut('fullscreen', key, sc)) {
    if (!deps.active()) return
    e.preventDefault()
    deps.fullscreen()
  } else if (matchShortcut('pip', key, sc)) {
    if (!deps.active()) return
    e.preventDefault()
    deps.pip()
  } else if (matchShortcut('prevEp', key, sc)) {
    if (deps.hasPrev()) {
      e.preventDefault()
      deps.previous()
    }
  } else if (matchShortcut('nextEp', key, sc)) {
    if (deps.hasNext()) {
      e.preventDefault()
      deps.next()
    }
  } else if (e.key === 'Escape') {
    // 保留给系统和 VideoPlayer 内部使用（退出全屏）
  }
}


onMounted(() => document.addEventListener('keydown',onPageKeyDown))
onBeforeUnmount(() => document.removeEventListener('keydown',onPageKeyDown))
}
