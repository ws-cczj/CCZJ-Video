<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { GetSetting, SetSetting, normalizeApiError } from '../api/app'
import Icon from '../components/Icon.vue'
import { Button, Select, ToggleSwitch } from '../components/ui'
import { builtinDanmakuSources, type DanmakuProvider } from '../constants/danmakuSources'
import DanmakuSourcePreview from '../player/danmaku/DanmakuSourcePreview.vue'
import { DANMAKU_SOURCE_SETTINGS_KEY, decodeSourceSettings, normalizeSourceSettings, sourceEnabled, updateSourceEnabled } from '../player/danmaku/sources'
import { presentationProfile } from '../platform/presentation'

const { t } = useI18n()
const mobile = presentationProfile().ui === 'mobile'
const settings = ref(decodeSourceSettings(''))
const ready = ref(false), saving = ref(false), saved = ref(false), error = ref('')
const enabledSources = computed(() => builtinDanmakuSources.filter(source => sourceEnabled(settings.value, source.id)))
const priorityOptions = computed(() => enabledSources.value.map(source => ({ value: source.id, label: t(`danmakuSources.names.${source.id}`) })))
const allEnabled = computed(() => enabledSources.value.length === builtinDanmakuSources.length)
let disposed = false
async function load() {
  ready.value = false; error.value = ''
  try { const value = await GetSetting(DANMAKU_SOURCE_SETTINGS_KEY); if (!disposed) { settings.value = decodeSourceSettings(value); ready.value = true } }
  catch (value) { if (!disposed) error.value = normalizeApiError(value).message }
}
function toggle(provider: DanmakuProvider, enabled: boolean) {
  settings.value = updateSourceEnabled(settings.value, provider, enabled)
  saved.value = false
}
function toggleAll() {
  const disabled = allEnabled.value ? builtinDanmakuSources.map(source => source.id) : []
  settings.value = normalizeSourceSettings({ ...settings.value, disabled })
  saved.value = false
}
function setPriority(value: string | number) {
  if (typeof value !== 'string' || !sourceEnabled(settings.value, value)) return
  settings.value = { ...settings.value, defaultProvider: value }
  saved.value = false
}
async function save() {
  if (!ready.value || saving.value) return
  saving.value = true; saved.value = false; error.value = ''
  try {
    if (disposed) return
    await SetSetting(DANMAKU_SOURCE_SETTINGS_KEY, JSON.stringify(settings.value))
    if (!disposed) saved.value = true
  }
  catch (value) { if (!disposed) error.value = normalizeApiError(value).message }
  finally { if (!disposed) saving.value = false }
}
onMounted(load)
onBeforeUnmount(() => { disposed = true })
</script>

<template>
  <div class="cczj-danmaku-sources" :class="{ 'cczj-mobile-danmaku-sources': mobile }">
    <h1 v-if="!mobile">{{ t('danmakuSources.title') }}</h1>
    <p v-if="error && !ready" role="alert">
      {{ error }} <Button variant="ghost" @click="load">{{ t('common.retry') }}</Button>
    </p>
    <p v-if="!ready && !error" role="status">{{ t('common.loading') }}</p>
    <template v-if="ready">
      <section class="cczj-danmaku-source-overview">
        <div class="cczj-flex cczj-items-start cczj-gap-6 cczj-min-w-0">
          <span class="cczj-danmaku-overview-icon" aria-hidden="true"><Icon name="danmaku-settings" :size="26" /></span>
          <div class="cczj-min-w-0">
            <h2>{{ t('danmakuSources.enabledCount', { count: enabledSources.length, total: builtinDanmakuSources.length }) }}</h2>
            <p>{{ t(enabledSources.length ? 'danmakuSources.defaultHint' : 'danmakuSources.allDisabled') }}</p>
          </div>
        </div>
        <div class="cczj-danmaku-priority">
          <span class="cczj-text-sm cczj-text-secondary">{{ t('danmakuSources.default') }}</span>
          <Select :model-value="settings.defaultProvider" :options="priorityOptions" :disabled="!enabledSources.length || saving"
            :aria-label="t('danmakuSources.default')" :placeholder="t('danmakuSources.none')" @update:model-value="setPriority" />
        </div>
        <DanmakuSourcePreview />
      </section>
      <div class="cczj-danmaku-source-toolbar">
        <h2>{{ t('danmakuSources.builtin') }}</h2>
        <Button type="button" :disabled="saving" @click="toggleAll">{{ t(allEnabled ? 'danmakuSources.disableAll' : 'danmakuSources.enableAll') }}</Button>
      </div>
      <fieldset :disabled="saving" :aria-busy="saving" class="cczj-danmaku-source-list">
        <legend class="cczj-sr-only">{{ t('danmakuSources.builtin') }}</legend>
        <article v-for="source in builtinDanmakuSources" :key="source.id" class="cczj-danmaku-source-row" :data-provider="source.id"
          :class="{ 'is-default': settings.defaultProvider === source.id, 'is-disabled': !sourceEnabled(settings, source.id) }">
          <span class="cczj-danmaku-provider-mark" aria-hidden="true"><Icon :name="source.icon" :size="17" /></span>
          <div class="cczj-danmaku-source-copy">
            <div class="cczj-flex cczj-flex-wrap cczj-items-center cczj-gap-3">
              <h3>{{ t(`danmakuSources.names.${source.id}`) }}</h3>
              <span v-if="settings.defaultProvider === source.id" class="cczj-danmaku-source-priority"><Icon name="star" :size="13" />{{ t('danmakuSources.default') }}</span>
            </div>
            <p>{{ t(`danmakuSources.providers.${source.id}`) }}</p>
          </div>
          <ToggleSwitch :model-value="sourceEnabled(settings, source.id)" :disabled="saving"
            :ariaLabel="t('danmakuSources.toggleSource', { source: t(`danmakuSources.names.${source.id}`) })"
            @update:model-value="toggle(source.id, $event)" />
        </article>
      </fieldset>
      <div class="cczj-danmaku-source-save">
        <span v-if="error" role="alert">{{ error }}</span>
        <span v-else role="status">{{ saved ? t('danmakuSources.saved') : '' }}</span>
        <Button type="button" variant="primary" :loading="saving" @click="save">{{ t('common.save') }}</Button>
      </div>
    </template>
  </div>
</template>

<style scoped src="../styles/views/danmaku-sources.css"></style>
