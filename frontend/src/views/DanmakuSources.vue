<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { GetSetting, SetSetting, normalizeApiError } from '../api/app'
import Icon from '../components/Icon.vue'
import { Button } from '../components/ui'
import { builtinDanmakuSources, DANMAKU_SOURCE_SETTINGS_KEY, decodeSourceSettings, normalizeSourceSettings, sourceEnabled, type DanmakuProvider } from '../player/danmaku/sources'
import { decodeDanmakuPreferences } from '../player/danmaku/preferences'

const { t } = useI18n()
const settings = ref(decodeSourceSettings(''))
const freshDays = ref(1)
const ready = ref(false), saving = ref(false), saved = ref(false), error = ref('')
const enabledCount = computed(() => builtinDanmakuSources.filter(source => sourceEnabled(settings.value, source.id)).length)
let disposed = false
async function load() {
  ready.value = false; error.value = ''
  try { const [value, preferences] = await Promise.all([GetSetting(DANMAKU_SOURCE_SETTINGS_KEY),GetSetting('danmaku_preferences_v1')]); if (!disposed) { settings.value = decodeSourceSettings(value); freshDays.value=decodeDanmakuPreferences(preferences).freshDays; ready.value = true } }
  catch (value) { if (!disposed) error.value = normalizeApiError(value).message }
}
function toggle(provider: DanmakuProvider, enabled: boolean) {
  const disabled = settings.value.disabled.filter(id => id !== provider)
  if (!enabled) disabled.push(provider)
  settings.value = normalizeSourceSettings({ ...settings.value, disabled }); saved.value = false
}
async function save() {
  if (!ready.value || saving.value) return
  saving.value = true; saved.value = false; error.value = ''
  try {
    // Merge only cache policy into the latest preferences so a display change
    // made since this page loaded cannot be overwritten by an old snapshot.
    const preferences=decodeDanmakuPreferences(await GetSetting('danmaku_preferences_v1'))
    if(disposed)return
    await SetSetting('danmaku_preferences_v1',JSON.stringify({...preferences,freshDays:freshDays.value}))
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
  <div class="cczj-danmaku-sources">
    <header class="cczj-danmaku-source-hero"><div><span class="cczj-danmaku-source-eyebrow"><Icon name="danmaku" :size="18" />DANMAKU</span><h1>{{ t('danmakuSources.title') }}</h1><p>{{ t('danmakuSources.description') }}</p><span class="cczj-danmaku-source-total">{{t('danmakuSources.enabledCount',{count:enabledCount})}}</span></div><div class="cczj-danmaku-source-preview" aria-hidden="true"><span><i></i>{{t('danmakuSources.preview1')}}</span><span>{{t('danmakuSources.preview2')}}<i></i></span><span><i></i>{{t('danmakuSources.preview3')}}</span></div></header>
    <p v-if="error" role="alert">{{ error }} <Button v-if="!ready" variant="ghost" @click="load">{{ t('common.retry') }}</Button></p>
    <p v-if="!ready && !error" role="status">{{ t('common.loading') }}</p>
    <fieldset :disabled="!ready || saving" :aria-busy="saving">
      <legend class="cczj-sr-only">{{ t('danmakuSources.title') }}</legend>
      <article v-for="source in builtinDanmakuSources" :key="source.id" class="cczj-danmaku-source-card" :data-provider="source.id" :class="{'is-default':settings.defaultProvider===source.id,'is-disabled':!sourceEnabled(settings,source.id)}">
        <div class="cczj-danmaku-source-heading"><span class="cczj-danmaku-provider-mark" aria-hidden="true">{{source.id==='bilibili'?'bili':source.id==='qq'?'TV':'iQIYI'}}</span><div><h2>{{ source.name }}</h2><small>{{ t('danmakuSources.builtin') }}</small></div><Icon name="danmaku" :size="22" /></div>
        <p>{{ t(`danmakuSources.providers.${source.id}`) }}</p>
        <div class="cczj-danmaku-source-features"><span><Icon name="check" :size="13" />{{t('danmakuSources.autoMatching')}}</span><span>{{t('danmakuSources.segmentCache')}}</span></div>
        <div class="cczj-danmaku-source-actions">
          <label><input type="checkbox" :checked="sourceEnabled(settings, source.id)" @change="toggle(source.id, ($event.target as HTMLInputElement).checked)" />{{ t('danmakuSources.enabled') }}</label>
          <label class="cczj-danmaku-default"><input type="radio" name="danmaku-default" :value="source.id" :checked="settings.defaultProvider === source.id" :disabled="!sourceEnabled(settings, source.id)" @change="settings.defaultProvider = source.id; saved = false" /><Icon v-if="settings.defaultProvider===source.id" name="check" :size="14" />{{ t('danmakuSources.default') }}</label>
        </div>
      </article>
    </fieldset>
    <section class="cczj-danmaku-source-cache"><label for="danmaku-cache-days">{{t('danmaku.cacheFreshness')}}</label><select id="danmaku-cache-days" v-model.number="freshDays" :disabled="!ready||saving" @change="saved=false"><option v-for="days in [1,3,7,14,30]" :key="days" :value="days">{{t('danmaku.days',{days})}}</option></select><p>{{t('danmaku.cacheHint')}}</p><p>{{t('danmakuSources.cacheScope')}}</p></section>
    <div class="cczj-danmaku-source-save"><Button :disabled="!ready || saving" @click="save">{{ saving ? t('common.loading') : t('common.save') }}</Button><span role="status">{{ saved ? t('danmakuSources.saved') : '' }}</span></div>
    <section class="cczj-danmaku-source-notes"><h2>{{ t('danmakuSources.usage') }}</h2><p>{{ t('danmakuSources.defaultHint') }}</p><p>{{ t('danmakuSources.requestHint') }}</p><p>{{ t('danmakuSources.availabilityHint') }}</p></section>
  </div>
</template>

<style scoped>
.cczj-danmaku-sources{max-width:1000px;margin:auto;padding:8px 0 28px;@apply cczj-text-primary}
.cczj-danmaku-sources h1{font-size:1.65rem;margin:0 0 10px}.cczj-danmaku-sources p{@apply cczj-text-secondary;line-height:1.7;margin:8px 0}.cczj-danmaku-sources fieldset{@apply cczj-grid;grid-template-columns:repeat(auto-fit,minmax(min(100%,260px),1fr));@apply cczj-gap-8;padding:0;border:0;margin:24px 0 16px;@apply cczj-min-w-0}
.cczj-danmaku-source-card{padding:20px;@apply cczj-border;background:var(--cczj-mobile-surface,var(--bg-card));@apply cczj-rounded-xl cczj-min-w-0}.cczj-danmaku-source-heading{@apply cczj-flex cczj-items-center cczj-gap-5}.cczj-danmaku-source-heading h2{font-size:1.1rem;margin:0}.cczj-danmaku-source-heading span{@apply cczj-ml-auto cczj-text-sm cczj-text-muted}.cczj-danmaku-source-card small{@apply cczj-block cczj-text-muted;line-height:1.6;margin:12px 0}
.cczj-danmaku-source-actions{@apply cczj-flex cczj-gap-10 cczj-flex-wrap cczj-border-top cczj-mt-8 cczj-pt-4}.cczj-danmaku-source-actions label{@apply cczj-flex cczj-items-center cczj-gap-4;min-height:44px;@apply cczj-cursor-pointer}.cczj-danmaku-source-actions input{width:18px;height:18px;accent-color:var(--accent)}.cczj-danmaku-source-actions input:focus-visible{outline:2px solid var(--accent);outline-offset:3px}.cczj-danmaku-source-save{@apply cczj-flex cczj-items-center cczj-gap-8;min-height:44px}.cczj-danmaku-source-save span{@apply cczj-text-secondary cczj-text-base}.cczj-danmaku-source-notes{margin-top:28px;@apply cczj-border-top cczj-pt-8}.cczj-danmaku-source-notes h2{font-size:1.1rem}.cczj-danmaku-source-notes p{@apply cczj-text-base}
.cczj-danmaku-source-cache{padding:18px 20px;margin:16px 0;background:var(--cczj-mobile-surface,var(--bg-card));@apply cczj-border;border-radius:14px}.cczj-danmaku-source-cache label{@apply cczj-font-semibold cczj-mr-8}.cczj-danmaku-source-cache select{min-height:36px;min-width:88px;padding:4px 12px;@apply cczj-border cczj-rounded-md cczj-text-primary;background:var(--bg-card)}.cczj-danmaku-source-cache select:focus-visible{outline:2px solid var(--accent);outline-offset:3px}.cczj-danmaku-source-cache p{@apply cczj-text-base}
</style>
<style scoped src="../styles/views/danmaku-sources.css"></style>
