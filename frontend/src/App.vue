<script setup lang="ts">
import { onMounted, onUnmounted, computed, defineAsyncComponent, watch } from 'vue'
import { useRouter } from 'vue-router'
import { tr } from './locales'
import TitleBar from './components/TitleBar.vue'
import './player/shortDrama/presentation.css'
import { shortDramaScene } from './player/shortDrama/scene'
import { presentationProfile } from './platform/presentation'
import { useApplicationMode } from './modes/applicationMode'
import { APPLICATION_MODES, APPLICATION_MODE_KEYS, modeHome, routeMode } from './modes/definitions'

import SplashScreen from './components/SplashScreen.vue'
import ErrorToastStack from './components/ErrorToastStack.vue'
import ConfirmModal from './components/ConfirmModal.vue'
import { MotionTransition } from './components/ui'
import { useErrorStore } from './stores/error'
import { useThemeStore } from './stores/theme'
import { GetSetting } from './api/app'
import UpdateModal from './components/UpdateModal.vue'
import LegacyDataPrompt from './components/LegacyDataPrompt.vue'
import SchemaNewerPrompt from './components/SchemaNewerPrompt.vue'
import LicenseGate from './components/LicenseGate.vue'
import { useViewActivity } from './composables/useViewActivity'
import { isMobileRuntime } from './api/runtime'
import { cacheLaunchBackground } from './platform/bootAppearance'
import { disposeRecordingObserver, recordDiagnosticEvent, refreshRecordingStatus } from './diagnostics/recording'

// Keep native window visibility current even when the current page has no
// images or diagnostic panels mounted (for example during video playback).
const foreground = useViewActivity()
watch(foreground, visible => { void recordDiagnosticEvent('window_state', { visible }) })
onMounted(() => { void refreshRecordingStatus() })
onUnmounted(disposeRecordingObserver)


const appMode = useApplicationMode()
const modeShells = Object.fromEntries(APPLICATION_MODE_KEYS.map(mode => [mode, defineAsyncComponent(APPLICATION_MODES[mode].loadShell)]))

const activeShell = computed(() => modeShells[appMode.mode])

const router = useRouter()
const shortDramaPlayback = computed(() => shortDramaScene(router.currentRoute.value, presentationProfile().ui === 'mobile') === 'playback')
function onAndroidBack(event: Event): void {
  if (event.defaultPrevented) return
  // Dispatch overlay handling in a separate phase. Window listeners installed
  // by lazy route components must run before the already-mounted router owner.
  if (!window.dispatchEvent(new Event('cczj:android-overlay-back', { cancelable: true }))) {
    event.preventDefault()
    return
  }
  if (router.currentRoute.value.path === modeHome(appMode.mode)) return
  event.preventDefault()
  if(appMode.mode==='video' && router.currentRoute.value.path==='/settings' && router.currentRoute.value.query.group && router.options.history.state.back!=='/settings') { void router.replace('/settings');return }
  // Android WebView.canGoBack() can miss the first Vue hash-history entry.
  // Keep SPA navigation in the router; the native host owns root/backgrounding.
  const back=router.options.history.state.back
  if (typeof back === 'string'&&routeMode(back.split('?')[0]!)===appMode.mode) router.back()
  else void router.replace(modeHome(appMode.mode))
}
onMounted(() => {
  if (isMobileRuntime()) window.addEventListener('cczj:android-back', onAndroidBack)
})
onUnmounted(() => window.removeEventListener('cczj:android-back', onAndroidBack))
const theme = useThemeStore()
watch(()=>appMode.mode==='novel'?'#faf9f6':appMode.mode==='comic'?'#fffaf5':theme.current.palette.bgApp,color=>cacheLaunchBackground(color,appMode.mode),{immediate:true})
const errorStore = useErrorStore()

// 立即应用一次默认主题，防止页面初始闪烁
theme.apply()

let winErrorHandler: ((e: ErrorEvent) => void) | null = null
let rejectHandler: ((e: PromiseRejectionEvent) => void) | null = null

async function safeGetSetting(key: string, fallback: string): Promise<string> {
  try { const v = await GetSetting(key); return v || fallback } catch { return fallback }
}

