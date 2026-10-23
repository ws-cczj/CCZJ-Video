<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import { GetSetting, SetSetting, GetAllowPrivateNetwork, SetAllowPrivateNetwork, GetDoubanIntervalMinutes, SetDoubanIntervalMinutes } from '../../api/app'
import { useErrorStore } from '../../stores/error'
import { Select } from '../../components/ui'
import { pluginSettingsTabs } from '../../plugins/registry'
const { t } = useI18n()
const emit = defineEmits<{ (e: 'open-group', id: string): void }>()
const errors = useErrorStore()
const freshness = ref('standard')
const interval = ref(30)
const allowPrivate = ref(false)
const ready = ref(false)
const loading = ref(true)
const saving = ref('')
let disposed = false
onBeforeUnmount(() => { disposed = true })
const policies = computed(() => [
  { id: 'fast', title: t('settings.freshnessFast'), detail: t('settings.cacheFastDetail') },
  { id: 'standard', title: t('settings.freshnessStandard'), detail: t('settings.cacheStandardDetail') },
  { id: 'saver', title: t('settings.freshnessSaver'), detail: t('settings.cacheSaverDetail') },
])
const intervalOptions = computed(() => [10,15,20,30,45,60,90,120].map(n => ({ value: String(n), label: t('settings.doubanEvery', { n }) })))
const diagnosticsAvailable = computed(() => pluginSettingsTabs.value.some(tab => tab.id === 'diagnostics'))
async function load(): Promise<void> {
  loading.value = true
  const values = await Promise.allSettled([GetSetting('detail_cache_freshness'), GetDoubanIntervalMinutes(), GetAllowPrivateNetwork()])
  if (disposed) return
  if (values[0].status === 'fulfilled') freshness.value = ['fast','saver'].includes(values[0].value) ? values[0].value : 'standard'
  if (values[1].status === 'fulfilled') interval.value = Number(values[1].value) || 30
  if (values[2].status === 'fulfilled') allowPrivate.value = values[2].value === true
  if (values.some(value => value.status === 'rejected')) errors.fromError(t('settings.dataLoadFailed'), new Error(t('settings.dataLoadFailed')), 'Settings.data')
  ready.value = values.every(value => value.status === 'fulfilled')
  loading.value = false
}
onMounted(load)
async function savePolicy(value: string): Promise<void> {
  if (!ready.value || saving.value) return
  saving.value = 'cache'
  try { await SetSetting('detail_cache_freshness', value); freshness.value = value }
  catch (error) { errors.fromError(t('settings.freshnessSaveFailed'), error, 'Settings.freshness') }
  finally { saving.value = '' }
}
async function saveInterval(value: string | number): Promise<void> {
  if (!ready.value || saving.value) return
  saving.value = 'interval'
  try { interval.value = await SetDoubanIntervalMinutes(Number(value)) }
  catch (error) { errors.fromError(t('settings.doubanIntervalSaveFailed'), error, 'Settings.douban') }
  finally { saving.value = '' }
}
async function saveAccess(event: Event): Promise<void> {
  const next = (event.target as HTMLInputElement).checked
  saving.value = 'access'
  try { await SetAllowPrivateNetwork(next); allowPrivate.value = next }
  catch (error) { (event.target as HTMLInputElement).checked = allowPrivate.value; errors.fromError(t('advanced.proxyAllowPrivateFailed'), error, 'Settings.network') }
  finally { saving.value = '' }
}
</script>

