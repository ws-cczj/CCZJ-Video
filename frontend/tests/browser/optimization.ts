import { createApp, defineAsyncComponent, defineComponent, h, KeepAlive, reactive, ref } from 'vue'
import { createPinia } from 'pinia'
import i18n from '../../src/locales'
import RemoteImage from '../../src/components/RemoteImage.vue'
import BookCarousel from '../../src/components/BookCarousel.vue'
import { createMemoryHistory, createRouter } from 'vue-router'
import DiagnosticsPanel from '../../src/components/DiagnosticsPanel.vue'
import { imageProxyStats, getProxiedImageUrl } from '../../src/utils'
import '../../src/style.css'
import '../../src/styles/cczj-utilities.css'

const state = reactive({ page: 'posters', draft: '', count: 120, carouselInterval: 200 })
const Image = location.search.includes('legacy')
  ? defineAsyncComponent(() => import(/* @vite-ignore */ (window as any).__legacyImageURL))
  : RemoteImage
let posterRoot: HTMLElement | null = null
const Posters = defineComponent({
  name: 'PosterGrid',
  setup: () => {
    const draft = ref('')
    ;(window as any).__posterMounts = ((window as any).__posterMounts || 0) + 1
    return () => h('div', { class: 'posters', ref: el => { posterRoot = el as HTMLElement | null } }, [
    h('input', { class: 'draft', value: draft.value, onInput: (event: Event) => { draft.value = (event.target as HTMLInputElement).value } }),
    h('div', { style: 'display:grid;grid-template-columns:repeat(6,minmax(0,1fr));gap:16px' },
      Array.from({ length: state.count }, (_, i) => h('div', { key: i, style: 'height:240px' }, [
        h(Image, { src: `https://posters.example/${i}`, alt: `Poster ${i}`, loading: 'lazy', style: 'width:100%;height:240px;object-fit:cover' }),
      ]))),
  ])
  },
})
const Blank = defineComponent({ name: 'Blank', setup: () => () => h('p', 'Other page') })
const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/', component: Blank }] })
const app = createApp(defineComponent({ setup: () => () => h('main', { class: 'main-content', style: 'height:100vh;padding:24px;overflow:auto' }, [
  h(KeepAlive, null, { default: () => h(state.page === 'posters' ? Posters : state.page === 'diagnostics' ? DiagnosticsPanel : state.page === 'carousel' ? BookCarousel : Blank, state.page === 'carousel' ? { sourceKey:'test', intervalMs:state.carouselInterval, slides:Array.from({length:5}, (_, i) => ({vod_id:String(i),vod_name:`Slide ${i}`,vod_pic:`https://posters.example/${1000+i}`})) } : {}) }),
]) })).use(createPinia()).use(i18n).use(router)
app.mount('#app')
;(window as any).optimizationTest = { state, imageProxyStats: () => ((window as any).__legacyImageProxyStats || imageProxyStats)(), getProxiedImageUrl,
  retainedImages: () => [...(posterRoot?.querySelectorAll('img') || [])].filter(image => image.src.startsWith('data:')).length,
}