onMounted(async () => {
  if (!theme.loaded) await theme.load()

  // 加载字体大小设置
  try {
    const fsk = await safeGetSetting('font_size_key', 'md')
    const fontSizePresetMap: Record<string, number> = {
      xs: 12, sm: 13, md: 14, lg: 16, xl: 18, xxl: 20,
    }
    const px = fontSizePresetMap[fsk] || 14
    document.documentElement.style.fontSize = px + 'px'
  } catch { /* ignore */ }
  // 加载字体设置
  try {
    const ff = await safeGetSetting('font_family', '')
    if (ff) {
      document.documentElement.style.fontFamily = ff
    } else {
      // 默认字体（与之前 CSS 中硬编码的保持一致）
      document.documentElement.style.fontFamily = '-apple-system, BlinkMacSystemFont, "Segoe UI", "PingFang SC", "Microsoft YaHei", sans-serif'
    }
  } catch { /* ignore */ }


  // 把未捕获的 JS 错误 / Promise 拒绝都记录到日志与弹窗
  winErrorHandler = (event: ErrorEvent) => {
    if (event.error) {
      errorStore.fromError(tr('errors.runtimeError'), event.error, 'GlobalErrorEvent')
    } else if (event.message) {
      errorStore.error(tr('errors.runtimeError'), event.message, '', 'GlobalErrorEvent')
    }
  }
  rejectHandler = (event: PromiseRejectionEvent) => {
    const reason: any = event.reason
    errorStore.fromError(tr('errors.unhandledRejection'), reason, 'UnhandledRejection')
  }
  window.addEventListener('error', winErrorHandler)
  window.addEventListener('unhandledrejection', rejectHandler)


})

onUnmounted(() => {
  if (winErrorHandler) window.removeEventListener('error', winErrorHandler)
  if (rejectHandler) window.removeEventListener('unhandledrejection', rejectHandler)
})
</script>

<template>
  <!-- 启动画面 -->
  <SplashScreen />

  <!-- 投放标记刻意不在这一层：带着它整窗都是投放区，拖到播放器上也会静默装一个扩展包。
       现在只有「设置 → 扩展包」那张卡片带 data-file-drop-target。 -->
  <div class="app-shell" :class="{'cczj-short-drama-surface':shortDramaPlayback}" :data-application-mode="appMode.mode">
    <!-- 自定义标题栏：最顶部一条，背景色与页面主背景一致 -->
    <TitleBar v-if="!isMobileRuntime()" />

    <component :is="activeShell" />

    <!-- 全局更新弹窗 -->
    <UpdateModal />

    <!-- 首启的旧数据找回提示：等条款闸门和更新弹窗都走完才开口（见组件里的 clear 闸门） -->
    <LegacyDataPrompt />

    <!-- 「这份数据比当前版本新」：降级或旧 exe 开新库时解释为什么列表缺一截，排在旧数据提示之后 -->
    <SchemaNewerPrompt />

    <!-- 首启许可条款闸门：没同意当前版本时盖在所有弹窗之上（挂在后面，同 z-index 下后画的赢） -->
    <LicenseGate />

    <!-- 全局错误/消息弹窗栈（右侧从右滑入，向下堆叠） -->
    <ErrorToastStack />

    <!-- 全局自定义确认弹窗：通过 useConfirmStore().confirm(...) 调用 -->
    <ConfirmModal />

  </div>
</template>

<style>
/* ====== 全局主题变量：默认值供 store 未加载时使用；真正值由 theme store 注入 ======
 * 默认走浅色（绿意盎然）配色，避免 store 加载前闪烁暗色。
 */
