<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '../components/Icon.vue'
import { Button } from '../components/ui'
import { formatPlaybackTime } from '../player/videoInfoFormat'

const props = defineProps<{
  compact?: boolean; device: string; status: string; paused: boolean; busy: boolean
  active: boolean; position: number; duration: number; hasPosition: boolean
}>()
const emit = defineEmits<{ play: []; stop: []; seek: [seconds: number]; change: []; expand: [] }>()
const { t } = useI18n()
const preview = ref<number | null>(null)
const duration = computed(() => Number.isFinite(props.duration) ? Math.max(0, props.duration) : 0)
const canSeek = computed(() => !props.busy && props.hasPosition && duration.value > 0)
const current = computed(() => Math.max(0, Math.min(duration.value, preview.value ?? props.position)))
function seek(event: Event): void {
  const value = Number((event.target as HTMLInputElement).value)
  preview.value = null
  if (canSeek.value && Number.isFinite(value)) emit('seek', value)
}
watch(canSeek, value => { if (!value) preview.value = null })
</script>

<template>
  <section class="cczj-cast-remote" :class="{ 'cczj-cast-remote-compact': compact }" :aria-label="t('mobile.castControls')" @click.stop @dblclick.stop @pointerdown.stop @keydown="compact && $event.key !== 'Escape' && $event.key !== 'Tab' && $event.stopPropagation()">
    <div class="cczj-cast-remote-device">
      <Icon name="cast" :size="20" />
      <strong class="cczj-truncate" :title="device">{{ device }}</strong>
      <Button variant="text" size="sm" :disabled="busy" @click="emit('change')">{{ t('mobile.castChangeDevice') }}</Button>
      <Button variant="text" icon :disabled="busy" :aria-label="t('mobile.castStop')" @click="emit('stop')"><Icon name="stop" :size="20" /></Button>
    </div>
    <div class="cczj-cast-remote-main">
      <p role="status" class="cczj-cast-remote-status">{{ status }}</p>
      <div class="cczj-cast-remote-transport">
        <Button variant="text" class="cczj-cast-remote-skip" :disabled="!canSeek" :aria-label="t('mobile.castRewind')" @click="emit('seek', Math.max(0, position - 10))"><Icon name="chevron-left" :size="26" /><span>10</span></Button>
        <Button variant="text" icon class="cczj-cast-remote-play" :disabled="busy || !active" :aria-label="t(paused ? 'player.play' : 'player.pause')" @click="emit('play')"><Icon :name="paused ? 'play' : 'pause'" :size="compact ? 30 : 42" /></Button>
        <Button variant="text" class="cczj-cast-remote-skip" :disabled="!canSeek" :aria-label="t('mobile.castForward')" @click="emit('seek', Math.min(duration, position + 10))"><span>10</span><Icon name="chevron-right" :size="26" /></Button>
      </div>
      <p v-if="!compact && active" class="cczj-cast-remote-hint">{{ t('mobile.castScreenOff') }}</p>
    </div>
    <div class="cczj-cast-remote-timeline">
      <div class="cczj-flex cczj-items-center cczj-gap-4">
        <input class="cczj-cast-remote-range" type="range" min="0" :max="duration" step="1" :value="current" :disabled="!canSeek" :aria-label="t('mobile.castProgress')" :aria-valuetext="hasPosition ? `${formatPlaybackTime(current)} / ${formatPlaybackTime(duration)}` : t('mobile.castPositionUnavailable')" @input="preview = Number(($event.target as HTMLInputElement).value)" @change="seek" @pointercancel="preview = null" />
        <Button v-if="compact" variant="text" icon :aria-label="t('mobile.castExpand')" @click="emit('expand')"><Icon name="fullscreen" :size="20" /></Button>
      </div>
      <p class="cczj-cast-remote-time">{{ hasPosition && duration > 0 ? `${formatPlaybackTime(current)} / ${formatPlaybackTime(duration)}` : t('mobile.castPositionUnavailable') }}</p>
    </div>
  </section>
</template>
