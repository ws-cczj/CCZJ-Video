<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { Button } from './ui'
import Icon from './Icon.vue'
import type { LogRecord } from '../stores/logs'

defineProps<{ record: LogRecord; originLabel: string }>()
defineEmits<{ copy: [] }>()
const { t } = useI18n()
</script>

<template>
  <div class="log-record-details cczj-flex cczj-flex-col cczj-gap-6 cczj-min-w-0">
    <div class="log-record-meta cczj-flex cczj-flex-wrap cczj-gap-4 cczj-text-sm cczj-text-muted">
      <strong>{{ record.level }}</strong>
      <span>{{ new Date(record.ts).toLocaleString() }}</span>
      <span>{{ originLabel }}</span>
      <span v-if="record.persisted === false">{{ t('logs.memoryOnly') }}</span>
    </div>
    <Button variant="secondary" size="sm" class="cczj-self-start" @click="$emit('copy')"><Icon name="copy" :size="14" /> {{ t('logs.copyLine') }}</Button>
    <pre class="log-record-message cczj-whitespace-pre cczj-p-6 cczj-text-sm cczj-text-primary cczj-border cczj-rounded-md">{{ record.message }}</pre>
    <details class="log-record-context cczj-text-sm cczj-text-muted">
      <summary>{{ t('logs.recordContext') }}</summary>
      <div class="log-record-meta cczj-flex cczj-flex-col cczj-gap-4">
        <span v-if="record.sourceKey">{{ t('logs.originLabel') }}: {{ record.sourceKey }}</span>
        <span v-if="record.caller">{{ record.caller }}</span>
        <span v-if="record.seq">#{{ record.seq }}</span>
        <span v-if="record.sessionId">{{ record.sessionId }}</span>
      </div>
    </details>
  </div>
</template>

<style scoped>
.log-record-meta { overflow-wrap: anywhere; }
.log-record-context summary { min-height: 44px; @apply cczj-cursor-pointer; }
.log-record-message {
  margin: 0;
  overflow-wrap: anywhere;
  background: var(--bg-secondary);
  font-family: ui-monospace, Consolas, monospace;
  line-height: 1.6;
}
</style>
