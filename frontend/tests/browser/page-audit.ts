import { createApp, markRaw } from 'vue'
import { createPinia } from 'pinia'
import App from '../../src/App.vue'
import router from '../../src/router'
import i18n, { loadLocale } from '../../src/locales'
import { useApplicationMode } from '../../src/modes/applicationMode'
import { useThemeStore } from '../../src/stores/theme'
import { useMotionStore } from '../../src/stores/motion'
import { useSourceStore } from '../../src/stores/source'
import { loadTypography } from '../../src/platform/typography'
import { loadPlatformPresentation } from '../../src/platform/presentation'
import { pluginSettingsTabs } from '../../src/plugins/registry'
import MotionPanel from '../../src/components/MotionPanel.vue'
import LogPanel from '../../src/components/LogPanel.vue'
import VideoDiagnosticsPanel from '../../src/components/VideoDiagnosticsPanel.vue'
import '../../src/style.css'
import '../../src/styles/cczj-utilities.css'
import '../../src/styles/animations.css'

await loadPlatformPresentation()
const app=createApp(App).use(createPinia()).use(i18n)
const errors:string[]=[]
app.config.errorHandler=(error, _instance, info)=>{errors.push(`${info}: ${String(error)}`)}
await Promise.all([useApplicationMode().load(),useThemeStore().load(),useMotionStore().load(),loadTypography(),loadLocale()])
pluginSettingsTabs.value=[
  {id:'motion',packId:'audit',label:()=>i18n.global.t('motion.title'),icon:'sparkles',order:25,component:markRaw(MotionPanel)},
  {id:'logs',packId:'audit',label:()=>i18n.global.t('logs.title'),icon:'terminal',order:40,component:markRaw(LogPanel)},
  {id:'diagnostics',packId:'audit',label:()=>i18n.global.t('settings.diagnostics'),icon:'monitor',order:50,component:markRaw(VideoDiagnosticsPanel)},
]
app.use(router)
await router.isReady()
app.mount('#app')
;(window as any).pageAudit={router,source:useSourceStore(),errors}
