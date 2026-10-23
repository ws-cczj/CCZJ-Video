<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import PlayerDialogFrame from './PlayerDialogFrame.vue'
import type { useVideoInfo } from '../useVideoInfo'
type VideoInfo = ReturnType<typeof useVideoInfo>['videoInfo']['value']
defineProps<{open:boolean;videoInfo:VideoInfo}>()
const emit=defineEmits<{close:[]}>()
const { t }=useI18n()
</script>
<template>
  <PlayerDialogFrame :open="open" :title="t('player.videoInfo')" variant="info" @close="emit('close')">
          <div class="vp-modal-body vp-modal-scroll" v-if="videoInfo">
            <!-- 基本播放信息 -->
            <div class="vp-info-section-title">{{ t('player.playbackInfo') }}</div>
            <div class="vp-info-grid">
              <div class="vp-info-item">
                <span class="vp-info-label">{{ t('player.durationLabel') }}</span>
                <span class="vp-info-val">{{ videoInfo.duration }}</span>
              </div>
              <div class="vp-info-item">
                <span class="vp-info-label">{{ t('player.currentTimeLabel') }}</span>
                <span class="vp-info-val">{{ videoInfo.current }} ({{ videoInfo.progress }})</span>
              </div>
              <div class="vp-info-item">
                <span class="vp-info-label">{{ t('player.playbackSpeed') }}</span>
                <span class="vp-info-val">{{ videoInfo.speed }}</span>
              </div>
              <div class="vp-info-item">
                <span class="vp-info-label">{{ t('player.volume') }}</span>
                <span class="vp-info-val">{{ videoInfo.volume }}</span>
              </div>
              <div v-if="videoInfo.buffered" class="vp-info-item">
                <span class="vp-info-label">{{ t('player.bufferProgress') }}</span>
                <span class="vp-info-val">{{ videoInfo.buffered }}</span>
              </div>
            </div>
            <!-- 视频流信息 -->
            <div class="vp-info-section-title">{{ t('player.videoStream') }}</div>
            <div class="vp-info-grid">
              <div v-if="videoInfo.resolution" class="vp-info-item">
                <span class="vp-info-label">{{ t('player.resolution') }}</span>
                <span class="vp-info-val">{{ videoInfo.resolution }}</span>
              </div>
              <div v-if="videoInfo.fps" class="vp-info-item">
                <span class="vp-info-label">{{ t('player.frameRate') }}</span>
                <span class="vp-info-val">{{ videoInfo.fps }}</span>
              </div>
              <div v-if="videoInfo.droppedFrames > 0" class="vp-info-item">
                <span class="vp-info-label">{{ t('player.droppedFrames') }}</span>
                <span class="vp-info-val vp-info-warn">{{ videoInfo.droppedFrames }}</span>
              </div>
              <div v-if="videoInfo.streamBitrate" class="vp-info-item">
                <span class="vp-info-label">{{ t('player.sourceBitrate') }}</span>
                <span class="vp-info-val vp-info-highlight">{{ videoInfo.streamBitrate }}</span>
              </div>
              <div v-if="videoInfo.currentBitrate" class="vp-info-item">
                <span class="vp-info-label">{{ t('player.currentBitrate') }}</span>
                <span class="vp-info-val">{{ videoInfo.currentBitrate }}</span>
              </div>
              <div v-if="videoInfo.codec" class="vp-info-item">
                <span class="vp-info-label">{{ t('player.codecFormat') }}</span>
                <span class="vp-info-val">{{ videoInfo.codec }}</span>
              </div>
              <div v-if="videoInfo.hlsLevelInfo" class="vp-info-item">
                <span class="vp-info-label">{{ t('player.hlsLevel') }}</span>
                <span class="vp-info-val">{{ videoInfo.hlsLevelInfo }}</span>
              </div>
            </div>
            <!-- 缓存 & 网络 -->
            <div class="vp-info-section-title">{{ t('player.cacheAndNetwork') }}</div>
            <div class="vp-info-grid">
              <div class="vp-info-item vp-info-full">
                <span class="vp-info-label">{{ t('player.cacheStatus') }}</span>
                <span class="vp-info-val">{{ videoInfo.cacheInfo }}</span>
              </div>
              <div class="vp-info-item">
                <span class="vp-info-label">{{ t('player.cacheModeLabel') }}</span>
                <span class="vp-info-val">{{ videoInfo.cacheMode }}</span>
              </div>
              <div v-if="videoInfo.networkMode" class="vp-info-item">
                <span class="vp-info-label">{{ t('player.networkModeLabel') }}</span>
                <span class="vp-info-val">{{ videoInfo.networkMode }}</span>
              </div>
            </div>
            <!-- 增强 & 源 -->
            <div class="vp-info-section-title">{{ t('player.enhanceAndSource') }}</div>
            <div class="vp-info-grid">
              <div class="vp-info-item">
                <span class="vp-info-label">{{ t('player.qualityModeLabel') }}</span>
                <span class="vp-info-val">{{ videoInfo.qualityMode }}</span>
              </div>
              <div v-if="videoInfo.sourceHost" class="vp-info-item vp-info-full">
                <span class="vp-info-label">{{ t('player.sourceHost') }}</span>
                <span class="vp-info-val vp-info-mono">{{ videoInfo.sourceHost }}</span>
              </div>
              <div v-if="videoInfo.sourceUrl" class="vp-info-item vp-info-full">
                <span class="vp-info-label">{{ t('player.sourceUrlLabel') }}</span>
                <span class="vp-info-val vp-info-mono vp-info-url">{{ videoInfo.sourceUrl }}</span>
              </div>
            </div>
          </div>
          <div class="vp-modal-body" v-else>
            <p class="vp-empty-text">{{ t('player.noVideoInfo') }}</p>
          </div>

  </PlayerDialogFrame>
</template>
<style scoped src="../../styles/components/player-video-info.css"></style>
