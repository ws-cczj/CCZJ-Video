<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '../../components/Icon.vue'
import PlayerVolumeControls from './PlayerVolumeControls.vue'
import PlayerSpeedControls from './PlayerSpeedControls.vue'
import { useTimeJump } from '../useTimeJump'
import { formatPlaybackTime as fmt } from '../videoInfoFormat'
import type { PlaybackControlState, DesktopPlaybackCommands } from './controlState'
const props = defineProps<{ playback: PlaybackControlState; commands: DesktopPlaybackCommands; visible: boolean; minimal?: boolean }>()
const emit = defineEmits<{toolsHost:[element:HTMLElement|null]}>()
const { t } = useI18n()
const showVolumePanel = ref(false), showSpeedPanel = ref(false)
const toolsHost = ref<HTMLElement|null>(null)
const { showTimeInput, timeInputValue, timeInputRef, toggleTimeInput, jumpToTime, cancelTimeInput } = useTimeJump({
  video: () => props.commands.video(), current: () => props.playback.current, duration: () => props.playback.duration,
})
const engaged = computed(() => showVolumePanel.value || showSpeedPanel.value || showTimeInput.value)
function close(): void { showVolumePanel.value=false;showSpeedPanel.value=false;cancelTimeInput() }
function selectSpeed(value:number): void { props.commands.speed(value);showSpeedPanel.value=false }
watch(toolsHost, element => emit('toolsHost', element), {flush:'post'})
onBeforeUnmount(() => emit('toolsHost',null))
defineExpose({ engaged, close })
</script>
<template>
    <div class="ctrl-bar" :class="{'ctrl-bar-minimal':minimal}" v-show="visible || engaged" @click.stop @mousedown.stop
      @pointerdown.stop @dblclick.stop @wheel.stop>
      <div class="ctrl-primary">
      <!-- 上一集 -->
      <button v-if="playback.hasPrev" class="ctrl-btn" data-player-action="prev" @click="commands.previous()" :aria-label="t('player.prev')" :title="t('player.prev')">
        <Icon name="prev" :size="16" />
      </button>

      <button class="ctrl-btn play-btn" data-player-action="play" @click="commands.play()"
        :aria-label="playback.playing ? t('player.pause') : t('player.play')" :title="playback.playing ? t('player.pause') : t('player.play')">
        <Icon :name="playback.playing ? 'pause' : 'play'" :size="18" />
      </button>

      <!-- 下一集 -->
      <button v-if="playback.hasNext" class="ctrl-btn" data-player-action="next" @click="commands.next()" :aria-label="t('player.next')" :title="t('player.next')">
        <Icon name="next" :size="16" />
      </button>

      <!-- B站风格时间显示 / 跳转 -->
      <div class="time-display" @click.stop>
        <template v-if="showTimeInput">
          <input ref="timeInputRef" class="time-input" v-model="timeInputValue" @keydown.enter.prevent="jumpToTime"
            @keydown.escape.prevent="cancelTimeInput" @blur="jumpToTime" @click.stop @mousedown.stop />
        </template>
        <template v-else>
          <div class="time-bar" @click="toggleTimeInput" :title="t('player.seekTimeTip')">
            <span class="time-current">{{ fmt(playback.current) }}</span>
            <span class="time-sep">/</span>
            <span class="time-duration">{{ fmt(playback.duration) }}</span>
          </div>
        </template>
      </div>

      </div>

      <div class="ctrl-center"><slot name="danmaku" /></div>
      <div class="ctrl-secondary">
      <slot name="quality" />
      <PlayerVolumeControls v-if="!minimal" v-model:open="showVolumePanel" :volume="playback.volume" :muted="playback.muted" @mute="commands.mute()" @input="commands.volume" @interaction="commands.interaction()" />
      <PlayerSpeedControls v-if="!minimal" v-model:open="showSpeedPanel" :speed="playback.speed" @change="selectSpeed" @interaction="showVolumePanel=false;commands.interaction()" />
      <slot name="actions" />
      <div ref="toolsHost" style="display:contents" />
      <!-- 全屏 -->
      <button class="ctrl-btn" data-player-action="fullscreen" :disabled="!playback.canFullscreen" @click="commands.fullscreen()" :aria-label="playback.fullscreen ? t('player.exitFullscreen') : t('player.fullscreenHotkey')" :title="playback.fullscreen ? t('player.exitFullscreen') : t('player.fullscreenHotkey')">
        <Icon :name="playback.fullscreen ? 'exit-fullscreen' : 'fullscreen'" :size="16" />
      </button>
      </div>
    </div>


</template>
<style scoped src="../../styles/components/player-desktop-controls.css"></style>
