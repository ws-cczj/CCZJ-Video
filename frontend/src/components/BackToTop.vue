<script setup lang="ts">
defineOptions({ name: 'BackToTop' })
import { ref, onMounted, onBeforeUnmount, onActivated, onDeactivated, computed } from 'vue'
import { tr } from '../locales'

const THRESHOLD = 400
const visible = ref(false)
const pulling = ref(false)
const retracting = ref(false)
const growPhase = ref(0)  // 0 未显示，1 生长中，2 静止
const vineHeight = ref(400) // 藤蔓高度，用 JS 设置为窗口一半

let retractTimer: ReturnType<typeof setTimeout> | null = null

function cancelRetract(): void {
  if (retractTimer) {
    clearTimeout(retractTimer)
    retractTimer = null
  }
  retracting.value = false
}

function updateVineHeight() {
  vineHeight.value = Math.max(280, Math.floor(window.innerHeight * 0.45))
}

function getScrollContainer(): HTMLElement | null {
  return document.querySelector('.main-content')
}

function onScroll(): void {
  const el = getScrollContainer()
  if (!el) return
  const shouldShow = el.scrollTop > THRESHOLD
  if (shouldShow && !visible.value && !pulling.value) {
    // 进入视图：先取消任何正在进行的缩回动画
    cancelRetract()
    visible.value = true
    growPhase.value = 1
    setTimeout(() => {
      if (visible.value) growPhase.value = 2
    }, 1200)
  } else if (!shouldShow && visible.value && !pulling.value && !retracting.value) {
    // 离开视图：播放缩回动画，结束后再隐藏
    retracting.value = true
    retractTimer = setTimeout(() => {
      visible.value = false
      retracting.value = false
      growPhase.value = 0
      retractTimer = null
    }, 300)
  } else if (shouldShow && retracting.value) {
    // 缩回中用户又向下滚动了：取消缩回
    cancelRetract()
  }
}

function onResize() {
  updateVineHeight()
}

function scrollToTop(): void {
  const el = getScrollContainer()
  if (!el || pulling.value) return
  pulling.value = true
  growPhase.value = 0
  const originalTop = el.scrollTop
  const snapDuration = 320
  const startTime = performance.now()
  function tick(now: number) {
    const p = Math.min(1, (now - startTime) / snapDuration)
    const eased = 1 - Math.pow(1 - p, 3)
    el!.scrollTop = originalTop * (1 - eased)
    if (p < 1) {
      requestAnimationFrame(tick)
    } else {
      el!.scrollTop = 0
      setTimeout(() => {
        pulling.value = false
        visible.value = false
      }, 200)
    }
  }
  requestAnimationFrame(tick)
}

let scroller: HTMLElement | null = null

function bindScroll(): void {
  unbindScroll()
  scroller = getScrollContainer()
  if (scroller) {
    scroller.addEventListener('scroll', onScroll, { passive: true })
    onScroll()
  }
  updateVineHeight()
  window.addEventListener('resize', onResize)
}

function unbindScroll(): void {
  if (scroller) {
    scroller.removeEventListener('scroll', onScroll)
    scroller = null
  }
  window.removeEventListener('resize', onResize)
}

onMounted(() => bindScroll())
onActivated(() => bindScroll())
onDeactivated(() => unbindScroll())
onBeforeUnmount(() => unbindScroll())

// 动态生成 SVG 视口
const vineViewBox = computed(() => `0 0 260 ${vineHeight.value}`)
const svgHeight = computed(() => `${vineHeight.value}px`)
// 下垂段的曲线终点坐标（从260像素处向下弯曲）
const flowerY = computed(() => vineHeight.value - 30)
// 下垂叶子沿茎分布的y坐标
const hangingLeaf1Y = computed(() => Math.floor(vineHeight.value * 0.35))
const hangingLeaf2Y = computed(() => Math.floor(vineHeight.value * 0.65))
const tendril1Y = computed(() => Math.floor(vineHeight.value * 0.2))
const tendril2Y = computed(() => Math.floor(vineHeight.value * 0.5))
const tendril3Y = computed(() => Math.floor(vineHeight.value * 0.75))
</script>

