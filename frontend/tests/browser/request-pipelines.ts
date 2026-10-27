import { createApp, h, KeepAlive, reactive } from 'vue'
import { createPinia } from 'pinia'
import { createRouter, createMemoryHistory } from 'vue-router'
import i18n from '../../src/locales'
import App from '../../src/App.vue'
import Sources from '../../src/views/Sources.vue'
import Search from '../../src/views/Search.vue'
import Home from '../../src/views/Home.vue'
import { useSourceStore } from '../../src/stores/source'
import { useThemeStore } from '../../src/stores/theme'
import * as api from '../../src/api/app'
import { requestPipelines } from '../../src/api/requestPipelines'
import '../../src/style.css'
import '../../src/styles/cczj-utilities.css'
import '../../src/styles/animations.css'

void App
const state = reactive({ active: true, view: 'sources' })
const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/:pathMatch(.*)*', component: Sources }] })
await router.push('/sources')
const app = createApp({ render: () => h(KeepAlive, () => state.active ? h(state.view === 'sources' ? Sources : state.view === 'home' ? Home : Search) : h('div', 'hidden')) })
  .use(createPinia()).use(i18n).use(router)
useThemeStore().apply()
app.mount('#app')
;(window as any).pipelineTest = { state, api, sources: useSourceStore(), snapshot: requestPipelines.snapshot }
