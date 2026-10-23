<script setup lang="ts">
import './player/presentation.css'
import { useI18n } from 'vue-i18n'
import Icon from '../components/Icon.vue'
import { formatPlaybackTime as fmt } from '../player/videoInfoFormat'
defineProps<{ playing: boolean; pip: boolean; canPip:boolean; canFullscreen:boolean; current: number; duration: number; hasEpisodes: boolean; episodesOpen: boolean }>()
const emit = defineEmits<{ play: []; fullscreen: []; pip: []; episodesPanel: [] }>()
const { t } = useI18n()
</script>
<template>
  <div class="cczj-mobile-player-controls" @click.stop @dblclick.stop @pointerdown.stop @wheel.stop>
    <button class="cczj-mobile-play" :aria-label="playing ? t('player.pause') : t('player.play')" @click="emit('play')"><Icon :name="playing ? 'pause' : 'play'" :size="22" /></button>
    <span class="cczj-mobile-player-time">{{ fmt(current) }} <span>/ {{ fmt(duration) }}</span></span>
    <button v-if="hasEpisodes" :aria-label="t('detail.episodes')" :aria-expanded="episodesOpen" @click="emit('episodesPanel')">{{ t('detail.episodes') }}</button>
    <button :aria-label="t('player.pipWithHotkey')" :aria-pressed="pip" :disabled="!canPip" @click="emit('pip')"><Icon name="pip" :size="22" /></button>
    <button class="cczj-mobile-fullscreen" :aria-label="t('player.fullscreen')" :disabled="!canFullscreen" @click="emit('fullscreen')"><Icon name="fullscreen" :size="22" /></button>
  </div>
</template>
