<script setup lang="ts">
import { tr } from '../locales'
import { useCollectStore } from '../stores/collect'
import { MotionTransition } from '../components/ui'
const collect = useCollectStore()
</script>
<template>
    <!-- 全局采集进度条 -->
    <MotionTransition preset="slide-up">
      <div v-if="collect.running || collect.done" class="collect-overlay" :class="{ done: collect.done }">
        <div class="collect-bar-inner">
          <div class="collect-header">
            <span class="collect-source">{{ collect.sourceKey }}</span>
            <span v-if="collect.running" class="collect-status running">{{ tr('common.collecting') }}</span>
            <span v-else-if="collect.error" class="collect-status error">{{ tr('common.failed') }}</span>
            <span v-else class="collect-status success">{{ tr('common.done') }}</span>
          </div>
          <div class="progress-track">
            <div class="progress-fill" :style="{ width: collect.progress + '%' }"></div>
          </div>
          <div class="collect-meta">
            <span v-if="collect.total > 0">{{ tr('common.pageOf', { current: collect.current, total: collect.total }) }}</span>
            <span class="collect-last-log">{{ collect.log[collect.log.length - 1] }}</span>
          </div>
        </div>
      </div>
    </MotionTransition>

</template>
