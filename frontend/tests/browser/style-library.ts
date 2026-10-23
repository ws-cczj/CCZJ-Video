import { createApp } from 'vue'
import { createPinia } from 'pinia'
import i18n from '../../src/locales'
import { useThemeStore } from '../../src/stores/theme'
import StyleLibrary from './StyleLibrary.vue'
import App from '../../src/App.vue'
import '../../src/style.css'
import '../../src/styles/cczj-utilities.css'
import '../../src/styles/animations.css'
import '../../src/novel/styles/novel.css'
import '../../src/comic/styles/comic.css'

void App // The application's actual reset and shared frame styles.
createApp(StyleLibrary).use(createPinia()).use(i18n).mount('#app')
useThemeStore().apply()
Object.assign(document.getElementById('app')!.style,{height:'auto',overflow:'visible'})
document.body.style.overflow='auto'
