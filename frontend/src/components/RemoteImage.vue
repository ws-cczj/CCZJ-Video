<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import type { ComponentPublicInstance } from 'vue'
import { getProxiedImageUrl } from '../utils'
import imageNotFound from '../assets/images/image_notfound.png'
import { useMotionStore } from '../stores/motion'

defineOptions({ inheritAttrs: false })

interface Props {
  src?: string | null
  alt?: string
  fallback?: string
}

const props = withDefaults(defineProps<Props>(), {
  src: '',
  alt: '',
  fallback: imageNotFound,
})

const motion = useMotionStore()

const resolvedSrc = ref(props.fallback)
/**
 * 这一格处在哪个阶段。'hidden' 是「还没有图亮着」，'dip' 是「旧图正在淡出」，
 * 'shown' 是「有图并且淡进过了」。用它而不是一个布尔，是因为第一张图不该淡出——
 * 那时淡的是空气，只会白等一次时长。
 */
const phase = ref<'hidden' | 'dip' | 'shown'>('hidden')
/**
 * src 已经换了、但手上这张还是上一张：resolveImage 在等代理回话，或者正卡在淡出里。
 * 这一段 phase 仍是 'shown'（旧图还亮着），所以只看 phase / 只看 opacity 都认不出来——
 * 轮播交棒的闸门要的正是"新图落好了没"，光看屏幕看不出来。
 *
 * 已知边角：上一张的 @load 如果正好落在"新请求已发出、还没轮到它赋值"这一格里，会提前
 * 把它放开一次。要堵它得给赋值也记一个请求号（现在只有请求 src 那边有），代价是这套
 * 记账翻倍；先留着，闸门本身有 1.5 秒上限兜底。
 */
const settling = ref(false)
const requestToken = ref(0)
let dipTimer: number | null = null

/** 交棒闸门问的就是这一格：新图解完码并且淡进来了，才算可以往台前滑。 */
defineExpose({
  ready: computed(() => phase.value === 'shown' && !settling.value),
})

// 淡入淡出走 animation 而不是 transition：transition 是一条整体属性，写进 style 会把
// 调用方给图片自己的过渡（比如 VideoCard 的悬停放大）整条顶掉。animation 没人占用，
// 时长取自令牌，所以「关闭动画」时它自己就退成 1ms 的直切。
const imageClass = computed(() => (phase.value === 'shown' ? 'cczj-image-in' : phase.value === 'dip' ? 'cczj-image-out' : ''))
const imageStyle = computed(() => ({ opacity: phase.value === 'shown' ? '1' : '0' }))

function clearDip(): void {
  if (dipTimer !== null) {
    clearTimeout(dipTimer)
    dipTimer = null
  }
}

/**
 * 换一张图。旧图先淡出，等它真到了全透明才把 src 换掉，新图解完码（@load）再淡进来——
 * 中间那一帧空档由格子自己的底色接着，所以看到的是溶解而不是一跳。
 * 动画关掉、或手上没有亮着的旧图时，跳过淡出直接换源，淡进来那一段照旧。
 */
function show(next: string): void {
  clearDip()
  if (next === resolvedSrc.value) {
    phase.value = 'shown'
    settling.value = false
    return
  }
  const dip = motion.dipMs()
  if (dip <= 0 || phase.value !== 'shown') {
    resolvedSrc.value = next
    return
  }
  phase.value = 'dip'
  dipTimer = window.setTimeout(() => {
    dipTimer = null
    resolvedSrc.value = next
  }, dip)
}

async function resolveImage(rawSrc: string | null | undefined): Promise<void> {
  const token = ++requestToken.value
  const source = String(rawSrc || '').trim()
  settling.value = true
  if (!source) {
    if (token === requestToken.value) show(props.fallback)
    return
  }

  let nextSrc = source
  if (/^https?:\/\//i.test(source)) {
    nextSrc = await getProxiedImageUrl(source)
  }

  if (token === requestToken.value) {
    show(nextSrc || props.fallback)
  }
}

function onImageLoad(): void {
  phase.value = 'shown'
  settling.value = false
}

function onImageError(): void {
  if (resolvedSrc.value !== props.fallback) {
    show(props.fallback)
    return
  }
  // 连兜底图都上不了屏也得把这一格亮起来，否则它永远停在透明状态。
  phase.value = 'shown'
  settling.value = false
}

/**
 * 这一格靠 @load 才亮起来，所以「load 早就跑完了」必须能自己发现：图片已经解码完成
 * （complete + 有实际宽度）就直接当它亮着，否则它会永远停在透明状态等一次不会来的事件。
 */
function onImgEl(el: Element | ComponentPublicInstance | null): void {
  if (el instanceof HTMLImageElement && el.complete && el.naturalWidth > 0) {
    phase.value = 'shown'
    settling.value = false
  }
}

watch(() => props.src, (value) => {
  void resolveImage(value)
}, { immediate: true })

onBeforeUnmount(clearDip)
</script>

<template>
  <img
    v-bind="$attrs"
    :ref="onImgEl"
    :src="resolvedSrc"
    :alt="alt"
    :class="imageClass"
    :style="imageStyle"
    @load="onImageLoad"
    @error="onImageError"
  />
</template>

<style scoped>
@keyframes cczj-image-fade-in {
  from { opacity: 0; }
  to { opacity: 1; }
}

@keyframes cczj-image-fade-out {
  from { opacity: 1; }
  to { opacity: 0; }
}

/* forwards 让动画停在末值：淡出之后新图还没解完码，这一格得一直是灭的。 */
img.cczj-image-in {
  animation: cczj-image-fade-in var(--cczj-motion-normal) var(--cczj-motion-ease-enter) forwards;
}
img.cczj-image-out {
  animation: cczj-image-fade-out var(--cczj-motion-fast) var(--cczj-motion-ease-exit) forwards;
}
</style>
