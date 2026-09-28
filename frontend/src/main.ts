import { createApp } from 'vue'
import { createPinia } from 'pinia'
import './styles/cczj-utilities.css'
import './styles/animations.css'
import './style.css'
import i18n, { loadLocale } from './locales'
import App from './App.vue'
import router from './router'
import { startCacheInvalidation } from './stores/cacheInvalidate'

const app = createApp(App)
app.use(createPinia())
app.use(router)
app.use(i18n)

// 缓存失效订阅要在 pinia 装好之后、挂载之前起：Go 侧一删片/一采完集就会发事件，
// 晚起一步等于让那一次失效永久漏掉。
startCacheInvalidation()

// 加载后端语言设置后挂载。升级不再删库，前端缓存也就不需要跟着整体作废：
// schema 迁移保住了数据，缓存的失效由各缓存自己的键与上限负责。
loadLocale().finally(() => {
  app.mount('#app')
})
