<script setup lang="ts">
import { tr } from '../../locales'
import { useCollectStore } from '../../stores/collect'
import { useSourceDisplay } from './useSourceDisplay'

/** 卡片展开区 1：采集进度面板。只读 collect store，不发任何请求。 */
defineProps<{ sourceKey: string }>()

const collectStore = useCollectStore()
const { isRunning, isPaused, progressFor } = useSourceDisplay()
</script>

<template>
  <!-- 1. 采集进度面板（仅运行时显示） -->
  <div v-if="isRunning(sourceKey) || isPaused(sourceKey)" class="collect-progress-panel">
    <div class="cp-progress-wrap">
      <div class="cp-progress-track">
        <div class="cp-progress-fill" :style="{ width: progressFor(sourceKey) + '%' }"></div>
      </div>
      <span class="cp-progress-pct">{{ progressFor(sourceKey) }}%</span>
    </div>
    <div class="cp-stats">
      <div class="cp-stat">
        <span class="cp-stat-label">{{ tr('sources.statPage') }}</span>
        <span class="cp-stat-value">{{ collectStore.getState(sourceKey).page }}/{{ collectStore.getState(sourceKey).total || '?' }}</span>
      </div>
      <div class="cp-stat">
        <span class="cp-stat-label">{{ tr('sources.statVideoCount') }}</span>
        <span class="cp-stat-value">{{ collectStore.getState(sourceKey).videoCount }}</span>
      </div>
      <div class="cp-stat">
        <span class="cp-stat-label">{{ tr('sources.statSpeed') }}</span>
        <span class="cp-stat-value">{{ collectStore.speedStr(sourceKey) }}</span>
      </div>
      <div class="cp-stat">
        <span class="cp-stat-label">{{ tr('sources.statElapsed') }}</span>
        <span class="cp-stat-value">{{ collectStore.elapsedStr(sourceKey) }}</span>
      </div>
      <div class="cp-stat">
        <span class="cp-stat-label">{{ tr('sources.statEta') }}</span>
        <span class="cp-stat-value">{{ collectStore.etaStr(sourceKey) }}</span>
      </div>
    </div>
    <!-- 当前页视频标签 -->
    <div v-if="collectStore.getState(sourceKey).pageNames && collectStore.getState(sourceKey).pageNames.length > 0" class="cp-page-names">
      <span class="cp-page-names-label">{{ tr('sources.pageNamesHeader', { page: collectStore.getState(sourceKey).page, count: collectStore.getState(sourceKey).pageNames.length }) }}</span>
      <div class="cp-name-tags">
        <span v-for="(name, idx) in collectStore.getState(sourceKey).pageNames.slice(0, 15)" :key="idx" class="cp-name-tag">{{ name }}</span>
        <span v-if="collectStore.getState(sourceKey).pageNames.length > 15" class="cp-name-more">+{{ collectStore.getState(sourceKey).pageNames.length - 15 }} {{ tr('common.more') }}</span>
      </div>
    </div>
    <!-- 错误信息 -->
    <div v-if="collectStore.getState(sourceKey).error" class="cp-error">{{ collectStore.getState(sourceKey).error }}</div>
  </div>
</template>

<style scoped>

/* === 1. 采集进度面板 === */
.collect-progress-panel {
  background: rgba(255,255,255,0.03);
  border: 1px solid var(--accent-alpha-15);
  border-radius: 10px;
  padding: 14px 16px;
}
.cp-progress-wrap {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 12px;
}
.cp-progress-track {
  flex: 1; height: 12px;
  background: rgba(255,255,255,0.08);
  border-radius: 6px; overflow: hidden;
}
.cp-progress-fill {
  height: 100%;
  background: linear-gradient(90deg, var(--accent), #818cf8, var(--accent));
  background-size: 200% 100%;
  border-radius: 6px;
  transition: width 0.3s ease;
  animation: gradientMove 2s ease-in-out infinite;
}
@keyframes gradientMove {
  0%, 100% { background-position: 0% 50%; }
  50% { background-position: 100% 50%; }
}
.cp-progress-pct {
  font-size: 16px; font-weight: 700; color: var(--accent);
  font-family: 'SF Mono', Consolas, monospace;
  min-width: 44px; text-align: right;
}
.cp-stats {
  display: flex;
  gap: 16px;
  flex-wrap: wrap;
  margin-bottom: 10px;
}
.cp-stat {
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.cp-stat-label {
  font-size: 10px; color: var(--text-muted); text-transform: uppercase; letter-spacing: 0.3px;
}
.cp-stat-value {
  font-size: 13px; color: var(--text-primary); font-weight: 600;
  font-family: 'SF Mono', Consolas, monospace;
}
.cp-page-names {
  margin-top: 8px;
}
.cp-page-names-label {
  font-size: 11px; color: var(--text-muted); font-weight: 600; display: block; margin-bottom: 6px;
}
.cp-name-tags {
  display: flex; flex-wrap: wrap; gap: 4px;
}
.cp-name-tag {
  padding: 3px 10px; border-radius: 12px;
  background: rgba(255,255,255,0.05);
  border: 1px solid rgba(255,255,255,0.08);
  font-size: 11px; color: var(--text-secondary);
  max-width: 200px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.cp-name-more {
  padding: 3px 10px; border-radius: 12px;
  background: var(--accent-alpha-10);
  color: var(--accent); font-size: 11px; font-weight: 600;
}
.cp-error {
  margin-top: 10px; padding: 8px 12px;
  background: rgba(239,83,80,0.1);
  border: 1px solid rgba(239,83,80,0.3);
  border-radius: 8px;
  color: #ef5350; font-size: 12px; font-weight: 500;
}

</style>
