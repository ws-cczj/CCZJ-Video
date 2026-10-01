import { createApp } from 'vue'
import { createPinia } from 'pinia'
import './styles/cczj-utilities.css'
import './styles/animations.css'
import './style.css'
import i18n, { loadLocale } from './locales'
import App from './App.vue'
import router from './router'
import { startCacheInvalidation } from './stores/cacheInvalidate'
import { useMotionStore } from './stores/motion'
import { syncPluginScripts } from './plugins/runtime'
import { startDropWatch } from './plugins/dropInstall'

// 弹窗、下拉、播放器浮层都是 Teleport 到 <body> 的，不在 .app-shell 里。运行时找不到
// [data-file-drop-target] 就把 dropEffect 强制成 none（鼠标变禁止符号），于是「拖到弹窗上
// 就装不了」。标记打在 body 上兜底：近处没有别的标记时，命中的永远是这一层。
document.body.setAttribute('data-file-drop-target', '')

const app = createApp(App)
app.use(createPinia())
app.use(router)
app.use(i18n)

// 缓存失效订阅要在 pinia 装好之后、挂载之前起：Go 侧一删片/一采完集就会发事件，
// 晚起一步等于让那一次失效永久漏掉。
startCacheInvalidation()

// 加载后端语言设置后挂载。升级不再删库，前端缓存也就不需要跟着整体作废：
// schema 迁移保住了数据，缓存的失效由各缓存自己的键与上限负责。
// 动画开关和语言一起等：先挂载再把 data-cczj-motion 补上，启动那几段动效照样会跑一遍，
// 那正是把动画关掉的人最不想看见的一下。
const motion = useMotionStore()
Promise.all([loadLocale(), motion.load()]).finally(() => {
  app.mount('#app')
  // 注入型扩展包只能挂在「已挂载」之后跑：它们要往 router 上加页面、往 head 里塞样式，
  // 有的还会立刻 push 路由——太早跑的话 <router-view> 还没就位，导航会被静默吞掉。
  // 不 await：坏包不该挡住应用启动，注入结果由日志面板和扩展面板各自汇报。
  void syncPluginScripts()
  // 拖放安装要在任何页面都收：Go 只会把被拖文件夹的绝对路径发上来一次，错过就没了，
  // 所以订阅挂在应用生命周期上，而不是设置页打开时才挂。
  startDropWatch()
})
