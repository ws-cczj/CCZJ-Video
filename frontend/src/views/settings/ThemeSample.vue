<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import Icon from '../../components/Icon.vue'
import { Button, Tag } from '../../components/ui'
defineProps<{ name: string; editable?: boolean }>()
const emit = defineEmits<{ edit: [] }>()
const { t } = useI18n()
</script>
<template>
  <section class="theme-sample cczj-border cczj-rounded-md cczj-p-8 cczj-mb-8" :aria-label="t('settings.previewTheme')">
    <div class="cczj-flex cczj-items-center cczj-flex-wrap cczj-gap-6">
      <strong class="cczj-text-primary cczj-min-w-0">{{ name }}</strong><Tag>{{ t('settings.previewTag') }}</Tag>
    </div>
    <p class="cczj-text-secondary">{{ t('settings.themePreviewText') }}</p>
    <p class="cczj-text-muted">{{ t('settings.themePreviewHint') }}</p>
    <div class="cczj-flex cczj-flex-wrap cczj-items-center cczj-gap-6 cczj-mb-6">
      <span class="theme-action-sample cczj-inline-flex cczj-items-center cczj-gap-4 cczj-rounded-md cczj-p-4"><Icon name="play" :size="15"/>{{t('settings.previewPrimaryAction')}}</span>
      <span class="theme-window-sample cczj-inline-flex cczj-items-center cczj-gap-4 cczj-rounded-md cczj-p-4"><Icon name="minimize" :size="15"/>{{t('common.minimize')}}</span>
      <span class="theme-window-sample cczj-inline-flex cczj-items-center cczj-gap-4 cczj-rounded-md cczj-p-4"><Icon name="maximize" :size="15"/>{{t('common.maximize')}}</span>
    </div>
    <Button v-if="editable" variant="primary" @click="emit('edit')">{{ t('settings.editTheme') }}</Button>
  </section>
</template>
<style scoped>
.theme-sample { background:var(--bg-card);overflow-wrap:anywhere; }
.theme-action-sample { background:var(--btn-solid);color:var(--btn-solid-text); }
.theme-window-sample { background:var(--btn-min);color:var(--btn-min-contrast); }
</style>
