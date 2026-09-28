import { watch } from 'vue'
import { createI18n } from 'vue-i18n'
import { GetSetting, SetSetting } from '../api/app'
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

// <html lang> 必须跟着 locale 走：无障碍朗读、字体回退和 WebView 的语言检测都读它，
// 而 index.html 里的是静态值。
watch(
  () => i18n.global.locale.value,
  (locale) => { document.documentElement.lang = locale === 'en' ? 'en' : 'zh-CN' },
  { immediate: true },
)

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

/**
 * 组件之外的翻译入口（store、工具函数、prop 默认值）。
 * 在模板里调用同样有效，并且会因为读取了 locale ref 而随语言切换重新渲染。
 */
export function tr(key: string, named?: Record<string, unknown>): string {
  return named === undefined ? i18n.global.t(key) : i18n.global.t(key, named)
}
