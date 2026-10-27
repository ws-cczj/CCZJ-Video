import { createApp, h } from 'vue'
import { createPinia } from 'pinia'
import i18n from '../../src/locales'
import UpdateModal from '../../src/components/UpdateModal.vue'
import ErrorToastStack from '../../src/components/ErrorToastStack.vue'
import VideoCollectionStatus from '../../src/modes/VideoCollectionStatus.vue'
import Sources from '../../src/views/Sources.vue'
import { createRouter, createMemoryHistory, RouterView } from 'vue-router'
import { useCollectStore } from '../../src/stores/collect'
import { useSourceStore } from '../../src/stores/source'
import { useThemeStore } from '../../src/stores/theme'
import { updateController } from '../../src/stores/updateState'
import '../../src/style.css'
import '../../src/styles/cczj-utilities.css'
import '../../src/styles/animations.css'

const router=createRouter({history:createMemoryHistory(),routes:[{path:'/',component:{render:()=>h('div','Home')}},{path:'/sources',component:Sources}]})
await router.push('/')
const app=createApp({render:()=>h('div',[h(UpdateModal),h(ErrorToastStack),h(VideoCollectionStatus),h(RouterView)])}).use(createPinia()).use(i18n).use(router)
useThemeStore().apply()
app.mount('#app')
;(window as any).updateTest = {
  check: () => updateController.checkUpdate?.(),
  open: () => updateController.openModal?.(),
  router, collect:useCollectStore(), sources:useSourceStore(),
}
