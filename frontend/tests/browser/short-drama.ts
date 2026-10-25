import { createApp, defineComponent, h, reactive } from 'vue'
import { createPinia } from 'pinia'
import { createRouter, createMemoryHistory } from 'vue-router'
import i18n from '../../src/locales'
import ShortDrama from '../../src/views/ShortDrama.vue'
import CczjMobileShell from '../../src/mobile/CczjMobileShell.vue'
import DesktopVideoShell from '../../src/modes/DesktopVideoShell.vue'
import TitleBar from '../../src/components/TitleBar.vue'
import '../../src/player/shortDrama/presentation.css'
import { useThemeStore } from '../../src/stores/theme'
import { shortDramaScene } from '../../src/player/shortDrama/scene'
import { useSourceStore } from '../../src/stores/source'
import { loadPlatformPresentation, presentationProfile } from '../../src/platform/presentation'
import { writeStorage } from '../../src/platform/storage'
import '../../src/style.css'
import '../../src/styles/cczj-utilities.css'

await loadPlatformPresentation()
writeStorage('cczj_auto_play', false) // Short drama autoplay must override landscape preference.
writeStorage('vp_settings', { speed: '1.5', auto_resume_jump: '0' })
const state = reactive({ source: 'fixture' })
const placeholder = defineComponent({ render: () => h('p', 'Other route') })
const router = createRouter({ history: createMemoryHistory(), routes: [
  {path:'/short-drama', component:ShortDrama},
  ...['/', '/search', '/favorites', '/history', '/settings', '/sources'].map(path => ({path, component:placeholder})),
] })
await router.push('/short-drama')
const app = createApp(defineComponent({ setup() {
  return () => h('div', {
    class:['app-shell cczj-h-full cczj-flex cczj-flex-col',{'cczj-short-drama-surface':shortDramaScene(router.currentRoute.value,presentationProfile().ui==='mobile')==='playback'}],
  }, presentationProfile().ui === 'mobile' ? [h(CczjMobileShell)] : [h(TitleBar),h(DesktopVideoShell)])
} }))
app.use(createPinia()).use(i18n).use(router)
useSourceStore().currentSourceKey = state.source
useThemeStore().apply()
app.mount('#app')
;(window as any).dramaTest = { router, source:useSourceStore(), theme:useThemeStore(), state, locale: (value: 'zh-CN' | 'en') => { i18n.global.locale.value = value } }
