<script setup lang="ts">
import { ref, onMounted, onUnmounted, KeepAlive } from 'vue'
import { useRouter } from 'vue-router'
import TitleBar from './components/TitleBar.vue'
import Sidebar from './components/Sidebar.vue'
import ErrorToastStack from './components/ErrorToastStack.vue'
import ConfirmModal from './components/ConfirmModal.vue'
import Icon from './components/Icon.vue'
import BackToTop from './components/BackToTop.vue'
import { useCollectStore } from './stores/collect'
import { useErrorStore } from './stores/error'
import { useThemeStore } from './stores/theme'
import { useDownloadStore } from './stores/download'

// 仅缓存"列表型"页面（Home/Search/Sources/...这些展示视频/源/下载/历史/收藏/设置的页面）。
// Player/Detail 每次进入都是不同的视频，不适合缓存，放在 exclude 里或不在 include 里。
// 所有被 include 的组件都必须显式 defineOptions({ name: 'Xxx' })，否则名字不可靠。
const KEEP_ALIVE_INCLUDE = [
  'Home',
  'Search',
  'Sources',
  'Favorites',
  'History',
  'Downloads',
  'Settings',
  'DevAdmin',
]

const router = useRouter()
const collect = useCollectStore()
const theme = useThemeStore()
const errorStore = useErrorStore()
const dl = useDownloadStore()

// 立即应用一次默认主题，防止页面初始闪烁
theme.apply()

// 下载悬浮球显示逻辑：任务<1 时不显示；任务>=1 且鼠标靠近右下角时显示
const fabVisible = ref(false)
function handleFabMouseMove(e: MouseEvent): void {
  if (dl.activeCount < 1) {
    fabVisible.value = false
    return
  }
  const proximity = 140 // 鼠标靠近的判定半径（像素）
  const w = window.innerWidth
  const h = window.innerHeight
  const dx = w - e.clientX
  const dy = h - e.clientY
  const dist = Math.sqrt(dx * dx + dy * dy)
  fabVisible.value = dist <= proximity
}
let hoverFab = false
function onFabEnter(): void {
  hoverFab = true
  fabVisible.value = true
}
function onFabLeave(): void {
  hoverFab = false
}

let winErrorHandler: ((e: ErrorEvent) => void) | null = null
let rejectHandler: ((e: PromiseRejectionEvent) => void) | null = null

onMounted(async () => {
  await theme.load()
  await dl.init()

  window.addEventListener('mousemove', handleFabMouseMove)

  // 把未捕获的 JS 错误 / Promise 拒绝都记录到日志与弹窗
  winErrorHandler = (event: ErrorEvent) => {
    if (event.error) {
      errorStore.fromError('运行时错误', event.error, 'GlobalErrorEvent')
    } else if (event.message) {
      errorStore.error('运行时错误', event.message, '', 'GlobalErrorEvent')
    }
  }
  rejectHandler = (event: PromiseRejectionEvent) => {
    const reason: any = event.reason
    errorStore.fromError('未处理的 Promise 异常', reason, 'UnhandledRejection')
  }
  window.addEventListener('error', winErrorHandler)
  window.addEventListener('unhandledrejection', rejectHandler)
})

onUnmounted(() => {
  window.removeEventListener('mousemove', handleFabMouseMove)
  if (winErrorHandler) window.removeEventListener('error', winErrorHandler)
  if (rejectHandler) window.removeEventListener('unhandledrejection', rejectHandler)
})
</script>