<template>
  <div
    v-show="visible"
    class="vine-wrap cczj-motion-ornament"
    :class="{ 'phase-grow': growPhase === 1, pulling: pulling, retracting: retracting }"
    :style="{ height: svgHeight }"
  >
    <!-- ===== 顶部水平藤蔓 + 下垂曲线（固定在右上角）===== -->
    <svg
      class="vine-svg"
      :width="260"
      :height="vineHeight"
      :viewBox="vineViewBox"
      xmlns="http://www.w3.org/2000/svg"
      aria-hidden="true"
    >
      <defs>
        <!-- 主藤颜色渐变：从浅绿到深绿，起点半透明 -->
        <linearGradient id="stemGrad" x1="0%" y1="0%" x2="0%" y2="100%">
          <stop offset="0%" stop-color="var(--accent)" stop-opacity="0.18" />
          <stop offset="15%" stop-color="var(--accent)" stop-opacity="0.55" />
          <stop offset="100%" stop-color="var(--accent)" stop-opacity="0.95" />
        </linearGradient>
        <!-- 主藤阴影 -->
        <filter id="stemShadow" x="-20%" y="-20%" width="140%" height="140%">
          <feGaussianBlur in="SourceAlpha" stdDeviation="1.2" />
          <feOffset dx="0" dy="1" result="offsetblur" />
          <feFlood flood-color="var(--accent)" flood-opacity="0.25" />
          <feComposite in2="offsetblur" operator="in" />
          <feMerge>
            <feMergeNode />
            <feMergeNode in="SourceGraphic" />
          </feMerge>
        </filter>

        <!-- 花瓣渐变（使用主题色） -->
        <radialGradient id="petalGrad" cx="50%" cy="40%" r="60%">
          <stop offset="0%" stop-color="#ffffff" stop-opacity="1" />
          <stop offset="40%" stop-color="var(--accent)" stop-opacity="0.75" />
          <stop offset="100%" stop-color="var(--accent)" stop-opacity="1" />
        </radialGradient>

        <!-- 叶子渐变 -->
        <linearGradient id="leafGrad" x1="0%" y1="0%" x2="100%" y2="100%">
          <stop offset="0%" stop-color="#a8d8a0" />
          <stop offset="100%" stop-color="#5faa55" />
        </linearGradient>

        <!-- 花心渐变 -->
        <radialGradient id="centerGrad" cx="50%" cy="50%" r="50%">
          <stop offset="0%" stop-color="#fff8c8" />
          <stop offset="60%" stop-color="#ffd86b" />
          <stop offset="100%" stop-color="#e0a53c" />
        </radialGradient>
      </defs>

      <!-- ==== 主藤路径：水平段 + 下垂曲线（一笔画）==== -->
      <!-- 从左上角附近开始，水平向右延伸，然后在右侧弯曲下垂 -->
      <path
        class="vine-stem main-stem"
        :d="`M 20 8 C 80 5, 150 10, 200 12 S 240 20, 245 50 C 248 ${hangingLeaf1Y - 20}, 240 ${hangingLeaf2Y - 20}, 230 ${flowerY}`"
        fill="none"
        stroke="url(#stemGrad)"
        stroke-width="3"
        stroke-linecap="round"
        filter="url(#stemShadow)"
      />

      <!-- ==== 细小的侧枝（装饰）==== -->
      <path
        class="vine-stem side-branch branch-1"
        d="M 50 10 C 45 20, 40 28, 35 42"
        fill="none"
        stroke="var(--accent)"
        stroke-width="1.5"
        stroke-opacity="0.5"
        stroke-linecap="round"
      />
      <path
        class="vine-stem side-branch branch-2"
        d="M 100 11 C 98 20, 94 28, 92 44"
        fill="none"
        stroke="var(--accent)"
        stroke-width="1.5"
        stroke-opacity="0.5"
        stroke-linecap="round"
      />
      <path
        class="vine-stem side-branch branch-3"
        d="M 160 12 C 162 22, 166 32, 170 46"
        fill="none"
        stroke="var(--accent)"
        stroke-width="1.5"
        stroke-opacity="0.5"
        stroke-linecap="round"
      />

      <!-- ==== 水平段上的小花（装饰，不可点击）==== -->
      <g class="mini-flower mf-1">
        <circle cx="70" cy="7" r="5" fill="url(#petalGrad)" />
        <circle cx="70" cy="7" r="1.8" fill="url(#centerGrad)" />
      </g>
      <g class="mini-flower mf-2">
        <circle cx="140" cy="8" r="4" fill="url(#petalGrad)" />
        <circle cx="140" cy="8" r="1.4" fill="url(#centerGrad)" />
      </g>
      <g class="mini-flower mf-3">
        <circle cx="200" cy="11" r="5" fill="url(#petalGrad)" />
        <circle cx="200" cy="11" r="1.8" fill="url(#centerGrad)" />
      </g>

      <!-- ==== 叶子（沿水平茎分布，向上生长）==== -->
      <path class="vine-leaf leaf-l-1"
        d="M 110 12 Q 104 2, 116 -1 Q 128 4, 120 14 Z"
        fill="url(#leafGrad)"
        opacity="0.95"
      />
      <path class="vine-leaf leaf-l-2"
        d="M 180 10 Q 186 0, 176 -2 Q 168 6, 174 14 Z"
        fill="url(#leafGrad)"
        opacity="0.95"
      />

      <!-- ==== 下垂段上的叶子和卷须（沿茎分布）==== -->
      <g class="hanging-leaf h-leaf-1">
        <path :d="`M 240 ${hangingLeaf1Y} Q 225 ${hangingLeaf1Y - 10}, 220 ${hangingLeaf1Y + 4} Q 228 ${hangingLeaf1Y + 14}, 240 ${hangingLeaf1Y} Z`"
          fill="url(#leafGrad)" />
      </g>
      <g class="hanging-leaf h-leaf-2">
        <path :d="`M 235 ${hangingLeaf2Y} Q 248 ${hangingLeaf2Y - 8}, 252 ${hangingLeaf2Y + 6} Q 244 ${hangingLeaf2Y + 14}, 235 ${hangingLeaf2Y} Z`"
          fill="url(#leafGrad)" />
      </g>

      <!-- 小卷须（小圆点） -->
      <circle class="vine-tendril t-1" cx="243" :cy="tendril1Y" r="2.5" fill="var(--accent)" opacity="0.7" />
      <circle class="vine-tendril t-2" cx="238" :cy="tendril2Y" r="2" fill="var(--accent)" opacity="0.6" />
      <circle class="vine-tendril t-3" cx="235" :cy="tendril3Y" r="2.5" fill="var(--accent)" opacity="0.7" />
    </svg>

    <!-- ===== 可点击的花朵（独立层，便于 hover/press 效果）===== -->
    <div
      class="flower-click-target"
      @click="scrollToTop"
      :title="tr('common.backToTopHint')"
    >
      <svg class="flower-svg" viewBox="0 0 80 80" aria-hidden="true">
        <defs>
          <radialGradient id="flowerPetal" cx="50%" cy="35%" r="65%">
            <stop offset="0%" stop-color="#ffffff" stop-opacity="1" />
            <stop offset="35%" stop-color="var(--accent)" stop-opacity="0.65" />
            <stop offset="100%" stop-color="var(--accent)" stop-opacity="1" />
          </radialGradient>
          <radialGradient id="flowerCenter" cx="50%" cy="50%" r="55%">
            <stop offset="0%" stop-color="#fff6c0" />
            <stop offset="55%" stop-color="#ffd86b" />
            <stop offset="100%" stop-color="#d89a30" />
          </radialGradient>
          <filter id="flowerGlow" x="-50%" y="-50%" width="200%" height="200%">
            <feGaussianBlur stdDeviation="2.5" result="blur" />
            <feMerge>
              <feMergeNode in="blur" />
              <feMergeNode in="SourceGraphic" />
            </feMerge>
          </filter>
        </defs>

        <!-- 5 片花瓣（樱花），围绕中心 -->
        <g class="petals">
          <path class="petal petal-1"
            d="M 40 10 C 48 18, 48 32, 40 40 C 32 32, 32 18, 40 10 Z"
            fill="url(#flowerPetal)" />
          <path class="petal petal-2"
            d="M 65 22 C 66 30, 62 42, 55 47 C 50 40, 50 26, 65 22 Z"
            fill="url(#flowerPetal)" />
          <path class="petal petal-3"
            d="M 65 58 C 58 60, 48 58, 43 50 C 48 44, 58 46, 65 58 Z"
            fill="url(#flowerPetal)" />
          <path class="petal petal-4"
            d="M 15 58 C 22 46, 32 44, 37 50 C 32 58, 22 60, 15 58 Z"
            fill="url(#flowerPetal)" />
          <path class="petal petal-5"
            d="M 15 22 C 14 30, 18 42, 25 47 C 30 40, 30 26, 15 22 Z"
            fill="url(#flowerPetal)" />
        </g>

        <!-- 花瓣纹路 -->
        <g class="petal-lines" opacity="0.35">
          <path d="M 40 14 L 40 36" stroke="var(--accent)" stroke-width="0.8" fill="none" />
          <path d="M 62 25 L 56 44" stroke="var(--accent)" stroke-width="0.8" fill="none" />
          <path d="M 62 55 L 46 50" stroke="var(--accent)" stroke-width="0.8" fill="none" />
          <path d="M 18 55 L 34 50" stroke="var(--accent)" stroke-width="0.8" fill="none" />
          <path d="M 18 25 L 24 44" stroke="var(--accent)" stroke-width="0.8" fill="none" />
        </g>

        <!-- 花心 + 花蕊 -->
        <circle cx="40" cy="40" r="9" fill="url(#flowerCenter)" filter="url(#flowerGlow)" />
        <g class="stamens" fill="#d89a30">
          <circle cx="36" cy="36" r="1.3" />
          <circle cx="44" cy="36" r="1.3" />
          <circle cx="40" cy="34" r="1.3" />
          <circle cx="37" cy="43" r="1.3" />
          <circle cx="43" cy="43" r="1.3" />
          <circle cx="40" cy="40" r="1.5" fill="#b87a1e" />
        </g>

        <!-- 向上箭头（在花心上，表示回到顶部） -->
        <g class="up-arrow" opacity="0.85">
          <path d="M 40 34 L 43 39 L 41 39 L 41 44 L 39 44 L 39 39 L 37 39 Z"
            fill="#ffffff" stroke="var(--accent)" stroke-width="0.5" />
        </g>
      </svg>
    </div>

    <!-- 漂浮的花瓣装饰 -->
    <div class="petal-float p-float-1">✿</div>
    <div class="petal-float p-float-2">❀</div>
    <div class="petal-float p-float-3">✿</div>
  </div>
</template>

<style scoped src="../styles/components/back-to-top.css"></style>
