<script setup lang="ts">
import { ref } from 'vue'
import { useConfirmStore } from '../stores/confirm'
import { useI18n } from 'vue-i18n'
import { useApplicationMode } from './applicationMode'
import { APPLICATION_MODES, type ApplicationMode } from './definitions'
import Icon from '../components/Icon.vue'
const appMode = useApplicationMode()
const { t } = useI18n()
const error = ref('')
async function select(mode: ApplicationMode): Promise<void> {
  error.value = ''
  try { await appMode.switchMode(mode,()=>useConfirmStore().confirm({message:t('modes.pauseBeforeSwitch'),okText:t('modes.confirmSwitch')})) } catch { error.value = t('modes.saveFailed') }
}
</script>
<template>
  <section class="mode-switch" aria-labelledby="application-mode-heading">
    <div><h3 id="application-mode-heading">{{ t('modes.title') }}</h3><p>{{ t('modes.hint') }}</p></div>
    <div class="mode-switch-options" role="group" :aria-label="t('modes.title')">
      <button v-for="(item, id) in APPLICATION_MODES" :key="id" :aria-pressed="appMode.mode === id" :disabled="appMode.switching" @click="select(id)">
        <Icon :name="item.icon" :size="20" /><span>{{ t(item.label) }}</span>
      </button>
    </div>
    <p v-if="appMode.switching" role="status">{{ t('modes.switching') }}</p><p v-if="error" class="mode-switch-error" role="alert">{{ error }}</p>
  </section>
</template>
<style scoped>
.mode-switch { padding: 16px 0; @apply cczj-min-w-0; }
h3 { margin: 0 0 6px; @apply cczj-text-base; } p { margin: 0 0 12px; @apply cczj-text-secondary cczj-text-sm; line-height: 1.65; }
.mode-switch-options { @apply cczj-grid; grid-template-columns: repeat(3,minmax(0,1fr)); @apply cczj-gap-4; }
button { @apply cczj-flex cczj-items-center cczj-justify-center cczj-gap-4; min-height: 52px; @apply cczj-p-4 cczj-border; border-radius: 10px; background: var(--bg-input); @apply cczj-text-primary; font: inherit; @apply cczj-text-13 cczj-cursor-pointer; transition: background-color var(--cczj-motion-fast),border-color var(--cczj-motion-fast); }
button[aria-pressed=true] { @apply cczj-border-primary; background: var(--accent-alpha-10); } button:disabled { opacity: .6; cursor: wait; }
.mode-switch-error { color: var(--color-danger,#b42318); @apply cczj-mt-5; }
@media(max-width:480px) { .mode-switch-options button { @apply cczj-flex-wrap cczj-gap-3; min-height: 76px; @apply cczj-text-sm; } .mode-switch-options button > span { flex-basis: 100%; order: 2; } }
</style>
