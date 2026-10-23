import { createApp, defineComponent, h, reactive } from 'vue'
import { createPinia } from 'pinia'
import { createRouter, createMemoryHistory } from 'vue-router'
import i18n from '../../src/locales'
import App from '../../src/App.vue'
import Settings from '../../src/views/Settings.vue'
import Sidebar from '../../src/components/Sidebar.vue'
import TitleBar from '../../src/components/TitleBar.vue'
import CczjMobileShell from '../../src/mobile/CczjMobileShell.vue'
import { loadPlatformPresentation } from '../../src/platform/presentation'
import MotionPanel from '../../src/components/MotionPanel.vue'
import { markRaw } from 'vue'
import SplashScreen from '../../src/components/SplashScreen.vue'
import { pluginSettingsTabs, pluginNavItems } from '../../src/plugins/registry'
import { useMotionStore } from '../../src/stores/motion'
import { useErrorStore } from '../../src/stores/error'
import { useThemeStore } from '../../src/stores/theme'
import '../../src/style.css'
import '../../src/styles/cczj-utilities.css'
import '../../src/styles/animations.css'

void App // Include the application's real shared frame styles without startup jobs.
const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/:pathMatch(.*)*', component: Settings }] })
const mobilePreview=new URLSearchParams(location.search).has('mobile')
if(mobilePreview)await loadPlatformPresentation()
await router.push(mobilePreview?'/settings?group=advanced':'/settings')
pluginNavItems.value = [{packId:'icons',path:'/icons',label:()=>i18n.global.locale.value==='en'?'Icon Library':'图标画廊',icon:'image'}, {packId:'check',path:'/check',label:()=>i18n.global.locale.value==='en'?'Plugin Check':'扩展包自检查',icon:'code'}]
const state = reactive({ splash: new URLSearchParams(location.search).has('splash') })
pluginSettingsTabs.value = ['motion', 'logs', 'diagnostics'].map((id, index) => ({ id, packId: id, label: () => i18n.global.t(id === 'motion' ? 'motion.title' : id === 'logs' ? 'logs.title' : 'settings.diagnostics'), icon: ['sparkles', 'terminal', 'monitor'][index], order: [25, 40, 50][index], component: id === 'motion' ? markRaw(MotionPanel) : markRaw(defineComponent({ render: () => h('p', id) })) }))
const app = createApp({ render: () => h('div', {class:'cczj-h-full'}, [state.splash ? h(SplashScreen) : null,
  h('div', { class: 'app-shell' }, mobilePreview?[h(CczjMobileShell)]:[h(TitleBar), h('div', { class: 'app-body' }, [h(Sidebar), h('main', { class: 'main-content' }, [h(Settings)])])])]) })
app.use(createPinia()).use(i18n).use(router).mount('#app')
void useMotionStore().load()
;(window as any).settingsTest = { state, theme: useThemeStore(), motion: useMotionStore(), errors: useErrorStore() }
