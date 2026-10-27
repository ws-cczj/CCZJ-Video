<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Button } from '../../components/ui'
import Icon from '../../components/Icon.vue'
import { formatPlaybackTime } from '../videoInfoFormat'

const props = defineProps<{ current: number; duration: number; playing: boolean; boosting: boolean; boostRate: number; muted: boolean; ready: boolean; controlsHost?: HTMLElement }>()
const emit = defineEmits<{ play: []; seek: [seconds: number]; sound: [] }>()
const { t } = useI18n()
const target = ref<number | null>(null)
const time = computed(() => target.value ?? props.current)
const finiteDuration = computed(() => Number.isFinite(props.duration) ? Math.max(0, props.duration) : 0)
const progress = computed(() => finiteDuration.value ? Math.max(0, Math.min(100, time.value / finiteDuration.value * 100)) : 0)
function begin(event: PointerEvent): void {
  if (!event.isPrimary || event.button !== 0) return
  target.value = props.current
  ;(event.currentTarget as HTMLInputElement).setPointerCapture(event.pointerId)
}
function input(event: Event): void {
  const seconds = Number((event.target as HTMLInputElement).value)
  if (target.value !== null) target.value = seconds
  else emit('seek', seconds) // Keyboard and assistive input commit immediately.
}
function end(event: PointerEvent): void {
  if (target.value === null) return
  if (event.type === 'pointerup') emit('seek', target.value)
  target.value = null
}
</script>

<template>
  <div v-if="boosting" class="portrait-boost cczj-absolute cczj-flex cczj-items-center cczj-gap-3" role="status"><Icon name="play" :size="18" />{{ t('shortDrama.boosting',{speed:boostRate}) }}</div>
  <Button v-if="ready && !playing && !boosting" class="portrait-play cczj-absolute" variant="text" icon :aria-label="t('player.play')" @pointerdown.stop @click.stop="emit('play')"><Icon name="play" :size="54" /></Button>
  <Teleport :to="controlsHost" :disabled="!controlsHost">
  <div class="portrait-controls cczj-absolute cczj-flex cczj-flex-col" :class="{'portrait-scrubbing':target !== null,'portrait-controls-wide':!!controlsHost}" @click.stop @dblclick.stop @pointerdown.stop @wheel.stop>
    <Button v-if="muted" class="portrait-sound" variant="overlay" @click="emit('sound')">{{ t('shortDrama.enableSound') }}</Button>
    <slot />
    <div class="portrait-timeline cczj-relative" :style="{'--portrait-progress':`${progress}%`}">
      <div class="portrait-track cczj-absolute" aria-hidden="true"><span /></div>
      <input class="portrait-progress cczj-absolute cczj-w-full" type="range" min="0" :max="finiteDuration" step="0.1" :value="time" :disabled="!finiteDuration" :aria-label="t('player.currentTimeLabel')" :aria-valuetext="`${formatPlaybackTime(time)} / ${formatPlaybackTime(finiteDuration)}`" @pointerdown="begin" @input="input" @pointerup="end" @pointercancel="end" @lostpointercapture="end" />
      <span class="portrait-time cczj-absolute">{{ formatPlaybackTime(time) }} <span>/ {{ formatPlaybackTime(finiteDuration) }}</span></span>
    </div>
    <div v-if="controlsHost" class="portrait-status cczj-relative"><slot name="status" /></div>
  </div>
  </Teleport>
</template>

<style scoped>
.portrait-controls { inset: auto 0 0; z-index: 25; padding: 60px 18px 0; color: #fff; background: linear-gradient(transparent, #0009 45%, #000b); }
.portrait-controls-wide { padding: 80px 24px 0; @apply cczj-pointer-events-auto; }
.portrait-controls-wide .portrait-timeline { margin: 0 -24px; }
.portrait-controls-wide .portrait-time { right: 24px; }
.portrait-status { height: 44px; margin: 0 -24px; background: var(--cczj-drama-chrome); }
.portrait-status :deep(.ctrl-bar) { padding: 4px 16px; background: transparent; }
.portrait-timeline { height: 28px; margin: 0 -18px; }
.portrait-track { left: 0; right: 0; bottom: 0; height: 2px; background: #ffffff38; transition: height .15s; }
.portrait-track span { @apply cczj-block cczj-h-full; width: var(--portrait-progress); background: #fff; border-radius: inherit; }
.portrait-progress { inset: 0; height: 28px; margin: 0; opacity: 0; @apply cczj-cursor-pointer; touch-action: none; }
.portrait-time { right: 0; bottom: 35px; opacity: 0; @apply cczj-whitespace-nowrap cczj-pointer-events-none; font-size: calc(13rem / 14); font-variant-numeric: tabular-nums; transition: opacity .15s; }
.portrait-time span { color: #b9b9c2; }
.portrait-timeline:hover .portrait-track,.portrait-timeline:focus-within .portrait-track,.portrait-scrubbing .portrait-track { height: 4px; }
.portrait-timeline:hover .portrait-time,.portrait-timeline:focus-within .portrait-time,.portrait-scrubbing .portrait-time { opacity: 1; }
.portrait-progress:focus-visible { opacity: 1; outline: 2px solid var(--accent); outline-offset: 2px; accent-color: var(--accent); }
.portrait-scrubbing :deep(.drama-info) { opacity: 0; pointer-events: none; }
.portrait-scrubbing .portrait-time { bottom: 62px; left: 0; right: 0; @apply cczj-text-center; font-size: calc(24rem / 14); }
.portrait-boost { top: 22%; left: 50%; transform: translateX(-50%); z-index: 30; border-radius: var(--radius-md); padding: 10px 16px; background: rgba(0,0,0,.65); color: #fff; @apply cczj-pointer-events-none cczj-whitespace-nowrap; }
.portrait-sound { align-self: flex-start; margin-bottom: 12px; }
.portrait-play { z-index: 24; left: 50%; top: 45%; transform: translate(-50%,-50%); min-width: 88px; min-height: 88px; color: #ffffffbe; background: transparent; }
.portrait-play:hover { color: #fff; background: #0003; }
:global(html[data-cczj-platform=mobile]) .portrait-play { top:50%; min-width:72px; min-height:72px; }
:global(html[data-cczj-platform=mobile]) .portrait-play :deep(svg) { width:44px; height:44px; }
</style>
