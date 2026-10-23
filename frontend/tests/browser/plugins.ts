import { createApp, h, nextTick } from 'vue'
import { createPinia } from 'pinia'
import i18n from '../../src/locales'
import { createPackRuntime } from '../../src/plugins/api'
import { usePluginStore, type PluginInfo } from '../../src/stores/plugins'
import { syncPluginScripts, activeScriptPacks } from '../../src/plugins/runtime'
import { scriptDigest } from '../../src/plugins/scriptTrust'
import { useThemeStore, resolveThemeBg, packBgRef } from '../../src/stores/theme'

createApp({ render: () => h('p', 'Plugin test host') }).use(createPinia()).use(i18n).mount('#app')
const pack = (id: string): PluginInfo => ({ id, kind: 'script', status: 'ready', version: '1.0.0', name: { en: id }, dir: '', permissions: [] })
;(window as any).pluginTest = {
  async runtimeChecks() {
    const results: Record<string, boolean> = {}
    const a = createPackRuntime(pack('test-a'))
    const b = createPackRuntime(pack('test-b'))
    const target = { method: () => 'base' }
    a.cczj.intercept(target, 'method', next => next() + ':a')
    b.cczj.intercept(target, 'method', next => next() + ':b')
    a.dispose()
    results.removeInnerInterceptor = target.method() === 'base:b'
    b.dispose()
    results.restoreOriginalInterceptor = target.method() === 'base'

    const watched = createPackRuntime(pack('test-watch'))
    const value = watched.cczj.vue.ref(0)
    let runs = 0
    watched.cczj.vue.watchEffect(() => { void value.value; runs++ })
    value.value++
    await nextTick()
    watched.dispose()
    value.value++
    await nextTick()
    results.disposeWatchEffect = runs === 2
    let refused = false
    try { watched.cczj.css('.test-late { color: red }') } catch { refused = true }
    results.rejectLateStyles = refused && !document.querySelector('style[data-cczj-pack="test-watch"]')
    watched.dispose()
    return results
  },
  async startPendingSetup() {
    const source = `export default async cczj => {
      cczj.css('.pending-style { color: blue }');
      window.__setupEntered = true;
      await new Promise(resolve => { window.__finishSetup = resolve });
      try { cczj.css('.late-style { color: red }'); } catch { window.__lateRefused = true; }
    }`
    const entry = { ...pack('test-pending'), script: { entry: 'main.js' } }
    const files = { 'plugin.json': JSON.stringify(entry), 'main.js': source }
    entry.builtin_digest = await scriptDigest([files['plugin.json'], source])
    ;(window as any).__packs = [entry]
    ;(window as any).__files = { [entry.id]: files }
    await usePluginStore().refresh(true)
    void syncPluginScripts()
  },
  async disablePendingSetup() {
    await usePluginStore().setEnabled('test-pending', false)
    ;(window as any).__disabledReturned = true
    await syncPluginScripts()
    ;(window as any).__syncDisabled = true
  },
  active: activeScriptPacks,
  async prepareTheme() {
    useThemeStore()
    const entry = { ...pack('test-theme'), kind: 'theme', theme: { presets: [{ id: 'one', name: { en: 'Theme' }, primary: '#334455', mode: 'dark', bg_image: 'background.png' }] } }
    ;(window as any).__packs = [entry]
    ;(window as any).__assets = { 'test-theme': { 'background.png': 'data:image/png;base64,new' } }
    ;(window as any).__assetReads = 0
    await usePluginStore().refresh(true)
    await nextTick()
  },
  themeBackground: () => resolveThemeBg(packBgRef('test-theme', 'background.png')),
  async disableTheme() { await usePluginStore().setEnabled('test-theme', false); await nextTick() },
  async repeatLifecycle() {
    let effects = 0, events = 0, cleanups = 0, timerCalls = 0
    for (let i = 0; i < 30; i++) {
      const runtime = createPackRuntime(pack('test-repeat'))
      const value = runtime.cczj.vue.ref(0)
      const handle = runtime.cczj.vue.watchEffect(() => { void value.value; effects++ })
      handle.pause()
      value.value++
      await nextTick()
      handle.resume()
      await nextTick()
      runtime.cczj.events.on('plugin:audit', () => events++)
      runtime.cczj.css('.test-repeat { color: green }')
      const timer = setInterval(() => timerCalls++, 20)
      runtime.cczj.onDispose(() => { cleanups++; clearInterval(timer) })
      ;(window as any)._wails.dispatchWailsEvent({ name: 'plugin:audit' })
      runtime.dispose()
      runtime.dispose()
      value.value++
      ;(window as any)._wails.dispatchWailsEvent({ name: 'plugin:audit' })
      await nextTick()
    }
    const before = timerCalls
    await new Promise(resolve => setTimeout(resolve, 60))
    return { effects, events, cleanups, noLateTimer: timerCalls === before, styles: document.querySelectorAll('style[data-cczj-pack="test-repeat"]').length }
  },
  async startUninstallScript() {
    const source = `export default cczj => cczj.css('.uninstall-check { color: blue }')`
    const entry = { ...pack('test-uninstall'), script: { entry: 'main.js' } }
    const files = { 'plugin.json': JSON.stringify(entry), 'main.js': source }
    entry.builtin_digest = await scriptDigest([files['plugin.json'], source])
    ;(window as any).__packs = [entry]
    ;(window as any).__files = { [entry.id]: files }
    await usePluginStore().refresh(true)
    await syncPluginScripts()
  },
  async uninstallWithFailedTrustCleanup() {
    ;(window as any).__failTrustSave = true
    const warning = await usePluginStore().uninstall('test-uninstall')
    await syncPluginScripts()
    ;(window as any).__failTrustSave = false
    return warning
  },
}
