<script setup lang="ts">
defineOptions({ name: 'BackToTop' })
import { ref, onMounted, onBeforeUnmount, onActivated, onDeactivated, computed } from 'vue'

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
    class="vine-wrap"
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
      title="拽一下回到顶部"
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

<style scoped>
/* ========== 容器：覆盖窗口上半部分（右侧）========== */
.vine-wrap {
  position: fixed;
  top: 0;
  right: 0;
  width: 280px;      /* 比 SVG 略宽，给漂浮花瓣留出空间 */
  pointer-events: none;
  z-index: 998;
  overflow: visible;
}

/* ========== SVG 主藤（固定在容器顶部）========== */
.vine-svg {
  position: absolute;
  top: 0;
  right: 0;
  display: block;
  overflow: visible;
}

/* 主藤绘制动画 */
.main-stem {
  stroke-dasharray: 1200;
  stroke-dashoffset: 1200;
  animation: stem-draw 1.0s cubic-bezier(0.5, 0, 0.3, 1) 0.1s forwards;
}

/* 侧枝：稍后出现 */
.side-branch {
  stroke-dasharray: 200;
  stroke-dashoffset: 200;
}
.branch-1 { animation: stem-draw 0.5s ease-out 0.4s forwards; }
.branch-2 { animation: stem-draw 0.5s ease-out 0.55s forwards; }
.branch-3 { animation: stem-draw 0.5s ease-out 0.7s forwards; }
.branch-4 { animation: stem-draw 0.5s ease-out 0.85s forwards; }

/* 水平段小花：逐个弹出 */
.mini-flower {
  transform-origin: center;
  transform: scale(0);
  opacity: 0;
}
.mf-1 { animation: flower-pop 0.5s cubic-bezier(0.34, 1.56, 0.64, 1) 0.55s forwards; }
.mf-2 { animation: flower-pop 0.5s cubic-bezier(0.34, 1.56, 0.64, 1) 0.8s forwards; }
.mf-3 { animation: flower-pop 0.5s cubic-bezier(0.34, 1.56, 0.64, 1) 0.95s forwards; }

/* 叶子 */
.vine-leaf {
  transform-origin: center;
  transform: scale(0) rotate(-20deg);
  opacity: 0;
}
.leaf-l-1 { animation: leaf-spring-sway 4.55s cubic-bezier(0.34, 1.56, 0.64, 1) 0.45s infinite; }
.leaf-l-2 { animation: leaf-spring-sway2 4.7s cubic-bezier(0.34, 1.56, 0.64, 1) 0.7s infinite; }

/* 下垂叶子 */
.hanging-leaf {
  transform-origin: center;
  transform: scale(0);
  opacity: 0;
}
.h-leaf-1 { animation: leaf-spring-sway 4.5s cubic-bezier(0.34, 1.56, 0.64, 1) 0.85s infinite; }
.h-leaf-2 { animation: leaf-spring-sway2 4.5s cubic-bezier(0.34, 1.56, 0.64, 1) 1.0s infinite; }

/* 卷须 */
.vine-tendril {
  transform-origin: center;
  transform: scale(0);
  opacity: 0;
}
.t-1 { animation: leaf-spring 0.35s cubic-bezier(0.34, 1.56, 0.64, 1) 0.7s forwards; }
.t-2 { animation: leaf-spring 0.35s cubic-bezier(0.34, 1.56, 0.64, 1) 0.9s forwards; }
.t-3 { animation: leaf-spring 0.35s cubic-bezier(0.34, 1.56, 0.64, 1) 1.05s forwards; }

/* ========== 可点击的花朵（位于容器右下角附近）========== */
.flower-click-target {
  position: absolute;
  right: 14px;
  bottom: 18px;
  width: 72px;
  height: 72px;
  pointer-events: auto;
  cursor: pointer;
  transform-origin: center;
  transform: scale(0) rotate(-30deg);
  opacity: 0;
  animation: flower-bloom 0.7s cubic-bezier(0.34, 1.56, 0.64, 1) 1.0s forwards,
             flower-sway 4s ease-in-out 1.7s infinite alternate;
  filter: drop-shadow(0 4px 10px var(--accent-alpha-35));
}

