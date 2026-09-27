import { createApp } from 'vue'
import { createPinia } from 'pinia'
import './styles/cczj-utilities.css'
import './styles/animations.css'
import './style.css'
import i18n, { loadLocale } from './locales'
import App from './App.vue'
import router from './router'
import './event' // 初始化全局事件总线 (window.app_event)
import { GetSetting } from './api/app'
import { applyDatabaseResetGeneration } from './platform/storage'

const app = createApp(App)
app.use(createPinia())
app.use(router)
app.use(i18n)

// 加载后端语言设置后挂载
Promise.all([loadLocale(), GetSetting('database_reset_generation').then(applyDatabaseResetGeneration).catch(() => undefined)]).finally(() => {
  app.mount('#app')
})
