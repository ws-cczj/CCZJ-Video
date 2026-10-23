import { createApp, defineComponent, h, nextTick, reactive } from 'vue'
import { createPinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import i18n from '../../src/locales'
import Player from '../../src/views/Player.vue'

const state = reactive({ open: true })
const listeners = new Set<EventListenerOrEventListenerObject>()
let replacementBindings = 0
const addVideo = HTMLVideoElement.prototype.addEventListener
HTMLVideoElement.prototype.addEventListener = function (name: string, listener: any, options?: any) {
  if (this.id === 'replacement-video' && (name === 'timeupdate' || name === 'loadedmetadata')) replacementBindings++
  addVideo.call(this, name, listener, options)
}
const add = document.addEventListener.bind(document)
const remove = document.removeEventListener.bind(document)
document.addEventListener = ((name: string, listener: EventListenerOrEventListenerObject, options?: any) => {
  if ((listener as Function).name === 'onPageKeyDown' || (listener as Function).name === 'onVisibilityChange') listeners.add(listener)
  add(name, listener, options)
}) as typeof document.addEventListener
document.removeEventListener = ((name: string, listener: EventListenerOrEventListenerObject, options?: any) => {
  listeners.delete(listener)
  remove(name, listener, options)
}) as typeof document.removeEventListener
const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/:pathMatch(.*)*', component: defineComponent({ render: () => null }) }] })
await router.push('/player/test/0/0?vod=test')
const app = createApp(defineComponent({ setup: () => () => state.open ? h(Player) : h('video', { class: 'native-video', id: 'replacement-video' }) }))
app.use(createPinia()).use(i18n).use(router).mount('#app')
;(window as any).pageTest = {
  state,
  listenerCount: () => listeners.size,
  replacementBindings: () => replacementBindings,
  async close() { state.open = false; await nextTick() },
}