.flower-svg {
  width: 100%;
  height: 100%;
  display: block;
  transition: transform 0.25s ease;
}

/* Hover：花朵放大 + 旋转 + 发光 */
.flower-click-target:hover .flower-svg {
  transform: scale(1.15) rotate(8deg);
  filter: drop-shadow(0 0 12px var(--accent));
}
.flower-click-target:hover .petal {
  animation: petal-breath 1.8s ease-in-out infinite;
}

/* 花瓣呼吸微动（默认状态） */
.petal {
  transform-origin: 40px 40px;
  transition: transform 0.3s ease;
}

/* 被拽上去：花朵快速回收 */
.vine-wrap.pulling .flower-click-target {
  animation: flower-yank 0.3s cubic-bezier(0.55, 0.05, 0.675, 0.19) forwards;
}

/* 被拽时主藤快速缩回 */
.vine-wrap.pulling .main-stem,
.vine-wrap.pulling .side-branch {
  animation: stem-retract 0.25s ease-in forwards !important;
}

/* ========== 手动滚动时的缩回动画（比 pulling 更柔和）========== */
.vine-wrap.retracting .main-stem {
  animation: stem-retract-soft 0.3s ease-in forwards !important;
}
.vine-wrap.retracting .side-branch {
  animation: stem-retract-soft 0.25s ease-in 0.05s forwards !important;
}
.vine-wrap.retracting .vine-leaf,
.vine-wrap.retracting .hanging-leaf {
  animation: leaf-fade-out 0.28s ease-in forwards !important;
}
.vine-wrap.retracting .mini-flower {
  animation: flower-fade-out 0.25s ease-in 0.05s forwards !important;
}
.vine-wrap.retracting .vine-tendril {
  animation: leaf-fade-out 0.22s ease-in forwards !important;
}
.vine-wrap.retracting .flower-click-target {
  animation: flower-fly-away 0.3s cubic-bezier(0.55, 0.05, 0.675, 0.19) forwards !important;
}
.vine-wrap.retracting .petal-float {
  animation: petal-vanish 0.3s ease-out forwards !important;
}

/* ========== 漂浮花瓣 ========== */
.petal-float {
  position: absolute;
  color: var(--accent);
  opacity: 0;
  pointer-events: none;
  animation: petal-fall 6s linear infinite;
  text-shadow: 0 1px 3px var(--accent-alpha-35);
}
.p-float-1 { top: 25%; right: 120px; animation-delay: 2.5s; font-size: 11px; }
.p-float-2 { top: 45%; right: 80px;  animation-delay: 4.5s; font-size: 9px;  opacity: 0.7; }
.p-float-3 { top: 15%; right: 50px;  animation-delay: 1.0s; font-size: 10px; opacity: 0.6; }

/* ============================================================
   关键帧动画定义
   ============================================================ */

/* 藤茎/侧枝绘制 */
@keyframes stem-draw {
  to { stroke-dashoffset: 0; }
}

/* 藤茎缩回 */
@keyframes stem-retract {
  from { stroke-dashoffset: 0; }
  to { stroke-dashoffset: 1200; opacity: 0.2; }
}

/* 藤茎柔和缩回（手动滚动时） */
@keyframes stem-retract-soft {
  from { stroke-dashoffset: 0; }
  to { stroke-dashoffset: 1200; opacity: 0; }
}

/* 叶子淡出缩回 */
@keyframes leaf-fade-out {
  0%   { transform: scale(1) rotate(0); opacity: 1; }
  100% { transform: scale(0) rotate(15deg); opacity: 0; }
}

