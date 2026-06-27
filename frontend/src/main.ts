import { createApp } from 'vue'
import { createPinia } from 'pinia'
import { MotionPlugin } from '@vueuse/motion'
import './styles/cczj-utilities.css'
import './style.css'
import i18n, { loadLocale } from './locales'
import App from './App.vue'
import router from './router'
import './event' // 初始化全局事件总线 (window.app_event)

const app = createApp(App)
app.use(createPinia())
app.use(router)
app.use(i18n)
app.use(MotionPlugin)

// 加载后端语言设置后挂载
loadLocale().finally(() => {
  app.mount('#app')
})
