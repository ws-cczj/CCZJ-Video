import { defineAsyncComponent, markRaw, type Component } from 'vue'
import { presentationProfile } from '../platform/presentation'

// The plugin contract stays the same. Resolve only on first render, after the
// runtime is ready; importing plugin APIs during bootstrap must not select a host.
// 两个宿主的面板 props 不完全一致（桌面版比移动版多几个可选 prop），三元表达式的
// 联合类型让 defineAsyncComponent 选不出重载。插件面板的契约只有 `active`，
// 所以按契约把工厂的返回类型写死一次：多出来的 prop 在桌面版自带默认值，
// 移动版永远收不到它，两边行为都不变。
type PanelModule = { default: Component }

export const builtinLogPanel = markRaw(defineAsyncComponent(
  (): Promise<PanelModule> =>
    presentationProfile().ui === 'mobile'
      ? import('../mobile/plugins/MobileLogPanel.vue')
      : import('../components/LogPanel.vue')))

export const builtinDiagnosticsPanel = markRaw(defineAsyncComponent(
  (): Promise<PanelModule> =>
    presentationProfile().ui === 'mobile'
      ? import('../mobile/plugins/MobileDiagnosticsPanel.vue')
      : import('../components/DiagnosticsPanel.vue')))
