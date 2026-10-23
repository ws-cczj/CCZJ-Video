<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, shallowRef, watch } from 'vue'
import type { ComponentPublicInstance } from 'vue'
import { getProxiedImageUrl } from '../utils'
import imageNotFound from '../assets/images/image_notfound.png'
import { useMotionStore } from '../stores/motion'
import { useViewActivity, windowForeground } from '../composables/useViewActivity'
import { whenImageNearViewport } from '../utils/imageVisibility'
import { deferImageRelease } from '../utils/imageRelease'

defineOptions({ inheritAttrs: false })

interface Props {
  src?: string | null
  alt?: string
  fallback?: string
  loading?: 'lazy' | 'eager'
  animate?: boolean
}

const props = withDefaults(defineProps<Props>(), {
  src: '',
  alt: '',
  fallback: imageNotFound,
  loading: 'eager',
  animate: true,
})

const motion = useMotionStore()

const resolvedSrc = ref('')
const imageEl = shallowRef<HTMLImageElement | null>(null)
const active = useViewActivity()
let stopObserving: (() => void) | null = null
let disposed = false
let resolving = false
let cancelRelease: (() => void) | null = null
let lastResolvedInput = ''
let lastShownInput: string | null = null
const animateImage = ref(props.animate)
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
 * 代理请求未返回时，旧图的 load 不能把闸门提前放开。隐藏或卸载会递增请求号，
 * 取消淡出与观察，晚返回的结果也不能重新填入已经释放的图片节点。
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
// An empty fallback reveals the parent's cover artwork. Keep the lazy-image
// node for observation, but do not paint its native broken-image indicator.
const imageClass = computed(() => (resolvedSrc.value && phase.value === 'shown' && animateImage.value ? 'cczj-image-in' : phase.value === 'dip' ? 'cczj-image-out' : ''))
const imageStyle = computed(() => ({ opacity: resolvedSrc.value && phase.value === 'shown' ? '1' : '0' }))

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
    lastShownInput = lastResolvedInput
    phase.value = 'shown'
    settling.value = false
    return
  }
  const dip = animateImage.value ? motion.dipMs() : 0
  if (dip <= 0 || phase.value !== 'shown') {
    resolvedSrc.value = next
    return
  }
  phase.value = 'dip'
  dipTimer = window.setTimeout(() => {
    dipTimer = null
    resolvedSrc.value = next
    phase.value = 'hidden'
  }, dip)
}

async function resolveImage(rawSrc: string | null | undefined): Promise<void> {
  const token = ++requestToken.value
  const source = String(rawSrc || '').trim()
  settling.value = true
  resolving = true
  if (!source) {
    if (token === requestToken.value && active.value && !disposed) {
      resolving = false
      lastResolvedInput = source
      animateImage.value = props.animate && lastShownInput !== source
      show(props.fallback)
    }
    return
  }

  let nextSrc = source
  if (/^https?:\/\//i.test(source)) {
    nextSrc = await getProxiedImageUrl(source)
  }

  if (token === requestToken.value && active.value && !disposed) {
    resolving = false
    lastResolvedInput = source
    animateImage.value = props.animate && lastShownInput !== source
    show(nextSrc || props.fallback)
  }
}

function onImageLoad(): void {
  if (!active.value || disposed || resolving || !resolvedSrc.value || phase.value === 'dip' || lastResolvedInput !== String(props.src || '').trim()) return
  lastShownInput = lastResolvedInput
  phase.value = 'shown'
  settling.value = false
}

function onImageError(): void {
  if (!active.value || disposed || resolving || !resolvedSrc.value) return
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
  imageEl.value = el instanceof HTMLImageElement ? el : null
  const expected = resolvedSrc.value.startsWith('data:') ? resolvedSrc.value : resolvedSrc.value ? new URL(resolvedSrc.value, document.baseURI).href : ''
  if (active.value && !resolving && phase.value !== 'dip' && lastResolvedInput === String(props.src || '').trim() && expected && el instanceof HTMLImageElement && el.currentSrc === expected && el.complete && el.naturalWidth > 0) {
    lastShownInput = lastResolvedInput
    phase.value = 'shown'
    settling.value = false
  }
}

function releaseSource(): void {
  resolvedSrc.value = ''
  phase.value = 'hidden'
}
function scheduleImage(): void {
  stopObserving?.()
  stopObserving = null
  clearDip()
  requestToken.value++
  settling.value = true
  if (!active.value || disposed) {
    resolving = false
    if (!windowForeground.value || disposed) {
      cancelRelease?.()
      cancelRelease = null
      releaseSource()
    } else if (!cancelRelease) {
      cancelRelease = deferImageRelease(() => {
        cancelRelease = null
        if (!active.value) releaseSource()
      })
    }
    return
  }
  cancelRelease?.()
  cancelRelease = null
  if (resolvedSrc.value && phase.value === 'shown' && lastResolvedInput === String(props.src || '').trim()) {
    animateImage.value = false
    settling.value = false
    return
  }
  if (props.loading === 'lazy') {
    if (imageEl.value) stopObserving = whenImageNearViewport(imageEl.value, () => {
      stopObserving = null
      if (active.value && !disposed) void resolveImage(props.src)
    })
  } else void resolveImage(props.src)
}
watch([active, () => props.src, () => props.loading], scheduleImage, { immediate: true })
onMounted(() => { if (props.loading === 'lazy') scheduleImage() })
onBeforeUnmount(() => { disposed = true; scheduleImage() })
</script>

<template>
  <img
    v-bind="$attrs"
    :ref="onImgEl"
    :src="resolvedSrc || undefined"
    :loading="loading"
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
