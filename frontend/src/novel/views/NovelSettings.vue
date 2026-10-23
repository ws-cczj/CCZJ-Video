<script setup lang="ts">
import { computed, onActivated, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import ApplicationModeSwitch from '../../modes/ApplicationModeSwitch.vue'
import NovelReaderPreferences from '../components/NovelReaderPreferences.vue'
import Icon from '../../components/Icon.vue'
import { listNovelSources } from '../data/novelLibrary'
import { useAsyncResource, isEmptyList } from '../../reading/asyncResource'

type SettingsGroup = 'index' | 'reading' | 'mode'
const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const sources = useAsyncResource({ load: listNovelSources, isEmpty: isEmptyList })
const allowedGroups: SettingsGroup[] = ['index', 'reading', 'mode']
const group = computed<SettingsGroup>(() => {
  const value = typeof route.query.group === 'string' ? route.query.group as SettingsGroup : 'index'
  return allowedGroups.includes(value) ? value : 'index'
})
const groupTitle = computed(() => group.value === 'reading' ? t('novel.readingSettings') : t('modes.title'))
const summary = computed(() => {
  const rows = sources.data.value ?? []
  if (sources.status.value !== 'ready' || rows.length === 0) return t('novel.noSourcesHint')
  const disabled = rows.filter(row => !row.enabled || row.autoDisabled).length
  return disabled > 0
    ? t('novel.sourcesSummaryDisabled', { total: rows.length, disabled })
    : t('novel.sourcesSummary', { total: rows.length })
})

onMounted(() => { void sources.run() })
let activated = false
onActivated(() => { if (activated) void sources.run(); activated = true })

function openGroup(next: SettingsGroup): void {
  void router.push(next === 'index' ? '/novel/settings' : { path: '/novel/settings', query: { group: next } })
}

function openSources(): void {
  void router.push('/novel/sources')
}

function openDiagnostics(): void {
  void router.push('/novel/diagnostics')
}
</script>

<template>
  <div class="novel-page novel-settings">
    <template v-if="group === 'index'">
      <header class="reading-page-heading"><div><h1>{{ t('sidebar.settings') }}</h1><p>{{ t('readingUI.settingsHint') }}</p></div></header>
      <nav class="novel-settings-menu cczj-grid cczj-gap-4" :aria-label="t('sidebar.settings')">
        <button class="novel-settings-entry cczj-flex cczj-items-center cczj-gap-6 cczj-w-full cczj-p-6 cczj-rounded-lg cczj-border cczj-bg-card" type="button" @click="openGroup('reading')">
          <span class="novel-settings-entry-icon cczj-grid"><Icon name="book-open" :size="22" /></span>
          <span class="cczj-flex cczj-flex-col cczj-gap-1 cczj-text-left cczj-flex-1"><strong>{{ t('novel.readingSettings') }}</strong><small>{{ t('novel.preferencesHint') }}</small></span>
          <Icon name="chevron-right" :size="18" aria-hidden="true" />
        </button>
        <button class="novel-settings-entry cczj-flex cczj-items-center cczj-gap-6 cczj-w-full cczj-p-6 cczj-rounded-lg cczj-border cczj-bg-card" type="button" @click="openGroup('mode')">
          <span class="novel-settings-entry-icon cczj-grid"><Icon name="layers" :size="22" /></span>
          <span class="cczj-flex cczj-flex-col cczj-gap-1 cczj-text-left cczj-flex-1"><strong>{{ t('modes.title') }}</strong><small>{{ t('modes.hint') }}</small></span>
          <Icon name="chevron-right" :size="18" aria-hidden="true" />
        </button>
        <button class="novel-settings-entry cczj-flex cczj-items-center cczj-gap-6 cczj-w-full cczj-p-6 cczj-rounded-lg cczj-border cczj-bg-card" type="button" @click="openSources">
          <span class="novel-settings-entry-icon cczj-grid"><Icon name="database" :size="22" /></span>
          <span class="cczj-flex cczj-flex-col cczj-gap-1 cczj-text-left cczj-flex-1"><strong>{{ t('novel.contentSource') }}</strong><small>{{ summary }}</small></span>
          <Icon name="chevron-right" :size="18" aria-hidden="true" />
        </button>
        <button class="novel-settings-entry cczj-flex cczj-items-center cczj-gap-6 cczj-w-full cczj-p-6 cczj-rounded-lg cczj-border cczj-bg-card" type="button" @click="openDiagnostics">
          <span class="novel-settings-entry-icon cczj-grid"><Icon name="monitor" :size="22" /></span>
          <span class="cczj-flex cczj-flex-col cczj-gap-1 cczj-text-left cczj-flex-1"><strong>{{ t('diagnostics.pageTitle') }}</strong><small>{{ t('diagnostics.activityDescription') }}</small></span>
          <Icon name="chevron-right" :size="18" aria-hidden="true" />
        </button>
      </nav>
    </template>

    <template v-else>
      <div class="novel-settings-subhead cczj-flex cczj-items-center cczj-gap-4">
        <button class="novel-icon-button" type="button" :aria-label="t('common.back')" @click="openGroup('index')"><Icon name="chevron-left" :size="22" /></button>
        <div><p class="cczj-text-sm cczj-text-secondary">{{ t('sidebar.settings') }}</p><h1>{{ groupTitle }}</h1></div>
      </div>
      <section v-if="group === 'reading'" class="novel-settings-card cczj-p-8 cczj-rounded-lg cczj-border cczj-bg-card">
        <p class="novel-settings-card-hint cczj-text-secondary">{{ t('novel.preferencesHint') }}</p>
        <NovelReaderPreferences preview />
      </section>
      <section v-else class="novel-settings-card cczj-p-8 cczj-rounded-lg cczj-border cczj-bg-card">
        <ApplicationModeSwitch />
      </section>
    </template>
  </div>
</template>