/* 小花淡出缩回 */
@keyframes flower-fade-out {
  0%   { transform: scale(1); opacity: 1; }
  100% { transform: scale(0); opacity: 0; }
}

/* 花朵轻柔向上飘走 */
@keyframes flower-fly-away {
  0%   { transform: scale(1) rotate(0); opacity: 1; }
  60%  { transform: scale(0.8) rotate(-8deg) translateY(-15px); opacity: 0.8; }
  100% { transform: scale(0.3) rotate(-15deg) translateY(-80px); opacity: 0; }
}

/* 漂浮花瓣快速消失 */
@keyframes petal-vanish {
  0%   { opacity: 0.7; }
  100% { opacity: 0; transform: translate(20px, -40px) rotate(90deg); }
}

/* 叶子弹出 + 持续摆动（合并成一个动画，避免冲突） */
@keyframes leaf-spring-sway {
  0%    { transform: scale(0) rotate(-20deg); opacity: 0; }
  12%   { transform: scale(1.2) rotate(5deg); opacity: 1; }
  20%   { transform: scale(1) rotate(0); opacity: 1; }
  60%   { transform: scale(1) rotate(2deg); opacity: 1; }
  80%   { transform: scale(1) rotate(-1deg); opacity: 1; }
  100%  { transform: scale(1) rotate(1deg); opacity: 1; }
}
@keyframes leaf-spring-sway2 {
  0%    { transform: scale(0) rotate(-20deg); opacity: 0; }
  12%   { transform: scale(1.15) rotate(5deg); opacity: 1; }
  20%   { transform: scale(1) rotate(0); opacity: 1; }
  55%   { transform: scale(1) rotate(-2deg); opacity: 1; }
  75%   { transform: scale(1) rotate(2deg); opacity: 1; }
  100%  { transform: scale(1) rotate(-1deg); opacity: 1; }
}

/* 小花弹出 */
@keyframes flower-pop {
  0%   { transform: scale(0); opacity: 0; }
  60%  { transform: scale(1.25); opacity: 1; }
  100% { transform: scale(1); opacity: 1; }
}

/* 主花朵绽放 */
@keyframes flower-bloom {
  0%   { transform: scale(0) rotate(-40deg); opacity: 0; }
  60%  { transform: scale(1.25) rotate(15deg); opacity: 1; }
  100% { transform: scale(1) rotate(0); opacity: 1; }
}

/* 主花朵被拽走 */
@keyframes flower-yank {
  0%   { transform: scale(1) rotate(0); opacity: 1; }
  40%  { transform: scale(1.3) rotate(15deg) translateY(-20px); opacity: 1; }
  100% { transform: scale(0.2) rotate(-20deg) translateY(-300px); opacity: 0; }
}

/* 花朵微风摆动（默认） */
@keyframes flower-sway {
  0%   { transform: scale(1) rotate(-4deg) translate(0, 0); }
  50%  { transform: scale(1) rotate(4deg) translate(1px, 3px); }
  100% { transform: scale(1) rotate(-2deg) translate(-1px, -1px); }
}

/* 花瓣呼吸 */
@keyframes petal-breath {
  0%, 100% { transform: scale(1); }
  50%      { transform: scale(1.05); }
}

/* 花瓣飘落 */
@keyframes petal-fall {
  0%   { transform: translate(0, -20px) rotate(0); opacity: 0; }
  10%  { opacity: 0.7; }
  50%  { transform: translate(-30px, 60px) rotate(180deg); opacity: 0.6; }
  90%  { opacity: 0.2; }
  100% { transform: translate(-60px, 140px) rotate(360deg); opacity: 0; }
}

/* 小屏幕自适应：缩小整个装饰 */
@media (max-width: 640px) {
  .vine-wrap {
    width: 180px;
  }
  .vine-svg {
    width: 160px !important;
  }
  .flower-click-target {
    width: 56px;
    height: 56px;
    right: 8px;
    bottom: 10px;
  }
}
</style>
