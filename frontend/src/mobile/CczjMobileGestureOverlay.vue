<script setup lang="ts">
import './player/presentation.css'
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '../components/Icon.vue'
import { formatPlaybackTime } from '../player/videoInfoFormat'

const props = defineProps<{
  kind: 'seek' | 'brightness' | 'volume' | 'speed' | ''
  hud: string
  level: number
  compact: boolean
  target: number
  duration: number
  image: string
  imageVisible: boolean
  imageTime: number
  previewState: 'idle' | 'loading' | 'ready' | 'unavailable'
}>()
const { t } = useI18n()
const value = computed(() => Math.round(Math.max(0, Math.min(1, props.level)) * 100))
</script>

<template>
  <Transition name="cczj-mobile-gesture-feedback">
    <div v-if="hud && kind === 'speed'" class="cczj-mobile-speed-feedback" role="status" aria-live="polite">
      <Icon name="play" :size="18" aria-hidden="true" /><strong>{{ t('mobile.gestureSpeedPlaying', { speed: hud }) }}</strong><small>{{ t('mobile.gestureSpeedRelease') }}</small>
    </div>
    <div v-else-if="hud && kind === 'seek'" class="cczj-mobile-seek-feedback" aria-live="off">
      <div class="cczj-mobile-seek-frame">
        <img v-if="imageVisible && image" :src="image" alt="" />
        <template v-else><span v-if="previewState === 'loading'" class="cczj-mobile-seek-spinner" /><small>{{ t(previewState === 'unavailable' ? 'mobile.previewUnavailable' : 'common.loading') }}</small></template>
        <span v-if="imageVisible && image && Math.abs(imageTime - target) > .25" class="cczj-mobile-seek-frame-time">{{ formatPlaybackTime(imageTime) }}</span>
      </div>
      <div class="cczj-mobile-seek-time"><strong>{{ formatPlaybackTime(Math.max(0,target)) }}</strong><span>/ {{ formatPlaybackTime(duration) }}</span></div>
      <small class="cczj-mobile-seek-delta">{{ hud.split('  ')[1] }}</small>
    </div>
    <div v-else-if="hud && (kind === 'brightness' || kind === 'volume')" class="cczj-mobile-level-feedback" :class="{'cczj-mobile-level-compact':compact}"
      role="meter" :aria-label="t(kind === 'brightness' ? 'mobile.gestureBrightness' : 'mobile.gestureVolume')" :aria-valuenow="value" aria-valuemin="0" aria-valuemax="100">
      <Icon :name="kind === 'brightness' ? 'sun' : value === 0 ? 'volume-off' : 'volume'" :size="22" />
      <span class="cczj-mobile-level-track"><span :style="{transform:`scaleX(${value / 100})`}" /></span>
      <strong>{{ value }}%</strong>
    </div>
  </Transition>
</template>
