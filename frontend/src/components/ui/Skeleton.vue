<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

// Text, list and card variants reserve content geometry during the first load.
const props = withDefaults(defineProps<{
  loading?: boolean; variant?: 'text' | 'cards' | 'list'; rows?: number; columns?: number; title?: boolean; label?: string
}>(), { loading: true, variant: 'text', rows: 3, columns: 2, title: true })
const { t } = useI18n()
const count = computed(() => Math.max(1, Math.min(12, Math.floor(props.rows) || 3)))
const columnCount = computed(() => Math.max(1, Math.min(6, Math.floor(props.columns) || 2)))
</script>

<template>
  <div v-if="loading" class="ui-skeleton cczj-w-full" role="status" aria-busy="true" :aria-label="label || t('common.loading')">
    <div aria-hidden="true" class="cczj-flex cczj-flex-col cczj-gap-6">
      <div v-if="title" class="ui-skeleton__title ui-skeleton__shape" />
      <div v-if="variant==='cards'" class="ui-skeleton__cards cczj-grid cczj-gap-6" :style="{gridTemplateColumns:`var(--cczj-skeleton-columns,repeat(${columnCount},minmax(0,1fr)))`}">
        <div v-for="index in count*columnCount" :key="index" class="ui-skeleton__card cczj-flex cczj-flex-col cczj-gap-4">
          <div class="ui-skeleton__poster ui-skeleton__shape" /><div class="ui-skeleton__caption"><div class="ui-skeleton__line ui-skeleton__shape" /></div>
        </div>
      </div>
      <template v-else>
        <div v-for="index in count" :key="index" class="cczj-flex cczj-items-center cczj-gap-6">
          <div v-if="variant==='list'" class="ui-skeleton__avatar ui-skeleton__shape cczj-flex-shrink-0" />
          <div class="cczj-flex cczj-flex-col cczj-gap-4 cczj-flex-1 cczj-min-w-0">
            <div class="ui-skeleton__line ui-skeleton__shape" /><div v-if="variant==='list'" class="ui-skeleton__short ui-skeleton__shape" />
          </div>
        </div>
      </template>
    </div>
  </div>
  <div v-else class="ui-skeleton__content"><slot /></div>
</template>

<style scoped>
.ui-skeleton__shape { background:var(--bg-secondary); border-radius:var(--radius-sm); }
.ui-skeleton__title { width:40%; height:24px; }
.ui-skeleton__line { height:16px; }
.ui-skeleton__short { width:65%; height:12px; }
.ui-skeleton__poster { aspect-ratio:2/3; }
.ui-skeleton__avatar { width:44px; height:44px; }
</style>
