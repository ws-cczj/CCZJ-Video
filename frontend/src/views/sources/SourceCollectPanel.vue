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
  @apply cczj-flex cczj-items-center cczj-gap-6 cczj-mb-6;
}
.cp-progress-track {
  @apply cczj-flex-1; height: 12px;
  background: rgba(255,255,255,0.08);
  @apply cczj-rounded cczj-overflow-hidden;
}
.cp-progress-fill {
  @apply cczj-h-full;
  background: linear-gradient(90deg, var(--accent), #818cf8, var(--accent));
  background-size: 200% 100%;
  @apply cczj-rounded;
  transition: width 0.3s ease;
  animation: gradientMove 2s ease-in-out infinite;
}
@keyframes gradientMove {
  0%, 100% { background-position: 0% 50%; }
  50% { background-position: 100% 50%; }
}
.cp-progress-pct {
  @apply cczj-text-lg cczj-font-bold cczj-text-accent;
  font-family: 'SF Mono', Consolas, monospace;
  min-width: 44px; @apply cczj-text-right;
}
.cp-stats {
  @apply cczj-flex cczj-gap-8 cczj-flex-wrap cczj-mb-5;
}
.cp-stat {
  @apply cczj-flex cczj-flex-col cczj-gap-1;
}
.cp-stat-label {
  @apply cczj-text-xs cczj-text-muted; text-transform: uppercase; letter-spacing: 0.3px;
}
.cp-stat-value {
  @apply cczj-text-13 cczj-text-primary cczj-font-semibold;
  font-family: 'SF Mono', Consolas, monospace;
}
.cp-page-names {
  @apply cczj-mt-4;
}
.cp-page-names-label {
  @apply cczj-text-11 cczj-text-muted cczj-font-semibold cczj-block cczj-mb-3;
}
.cp-name-tags {
  @apply cczj-flex cczj-flex-wrap cczj-gap-2;
}
.cp-name-tag {
  @apply cczj-px-5-py-2 cczj-rounded-lg;
  background: rgba(255,255,255,0.05);
  border: 1px solid rgba(255,255,255,0.08);
  @apply cczj-text-11 cczj-text-secondary;
  max-width: 200px; @apply cczj-overflow-hidden; text-overflow: ellipsis; @apply cczj-whitespace-nowrap;
}
.cp-name-more {
  @apply cczj-px-5-py-2 cczj-rounded-lg;
  background: var(--accent-alpha-10);
  @apply cczj-text-accent cczj-text-11 cczj-font-semibold;
}
.cp-error {
  @apply cczj-mt-5 cczj-px-6-py-4;
  background: rgba(239,83,80,0.1);
  border: 1px solid rgba(239,83,80,0.3);
  @apply cczj-rounded-md;
  color: #ef5350; @apply cczj-text-sm cczj-font-medium;
}

</style>
