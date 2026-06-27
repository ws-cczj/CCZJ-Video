<script setup lang="ts">
/**
 * SplashScreen — 启动画面
 *
 * 根据当前主题显示对应的背景图片，1.5 秒后自动消失。
 * 图片透明区域会根据主题模式显示白底（浅色主题）或深灰底（深色主题）。
 */
import { ref, computed, onMounted } from 'vue'
import { useThemeStore } from '../stores/theme'

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

const bgSrc = computed(() => {
  const tid = theme.currentId
  if (SPLASH_MAP[tid]) return SPLASH_MAP[tid]
  // 兜底
  return theme.current.mode === 'dark' ? splashDeepblue : splashGreen
})

// 背景色：使用主题背景色，最后按模式兜底
const bgStyle = computed(() => {
  const t = theme.current
  return { backgroundColor: t.palette?.bgApp || (t.mode === 'dark' ? '#1a1a1e' : '#ffffff') }
})

const show = ref(true)

onMounted(() => {
  setTimeout(() => { show.value = false }, 1500)
})
</script>

<template>
  <div v-if="show" class="splash-wrap cczj-fixed cczj-items-center cczj-justify-center cczj-flex cczj-z-max"
    :style="bgStyle">
    <div class="splash-brand cczj-absolute">CCZJ Video</div>
    <img :src="bgSrc" class="splash-img cczj-text-2xl cczj-z-10 cczj-font-medium cczj-opacity-70" />
  </div>
</template>

<style scoped>
.splash-wrap {
  inset: 0;
}

.splash-img {
  width: 65%;
  height: 65%;
  object-fit: contain;
}

.splash-brand {
  bottom: 40px;
  letter-spacing: 0.15em;
  color: #fff;
}
</style>