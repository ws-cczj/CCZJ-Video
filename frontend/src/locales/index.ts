import { createI18n } from 'vue-i18n'
import { GetSetting, SetSetting } from '../../bindings/cczjVideo/app'
import zhCN from './zh-CN'
import en from './en'

export type Locale = 'zh-CN' | 'en'

const messages = {
  'zh-CN': zhCN,
  'en': en,
}

const i18n = createI18n({
  legacy: false, // 使用 Composition API 模式
  locale: 'zh-CN', // 默认语言
  fallbackLocale: 'zh-CN',
  messages,
})

let _loaded = false

/** 从 Go 后端加载语言设置 */
export async function loadLocale(): Promise<void> {
  if (_loaded) return
  _loaded = true
  try {
    const v = await GetSetting('language')
    if (v && (v === 'zh-CN' || v === 'en')) {
      i18n.global.locale.value = v as Locale
    }
  } catch { /* ignore */ }
}

/** 保存语言设置到 Go 后端并切换 */
export async function setLocale(locale: string): Promise<void> {
  if (locale === 'zh-CN' || locale === 'en') {
    i18n.global.locale.value = locale as Locale
  }
  try { await SetSetting('language', locale) } catch { /* ignore */ }
}

export default i18n