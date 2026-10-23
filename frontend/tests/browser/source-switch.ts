import { createApp,defineComponent,h } from 'vue'
import { createRouter,createWebHashHistory } from 'vue-router'
import { createPinia } from 'pinia'
import i18n from '../../src/locales'
import NovelSourceSwitch from '../../src/novel/components/NovelSourceSwitch.vue'
import ComicSourceSwitch from '../../src/comic/components/ComicSourceSwitch.vue'
import '../../src/style.css'
const router=createRouter({history:createWebHashHistory(),routes:[{path:'/:pathMatch(.*)*',component:{render:()=>null}}]})
const Root=defineComponent({setup:()=>()=>h('main',{style:'max-width:700px;margin:40px auto;padding:20px;background:#faf7f1;color:#352e29'},[
  h('h1','小说换源'),h(NovelSourceSwitch,{itemId:1,chapterId:11}),h('hr'),h('h1','漫画换源'),h(ComicSourceSwitch,{itemId:2,chapterId:22})
])})
createApp(Root).use(createPinia()).use(router).use(i18n).mount('#app')