/* ====== 设计令牌 (Design Tokens) ====== */
:root {
  color-scheme: light;
  /* 背景 */
  --bg-primary: #e6f4ea;
  --bg-secondary: #f0f8f2;
  --bg-card: #ffffff;
  --bg-hover: #d1ebd8;
  --bg-input: #ffffff;
  --bg-overlay: rgba(15, 18, 28, 0.55);

  /* 边框 */
  --border: #d7e5dd;
  --border-strong: #a8c9b5;

  /* 文字 */
  --text-primary: #1f2430;
  --text-secondary: #4a5166;
  --text-muted: #8a92a6;

  /* 品牌色 */
  --accent: #16a34a;
  --accent-dim: #15803d;
  --accent-contrast: #ffffff;
  --accent-rgb: 22, 163, 74;

  /* 语义色 */
  --danger: #e53935;
  --danger-hover: #c62828;
  --danger-contrast: #ffffff;
  --success: #16a34a;
  --success-hover: #15803d;
  --success-contrast: #ffffff;
  --warning: #f59e0b;
  --warning-hover: #d97706;
  --warning-text: #b45309;
  --warning-contrast: #111827;
  --info: #0288d1;
  --info-hover: #0369a1;
  --info-contrast: #ffffff;

  /* 透明度变体 */
  --accent-alpha-10: rgba(22,163,74,0.10);
  --accent-alpha-15: rgba(22,163,74,0.15);
  --accent-alpha-20: rgba(22,163,74,0.20);
  --accent-alpha-35: rgba(22,163,74,0.35);
  --accent-alpha-40: rgba(22,163,74,0.40);
  --success-alpha-10: rgba(22,163,74,0.10);
  --danger-alpha-10: rgba(229,57,53,0.10);
  --warning-alpha-10: rgba(245,158,11,0.10);
  --info-alpha-10: rgba(2,136,209,0.10);

  /* Component tokens derived from the active palette. */
  --bg-tag: #e8f5eb;
  --border-light: #c5e6cf;
  --btn-solid: #16a34a;
  --btn-solid-text: #ffffff;
  --btn-solid-hover: #15803d;
  --btn-solid-alpha-20: rgba(22,163,74,0.20);
  --btn-solid-alpha-35: rgba(22,163,74,0.35);
  --btn-soft: #d1ebd8;
  --btn-soft-text: #166534;
  --tag-highlight-bg: rgba(22,163,74,0.18);
  --tag-highlight-text: #166534;
  --episode-bg: #e8f5eb;
  --episode-text: #166534;
  --carousel-control: #16a34a;
  --carousel-control-text: #ffffff;

  /* 阴影层级（默认主题 green 的派生值；apply() 会按当前主题覆写） */
  --shadow-sm: 0 1px 3px rgba(13, 98, 44, 0.14);
  --shadow: 0 6px 22px rgba(31,36,48,0.08);
  --shadow-lg: 0 12px 48px rgba(9, 65, 30, 0.24);
  --shadow-button: 0 2px 8px var(--accent-alpha-20);

  /* 圆角 */
  --radius-sm: 6px;
  --radius: 8px;
  --radius-lg: 12px;
  --radius-xl: 14px;
  --radius-full: 9999px;

  /* 间距 */
  --spacing-xs: 4px;
  --spacing-sm: 8px;
  --spacing: 12px;
  --spacing-md: 16px;
  --spacing-lg: 20px;
  --spacing-xl: 24px;

  /* 字体大小 */
  --font-xs: 11px;
  --font-sm: 12px;
  --font: 13px;
  --font-md: 14px;
  --font-lg: 16px;

  /* z-index 层级 */
  --z-sidebar: 100;
  --z-titlebar: 200;
  --z-fab: 998;
  --z-progress: 999;
  --z-toast: 1999;
  --z-modal-overlay: 10000;
  --z-modal: 10001;

  /* 过渡 */
  --transition-fast: var(--cczj-motion-fast) var(--cczj-motion-ease-standard);
  --transition: var(--cczj-motion-normal) var(--cczj-motion-ease-standard);
  --transition-slow: var(--cczj-motion-slow) var(--cczj-motion-ease-standard);

  /* 窗口控制按钮 */
  --btn-hide: #3bc2b2;
  --btn-hide-contrast: #111827;
  --btn-min: #85c43b;
  --btn-min-contrast: #111827;
  --btn-close: #fab4a0;
  --btn-close-contrast: #111827;

  /* 背景图 */
  --bg-image: none;
}

* { margin: 0; padding: 0; box-sizing: border-box; }

/* 渲染背景图：把 --bg-image 作为顶层背景图叠加在背景色上 */
html, body {
  @apply cczj-h-full;
  margin: 0;
  @apply cczj-bg-primary;
  background-image: var(--bg-image);
  background-position: center;
  background-size: cover;
  background-repeat: no-repeat;
  background-attachment: fixed;
  @apply cczj-text-primary cczj-overflow-hidden;
  -webkit-font-smoothing: antialiased;
  user-select: none;
  -webkit-user-select: none;
}

/* 允许在内容输入区、视频描述、以及真正的文字区域中选择文字 */
input, textarea, [contenteditable="true"], .allow-select, .video-description, .detail-description {
  user-select: text;
  -webkit-user-select: text;
}

#app {
  @apply cczj-h-full cczj-bg-transparent cczj-text-primary;
  transition: background 0.3s, color 0.3s;
}

/* ====== 主体布局 ====== */
.app-shell {
  /* Keep native resize hit areas outside scrollbars and interactive content. */
  padding: 0 6px 6px 0;
  box-sizing: border-box;
  @apply cczj-h-screen cczj-bg-primary;
  background-image: var(--bg-image, none);
  background-position: center;
  background-size: cover;
  background-repeat: no-repeat;
  background-attachment: fixed;
  @apply cczj-flex cczj-flex-col;
  transition: background 0.3s;
}
/* 拖放态的描边不再由这里画：投放区收到只有扩展包那张卡片那么大，
   高亮跟着卡片走（styles/components/extensions-panel.css）。 */
