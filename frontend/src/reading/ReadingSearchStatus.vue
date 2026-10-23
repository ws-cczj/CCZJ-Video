<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { Button, Spinner } from '../components/ui'
const props = defineProps<{
  searched: number
  pending: number
  loading: boolean
  partialMatch?: boolean
  failures: { sourceKey: string; sourceName: string; errorMessage: string; errorCode: string }[]
}>()
defineEmits<{ stop: [] }>()
const { t } = useI18n()
</script>

<template>
  <aside class="reading-search-status" :aria-label="t('sourceRouting.searchStatus')">
    <div class="cczj-flex cczj-items-center cczj-justify-between cczj-flex-wrap cczj-gap-4">
      <p class="cczj-flex cczj-items-center cczj-flex-wrap cczj-gap-4" role="status">
        <Spinner v-if="loading" size="sm" />
        <span>{{ t(loading ? 'sourceRouting.searchingCount' : 'sourceRouting.searchedCount', { count: searched }) }}</span>
        <span v-if="!loading && pending">{{ t('sourceRouting.pendingCount', { count: pending }) }}</span>
      </p>
      <Button v-if="loading" size="sm" variant="text" type="button" @click="$emit('stop')">{{ t('sourceRouting.stopSearch') }}</Button>
    </div>
    <p v-if="partialMatch&&!loading" class="reading-search-match-note">{{ t('sourceRouting.partialMatch') }}</p>
    <details v-if="props.failures.length" class="reading-search-issues">
      <summary class="cczj-cursor-pointer">{{ t('sourceRouting.sourceIssues', { count: failures.length }) }}</summary>
      <ul class="cczj-grid cczj-gap-5 cczj-max-h-64 cczj-overflow-auto">
        <li v-for="failure in failures" :key="failure.sourceKey" class="cczj-grid cczj-gap-2">
          <span>{{ failure.sourceName }}</span><small class="cczj-wrap-anywhere">{{ failure.errorMessage || failure.errorCode }}</small>
        </li>
      </ul>
    </details>
  </aside>
</template>

<style scoped>
.reading-search-status { color: var(--reading-muted); font-size: 12px; margin: 0 0 20px; }
.reading-search-status p { margin: 0; line-height: 1.8; }
.reading-search-status .reading-search-match-note { margin-top: 8px; color: var(--reading-ink); }
.reading-search-issues { margin-top: 8px; }
.reading-search-issues summary { width: fit-content; padding: 6px 0; }
.reading-search-issues ul { list-style: none; margin: 8px 0 0; padding: 12px 16px; border-radius: 10px; background: var(--reading-surface); border: 1px solid var(--reading-line); }
.reading-search-issues small { color: var(--reading-muted); line-height: 1.5; }
</style>
