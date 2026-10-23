<script setup lang="ts">
import { computed, onActivated, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import ApplicationModeSwitch from '../../modes/ApplicationModeSwitch.vue'
import Icon from '../../components/Icon.vue'
import ComicReaderPreferences from '../components/ComicReaderPreferences.vue'
import { listComicSources } from '../data/repository'
import { useAsyncResource } from '../../reading/asyncResource'

const { t } = useI18n(), route = useRoute(), router = useRouter()
const sources = useAsyncResource({ load: listComicSources })
void sources.run()
let activated = false
onActivated(() => { if (activated) void sources.run(); activated = true })
const group = computed(() => ['reading', 'mode'].includes(String(route.query.group)) ? String(route.query.group) : 'index')
const summary = computed(() => {
  const rows = sources.data.value ?? [], disabled = rows.filter(row => !row.enabled || row.autoDisabled).length
  return !rows.length ? t('readingUI.sourceEmptyHint') : t(disabled ? 'comic.sourcesSummaryDisabled' : 'comic.sourcesSummary', { total: rows.length, disabled })
})
const menu = computed(() => [
  { id: 'reading', title: t('comic.readerSettings'), hint: t('comic.gestureHelp'), icon: 'panels', path: '/comic/settings?group=reading' },
  { id: 'sources', title: t('comic.contentSource'), hint: summary.value, icon: 'database', path: '/comic/sources' },
  { id: 'mode', title: t('modes.title'), hint: t('modes.hint'), icon: 'layers', path: '/comic/settings?group=mode' },
  { id: 'diagnostics', title: t('diagnostics.pageTitle'), hint: t('diagnostics.activityDescription'), icon: 'monitor', path: '/comic/diagnostics' },
])
</script>

<template>
  <div class="comic-page comic-settings">
    <template v-if="group === 'index'">
      <header class="reading-page-heading"><div><h1>{{ t('sidebar.settings') }}</h1><p>{{ t('readingUI.settingsHint') }}</p></div></header>
      <nav class="comic-settings-menu" :aria-label="t('sidebar.settings')"><router-link v-for="item in menu" :key="item.id" :to="item.path" class="comic-settings-entry"><span class="comic-settings-entry-icon"><Icon :name="item.icon" :size="22" /></span><span><strong>{{ item.title }}</strong><small>{{ item.hint }}</small></span><Icon name="chevron-right" :size="18" /></router-link></nav>
    </template>
    <template v-else>
      <div class="comic-settings-subhead"><button class="reading-icon-button" :aria-label="t('common.back')" @click="router.push('/comic/settings')"><Icon name="chevron-left" :size="22" /></button><h1>{{ t(group === 'reading' ? 'comic.readerSettings' : 'modes.title') }}</h1></div>
      <section class="comic-settings-card"><ComicReaderPreferences v-if="group === 'reading'" /><ApplicationModeSwitch v-else /></section>
    </template>
  </div>
</template>