<template>
  <div class="app-shell">
    <!-- 自定义标题栏：最顶部一条，背景色与页面主背景一致 -->
    <TitleBar />

    <!-- 主体：侧边栏 + 内容 -->
    <div class="app-body">
      <Sidebar />
      <main class="main-content">
        <router-view v-slot="{ Component }">
          <KeepAlive :include="KEEP_ALIVE_INCLUDE">
            <component :is="Component" />
          </KeepAlive>
        </router-view>
      </main>
    </div>

    <!-- 全局采集进度条 -->
    <div v-if="collect.running || collect.done" class="collect-overlay" :class="{ done: collect.done }">
      <div class="collect-bar-inner">
        <div class="collect-header">
          <span class="collect-source">{{ collect.sourceKey }}</span>
          <span v-if="collect.running" class="collect-status running">采集中...</span>
          <span v-else-if="collect.error" class="collect-status error">失败</span>
          <span v-else class="collect-status success">完成</span>
        </div>
        <div class="progress-track">
          <div class="progress-fill" :style="{ width: collect.progress + '%' }"></div>
        </div>
        <div class="collect-meta">
          <span v-if="collect.total > 0">{{ collect.current }} / {{ collect.total }} 页</span>
          <span class="collect-last-log">{{ collect.log[collect.log.length - 1] }}</span>
        </div>
      </div>
    </div>

    <!-- 全局错误/消息弹窗栈（右侧从右滑入，向下堆叠） -->
    <ErrorToastStack />

    <!-- 全局自定义确认弹窗：通过 useConfirmStore().confirm(...) 调用 -->
    <ConfirmModal />

    <!-- 右下角下载悬浮球 -->
    <button
      class="download-fab"
      :class="{ 'fab-visible': fabVisible || hoverFab }"
      @click="router.push('/downloads')"
      @mouseenter="onFabEnter"
      @mouseleave="onFabLeave"
      title="下载管理"
    >
      <Icon name="download" :size="18" />
      <span v-if="dl.activeCount > 0" class="fab-badge">{{ dl.activeCount }}</span>
    </button>

    <!-- 左下角：回到顶部（滚动超过一定距离后出现，与下载悬浮球左右错开，不重叠） -->
    <BackToTop />
  </div>
</template>

<style>
/* ====== 全局主题变量：默认值供 store 未加载时使用；真正值由 theme store 注入 ======
 * 默认走浅色（绿意盎然）配色，避免 store 加载前闪烁暗色。
 */
/* ====== 设计令牌 (Design Tokens) ====== */
:root {
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

  /* 语义色 */
  --danger: #e53935;
  --danger-hover: #c62828;
  --success: #16a34a;
  --warning: #f59e0b;
  --warning-text: #b45309;
  --info: #0288d1;

  /* 透明度变体 */
  --accent-alpha-10: rgba(22,163,74,0.10);
  --accent-alpha-15: rgba(22,163,74,0.15);
  --accent-alpha-20: rgba(22,163,74,0.20);
  --accent-alpha-35: rgba(22,163,74,0.35);
  --success-alpha-10: rgba(22,163,74,0.10);
  --danger-alpha-10: rgba(229,57,53,0.10);
  --warning-alpha-10: rgba(245,158,11,0.10);

  /* 阴影层级 */
  --shadow-sm: 0 1px 3px rgba(31,36,48,0.06);
  --shadow: 0 6px 22px rgba(31,36,48,0.08);
  --shadow-lg: 0 12px 48px rgba(0,0,0,0.35);
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
  --transition-fast: 0.15s ease;
  --transition: 0.2s ease;
  --transition-slow: 0.3s ease;

  /* 窗口控制按钮 */
  --btn-hide: #3bc2b2;
  --btn-min: #85c43b;
  --btn-close: #fab4a0;

  /* 背景图 */
  --bg-image: none;
}

* { margin: 0; padding: 0; box-sizing: border-box; }

/* 渲染背景图：把 --bg-image 作为顶层背景图叠加在背景色上 */
html, body {
  height: 100%;
  margin: 0;
  background-color: var(--bg-primary);
  background-image: var(--bg-image);
  background-position: center;
  background-size: cover;
  background-repeat: no-repeat;
  background-attachment: fixed;
  color: var(--text-primary);
  font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', 'PingFang SC', 'Microsoft YaHei', sans-serif;
  overflow: hidden;
  -webkit-font-smoothing: antialiased;
  font-size: 14px;
  user-select: none;
  -webkit-user-select: none;
}

/* 允许在内容输入区、视频描述、以及真正的文字区域中选择文字 */
input, textarea, [contenteditable="true"], .allow-select, .video-description, .detail-description {
  user-select: text;
  -webkit-user-select: text;
}

