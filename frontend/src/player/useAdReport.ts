import { ref, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import { TsCache } from '../utils/tsCache'

export function useAdReport(getUrl:()=>string) {
const {t}=useI18n()
const showReportAd = ref(false)
const reportAdDomains = ref<string[]>([])
const reportAdToast = ref('')
let _reportAdToastTimer: number | null = null
// ========= 报告广告 =========
function toggleReportAd(): void {
  showReportAd.value = !showReportAd.value
  if (showReportAd.value) {
    // 从当前 m3u8 缓存中提取片段域名
    const domains = new Set<string>()
    try {
      const cached = TsCache.getM3u8FromCache(getUrl())
      if (cached) {
        const base = getUrl().substring(0, getUrl().lastIndexOf('/') + 1)
        for (const line of cached.split('\n')) {
          const t = line.trim()
          if (!t || t.startsWith('#')) continue
          try {
            const abs = new URL(t, base).href
            const h = new URL(abs).hostname
            if (h) domains.add(h)
          } catch { }
        }
      }
    } catch { }
    // 如果 m3u8 缓存无片段，回退到 getUrl() 自身域名
    if (domains.size === 0 && getUrl()) {
      try {
        const h = new URL(getUrl()).hostname
        if (h) domains.add(h)
      } catch { }
    }
    const blacklist = TsCache.getAdDomains()
    reportAdDomains.value = [...domains].filter(d => !blacklist.some(b => d.includes(b) || b.includes(d)))
  }
}
function doReportAd(domain: string): void {
  const ok = TsCache.addAdDomain(domain)
  showReportAd.value = false
  if (ok) {
    reportAdToast.value = t('player.adBlacklisted', { domain })
  } else {
    reportAdToast.value = t('player.adAlreadyBlacklisted')
  }
  if (_reportAdToastTimer != null) clearTimeout(_reportAdToastTimer)
  _reportAdToastTimer = window.setTimeout(() => { reportAdToast.value = '' }, 3000)
}
onBeforeUnmount(()=>{if(_reportAdToastTimer!==null)clearTimeout(_reportAdToastTimer)})
return {showReportAd,reportAdDomains,reportAdToast,toggleReportAd,doReportAd}
}
