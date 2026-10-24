<script setup lang="ts">
import './player/presentation.css'
import { useI18n } from 'vue-i18n'
import Icon from '../components/Icon.vue'
import { formatPlaybackTime as fmt } from '../player/videoInfoFormat'
defineProps<{ playing: boolean; pip: boolean; canPip:boolean; canFullscreen:boolean; current: number; duration: number; speed: number; speedOpen: boolean; hasEpisodes: boolean; episodesOpen: boolean; hasPrev:boolean; hasNext:boolean }>()
const emit = defineEmits<{ play: []; fullscreen: []; pip: []; speedPanel: []; episodesPanel: []; previous: []; next: [] }>()
const { t } = useI18n()
</script>
<template>
  <div class="cczj-mobile-player-controls cczj-mobile-fullscreen-controls" @click.stop @dblclick.stop @pointerdown.stop @wheel.stop>
    <div class="cczj-mobile-transport">
      <button v-if="hasEpisodes || hasPrev || hasNext" class="cczj-mobile-previous" :aria-label="t('player.prev')" :title="t('player.prev')" :disabled="!hasPrev" @click="emit('previous')"><Icon name="prev" :size="22" /></button>
      <button :aria-label="playing ? t('player.pause') : t('player.play')" @click="emit('play')"><Icon :name="playing ? 'pause' : 'play'" :size="22" /></button>
      <button v-if="hasEpisodes || hasPrev || hasNext" class="cczj-mobile-next" :aria-label="t('player.next')" :title="t('player.next')" :disabled="!hasNext" @click="emit('next')"><Icon name="next" :size="22" /></button>
    </div>
    <span class="cczj-mobile-player-time">{{ fmt(current) }} <span>/ {{ fmt(duration) }}</span></span>
    <slot />
    <div class="cczj-mobile-control-popover">
      <button :aria-label="t('player.playbackSpeed')" :aria-expanded="speedOpen" @click="emit('speedPanel')">{{ speed }}x</button>
    </div>
    <button v-if="hasEpisodes" :aria-label="t('detail.episodes')" :aria-expanded="episodesOpen" @click="emit('episodesPanel')">{{ t('detail.episodes') }}</button>
    <button :aria-label="t('player.pip')" :aria-pressed="pip" :disabled="!canPip" @click="emit('pip')"><Icon name="pip" :size="22" /></button>
    <button :aria-label="t('player.exitFullscreen')" :disabled="!canFullscreen" @click="emit('fullscreen')"><Icon name="exit-fullscreen" :size="22" /></button>
  </div>
</template>
