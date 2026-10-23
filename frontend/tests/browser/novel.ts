import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from '../../src/App.vue'
import router from '../../src/router'
import i18n from '../../src/locales'
import { useApplicationMode } from '../../src/modes/applicationMode'
import '../../src/style.css'
import '../../src/styles/cczj-utilities.css'
import '../../src/styles/animations.css'
const app=createApp(App)
app.use(createPinia()).use(router).use(i18n)
Promise.all([useApplicationMode().load(),router.isReady()]).then(()=>app.mount('#app'))