<template>
  <div class="data-preferences">
    <p v-if="!loading && !ready">{{ t('settings.dataLoadFailed') }} <button class="diagnostic-link" @click="load">{{ t('common.refresh') }}</button></p>
    <section class="data-block">
      <h4>{{ t('settings.cacheSection') }}</h4>
      <p>{{ t('settings.cacheSectionDesc') }}</p>
      <div class="policy-options" role="group" :aria-label="t('settings.freshness')">
        <button v-for="policy in policies" :key="policy.id" :aria-pressed="freshness === policy.id" :disabled="!ready || !!saving"
          :class="{ selected: freshness === policy.id }" @click="savePolicy(policy.id)">
          <strong>{{ policy.title }}</strong><span>{{ policy.detail }}</span>
        </button>
      </div>
      <p class="note">{{ t('settings.mediaCacheNote') }}</p>
    </section>
    <section class="data-block">
      <h4>{{ t('settings.metadataSection') }}</h4>
      <div class="data-row">
        <div><strong>{{ t('settings.doubanPolling') }}</strong><p>{{ t('settings.doubanPollingDesc') }}</p></div>
        <Select :aria-label="t('settings.doubanPolling')" :model-value="String(interval)" :options="intervalOptions" :disabled="!ready || !!saving" @update:model-value="saveInterval" />
      </div>
      <p class="note">{{ t('settings.metadataHint') }}</p>
      <p class="note">{{ t('mobile.doubanCacheHint') }}</p>
    </section>
    <section class="data-block">
      <h4>{{ t('settings.networkSection') }}</h4>
      <div class="data-row">
        <div><strong>{{ t('advanced.proxyAllowPrivate') }}</strong><p>{{ t('advanced.proxyAllowPrivateDesc') }}</p></div>
        <label class="access-switch">
          <input type="checkbox" :checked="allowPrivate" :aria-label="t('advanced.proxyAllowPrivate')" :disabled="!ready || !!saving" @change="saveAccess" /><span />
        </label>
      </div>
      <button v-if="diagnosticsAvailable" class="diagnostic-link" @click="emit('open-group', 'diagnostics')">{{ t('settings.inspectNetwork') }} <span aria-hidden="true">→</span></button>
    </section>
  </div>
</template>

<style scoped>
.data-preferences { @apply cczj-min-w-0; }
.data-block { padding: 15px 0; }
.data-block + .data-block { @apply cczj-border-top; }
h4 { font-size: .93rem; margin: 0 0 7px; @apply cczj-text-primary; }
p { @apply cczj-text-muted; line-height: 1.65; font-size: .82rem; margin: 3px 0 10px; }
.policy-options { @apply cczj-grid cczj-gap-4; grid-template-columns: repeat(3, minmax(0,1fr)); }
.policy-options button { @apply cczj-text-left; font: inherit; padding: 11px; @apply cczj-border; border-radius: 10px; background: var(--bg-secondary); @apply cczj-text-secondary cczj-cursor-pointer; transition: border-color var(--cczj-motion-fast), background-color var(--cczj-motion-fast); }
.policy-options strong { @apply cczj-block; font-size: .84rem; margin-bottom: 5px; }
.policy-options span { @apply cczj-block; font-size: .76rem; line-height: 1.6; }
.policy-options .selected { @apply cczj-border-primary; background: var(--accent-alpha-10); @apply cczj-text-accent; }
.policy-options button:focus-visible, .diagnostic-link:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
.policy-options button:disabled { @apply cczj-cursor-default; opacity: .65; }
.data-row { @apply cczj-flex cczj-justify-between cczj-items-center; gap: 18px; }
.data-row > div:first-child { @apply cczj-flex-1 cczj-min-w-0; }
.data-row strong { font-size: .87rem; @apply cczj-font-medium; }
.data-row :deep(.select-dropdown) { width: 160px; @apply cczj-flex-shrink-0; }
.note { margin: 10px 0 0; font-size: .76rem; }
.access-switch { @apply cczj-relative; width: 40px; height: 22px; @apply cczj-flex-shrink-0; }
.access-switch input { @apply cczj-absolute cczj-opacity-0 cczj-w-full cczj-h-full; margin: 0; @apply cczj-cursor-pointer; }
.access-switch span { @apply cczj-absolute; inset: 0; @apply cczj-border-strong; border-radius: 20px; background: var(--bg-secondary); @apply cczj-pointer-events-none; transition: background-color var(--cczj-motion-fast); }
.access-switch span::before { content: ''; @apply cczj-absolute; left: 2px; top: 2px; @apply cczj-w-16 cczj-h-16; background: var(--text-secondary); @apply cczj-rounded-50; transition: transform var(--cczj-motion-fast); }
.access-switch input:checked + span { background: var(--accent); @apply cczj-border-primary; }
.access-switch input:checked + span::before { transform: translateX(18px); background: var(--accent-contrast); }
.access-switch input:focus-visible + span { outline: 2px solid var(--accent); outline-offset: 3px; }
.diagnostic-link { @apply cczj-inline-flex cczj-items-center cczj-gap-4; margin-top: 7px; border: 0; background: transparent; @apply cczj-text-accent; padding: 6px 0; @apply cczj-cursor-pointer; font: inherit; font-size: .84rem; }
@container (max-width: 490px) { .policy-options { grid-template-columns: 1fr; } .policy-options button { @apply cczj-grid; grid-template-columns: 85px minmax(0,1fr); @apply cczj-gap-4; } .data-row { @apply cczj-flex-wrap; } }
</style>
