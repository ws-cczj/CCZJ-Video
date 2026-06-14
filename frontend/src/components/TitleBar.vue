<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount } from 'vue'
import { Window } from '@wailsio/runtime'
import { WindowIsMax, WindowToggleMax } from '../../bindings/cczjVideo/app'
import Icon from './Icon.vue'

const isMaximized = ref(false)

async function refreshMaxState(): Promise<void> {
  try {
    isMaximized.value = await WindowIsMax()
  } catch {
    // 浏览器开发环境下忽略
  }
}

let resizeTimer: number | undefined = undefined
function onResize(): void {
  if (resizeTimer !== undefined) {
    window.clearTimeout(resizeTimer)
  }
  resizeTimer = window.setTimeout(() => {
    refreshMaxState()
  }, 80)
}

onMounted(() => {
  refreshMaxState()
  window.addEventListener('resize', onResize)
})

onBeforeUnmount(() => {
  window.removeEventListener('resize', onResize)
  if (resizeTimer !== undefined) {
    window.clearTimeout(resizeTimer)
  }
})

function onToggleMax(): void {
  try {
    WindowToggleMax()
  } catch {
    // 忽略
  }
}

function onMinimize(): void {
  try {
    Window.Minimise()
  } catch {
    // 忽略
  }
}

function onQuit(): void {
  try {
    Window.Close()
  } catch {
    // 忽略
  }
}
</script>

<template>
  <!--
    Wails v2 无边框窗口下，使用 CSS 自定义属性 `--wails-draggable: drag`
    来标记可拖动区域。本组件：
      · <header> 整体设为 drag，允许拖动
      · 右侧按钮容器显式设为 no-drag，确保按钮点击不被拖拽吞噬
  -->
  <header
    class="titlebar"
    style="--wails-draggable: drag"
    @dblclick="onToggleMax"
  >
    <div class="titlebar-left" style="--wails-draggable: drag">
      <div class="app-badge">
        <Icon name="film" :size="14" />
      </div>
      <span class="app-title">CCZJ Video</span>
    </div>
    <div class="titlebar-controls" style="--wails-draggable: no-drag">
      <button
        class="tb-btn minimize"
        @click="onMinimize"
        title="最小化"
        aria-label="最小化"
      >
        <Icon name="minimize" :size="10" />
      </button>
      <button
        class="tb-btn maximize"
        @click="onToggleMax"
        :title="isMaximized ? '还原' : '最大化'"
        :aria-label="isMaximized ? '还原' : '最大化'"
      >
        <Icon v-if="!isMaximized" name="maximize" :size="10" />
        <svg
          v-else
          class="restore-icon"
          width="10"
          height="10"
          viewBox="0 0 10 10"
          fill="none"
          stroke="currentColor"
          stroke-width="1.2"
          stroke-linejoin="round"
        >
          <rect x="2.5" y="0.5" width="6" height="6" rx="0.5" />
          <rect x="0.5" y="3.5" width="6" height="6" rx="0.5" fill="var(--bg-card)" />
        </svg>
      </button>
      <button
        class="tb-btn close"
        @click="onQuit"
        title="关闭"
        aria-label="关闭"
      >
        <Icon name="close" :size="10" />
      </button>
    </div>
  </header>
</template>

<style scoped>
.titlebar {
  height: 36px;
  width: 100%;
  display: flex;
  align-items: center;
  justify-content: space-between;
  background: transparent;
  color: var(--text-primary);
  border-bottom: 1px solid rgba(127, 127, 127, 0.18);
  -webkit-user-select: none;
  user-select: none;
  transition: background 0.3s ease, border-color 0.3s ease, color 0.3s ease;
  position: relative;
  z-index: 1000;
  box-sizing: border-box;
  backdrop-filter: saturate(1.1) blur(4px);
  -webkit-backdrop-filter: saturate(1.1) blur(4px);
}

.titlebar-left {
  display: flex;
  align-items: center;
  gap: 10px;
  padding-left: 14px;
}

.app-badge {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 22px;
  height: 22px;
  background: var(--accent);
  color: var(--accent-contrast);
  border-radius: 6px;
  box-shadow: 0 2px 8px var(--accent-alpha-35);
  flex-shrink: 0;
}

.app-title {
  font-size: 12px;
  font-weight: 600;
  letter-spacing: 0.3px;
  color: var(--text-primary);
  opacity: 0.9;
}

.titlebar-controls {
  display: flex;
  align-items: stretch;
  height: 100%;
}

.tb-btn {
  width: 46px;
  height: 36px;
  border: none;
  background: transparent;
  color: var(--text-secondary);
  cursor: pointer;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  transition: background 0.15s ease, color 0.15s ease, transform 0.1s ease;
  outline: none;
  position: relative;
}

.tb-btn:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
}

.tb-btn:active {
  transform: scale(0.92);
}

.tb-btn.close:hover {
  background: #e81123;
  color: #ffffff;
}

.tb-btn.close:active {
  background: #c00c1e;
}

.restore-icon {
  display: block;
  color: currentColor;
}
</style>