#app {
  height: 100%;
  background-color: transparent;
  color: var(--text-primary);
  transition: background 0.3s, color 0.3s;
}

/* ====== 主体布局 ====== */
.app-shell {
  height: 100vh;
  background-color: var(--bg-primary);
  background-image: var(--bg-image, none);
  background-position: center;
  background-size: cover;
  background-repeat: no-repeat;
  background-attachment: fixed;
  display: flex;
  flex-direction: column;
  transition: background 0.3s;
}
.app-body {
  display: flex;
  flex: 1;
  min-height: 0;
  background: transparent;
}
.main-content {
  flex: 1;
  overflow-y: auto;
  padding: 20px 24px 24px;
  color: var(--text-primary);
  transition: background 0.3s;
}

/* 滚动条：跟随主题 */
::-webkit-scrollbar { width: 8px; height: 8px; }
::-webkit-scrollbar-track { background: transparent; }
::-webkit-scrollbar-thumb {
  background: var(--border-strong);
  border-radius: 4px;
  transition: background 0.2s;
}
::-webkit-scrollbar-thumb:hover { background: var(--accent); }

::selection { background: var(--accent); color: var(--accent-contrast); }

/* ====== 采集进度条 ====== */
.collect-overlay {
  position: fixed; bottom: 0; left: 0; right: 0; z-index: 999;
  background: var(--bg-card);
  border-top: 1px solid var(--accent);
  padding: 10px 24px;
  transition: all 0.3s;
}
.collect-overlay.done { border-top-color: var(--success); }
.collect-bar-inner { max-width: 1200px; margin: 0 auto; }
.collect-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 6px; }
.collect-source { font-size: 13px; font-weight: 600; color: var(--text-primary); }
.collect-status { font-size: 11px; padding: 3px 10px; border-radius: 10px; font-weight: 500; }
.collect-status.running { background: var(--accent-alpha-20); color: var(--accent); }
.collect-status.success { background: var(--success-alpha-10); color: var(--success); }
.collect-status.error { background: var(--danger-alpha-10); color: var(--danger); }
.progress-track { height: 4px; background: var(--border); border-radius: 2px; overflow: hidden; margin-bottom: 6px; }
.progress-fill { height: 100%; background: var(--accent); border-radius: 2px; transition: width 0.3s; }
.collect-overlay.done .progress-fill { background: var(--success); }
.collect-meta { display: flex; justify-content: space-between; font-size: 12px; color: var(--text-muted); }
.collect-last-log { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; max-width: 60%; }

/* ====== 右下角下载悬浮球 ====== */
.download-fab {
  position: fixed;
  right: 24px;
  bottom: 24px;
  width: 52px;
  height: 52px;
  border-radius: 50%;
  border: none;
  background: var(--accent);
  color: var(--accent-contrast);
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  box-shadow: 0 8px 24px rgba(0, 0, 0, 0.25), 0 0 0 4px var(--accent-alpha-10);
  z-index: 998;
  transition: transform 0.2s ease, box-shadow 0.2s ease, opacity 0.2s ease;
  font-family: inherit;
  /* 默认隐藏：任务<1 或鼠标未靠近时都不显示 */
  opacity: 0;
  transform: scale(0.6);
  pointer-events: none;
}
.download-fab.fab-visible {
  opacity: 1;
  transform: scale(1);
  pointer-events: auto;
}
.download-fab.fab-visible:hover {
  transform: scale(1.08);
  box-shadow: 0 10px 28px rgba(0, 0, 0, 0.3), 0 0 0 6px var(--accent-alpha-20);
}
.download-fab .fab-badge {
  position: absolute;
  top: -4px;
  right: -4px;
  min-width: 20px;
  height: 20px;
  padding: 0 6px;
  border-radius: 10px;
  background: #ef4444;
  color: #fff;
  font-size: 11px;
  font-weight: 600;
  display: flex;
  align-items: center;
  justify-content: center;
  line-height: 1;
  border: 2px solid var(--bg-primary, #fff);
}
</style>
