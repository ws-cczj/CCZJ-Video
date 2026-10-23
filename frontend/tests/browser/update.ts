import { createApp } from 'vue'
import { createPinia } from 'pinia'
import i18n from '../../src/locales'
import UpdateModal from '../../src/components/UpdateModal.vue'
import { updateController } from '../../src/stores/updateState'
import '../../src/style.css'
import '../../src/styles/cczj-utilities.css'

createApp(UpdateModal).use(createPinia()).use(i18n).mount('#app')
;(window as any).updateTest = {
  check: () => updateController.checkUpdate?.(),
}
