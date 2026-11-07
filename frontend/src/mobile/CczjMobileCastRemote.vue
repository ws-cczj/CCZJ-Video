<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import '../player/controlPalette.css'
import { useI18n } from 'vue-i18n'
import Icon from '../components/Icon.vue'
import { Button } from '../components/ui'
import { formatPlaybackTime } from '../player/videoInfoFormat'

const props = withDefaults(defineProps<{
  layout?: 'compact' | 'page' | 'fullscreen'; title: string; device: string; status: string; paused: boolean; busy: boolean
  active: boolean; attention: boolean; position: number; duration: number; hasPosition: boolean
  hasLines?: boolean
}>(), { layout: 'page' })
const emit = defineEmits<{ play: []; stop: []; seek: [seconds: number]; change: []; lines: []; expand: []; exitFullscreen: [] }>()
const { t } = useI18n()
const compact = computed(() => props.layout === 'compact')
const preview = ref<number | null>(null)
const duration = computed(() => Number.isFinite(props.duration) ? Math.max(0, props.duration) : 0)
const canSeek = computed(() => !props.busy && props.active && props.hasPosition && duration.value > 0)
const current = computed(() => Math.max(0, Math.min(duration.value, preview.value ?? props.position)))
const progress = computed(() => duration.value ? current.value / duration.value * 100 : 0)
function seek(event: Event): void {
  const value = Number((event.target as HTMLInputElement).value)
  preview.value = null
  if (canSeek.value && Number.isFinite(value)) emit('seek', value)
}
watch(canSeek, value => { if (!value) preview.value = null })
</script>

<template>
  <section class="cczj-cast-remote cczj-player-dark-controls" :class="`cczj-cast-remote-${layout}`" :aria-label="t('mobile.castControls')" @click.stop @dblclick.stop @pointerdown.stop @keydown="layout !== 'page' && $event.key !== 'Escape' && $event.key !== 'Tab' && $event.stopPropagation()">
    <header v-if="layout === 'fullscreen'" class="cczj-cast-remote-heading">
      <strong class="cczj-flex-1 cczj-min-w-0 cczj-truncate cczj-font-normal">{{ title }}</strong>
      <Button variant="text" icon :disabled="busy" :aria-label="t('mobile.castStop')" @click="emit('stop')"><Icon name="power" :size="24" /></Button>
    </header>
    <div class="cczj-cast-remote-surface">
      <div class="cczj-cast-remote-device">
        <div class="cczj-cast-remote-device-name"><strong class="cczj-truncate cczj-font-normal" :title="device">{{ device }}</strong></div>
        <div class="cczj-cast-remote-device-actions cczj-flex cczj-flex-wrap cczj-items-center cczj-justify-center cczj-gap-3">
          <Button class="cczj-cast-remote-change" :variant="compact ? 'text' : 'secondary'" size="sm" :disabled="busy" @click="emit('change')">{{ t('mobile.castChangeDevice') }}</Button>
          <Button v-if="hasLines" data-cast-action="lines" :variant="compact ? 'text' : 'secondary'" :icon="compact" size="sm" :disabled="busy" :aria-label="t('mobile.castLines')" @click="emit('lines')"><Icon v-if="compact" name="source-switch" :size="20"/><span v-else>{{t('mobile.castLines')}}</span></Button>
        </div>
        <Button v-if="compact" variant="text" icon :disabled="busy" :aria-label="t('mobile.castStop')" @click="emit('stop')"><Icon name="power" :size="20" /></Button>
        <p v-if="!compact" role="status" :class="attention ? 'cczj-cast-remote-status' : 'cczj-sr-only'">{{ status }}</p>
      </div>
      <div class="cczj-cast-remote-main">
        <p v-if="compact" role="status" class="cczj-cast-remote-status">{{ status }}</p>
        <div class="cczj-cast-remote-transport">
          <Button variant="text" icon class="cczj-cast-remote-skip" :disabled="!canSeek" :aria-label="t('mobile.castRewind')" @click="emit('seek', Math.max(0, position - 10))"><Icon name="rewind-10" :size="compact ? 28 : 36" /></Button>
          <Button variant="text" icon class="cczj-cast-remote-play" :disabled="busy || !active" :aria-label="t(paused ? 'player.play' : 'player.pause')" @click="emit('play')"><Icon :name="paused ? 'play' : 'pause'" :size="compact ? 30 : 48" /></Button>
          <Button variant="text" icon class="cczj-cast-remote-skip" :disabled="!canSeek" :aria-label="t('mobile.castForward')" @click="emit('seek', Math.min(duration, position + 10))"><Icon name="forward-10" :size="compact ? 28 : 36" /></Button>
        </div>
      </div>
      <div class="cczj-cast-remote-timeline">
        <div class="cczj-flex cczj-items-center cczj-gap-4">
          <input class="cczj-cast-remote-range" :style="{'--cczj-cast-progress':`${progress}%`}" type="range" min="0" :max="duration" step="1" :value="current" :disabled="!canSeek" :aria-label="t('mobile.castProgress')" :aria-valuetext="hasPosition ? `${formatPlaybackTime(current)} / ${formatPlaybackTime(duration)}` : t('mobile.castPositionUnavailable')" @input="preview = Number(($event.target as HTMLInputElement).value)" @change="seek" @pointercancel="preview = null" />
          <Button v-if="compact" variant="text" icon :aria-label="t('mobile.castExpand')" @click="emit('expand')"><Icon name="fullscreen" :size="20" /></Button>
          <Button v-else-if="layout === 'fullscreen'" variant="text" icon :aria-label="t('player.exitFullscreen')" @click="emit('exitFullscreen')"><Icon name="exit-fullscreen" :size="22" /></Button>
        </div>
        <div v-if="hasPosition && duration > 0" class="cczj-cast-remote-time"><span>{{ formatPlaybackTime(current) }}</span><span>{{ formatPlaybackTime(duration) }}</span></div>
        <p v-else class="cczj-cast-remote-time-unavailable">{{ t('mobile.castPositionUnavailable') }}</p>
      </div>
    </div>
  </section>
</template>
