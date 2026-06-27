/// <reference types="vite/client" />

declare module '*.vue' {
    import type {DefineComponent} from 'vue'
    const component: DefineComponent<{}, {}, any>
    export default component
}

declare module '*.glsl?raw' {
    const value: string
    export default value
}

import type { AppEvent } from './event/appEvent'

declare global {
  interface Window {
    /** 全局应用事件总线 — 由 event/index.ts 在启动时挂载 */
    app_event: AppEvent
  }
}
