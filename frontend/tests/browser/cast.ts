import {createApp,h,ref} from 'vue'
import {createPinia} from 'pinia'
import i18n from '../../src/locales'
import CczjMobileCast from '../../src/mobile/CczjMobileCast.vue'
import '../../src/style.css'
import '../../src/styles/cczj-utilities.css'
const open=ref(false)
createApp({setup:()=>()=>h('main',[h('button',{onClick:()=>open.value=true},'Open cast'),h(CczjMobileCast,{open:open.value,url:'https://example.org/video.mp4',title:'Cast test',position:12,onClose:()=>open.value=false})])}).use(createPinia()).use(i18n).mount('#app')
