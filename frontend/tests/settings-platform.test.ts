import { expect, it, vi } from 'vitest'
import { createSSRApp } from 'vue'
import { createPinia } from 'pinia'
import { renderToString } from '@vue/server-renderer'
import { createI18n } from 'vue-i18n'

const platform = vi.hoisted(() => ({ mobile: true, ios: false }))
vi.mock('../src/api/runtime', async original => ({ ...await original<object>(), isMobileRuntime: () => platform.mobile, isIOSRuntime: () => platform.ios }))
vi.mock('../src/router', () => ({ default: {} }))
vi.mock('../src/locales', () => ({ tr: (key: string) => key, setLocale: async () => {} }))
vi.mock('../src/stores/theme', () => ({ useThemeStore: () => ({ current: { name: 'Test', mode: 'light' } }) }))
vi.mock('../src/stores/plugins', () => ({ usePluginStore: () => ({ packs: [], loaded: true, directory: '/data/user/0/app/files/plugins' }) }))
import ExtensionsPanel from '../src/components/ExtensionsPanel.vue'
import SettingsAboutGroup from '../src/views/settings/SettingsAboutGroup.vue'
import zh from '../src/locales/zh-CN'

async function render(component: Parameters<typeof createSSRApp>[0], props = {}) {
  return (await renderToString(createSSRApp(component, props).use(createPinia()).use(createI18n({ legacy: false, locale: 'zh', messages: { zh } })))).replace(/<!--[\s\S]*?-->/g, '')
}

it('offers extension management without Explorer paths or folder drop instructions on mobile', async () => {
  platform.mobile = true
  const html = await render(ExtensionsPanel)
  expect(html).not.toContain('打开目录')
  expect(html).not.toContain('/data/user/0/app/files/plugins')
  expect(html).not.toContain('资源管理器')
  expect(html).not.toContain('拖进窗口')
  expect(html).not.toContain('data-file-drop-target')
})

it('retains folder access and drop installation on desktop', async () => {
  platform.mobile = false
  const html = await render(ExtensionsPanel)
  expect(html).toContain('打开目录')
  expect(html).toContain('data-file-drop-target')
})

it('offers update checks on mobile without desktop restart or rollback controls', async () => {
  platform.mobile = true
  const html = await render(SettingsAboutGroup, { appVersion: '2.4.0' })
  expect(html).toContain('检查更新')
  expect(html).not.toContain('> 重启应用')
  expect(html).not.toContain('退回上一版')
})

it('explains iOS signing updates without mounting an APK update action', async () => {
  platform.mobile = true; platform.ios = true
  try {
    const html = await render(SettingsAboutGroup, { appVersion: '2.4.0' })
    expect(html).toContain('重新签名')
    expect(html).not.toContain('检查更新')
    expect(html).not.toContain('APK')
  } finally { platform.ios = false }
})
