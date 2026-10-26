import {createApp,h,ref} from 'vue'
import {createPinia} from 'pinia'
import i18n from '../../src/locales'
import CczjMobileCast from '../../src/mobile/CczjMobileCast.vue'
import {useThemeStore} from '../../src/stores/theme'
import '../../src/style.css'
import '../../src/styles/cczj-utilities.css'
import '../../src/styles/mobile.css'
document.documentElement.dataset.cczjPlatform='mobile'
const pinia=createPinia()
const theme=useThemeStore(pinia)
theme.apply()
const open=ref(false)
createApp({setup:()=>()=>h('main',[h('button',{onClick:()=>open.value=true},'Open cast'),h(CczjMobileCast,{open:open.value,url:'https://example.org/video.mp4',title:'Cast test',position:12,onClose:()=>open.value=false,onConnected:()=>{const state=window as unknown as {__connected?:number};state.__connected=(state.__connected||0)+1}})])}).use(pinia).use(i18n).mount('#app')
