/**
 * 原生拖放安装：Go 把「被拖进来的文件夹绝对路径」用 plugin:filedrop 事件交上来，
 * 这里逐个装、把结果整理成面板能直接显示的报告。
 *
 * 为什么不在前端读文件字节：WebView2 的原生拖放只交出路径（ICoreWebView2File::GetPath），
 * 目录里的内容前端拿不到。所以读目录、丢杂物、复算上限、校验 manifest、放不放进
 * plugins 全在 app/plugin 那边，这里只负责「装了哪几个、结果如何、要不要重注入」。
 *
 * 这里不产出界面文案：失败只带原因码和 Go 那句人话，由调用方按 i18n 翻译。
 */
import { ref } from 'vue'
import { InstallPlugin, normalizeApiError } from '../api/app'
import { onBackendEvent } from '../api/events'
import { tr } from '../locales'
import { useErrorStore } from '../stores/error'
import { usePluginStore } from '../stores/plugins'
import { syncPluginScripts } from './runtime'

export interface InstallReport {
  /** 被拖进来的那个文件夹名：Go 按 manifest 的 id 落盘，报告里留原名才对得上用户看到的 */
  dropped: string
  ok: boolean
  /** 成功时是 Go 认定的包 id（可能和文件夹名不同） */
  packId?: string
  /** 失败细节：Go 报回来的那句人话，原样显示 */
  detail?: string
  /** Go 的原因码，形如 unknown_field / path_missing，用来指出该改 manifest 的哪个键 */
  reasonCode?: string
}

/** 最近几次的拖放结果，扩展包面板直接渲染它；清空由面板自己做。 */
export const dropReports = ref<InstallReport[]>([])
/** 一次拖放里逐个包串行安装中：面板用它把「正在安装」说出口。 */
export const installing = ref(false)

let detach: (() => void) | null = null

/**
 * 挂上原生拖放。投放区只有「设置 → 扩展包」那张卡片（唯一的 data-file-drop-target），
 * 分组切走时它是 display:none，所以拖到别处不该装包。
 *
 * 订阅仍挂在应用生命周期上而不是面板里：Go 的 WindowFilesDropped 是窗口级事件，
 * 一次拖放只发一遍，晚挂就漏。放不放行由下面 dropZoneVisible 判。
 */
export function startDropWatch(): void {
  if (detach) return
  detach = onBackendEvent<string[]>('plugin:filedrop', paths => {
    if (!dropZoneVisible()) return
    void installDroppedPaths(paths ?? [])
  })
}

/**
 * 投放区此刻真的在屏幕上吗。运行时会把落在标记外的拖放强制成 dropEffect=none，
 * 正常走不到「收得到事件但区域不可见」；留这一道是因为装扩展包的副作用用户完全
 * 看不见——正在放视频的人不会明白窗口为什么突然多出一个侧栏条目。
 */
function dropZoneVisible(): boolean {
  const zone = document.querySelector('[data-file-drop-target]')
  return zone instanceof HTMLElement && zone.offsetParent !== null
}

/**
 * 一次拖放可能有多个顶层项：每个目录各自是一个候选扩展包，逐个安装，
 * 一个坏掉不影响其余（与 Go 那边「单包失败不传染」的口径一致）。
 */
export async function installDroppedPaths(paths: string[]): Promise<InstallReport[]> {
  if (paths.length === 0) return []
  const pluginStore = usePluginStore()
  const errorStore = useErrorStore()
  installing.value = true
  const reports: InstallReport[] = []
  try {
    for (const path of paths) {
      const dropped = droppedName(path)
      try {
        const info = await InstallPlugin(path)
        reports.push({ dropped, ok: true, packId: info?.id ?? dropped })
      } catch (e) {
        const api = normalizeApiError(e)
        reports.push({ dropped, ok: false, reasonCode: api.code, detail: api.message })
        errorStore.warn(tr('settings.extensionsTitle'), `${dropped} · ${api.message}`, '', 'dropInstall')
        continue
      }
    }
    if (reports.some(report => report.ok)) {
      // 装成功就得让注册表和注入运行时同时跟上：少一步就是「文件在、界面没有」。
      await pluginStore.refresh(true)
      await syncPluginScripts()
    }
    dropReports.value = [...dropReports.value, ...reports].slice(-6)
  } finally {
    installing.value = false
  }
  return reports
}

/** 报告里要说的是用户认识的那个名字：路径最后一段，也就是他拖进来的文件夹名。 */
function droppedName(path: string): string {
  const trimmed = path.replace(/[\\/]+$/, '')
  const index = Math.max(trimmed.lastIndexOf('/'), trimmed.lastIndexOf('\\'))
  return index >= 0 ? trimmed.slice(index + 1) : trimmed
}
