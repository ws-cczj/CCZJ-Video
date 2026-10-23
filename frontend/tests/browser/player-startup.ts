import { createApp, h } from 'vue'
import { createPinia } from 'pinia'
import i18n from '../../src/locales'
import VideoPlayer from '../../src/components/VideoPlayer.vue'
import '../../src/style.css'
import '../../src/styles/cczj-utilities.css'
;(window as any).startupProbe = { plays: 0, pauses: 0, events: [], logs: [], errors: [] }
const app = createApp({ render: () => h('div', { style: 'width:920px;height:600px;position:relative' }, [h(VideoPlayer, { url: 'https://example.invalid/test.m3u8', danmakuKey: 'startup', mediaTitle: 'sample', episodeLabel: '第01集', hasPrev:true, hasNext:true })]) })
app.use(createPinia()).use(i18n)
app.config.errorHandler = error => (window as any).startupProbe.errors.push(String(error))
app.mount('#app')
