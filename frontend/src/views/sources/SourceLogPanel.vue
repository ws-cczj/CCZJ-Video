<script setup lang="ts">
import { tr } from '../../locales'
import { useCollectStore } from '../../stores/collect'

/** 卡片展开区 2：采集日志区。只读 collect store 的每源日志。 */
defineProps<{ sourceKey: string }>()

const collectStore = useCollectStore()
</script>

<template>
  <!-- 2. 采集日志区 -->
  <div class="collect-log-panel">
    <div class="clog-title">{{ tr('sources.collectLogTitle', { count: collectStore.getState(sourceKey).log.length }) }}</div>
    <div v-if="collectStore.getState(sourceKey).log.length > 0" class="clog-list">
      <div v-for="(msg, idx) in collectStore.getState(sourceKey).log.slice(-15)" :key="idx" class="clog-line">{{ msg }}</div>
    </div>
    <div v-else class="clog-empty">{{ tr('sources.noLogs') }}</div>
  </div>
</template>

<style scoped>

/* === 2. 采集日志 === */
.collect-log-panel {
  background: rgba(255,255,255,0.02);
  border: 1px solid rgba(255,255,255,0.06);
  border-radius: 10px;
  padding: 12px 14px;
}
.clog-title {
  font-size: 12px; font-weight: 600; color: var(--text-secondary);
  margin-bottom: 8px;
}
.clog-list {
  max-height: 180px;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.clog-list::-webkit-scrollbar { width: 4px; }
.clog-list::-webkit-scrollbar-thumb { background: rgba(255,255,255,0.1); border-radius: 2px; }
.clog-line {
  font-size: 11px; color: var(--text-muted);
  font-family: 'SF Mono', Consolas, monospace;
  padding: 2px 0;
  line-height: 1.5;
}
.clog-empty {
  font-size: 12px; color: var(--text-muted); text-align: center;
  padding: 16px 0;
}

</style>
