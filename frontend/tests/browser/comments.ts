import {createApp,h,ref} from 'vue'
import {createPinia} from 'pinia'
import i18n from '../../src/locales'
import DoubanComments from '../../src/components/DoubanComments.vue'
import '../../src/style.css'
const id=ref('test-first')
Object.assign(window,{changeCommentSubject:(value:string)=>{id.value=value}})
createApp({setup:()=>()=>h(DoubanComments,{doubanId:id.value})}).use(createPinia()).use(i18n).mount('#app')
