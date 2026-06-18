import { ref, computed } from 'vue'
import { GetSetting, SetSetting } from '../../bindings/cczjVideo/app'
import zhCN from './zh-CN'
import en from './en'

type Locale = 'zh-CN' | 'zh-TW' | 'en'

// 使用繁体中文时直接复用简体中文（后续可独立翻译）
const messages: Record<string, Record<string, any>> = {
  'zh-CN': zhCN,
  'zh-TW': zhCN, // 繁体暂时复用简体
  'en': en,
}

const currentLocale = ref<Locale>('zh-CN')
let _loaded = false

/** 从 Go 后端加载语言设置 */
async function load(): Promise<void> {
  if (_loaded) return
  _loaded = true
  try {
    const v = await GetSetting('language')
    if (v && (v === 'zh-CN' || v === 'zh-TW' || v === 'en')) {
      currentLocale.value = v as Locale
    }
  } catch { /* ignore */ }
}

/** 保存语言设置到 Go 后端并切换 */
async function setLocale(locale: string): Promise<void> {
  if (locale === 'zh-CN' || locale === 'zh-TW' || locale === 'en') {
    currentLocale.value = locale
  }
  try { await SetSetting('language', locale) } catch { /* ignore */ }
}

/** 翻译函数：支持点分隔的 key 路径，如 'settings.fontSize' */
function t(key: string, params?: Record<string, string | number>): string {
  const locale = currentLocale.value
  const msg = messages[locale] || messages['zh-CN']
  if (!msg) return key

  const parts = key.split('.')
  let result: any = msg
  for (const p of parts) {
    if (result == null) return key
    result = result[p]
  }
  if (typeof result !== 'string') return key

  // 参数替换 {n} -> value
  if (params) {
    return result.replace(/\{(\w+)\}/g, (_, k) => String(params[k] ?? `{${k}}`))
  }
  return result
}

/** 获取当前语言 */
const locale = computed(() => currentLocale.value)

export function useI18n() {
  return { t, locale, setLocale, load }
}