.app-body {
  @apply cczj-min-w-0 cczj-flex cczj-flex-1 cczj-min-h-0;
  background: transparent;
}
.main-content {
  @apply cczj-min-w-0 cczj-flex-1 cczj-overflow-y-auto cczj-overflow-x-hidden;
  /* 滚动条占 8px 且是经典式（挤版心）。启用一个扩展包常把内容顶过一屏，滚动条就在这时出现，
     整页版心窄 8px、每一行重排——用户看到的正是那一下「抖」。留出常驻槽位，出现与否不再改宽度。 */
  scrollbar-gutter: stable;
  padding: 20px 24px 24px;
  @apply cczj-text-primary;
  transition: background 0.3s;
}

/* 滚动条：跟随主题 */
::-webkit-scrollbar { @apply cczj-w-2 cczj-h-2; }
::-webkit-scrollbar-track { background: transparent; }
::-webkit-scrollbar-thumb {
  background: var(--border-strong);
  @apply cczj-rounded-sm;
  transition: background 0.2s;
}
::-webkit-scrollbar-thumb:hover { background: var(--accent); }

::selection { background: var(--accent); color: var(--accent-contrast); }

/* Shared fallback for legacy controls that have not yet migrated to a UI
 * component.  It gives focus, links, and native form fields the active theme
 * without changing their individual layouts. */
a { @apply cczj-text-accent; }
input, textarea, select {
  @apply cczj-text-primary;
  background-color: var(--bg-input);
  @apply cczj-border-gray-300;
}
input:focus-visible, textarea:focus-visible, select:focus-visible, button:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: 2px;
}

/* ====== 采集进度条 ====== */
.collect-overlay {
  @apply cczj-fixed cczj-bottom-0 cczj-left-0 cczj-right-0 cczj-z-999;
  background: var(--bg-card);
  border-top: 1px solid var(--accent);
  padding: 10px 24px;
  transition: all 0.3s;
}
.collect-overlay.done { border-top-color: var(--success); }
.collect-bar-inner { max-width: 1200px; margin: 0 auto; }
.collect-header { @apply cczj-flex cczj-justify-between cczj-items-center cczj-mb-3; }
.collect-source { @apply cczj-text-13 cczj-font-semibold cczj-text-primary; }
.collect-status { @apply cczj-text-11 cczj-px-5-py-2; border-radius: 10px; @apply cczj-font-medium; }
.collect-status.running { background: var(--accent-alpha-20); @apply cczj-text-accent; }
.collect-status.success { background: var(--success-alpha-10); @apply cczj-text-success; }
.collect-status.error { @apply cczj-bg-danger-alpha cczj-text-danger; }
.progress-track { height: 4px; @apply cczj-bg-border cczj-rounded-xs cczj-overflow-hidden cczj-mb-3; }
.progress-fill { @apply cczj-h-full; background: var(--accent); @apply cczj-rounded-xs; transition: width 0.3s; }
.collect-overlay.done .progress-fill { background: var(--success); }
.collect-meta { @apply cczj-flex cczj-justify-between cczj-text-sm cczj-text-muted; }
.collect-last-log { @apply cczj-overflow-hidden; text-overflow: ellipsis; @apply cczj-whitespace-nowrap; max-width: 60%; }

/* ====== 右下角下载悬浮球 ====== */
.download-fab {
  @apply cczj-fixed;
  right: 24px;
  bottom: 24px;
  width: 52px;
  height: 52px;
  @apply cczj-rounded-50;
  border: none;
  background: var(--accent);
  color: var(--accent-contrast);
  @apply cczj-flex cczj-items-center cczj-justify-center cczj-cursor-pointer;
  box-shadow: 0 8px 24px rgba(0, 0, 0, 0.25), 0 0 0 4px var(--accent-alpha-10);
  z-index: 998;
  transition: transform 0.2s ease, box-shadow 0.2s ease, opacity 0.2s ease;
  font-family: inherit;
  /* 默认隐藏：任务<1 或鼠标未靠近时都不显示 */
  @apply cczj-opacity-0;
  transform: scale(0.6);
  @apply cczj-pointer-events-none;
}
.download-fab.fab-visible {
  @apply cczj-opacity-100;
  transform: scale(1);
  @apply cczj-pointer-events-auto;
}
.download-fab.fab-visible:hover {
  transform: scale(1.08);
  box-shadow: 0 10px 28px rgba(0, 0, 0, 0.3), 0 0 0 6px var(--accent-alpha-20);
}
.download-fab .fab-badge {
  @apply cczj-absolute;
  top: -4px;
  right: -4px;
  min-width: 20px;
  height: 20px;
  padding: 0 6px;
  border-radius: 10px;
  background: #ef4444;
  @apply cczj-text-white cczj-text-11 cczj-font-semibold cczj-flex cczj-items-center cczj-justify-center cczj-leading-none;
  border: 2px solid var(--bg-primary, #fff);
}
body[data-player-fullscreen='1'] .app-shell { padding: 0; }
</style>
