import { createApp, defineComponent, h, nextTick, reactive } from 'vue'
import { createPinia } from 'pinia'
import i18n from '../../src/locales'
import VideoPlayer from '../../src/components/VideoPlayer.vue'
import ConfirmModal from '../../src/components/ConfirmModal.vue'
import { usePluginStore } from '../../src/stores/plugins'
import { syncPluginScripts } from '../../src/plugins/runtime'
import { scriptTrust } from '../../src/plugins/trust'
import { useViewActivity } from '../../src/composables/useViewActivity'
import { writeStorage } from '../../src/platform/storage'
import { captureBrowserResources } from '../../src/diagnostics/browserResources'
import { TsCache } from '../../src/utils/tsCache'
import { cacheSet } from '../../src/utils/ts-cache/memory-cache'
import { proxyHlsURL } from '../../src/player/hls/proxy'
import { initializeRuntimeEnvironment } from '../../src/api/runtime'
import { loadPlatformPresentation } from '../../src/platform/presentation'
import '../../src/style.css'
import '../../src/styles/cczj-utilities.css'

if ((window as unknown as { wails?: unknown }).wails) await initializeRuntimeEnvironment()
await loadPlatformPresentation()

writeStorage('vp_settings', { auto_resume_jump: new URLSearchParams(location.search).get('resume') === '0' ? '0' : '1', ai_warning_accepted: '1' })
writeStorage('cczj_auto_play', new URLSearchParams(location.search).has('autoplay'))
const state = reactive({ open: !new URLSearchParams(location.search).has('closed'), episode: 1, line: 1 })
const mediaURL = new URLSearchParams(location.search).get('media') || `${location.origin}/tests/browser/sample.webm`
const bareVideo = new URLSearchParams(location.search).has('bare')
const app = createApp(defineComponent({
  setup: () => {
    useViewActivity()
    return () => h('div', { class: 'cczj-flex cczj-flex-col cczj-h-full' }, [state.open ? bareVideo ? h('div', { class: 'player-wrapper' }, [h('video', { class: 'native-video', src: proxyHlsURL(mediaURL), preload: 'auto', muted: true })]) : h(VideoPlayer, {
    url: /^(data|blob):/.test(mediaURL) ? mediaURL : `${mediaURL}${mediaURL.includes('?') ? '&' : '?'}line=${state.line}`,
    videoKey: `browser-episode-${state.episode}`,
    title: `Episode ${state.episode}`,
    autoplay: new URLSearchParams(location.search).has('autoplay'),
    hasNext: true,
    resolveResume: async () => Number(new URLSearchParams(location.search).get('saved')) || 0,
    onNext: () => { state.episode++; state.line++ },
  }) : h('p', 'Player closed'), h(ConfirmModal)])
  },
}))
app.use(createPinia()).use(i18n).mount('#app')
;(window as any).playerTest = {
  state,
  resources: captureBrowserResources,
  segmentStats: () => TsCache.stats(),
  seedSegmentCache() {
    // Match the retained multi-episode cache observed in the noon diagnostics.
    for (let index = 0; index < 70; index++) {
      const bytes = new Uint8Array(1024 * 1024)
      bytes.fill(index + 1)
      cacheSet(`https://fixture.invalid/${index}.ts`, bytes.buffer, `idle-episode-${Math.floor(index / 32)}`)
    }
  },
  async enableFilm() {
    state.open = false
    await nextTick()
    writeStorage('vp_settings', { auto_resume_jump: '1', ai_warning_accepted: '1', quality_mode: 'ai_film' })
    state.open = true
  },
  async seedScript(revision = 1, clearTrust = false, enable = false) {
    const pack = {
      id: 'browser-test-script', kind: 'script', version: '1.0.0',
      name: { 'zh-CN': '浏览器测试脚本', en: 'Browser test script' },
      status: 'ready', script: { entry: 'main.js' }, permissions: [],
    }
    ;(window as any).__testPacks = [pack]
    ;(window as any).__testFiles = {
      'plugin.json': JSON.stringify(pack),
      'main.js': `window.__scriptExecutions = (window.__scriptExecutions || 0) + 1; export default () => {}; // revision ${revision}`,
    }
    if (clearTrust) await scriptTrust.revoke(pack.id)
    await usePluginStore().refresh(true)
    await syncPluginScripts()
    if (enable) {
      await usePluginStore().setEnabled(pack.id, true)
      await syncPluginScripts()
    }
    ;(window as any).__scriptSyncDone = true
  },
}
