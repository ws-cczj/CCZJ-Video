<script setup lang="ts">
/**
 * SplashScreen — 启动画面
 *
 * 等主题恢复后只选择一张图片；异步解码后淡入，启动等待有上限。
 * 图片透明区域会根据主题模式显示白底（浅色主题）或深灰底（深色主题）。
 */
import { ref, computed, onMounted, onBeforeUnmount, watch } from 'vue'
import { useThemeStore } from '../stores/theme'
import { MotionTransition } from './ui'
import Icon from './Icon.vue'
import { useApplicationMode } from '../modes/applicationMode'
import { APPLICATION_MODES } from '../modes/definitions'
import { markStartup } from '../diagnostics/startup'

import splashGreen from '../assets/images/green.webp'
import splashBitblue from '../assets/images/skyblue.webp'
import splashBitpurple from '../assets/images/bitpurple.webp'
import splashPink from '../assets/images/pink.webp'
import splashRed from '../assets/images/red.webp'
import splashGrey from '../assets/images/grey.webp'
import splashYellow from '../assets/images/yellow.webp'
import splashDeepblue from '../assets/images/deepblue.webp'
import splashSkyblue from '../assets/images/skyblue.webp'
import splashPurple from '../assets/images/purple.webp'

const theme = useThemeStore()
const appMode = useApplicationMode()

// 主题 ID → 启动图片 映射（按色系归类）
const SPLASH_MAP: Record<string, string> = {
  // 蓝色系
  blue: splashBitblue, sky: splashBitblue, indigo: splashBitblue,
  'd-blue': splashBitblue, 'd-cyan': splashBitblue,
  deepblue: splashDeepblue, skyblue: splashSkyblue,
  bitblue: splashBitblue,
  // 木叶之村也是蓝色系
  naruto: splashBitblue,
  // 绿色系
  green: splashGreen, 'd-green': splashGreen, lime: splashGreen, teal: splashGreen, cyan: splashGreen,
  bitgreen: splashGreen,
  // 紫色系
  purple: splashPurple, 'd-violet': splashPurple, 'd-indigo': splashPurple,
  bitpurple: splashBitpurple,
  // 粉色系
  pink: splashPink, 'd-rose': splashPink,
  // 红色系
  red: splashRed, orange: splashRed,
  happy_new_year: splashRed,
  // 灰色系
  gray: splashGrey, grey: splashGrey,
  china_ink: splashGrey, black: splashGrey,
  // 黄色系
  yellow: splashYellow,
  // 中秋（暗紫）
  mid_autumn: splashBitpurple,
}

// Select once after theme restoration, with a short fallback deadline.
// Never replace an image that has already begun decoding during startup.
const bgSrc = ref('')
const ready = ref(false)
const show = ref(true)
let disposed = false
let selectionTimer: ReturnType<typeof setTimeout> | undefined
let finishTimer: ReturnType<typeof setTimeout> | undefined
let deadlineTimer: ReturnType<typeof setTimeout> | undefined
let stopTheme: (() => void) | undefined
let revealFrame = 0
function selectImage(): void {
  if (disposed || bgSrc.value) return
  clearTimeout(selectionTimer)
  stopTheme?.()
  bgSrc.value = SPLASH_MAP[theme.currentId] || (theme.current.mode === 'dark' ? splashDeepblue : splashGreen)
}
function dismiss(): void {
  markStartup('splash_closed')
  show.value = false
  dispose()
}
function dispose(): void {
  disposed = true
  clearTimeout(selectionTimer)
  clearTimeout(finishTimer)
  clearTimeout(deadlineTimer)
  stopTheme?.()
  cancelAnimationFrame(revealFrame)
}
async function reveal(event: Event): Promise<void> {
  const began = performance.now()
  try { await (event.target as HTMLImageElement).decode() } catch { /* onload image is still usable */ }
  if (disposed) return
  markStartup('image_decode',performance.now()-began)
  // Cached images may decode before their initial opacity has been painted.
  // Keep a full frame between insertion and revealing the image and caption.
  revealFrame = requestAnimationFrame(()=>{
    revealFrame = requestAnimationFrame(()=>{
      if (disposed) return
      ready.value = true
      markStartup('splash_reveal')
      finishTimer = setTimeout(dismiss, 850)
    })
  })
}
onMounted(() => {
  if (appMode.mode !== 'video') { ready.value = true; finishTimer = setTimeout(dismiss, 850); return }
  // Bound the splash even when theme IPC or image loading fails.
  deadlineTimer = setTimeout(dismiss, 2200)
  selectionTimer = setTimeout(selectImage, 250)
  stopTheme = watch(() => theme.loaded, loaded => { if (loaded) selectImage() })
  if (theme.loaded) selectImage()
})
onBeforeUnmount(dispose)

// 背景色：使用主题背景色，最后按模式兜底
const bgStyle = computed(() => {
  if (appMode.mode === 'novel') return { backgroundColor: '#faf9f6', '--text-primary': '#272722', '--text-muted': '#716f68', '--accent': '#ad302c' }
  if (appMode.mode === 'comic') return { backgroundColor: '#fffaf5', '--text-primary': '#22212a', '--text-muted': '#806753', '--accent': '#b84929' }
  const t = theme.current
  return { backgroundColor: t.palette?.bgApp || (t.mode === 'dark' ? '#1a1a1e' : '#ffffff') }
})

</script>

<template>
  <MotionTransition preset="fade">
    <div v-if="show" class="splash-wrap cczj-fixed cczj-items-center cczj-justify-center cczj-flex cczj-z-max"
      :style="bgStyle">
      <div class="splash-brand cczj-motion-ornament" :class="{ready}">
        <span class="brand-mark">CCZJ</span><span class="brand-caption">{{ APPLICATION_MODES[appMode.mode].caption }}</span>
      </div>
      <Icon v-if="appMode.mode !== 'video'" :name="APPLICATION_MODES[appMode.mode].icon" :size="80" class="novel-splash-mark cczj-motion-reveal" />
      <img v-else-if="bgSrc" :src="bgSrc" alt="" decoding="async" fetchpriority="high"
        class="splash-img cczj-motion-ornament" :class="{ ready }" @load="reveal" @error="dismiss" />
    </div>
  </MotionTransition>
</template>

<style scoped>
.splash-wrap { inset: 0; @apply cczj-overflow-hidden cczj-flex-col cczj-gap-10; }
.novel-splash-mark { order: -1; color: #ad302c; @apply cczj-mb-6; }
.splash-img {
  order: -1; width: min(52vw, 440px); height: min(65vh, 580px); object-fit: contain;
  @apply cczj-opacity-0; transform: translateY(8px) scale(.98);
  transition: opacity var(--cczj-motion-slow) ease-out, transform var(--cczj-motion-emphasis) ease-out;
}
.splash-img.ready { opacity: .9; transform: translateY(0) scale(1); }
.splash-brand { @apply cczj-flex cczj-items-baseline cczj-gap-6 cczj-text-primary cczj-opacity-0; transform:translateY(5px); transition:opacity var(--cczj-motion-slow) ease-out,transform var(--cczj-motion-slow) ease-out; }
.splash-brand.ready { @apply cczj-opacity-100; transform:translateY(0); }
.brand-mark { font-size: 1.6rem; @apply cczj-font-bold; letter-spacing: .15em; }
.brand-caption { font-size: .8rem; letter-spacing: .35em; @apply cczj-text-muted; }
@keyframes brand-enter { from { opacity: 0; transform: translateY(5px); } to { opacity: 1; transform: translateY(0); } }
</style>
