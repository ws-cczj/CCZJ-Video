import { createApp, defineComponent, h, markRaw, reactive } from 'vue'
import { createPinia } from 'pinia'
import i18n from '../../src/locales'
import { createRouter, createMemoryHistory } from 'vue-router'
import Settings from '../../src/views/Settings.vue'
import { addPluginSettingsTab } from '../../src/plugins/registry'
import DiagnosticsPanel from '../../src/components/DiagnosticsPanel.vue'
import { useThemeStore } from '../../src/stores/theme'
import { usePerfStore } from '../../src/stores/perf'
import '../../src/style.css'
import '../../src/styles/cczj-utilities.css'

const settingsMode = location.search.includes('settings')
const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/settings', component: Settings }] })
addPluginSettingsTab({ packId: 'diagnostics-panel', id: 'diagnostics', label: '诊断', icon: 'monitor', order: 50, component: markRaw(DiagnosticsPanel) })
const state = reactive({ active: true, mounted: true })
const app = createApp(defineComponent({ setup: () => () => h('main', { class: 'main-content', style: 'height:100vh;padding:24px;box-sizing:border-box;overflow:auto' }, [
  state.mounted ? h('div', { style: state.active ? 'height:100%;min-height:0' : 'display:none' }, [settingsMode ? h(Settings) : h(DiagnosticsPanel, { active: state.active })]) : null,
]) })).use(createPinia()).use(i18n).use(router)
void useThemeStore().load().finally(async () => {
  await router.push('/settings#diagnostics')
  await router.isReady()
  app.mount('#app')
})
document.body.style.background = 'var(--bg-primary)'
document.body.style.fontFamily = 'system-ui, sans-serif'
;(window as any).diagnosticsTest = { state, perf: usePerfStore() }
